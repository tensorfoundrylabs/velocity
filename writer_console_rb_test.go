package velocity

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// rbConsoleGateWriter blocks the first underlying write until gate is closed,
// signalling via entered, so queue state can be made deterministic while the
// drain is parked.
type rbConsoleGateWriter struct {
	gate    chan struct{}
	entered chan struct{}
	mu      sync.Mutex
	buf     []byte
}

func (w *rbConsoleGateWriter) Write(p []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.gate
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	w.mu.Unlock()
	return len(p), nil
}

func (w *rbConsoleGateWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// TestConsoleWriterRB_SecureFieldRedactedOnNonTTY is a regression test for the bug
// where ConsoleWriterRB always passed trusted=true to the template, meaning Secure
// fields were rendered in plaintext even when the writer was not a terminal.
// The fix uses isTTY (false for bytes.Buffer) so Secure fields are redacted.
func TestConsoleWriterRB_SecureFieldRedactedOnNonTTY(t *testing.T) {
	t.Parallel()

	// Use safeBuffer (mutex-protected) because the ring buffer flusher goroutine
	// writes to it concurrently with the test's Len() poll in waitFor.
	var buf safeBuffer
	// Use a theme so the template path is exercised (not the fallback formatEntry path).
	w := NewConsoleWriterRB(&buf, ThemeNightOwl, nil, FieldDisplayInline)
	// safeBuffer is not a *os.File, so resolveColourForWriter returns false → isTTY=false → untrusted.

	entry := GetEntry()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("login")
	entry.WithFields(Secure("password", "s3cr3t"))
	entry.SetTime(time.Now())
	entry.Write()

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 5*time.Second, 5*time.Millisecond, "data should flush from ConsoleWriterRB")

	_ = w.Close()

	output := buf.String()
	if strings.Contains(output, "s3cr3t") {
		t.Errorf("Secure field plaintext leaked to non-TTY ConsoleWriterRB: %q", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in non-TTY ConsoleWriterRB output, got: %q", output)
	}
}

func TestConsoleWriterRB_Timezone(t *testing.T) {
	var buf safeBuffer

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York timezone not available:", err)
	}

	// Nil theme forces the formatEntry fallback path which must apply displayTimezone.
	w := NewConsoleWriterRB(&buf, nil, loc, FieldDisplayInline)

	entry := GetEntry()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("tz test")
	// Jan 15 is EST (UTC-5). 12:00 UTC becomes 07:00 EST.
	entry.SetTime(time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC))
	entry.Write()

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 5*time.Second, 5*time.Millisecond, "data should flush from ConsoleWriterRB")

	_ = w.Close()

	output := buf.String()
	if !strings.Contains(output, "-05:00") {
		t.Fatalf("expected -05:00 timezone offset in output, got: %s", output)
	}
}

func TestConsoleWriterRB_WriteAfterCloseRejected(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	w := NewConsoleWriterRB(&buf, nil, nil, FieldDisplayInline)

	if err := w.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	entry := GetEntry()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("too late")
	entry.SetTime(time.Now())
	entry.Write()

	if err := w.Write(entry); !errors.Is(err, ErrWriterClosed) {
		t.Errorf("post-close Write returned %v, want ErrWriterClosed", err)
	}
	if strings.Contains(buf.String(), "too late") {
		t.Error("post-close entry delivered")
	}
}

// TestConsoleWriterRB_ConcurrentCloseWaitsForDrain: both Close callers must
// block until the same drain completes — the second Close may not return
// early while the first is still draining behind a blocked sink.
func TestConsoleWriterRB_ConcurrentCloseWaitsForDrain(t *testing.T) {
	t.Parallel()

	gw := &rbConsoleGateWriter{gate: make(chan struct{}), entered: make(chan struct{}, 1)}
	w := NewConsoleWriterRB(gw, nil, nil, FieldDisplayInline)

	entry := GetEntry()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("drain-me")
	entry.SetTime(time.Now())
	entry.Write()
	if err := w.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case <-gw.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("drain never reached the underlying writer")
	}

	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- w.Close() }()
	}

	select {
	case err := <-errs:
		t.Fatalf("Close returned (%v) while the underlying writer was still blocked", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(gw.gate)
	for range 2 {
		select {
		case err := <-errs:
			if err != nil {
				t.Errorf("Close returned error: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Close never returned after the sink unblocked")
		}
	}

	if !strings.Contains(gw.String(), "drain-me") {
		t.Error("accepted entry lost during double close")
	}

	// The write gate must hold after both Closes return.
	entry2 := GetEntry()
	entry2.SetLevel(LevelInfo)
	entry2.SetMessage("post-close")
	entry2.SetTime(time.Now())
	entry2.Write()
	if err := w.Write(entry2); !errors.Is(err, ErrWriterClosed) {
		t.Errorf("post-close Write returned %v, want ErrWriterClosed", err)
	}
}

// TestConsoleWriterRB_FullQueueDropsWithoutReordering: with the drain gated
// open, entries beyond the queue capacity are dropped and counted in Metrics
// (never written directly around queued records), and after the sink unblocks
// exactly the accepted entries are delivered.
func TestConsoleWriterRB_FullQueueDropsWithoutReordering(t *testing.T) {
	t.Parallel()

	gw := &rbConsoleGateWriter{gate: make(chan struct{}), entered: make(chan struct{}, 1)}
	w := NewConsoleWriterRB(gw, nil, nil, FieldDisplayInline)

	newEntry := func(i int) *Entry {
		e := GetEntry()
		e.SetLevel(LevelInfo)
		e.SetMessage(fmt.Sprintf("entry-%03d", i))
		e.SetTime(time.Now())
		e.Write()
		return e
	}

	// Park the drain inside the first underlying write.
	if err := w.Write(newEntry(0)); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	select {
	case <-gw.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("drain never reached the underlying writer")
	}

	accepted := 1
	for i := 1; i < 5000; i++ {
		if err := w.Write(newEntry(i)); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		_, errs, _ := w.Metrics()
		if errs > 0 {
			// This entry hit the full queue and was dropped.
			accepted = i // entries 0..i-1 accepted; entry i dropped
			break
		}
	}

	_, errs, dropped := w.Metrics()
	if errs == 0 || dropped == 0 {
		t.Fatalf("queue overflow not accounted: errors=%d dropped=%d", errs, dropped)
	}

	close(gw.gate)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Exactly the accepted entries are delivered, in order, and the dropped
	// one is absent — the drop must not be written directly around them.
	out := gw.String()
	for i := range accepted {
		if !strings.Contains(out, fmt.Sprintf("entry-%03d", i)) {
			t.Errorf("accepted entry %d missing from output", i)
		}
	}
	if strings.Count(out, "entry-") != accepted {
		t.Errorf("delivered %d entries, want exactly the %d accepted", strings.Count(out, "entry-"), accepted)
	}
}

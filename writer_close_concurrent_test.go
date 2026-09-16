package velocity

// Regression coverage for the final re-review residual: a second CONCURRENT
// Close called directly on a ConsoleWriter or JSONWriter must wait for the
// same completed drain and return the same recorded result — not return nil
// early while the first Close is still in inFlight.Wait() (WP2 contract,
// mirroring MultiWriter's closeOnce/closeDone). Not reachable via
// Logger.Close (the family closeOnce guards it); exercised here on the
// writers directly.

import (
	"errors"
	"testing"
	"time"
)

func TestClose_ConcurrentWriterCloseWaitsForSameDrain(t *testing.T) {
	t.Run("ConsoleWriter", func(t *testing.T) {
		sink := &wp2FlushFailWriter{failFlush: true}
		testConcurrentCloseWaits(t, consoleCloseSubject{w: NewConsoleWriterWithOptions(sink, ThemeNightOwl, time.Local, FieldDisplayInline)})
	})
	t.Run("JSONWriter", func(t *testing.T) {
		sink := &wp2FlushFailWriter{failFlush: true}
		testConcurrentCloseWaits(t, jsonCloseSubject{w: NewJSONWriter(sink)})
	})
}

// closeSubject adapts the two writers to one test body.
type closeSubject interface {
	writeAsync(e *Entry, entered, release chan struct{})
	closedFlag() bool
	close() error
}

type consoleCloseSubject struct{ w *ConsoleWriter }

func (s consoleCloseSubject) writeAsync(e *Entry, entered, release chan struct{}) {
	// Run WriteSecure in a goroutine with a blocking Stringer field so an
	// admitted write cycle is parked mid-format when Close starts.
	_ = s.w.WriteSecure(e.WithFields(Stringer("f", frStringer{entered: entered, release: release})), true, "[REDACTED]")
}

func (s consoleCloseSubject) closedFlag() bool {
	s.w.mu.Lock()
	defer s.w.mu.Unlock()
	return s.w.closed
}

func (s consoleCloseSubject) close() error { return s.w.Close() }

type jsonCloseSubject struct{ w *JSONWriter }

func (s jsonCloseSubject) writeAsync(e *Entry, entered, release chan struct{}) {
	_ = s.w.WriteSecure(e.WithFields(Stringer("f", frStringer{entered: entered, release: release})), false, "[REDACTED]")
}

func (s jsonCloseSubject) closedFlag() bool {
	s.w.mu.Lock()
	defer s.w.mu.Unlock()
	return s.w.closed
}

func (s jsonCloseSubject) close() error { return s.w.Close() }

func testConcurrentCloseWaits(t *testing.T, subj closeSubject) {
	entered := make(chan struct{})
	release := make(chan struct{})

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("fr-final-drain-entry")
	e.Write()

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		subj.writeAsync(e, entered, release)
	}()
	<-entered // admitted cycle parked in formatting

	firstErr := make(chan error, 1)
	go func() { firstErr <- subj.close() }()

	// Wait until the first Close has set closed and is parked in the drain.
	waitFor(t, subj.closedFlag, 5*time.Second, time.Millisecond, "first Close to reach the drain")

	secondErr := make(chan error, 1)
	go func() { secondErr <- subj.close() }()

	// The drain provably cannot progress while the Stringer is parked. A
	// return inside this window is the early-nil defect.
	select {
	case err := <-secondErr:
		t.Fatalf("second concurrent Close returned %v while the first was still draining", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	<-writeDone

	var errA, errB error
	select {
	case errA = <-firstErr:
	case <-time.After(10 * time.Second):
		t.Fatal("first Close never returned after the drain finished")
	}
	select {
	case errB = <-secondErr:
	case <-time.After(10 * time.Second):
		t.Fatal("second Close never returned after the drain finished")
	}

	// Both waited for the same completion and both carry the recorded result.
	if !errors.Is(errA, errWP2Flush) {
		t.Errorf("first Close returned %v, want recorded flush error %v", errA, errWP2Flush)
	}
	if !errors.Is(errB, errWP2Flush) {
		t.Errorf("second Close returned %v, want the same recorded flush error", errB)
	}
}

// Serial third Close also replays the recorded result without a new drain.
func TestClose_ConcurrentWriterCloseReplaysRecordedResult(t *testing.T) {
	sink := &wp2FlushFailWriter{failFlush: true}
	w := NewJSONWriter(sink)
	cw := NewConsoleWriterWithOptions(sink, ThemeNightOwl, time.Local, FieldDisplayInline)

	if err := w.Close(); !errors.Is(err, errWP2Flush) {
		t.Errorf("JSON first Close: %v, want %v", err, errWP2Flush)
	}
	if err := w.Close(); !errors.Is(err, errWP2Flush) {
		t.Errorf("JSON repeat Close: %v, want the same recorded result", err)
	}
	if err := cw.Close(); !errors.Is(err, errWP2Flush) {
		t.Errorf("console first Close: %v, want %v", err, errWP2Flush)
	}
	if err := cw.Close(); !errors.Is(err, errWP2Flush) {
		t.Errorf("console repeat Close: %v, want the same recorded result", err)
	}
}

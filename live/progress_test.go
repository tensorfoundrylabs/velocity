package live

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

// TestProgressBar_ConcurrentComplete verifies that simultaneous Complete calls
// do not panic due to double-close of the done channel.
func TestProgressBar_ConcurrentComplete(_ *testing.T) {
	pb := NewProgressBar(io.Discard, 100, "test")

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pb.Complete()
		}()
	}
	wg.Wait()
}

// TestSpinner_ConcurrentStop verifies that simultaneous Stop calls
// do not panic due to double-close of the done channel.
func TestSpinner_ConcurrentStop(_ *testing.T) {
	s := NewSpinner(io.Discard, "test")

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Stop()
		}()
	}
	wg.Wait()
}

// TestMultiProgress_ConcurrentStop verifies that simultaneous Stop calls
// do not panic due to double-close of the done channel.
func TestMultiProgress_ConcurrentStop(_ *testing.T) {
	mp := NewMultiProgress(io.Discard)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mp.Stop()
		}()
	}
	wg.Wait()
}

// TestSpinner_SetStyle_NilReceiver verifies nil safety, consistent with other
// Spinner methods.
func TestSpinner_SetStyle_NilReceiver(_ *testing.T) {
	var s *Spinner
	s.SetStyle(SpinnerStyleDots)
}

// --- Non-TTY branch: bytes.Buffer is not a terminal ---

// TestProgressBar_NonTTY_NoControlSequences verifies that a ProgressBar writing to
// a bytes.Buffer (non-TTY) never emits \r or ANSI erase sequences during updates,
// and that Complete() writes a plain summary line instead.
func TestProgressBar_NonTTY_NoControlSequences(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pb := NewProgressBar(&buf, 10, "loading")

	if pb.isTTY {
		t.Skip("test environment has a TTY-backed buffer — skipping non-TTY test")
	}

	// Update a few times; the non-TTY render should produce no output.
	pb.Update(3)
	pb.Update(7)

	if buf.Len() != 0 {
		t.Errorf("non-TTY ProgressBar wrote bytes before Complete: %q", buf.String())
	}

	pb.Complete()

	out := buf.String()
	if strings.Contains(out, "\r") || strings.Contains(out, "\033[") {
		t.Errorf("non-TTY ProgressBar emitted control sequences: %q", out)
	}
	// Must include the label and completion indication.
	if !strings.Contains(out, "loading") {
		t.Errorf("expected label in non-TTY summary: %q", out)
	}
	if !strings.Contains(out, "completed") {
		t.Errorf("expected 'completed' in non-TTY summary: %q", out)
	}
}

// TestSpinner_NonTTY_NoControlSequences verifies that a Spinner writing to a
// bytes.Buffer never emits \r or ANSI erase sequences per frame, but still
// writes a message on StopWithMessage.
func TestSpinner_NonTTY_NoControlSequences(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	s := NewSpinner(&buf, "working")

	if s.isTTY {
		t.Skip("test environment has a TTY-backed buffer — skipping non-TTY test")
	}

	// After construction the goroutine may have ticked once; give it a moment,
	// then confirm no control bytes were written.
	s.Stop()

	out := buf.String()
	if strings.Contains(out, "\r") || strings.Contains(out, "\033[") {
		t.Errorf("non-TTY Spinner emitted control sequences on Stop: %q", out)
	}
}

// TestSpinner_NonTTY_StopWithMessage writes a final line on non-TTY.
func TestSpinner_NonTTY_StopWithMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	s := NewSpinner(&buf, "working")

	if s.isTTY {
		t.Skip("test environment has a TTY-backed buffer — skipping non-TTY test")
	}

	s.StopWithMessage("all done")

	out := buf.String()
	if strings.Contains(out, "\r") || strings.Contains(out, "\033[") {
		t.Errorf("non-TTY Spinner emitted control sequences: %q", out)
	}
	if !strings.Contains(out, "all done") {
		t.Errorf("expected message in output: %q", out)
	}
}

package live

// Live lifecycle regression tests (R05): style-switch frame-index
// normalisation, immediate Stop, competing finalisers, no output after
// Stop/Complete, and exactly-one non-terminal progress summaries. The
// fake-terminal variants here are deterministic because finalisation joins
// the render goroutine; the pty variants in pty_linux_test.go add
// real-terminal evidence.

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Row 1: SetStyle must normalise the frame index for every switch from
// every valid old index, or the next tick panics in the render goroutine.

func TestSpinner_SetStyle_EveryOldIndex_NoOutOfRangeFrame(t *testing.T) {
	t.Parallel()

	targets := map[SpinnerStyle]int{
		SpinnerStyleBraille: 10,
		SpinnerStyleDots:    8,
		SpinnerStyleArrows:  8,
		SpinnerStyleBounce:  8,
		SpinnerStyleBar:     4,
	}

	for oldIdx := range 10 {
		for style, frameCount := range targets {
			t.Run(fmt.Sprintf("old%d/to_%d_frames", oldIdx, frameCount), func(t *testing.T) {
				s := &Spinner{writer: io.Discard, label: "scan", frames: brailleFramesSlice(), current: oldIdx, isTTY: true}
				s.SetStyle(style)
				if s.current >= len(s.frames) {
					t.Fatalf("SetStyle left frame index %d beyond new %d-frame set", s.current, len(s.frames))
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("render panicked after style switch at old index %d: %v", oldIdx, r)
						}
					}()
					s.render()
				}()
			})
		}
	}
}

func brailleFramesSlice() []string {
	return strings.Fields("⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏")
}

// --- Immediate Stop: finalisation joins the render goroutine, so once Stop
// returns nothing more is written and the screen holds no frame. The screen
// assertion is deterministic regardless of whether the goroutine's guarded
// first render fired before or after the CAS.

func TestSpinner_ImmediateStop_NoStrayFrame(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	s := NewSpinner(ft, "loading")
	if !s.isTTY {
		t.Fatal("syncTerminal must be detected as a terminal")
	}
	s.Stop()

	if s.active.Load() {
		t.Fatal("Stop returned with the spinner still active")
	}
	out := ft.String()
	lastErase := strings.LastIndex(out, "\x1b[K")
	lastFrame := strings.LastIndexAny(out, brailleFrames)
	if lastFrame > lastErase {
		t.Errorf("frame rendered after Stop's erase; transcript tail: %q", out)
	}
	joined := screenJoin(replay(out))
	if strings.ContainsAny(joined, brailleFrames) {
		t.Errorf("stray spinner frame visible after Stop; screen:\n%s\nraw: %q", joined, out)
	}
}

// --- No writes to the destination after Stop/Complete return.

func TestSpinner_NoWritesAfterStop(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	s := NewSpinner(ft, "working")
	s.Stop()

	n := len(ft.String())
	// Bounded observation window spanning at least one 80ms tick. This is
	// observation of a structural guarantee (the render goroutine is joined),
	// not sleep-based synchronisation.
	time.Sleep(200 * time.Millisecond)
	if got := len(ft.String()); got != n {
		t.Errorf("spinner wrote %d bytes after Stop returned: %q", got-n, ft.String()[n:])
	}
}

func TestProgressBar_NoWritesAfterComplete(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	pb := NewProgressBar(ft, 100, "build")
	if !pb.isTTY {
		t.Fatal("syncTerminal must be detected as a terminal")
	}
	pb.Update(40)
	pb.render()
	pb.Complete()

	n := len(ft.String())
	time.Sleep(250 * time.Millisecond) // bounded window spanning 100ms ticks
	if got := len(ft.String()); got != n {
		t.Errorf("progress bar wrote %d bytes after Complete returned: %q", got-n, ft.String()[n:])
	}

	joined := screenJoin(replay(ft.String()))
	if !strings.Contains(joined, "completed in") {
		t.Errorf("final bar line missing from screen; screen:\n%s\nraw: %q", joined, ft.String())
	}
}

// --- Competing finalisers on a plain (non-coordinated) writer: at most one
// finalisation per instance, and every caller waits for it. The Output path
// is covered in output_test.go; this pins the standalone contract.

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (lw *lockedBuffer) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.buf.Write(p)
}

func (lw *lockedBuffer) String() string {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.buf.String()
}

func TestSpinner_ConcurrentFinalisers_AtMostOne(t *testing.T) {
	t.Parallel()

	lw := &lockedBuffer{}
	s := NewSpinner(lw, "working")

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, fn := range []func(){
		func() { s.StopWithMessage("plain-done") },
		func() { s.StopWithSuccess("success-done") },
		func() { s.StopWithError("error-done") },
	} {
		wg.Add(1)
		go func(fn func()) {
			defer wg.Done()
			<-start
			fn()
		}(fn)
	}
	close(start)
	wg.Wait()

	out := lw.String()
	finals := 0
	for _, marker := range []string{"plain-done", "success-done", "error-done"} {
		if strings.Contains(out, marker) {
			finals++
		}
	}
	if finals > 1 {
		t.Errorf("concurrent finalisers produced %d final messages; at most one finalisation is allowed: %q", finals, out)
	}
}

// --- Exactly one non-terminal completion summary, including total 0 and
// Update(total) long before Complete.

func countSummary(t *testing.T, out string) int {
	t.Helper()
	n := strings.Count(out, "completed in")
	// Cursor control must never reach a non-terminal; SGR styling may, under
	// FORCE_COLOR.
	if strings.Contains(out, "\r") {
		t.Errorf("non-TTY summary contains carriage returns: %q", out)
	}
	if seq := firstCursorSeq(out); seq != "" {
		t.Errorf("non-TTY summary contains cursor-control sequence %q: %q", seq, out)
	}
	return n
}

func TestProgressBar_NonTTY_TotalZero_SingleSummary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pb := NewProgressBar(&buf, 0, "empty-job")
	if pb.isTTY {
		t.Skip("test environment has a TTY-backed buffer")
	}

	pb.Update(0)
	pb.Complete()
	pb.Complete() // second Complete must be a no-op

	if n := countSummary(t, buf.String()); n != 1 {
		t.Errorf("total==0 progress bar emitted %d summary lines, want exactly 1: %q", n, buf.String())
	}
}

func TestProgressBar_NonTTY_NoLabel_SingleSummary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pb := NewProgressBar(&buf, 5, "")
	if pb.isTTY {
		t.Skip("test environment has a TTY-backed buffer")
	}

	pb.Update(5)
	pb.Complete()
	if n := countSummary(t, buf.String()); n != 1 {
		t.Errorf("unlabelled progress bar emitted %d summary lines, want exactly 1: %q", n, buf.String())
	}
}

func TestProgressBar_NonTTY_UpdateTotalEarly_SingleSummary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	pb := NewProgressBar(&buf, 100, "bulk-copy")
	if pb.isTTY {
		t.Skip("test environment has a TTY-backed buffer")
	}

	// Reach the total many ticks before completion; the bar must stay silent
	// (non-terminal, no render goroutine at all) and still summarise exactly
	// once on Complete.
	pb.Update(100)
	pb.Update(100)
	pb.Increment(10) // already at total; must clamp, not complete
	if buf.Len() != 0 {
		t.Errorf("progress bar wrote before Complete: %q", buf.String())
	}
	pb.Complete()
	if n := countSummary(t, buf.String()); n != 1 {
		t.Errorf("early-Update(total) progress bar emitted %d summary lines, want exactly 1: %q", n, buf.String())
	}
}

// --- Concurrent Complete on the coordinated path: one final line, all
// callers return only after it is written.

func TestProgressBar_ConcurrentComplete_SingleFinalLine(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	o := NewOutput(ft)
	pb := NewProgressBar(o, 50, "copy")

	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			pb.Complete()
		}()
	}
	close(start)
	wg.Wait()

	out := ft.String()
	if n := strings.Count(out, "completed in"); n != 1 {
		t.Errorf("concurrent Complete produced %d final lines, want exactly 1: %q", n, out)
	}
}

// --- Non-TTY spinners never draw frames, so Stop has nothing to erase and
// must write nothing at all.

func TestSpinner_NonTTY_NoWritesAfterStop(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	s := NewSpinner(&buf, "working")
	if s.isTTY {
		t.Skip("writer unexpectedly detected as TTY")
	}
	s.Stop()
	time.Sleep(200 * time.Millisecond) // bounded observation of the 80ms tick
	if n := buf.Len(); n != 0 {
		t.Errorf("non-TTY spinner wrote %d bytes after Stop: %q", n, buf.String())
	}
}

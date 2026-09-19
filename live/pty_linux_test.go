//go:build linux

package live

// Real-pty lifecycle tests: the TTY code paths key off term.IsTerminal on an
// *os.File, so a bytes.Buffer can never reach them. A pty pair lets the
// public API be observed exactly as a terminal would receive it. These are
// Linux-only; other platforms get the deterministic fake-terminal coverage
// in lifecycle_test.go and output_test.go.

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/term"
)

func ptyIoctl(fd, req, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

// newPty opens a pty pair: master is handed to the widgets as the writer,
// slave is read back for the transcript.
func newPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("cannot open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })

	// Unlock the slave and read its index. TIOCSPTLCK takes a pointer to the
	// lock value; passing NULL draws EFAULT on Linux.
	unlocked := int32(0)
	if err := ptyIoctl(master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlocked))); err != nil {
		t.Skipf("TIOCSPTLCK: %v", err)
	}
	var ptn int32
	if err := ptyIoctl(master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&ptn))); err != nil {
		t.Skipf("TIOCGPTN: %v", err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", ptn), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open slave: %v", err)
	}
	t.Cleanup(func() { _ = slave.Close() })

	// Raw mode so the transcript is byte-exact (no ONLCR \n -> \r\n rewriting).
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil { //nolint:gosec // fd fits in int on all supported platforms
		t.Skipf("MakeRaw: %v", err)
	}
	return master, slave
}

// ptyTranscript continuously drains the slave so tests observe writes made by
// widget goroutines without wall-clock synchronisation.
type ptyTranscript struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func startPtyTranscript(t *testing.T, slave *os.File) *ptyTranscript {
	t.Helper()
	tr := &ptyTranscript{}
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := slave.Read(b)
			if n > 0 {
				tr.mu.Lock()
				tr.buf.Write(b[:n])
				tr.mu.Unlock()
			}
			if err != nil {
				return
			}
			select {
			case <-done:
				return
			default:
			}
		}
	}()
	return tr
}

func (tr *ptyTranscript) String() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.buf.String()
}

func (tr *ptyTranscript) Len() int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.buf.Len()
}

// waitUntil polls the transcript until pred matches or the deadline passes.
// The predicate is on bytes actually written — bounded observation, not
// sleep-based synchronisation.
func (tr *ptyTranscript) waitUntil(t *testing.T, pred func(string) bool, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred(tr.String()) {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return pred(tr.String())
}

// Style switch at the highest old frame index: before the R05 fix the next
// tick indexed the new 4-frame set at 9 and killed the process. Now every
// switch normalises the index and rendering continues with the new set.
func TestSpinner_Pty_StyleSwitchAtFrame8_NoPanic(t *testing.T) {
	master, slave := newPty(t)
	tr := startPtyTranscript(t, slave)

	s := NewSpinner(master, "scanning")
	t.Cleanup(s.Stop)
	if !s.isTTY {
		t.Fatal("pty master must be detected as a terminal")
	}

	// The dangerous state is current==9, reached after frame 8 (⠇) draws.
	if !tr.waitUntil(t, func(out string) bool { return strings.Contains(out, "⠇") }, 3*time.Second) {
		t.Fatal("frame 8 never appeared; cannot stage the style switch")
	}

	s.SetStyle(SpinnerStyleBar)

	// The next tick must render a bar-style frame without panicking.
	if !tr.waitUntil(t, func(out string) bool {
		return strings.Contains(out, "|") || strings.Contains(out, "/") ||
			strings.Contains(out, "-") || strings.Contains(out, "\\")
	}, 500*time.Millisecond) {
		t.Errorf("no bar-style frame rendered after the style switch; transcript: %q", tr.String())
	}
}

// Immediate Stop on a real terminal: the initial render races Stop, but
// finalisation joins the render goroutine, so the erase always lands after
// any admitted frame and the visible screen holds no frame afterwards.
func TestSpinner_Pty_ImmediateStop_NoStrayFrame(t *testing.T) {
	const iterations = 20

	stray := 0
	for i := range iterations {
		master, slave := newPty(t)
		tr := startPtyTranscript(t, slave)

		s := NewSpinner(master, "loading")
		if !s.isTTY {
			t.Fatal("pty master must be detected as a terminal")
		}
		s.Stop()

		// Stop joins the render goroutine, so the transcript is complete the
		// moment Stop returns; a short drain window covers pty buffering.
		if !tr.waitUntil(t, func(out string) bool { return strings.Contains(out, "\x1b[K") || out == "" }, 500*time.Millisecond) {
			t.Fatalf("iteration %d: stop sequence never drained", i)
		}

		out := tr.String()
		lastErase := strings.LastIndex(out, "\x1b[K")
		lastFrame := strings.LastIndexAny(out, brailleFrames)
		if lastFrame > lastErase {
			stray++
		}
	}
	if stray > 0 {
		t.Errorf("immediate Stop left a stray frame in %d/%d pty iterations", stray, iterations)
	}
}

// No writes to the terminal after Complete returns, across at least one
// further 100ms tick.
func TestProgressBar_Pty_NoWritesAfterComplete(t *testing.T) {
	const iterations = 10

	late := 0
	for i := range iterations {
		master, slave := newPty(t)
		tr := startPtyTranscript(t, slave)

		pb := NewProgressBar(master, 100, "build")
		if !pb.isTTY {
			t.Fatal("pty master must be detected as a terminal")
		}

		if !tr.waitUntil(t, func(out string) bool { return strings.Contains(out, "%") }, 2*time.Second) {
			t.Fatalf("iteration %d: progress bar never rendered", i)
		}
		pb.Complete()
		// Let Complete's own writes drain before snapshotting.
		time.Sleep(50 * time.Millisecond)
		n := tr.Len()

		time.Sleep(250 * time.Millisecond) // bounded window spanning ticks
		if tr.Len() > n {
			late++
		}
	}
	if late > 0 {
		t.Errorf("%d/%d iterations wrote to the terminal after Complete returned", late, iterations)
	}
}

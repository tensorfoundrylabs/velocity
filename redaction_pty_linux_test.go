//go:build linux

package velocity

// Real-terminal trust coverage promoted from the adversarial battery
// (.verify/scratch/wp3). Uses a raw pty pair via the stdlib syscall package so
// no dependency changes are needed; skips when /dev/ptmx is unavailable.
// Linux-only: the ioctl constants differ elsewhere and the evidence runs
// were Linux; other platforms simply lack these tests rather than failing.

import (
	"bytes"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// wp1OpenPTYPair creates a raw pty pair without draining, so writes to the
// master block once the kernel buffer fills. Callers own both files.
func wp1OpenPTYPair(t *testing.T) (master, slave *os.File) {
	t.Helper()

	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx available: %v", err)
	}

	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); errno != 0 { //nolint:gosec // G115: fd widening is inherent to ioctl; test-only
		_ = syscall.Close(fd)
		t.Skipf("TIOCGPTN failed: %v", errno)
	}
	// 0 unlocks the slave.
	var unlocked int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlocked))); errno != 0 { //nolint:gosec // G115: fd widening is inherent to ioctl; test-only
		_ = syscall.Close(fd)
		t.Skipf("TIOCSPTLCK failed: %v", errno)
	}

	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR, 0)
	if err != nil {
		_ = syscall.Close(fd)
		t.Skipf("cannot open pty slave: %v", err)
	}
	return os.NewFile(uintptr(fd), "wp1-pty-master"), slave //nolint:gosec // G115: fd widening is inherent to file descriptors
}

// wp1DrainedPTY owns a pty pair and drains the slave so console output can be
// inspected. A sentinel written after the last console line ends the capture
// deterministically (closing the master can discard unread input with EIO).
type wp1DrainedPTY struct {
	master *os.File
	slave  *os.File
	out    bytes.Buffer
	done   chan struct{}
	once   sync.Once
}

const wp1Sentinel = "__WP1_EOF__\n"

func wp1OpenPTY(t *testing.T) *wp1DrainedPTY {
	t.Helper()

	master, slave := wp1OpenPTYPair(t)
	p := &wp1DrainedPTY{
		master: master,
		slave:  slave,
		done:   make(chan struct{}),
	}
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := p.slave.Read(buf)
			if n > 0 {
				p.out.Write(buf[:n])
				if bytes.Contains(p.out.Bytes(), []byte(wp1Sentinel)) {
					break
				}
			}
			if err != nil {
				break
			}
		}
		close(p.done)
	}()
	return p
}

// Output writes the sentinel, waits for the drain to observe it (which proves
// every preceding console write was captured) and returns the captured output.
func (p *wp1DrainedPTY) Output(t *testing.T) string {
	t.Helper()
	p.once.Do(func() { _, _ = p.master.WriteString(wp1Sentinel) })
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("pty drain did not observe the sentinel within 5s")
	}
	_ = p.master.Close()
	_ = p.slave.Close()
	if i := bytes.Index(p.out.Bytes(), []byte(wp1Sentinel)); i >= 0 {
		return string(p.out.Bytes()[:i])
	}
	return p.out.String()
}

// wp1Poll waits for cond, bounded by a deadline.
func wp1Poll(cond func() bool, deadline time.Duration) bool {
	deadlineAt := time.Now().Add(deadline)
	for time.Now().Before(deadlineAt) {
		if cond() {
			return true
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// wp1BlockedInWrite reports whether a goroutine is parked inside logInternal's
// console write syscall — the handshake that proves an entry's secure-scan
// flag was computed before AddWriter runs.
func wp1BlockedInWrite() bool {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	for stack := range bytes.SplitSeq(buf[:n], []byte("\n\n")) {
		if bytes.Contains(stack, []byte("logInternal")) &&
			(bytes.Contains(stack, []byte("syscall.Syscall")) ||
				bytes.Contains(stack, []byte("internal/poll."))) {
			return true
		}
	}
	return false
}

// wp1LargeTagged builds a multi-megabyte message containing a secure tag so a
// single console write to an unread pty is guaranteed to block partway.
func wp1LargeTagged(secret string) string {
	var b bytes.Buffer
	b.WriteString("big deployment log line ")
	b.WriteString("<secure>")
	b.WriteString(secret)
	b.WriteString("</secure>")
	b.WriteString(" padding ")
	for b.Len() < 2<<20 {
		b.WriteString("0123456789abcdef")
	}
	return b.String()
}

func wp1ConsoleIsTTY(log *Logger) bool {
	return log != nil && log.consoleWriter != nil && log.consoleWriter.IsTTY()
}

// NO_COLOR on a real terminal suppresses colour but must not change trust:
// the terminal is still trusted, so secure plaintext is shown.
func TestPTYTrust_NoColourTrustUnchanged(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")

	pty := wp1OpenPTY(t)
	log := New(WithConsoleOutput(pty.master), WithLevel(LevelDebug))
	log.Info("credentials <secure>"+wp1Secret+"</secure> accepted", Secure("token", wp1Secret))
	_ = log.Close()
	out := pty.Output(t)

	if !wp1ConsoleIsTTY(log) {
		t.Fatalf("precondition: pty console writer must classify as TTY")
	}
	// Trust, not colour, decides plaintext visibility: the trusted terminal
	// must show the secret even with colour off.
	if !strings.Contains(out, wp1Secret) {
		t.Errorf("trusted terminal must show plaintext under NO_COLOR (colour off, trust on), got:\n%s", out)
	}
	if wp1HasANSI(out) {
		t.Errorf("NO_COLOR should suppress ANSI on a terminal, got colour codes:\n%q", out[:min(len(out), 200)])
	}
}

// FORCE_COLOR on a real terminal: colour on, trust on. Both observable.
func TestPTYTrust_ForceColourTrustedAndColoured(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")

	pty := wp1OpenPTY(t)
	log := New(WithConsoleOutput(pty.master), WithLevel(LevelDebug))
	log.Info("credentials <secure>" + wp1Secret + "</secure> accepted")
	_ = log.Close()
	out := pty.Output(t)

	if !strings.Contains(out, wp1Secret) {
		t.Errorf("trusted terminal under FORCE_COLOR should show plaintext, got:\n%s", out)
	}
	if !wp1HasANSI(out) {
		t.Errorf("FORCE_COLOR on a terminal should emit colour, got none")
	}
}

// Explicit colour disable survives theme swaps on a real TTY — the adversarial
// variant, since the console writer re-derives useColours when the theme
// changes and the terminal itself reports colour-capable.
func TestPTYTheme_ColourDisableSurvivesSwap(t *testing.T) {
	wp1ClearColourEnv(t)

	pty := wp1OpenPTY(t)
	log := New(WithConsoleOutput(pty.master), WithLevel(LevelDebug), WithColour(false))
	if !wp1ConsoleIsTTY(log) {
		t.Fatalf("precondition: pty console must classify as TTY")
	}

	log.Info("before swap")
	log.SetTheme(ThemeDracula)
	log.Info("after swap")
	_ = log.Close()
	out := pty.Output(t)

	if wp1HasANSI(out) {
		head := out
		if len(head) > 400 {
			head = head[:400]
		}
		t.Errorf("colour re-enabled on terminal by SetTheme despite WithColour(false):\n%q", head)
	}
	// Style() must keep reporting a colour-free theme while colour is disabled.
	if name := log.Style().Name(); name != "none" && name != ThemeMono.Name() {
		t.Errorf("Style() should return the no-colour theme while colour is disabled, got %q", name)
	}
}

// wp1PublicationAttempt gates a log call inside the console write on an unread
// pty (whose topology is all-trusted), then registers an untrusted ring writer
// before the call dispatches. Returns (engaged, leaked); engaged=false means
// the gate did not engage this attempt and the caller should retry.
func wp1PublicationAttempt(t *testing.T, secret string, useChild bool) (engaged, leaked bool) {
	t.Helper()

	master, slave := wp1OpenPTYPair(t)
	log := New(WithConsoleOutput(master), WithLevel(LevelDebug))
	if !wp1ConsoleIsTTY(log) {
		_ = log.Close()
		_ = master.Close()
		_ = slave.Close()
		t.Fatalf("precondition: pty console must be trusted")
	}

	msg := wp1LargeTagged(secret)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if useChild {
			child := log.With(String("svc", "gate"))
			child.Info(msg)
		} else {
			log.Info(msg)
		}
	}()

	// Handshake: wait until the log call is parked inside the pty write — its
	// entry's maybeSecure flag is already computed at this point.
	if !wp1Poll(wp1BlockedInWrite, 2*time.Second) {
		_ = log.Close()
		_ = master.Close()
		_ = slave.Close()
		return false, false
	}

	ring := NewRingBufferWriter(8)
	log.AddWriter("late", ring)

	// Drain the pty so the blocked write completes and the call dispatches to
	// the now-registered untrusted writer.
	go func() {
		buf := make([]byte, 64<<10)
		for {
			if _, err := slave.Read(buf); err != nil {
				return
			}
		}
	}()
	<-done
	_ = log.Close()
	_ = master.Close()
	_ = slave.Close()

	for _, s := range ring.Snapshot(8) {
		if strings.Contains(s.Message, secret) {
			return true, true
		}
	}
	return true, false
}

// A writer added between an entry's scan-flag computation and its dispatch
// must not see tag plaintext — the flag is content-derived, so the late
// untrusted writer still receives a flagged entry and redacts it.
func TestPTYPublication_LateUntrustedWriterCannotSeePlaintext(t *testing.T) {
	wp1ClearColourEnv(t)

	for attempt := range 3 {
		engaged, leaked := wp1PublicationAttempt(t, wp1Secret, false)
		if !engaged {
			t.Logf("attempt %d: gate did not engage, retrying", attempt)
			continue
		}
		if leaked {
			t.Fatalf("untrusted writer registered after the entry's scan flag was computed received tag plaintext (attempt %d)", attempt)
		}
		return
	}
	t.Fatal("gate never engaged; test inconclusive after 3 attempts")
}

// The same invariant on a child logger created before the writer was added.
func TestPTYPublication_LateUntrustedWriterChildLoggerSame(t *testing.T) {
	wp1ClearColourEnv(t)

	for attempt := range 3 {
		engaged, leaked := wp1PublicationAttempt(t, wp1Secret, true)
		if !engaged {
			continue
		}
		if leaked {
			t.Fatalf("child logger leaked tag plaintext to a late untrusted writer (attempt %d)", attempt)
		}
		return
	}
	t.Fatal("gate never engaged; test inconclusive after 3 attempts")
}

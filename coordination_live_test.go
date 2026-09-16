package velocity_test

// Integration of the shared terminal coordinator (velocity/live's Output)
// with the console logger: the same object is passed to WithConsoleOutput
// and the widget constructors, and log records emitted mid-progress must
// clear, write and redraw atomically. R12 acceptance lives here because only
// the root package can build the logger.

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tensorfoundrylabs/velocity/v2"
	"github.com/tensorfoundrylabs/velocity/v2/live"
)

// coordTerminal is a terminal-shaped writer guarded by a mutex so tests can
// snapshot the transcript while widget ticker goroutines are still writing.
type coordTerminal struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (f *coordTerminal) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}
func (f *coordTerminal) IsTerminal() bool { return true }
func (f *coordTerminal) String() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.String()
}

// unsafeTerminal is deliberately unsynchronised: any concurrent write not
// serialised by the coordinator trips the race detector.
type unsafeTerminal struct {
	buf bytes.Buffer
}

func (f *unsafeTerminal) Write(p []byte) (int, error) { return f.buf.Write(p) }
func (f *unsafeTerminal) IsTerminal() bool            { return true }

// --- Minimal terminal model (same as the live package tests): replay \r,
// \n, CSI K and CSI nA into the final screen.

func coordReplay(s string) []string {
	type state struct {
		lines [][]rune
		cols  []int
		row   int
	}
	width := func(r rune) int {
		if r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff {
			return 2
		}
		return 1
	}
	sc := &state{lines: [][]rune{{}}, cols: []int{0}}
	trunc := func(row, col int) {
		w := 0
		for i, r := range sc.lines[row] {
			if w >= col {
				sc.lines[row] = sc.lines[row][:i]
				return
			}
			w += width(r)
		}
	}
	i := 0
	for i < len(s) {
		switch {
		case s[i] == '\r':
			sc.cols[sc.row] = 0
			i++
		case s[i] == '\n':
			sc.lines = append(sc.lines, []rune{})
			sc.cols = append(sc.cols, 0)
			sc.row++
			i++
		case s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[':
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j >= len(s) {
				i = len(s)
				continue
			}
			final, params := s[j], s[i+2:j]
			switch final {
			case 'K':
				trunc(sc.row, sc.cols[sc.row])
			case 'A':
				n := 1
				if params != "" {
					_, _ = fmt.Sscanf(params, "%d", &n)
				}
				sc.row -= n
				if sc.row < 0 {
					sc.row = 0
				}
			}
			i = j + 1
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if size == 0 {
				size = 1
			}
			sc.lines[sc.row] = append(sc.lines[sc.row], r)
			sc.cols[sc.row] += width(r)
			i += size
		}
	}
	out := make([]string, len(sc.lines))
	for i, line := range sc.lines {
		out[i] = strings.TrimRight(string(line), " ")
	}
	return out
}

func coordScreen(s string) string {
	return strings.Join(coordReplay(s), "\n")
}

func coordWaitUntil(t *testing.T, ft *coordTerminal, pred func(string) bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pred(ft.String()) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; transcript: %q", what, ft.String())
}

const coordBraille = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

// --- Logs during a spinner, including a multi-line renderable, on a shared
// coordinator over a terminal-shaped destination.

func TestLiveCoordination_LogsDuringSpinner(t *testing.T) {
	t.Parallel()

	ft := &coordTerminal{}
	out := live.NewOutput(ft)

	logger := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(out))
	t.Cleanup(func() { _ = logger.Close() })

	s := live.NewSpinner(out, "scanning")
	coordWaitUntil(t, ft, func(out string) bool { return strings.Contains(out, "scanning") }, "first spinner frame")

	logger.Info("log arrived mid-spin")
	// A multi-line renderable so the record itself spans rows.
	logger.Render(velocity.NewBox("note", "line one\nline two", nil))

	// Let the spinner redraw at least once after the log lines.
	coordWaitUntil(t, ft, func(out string) bool { return strings.Count(out, "\x1b[K") > 3 }, "redraw after logs")

	s.StopWithMessage("scan complete")
	coordWaitUntil(t, ft, func(out string) bool { return strings.Contains(out, "scan complete") }, "final message")

	screen := coordScreen(ft.String())
	for _, want := range []string{"log arrived mid-spin", "line one", "line two", "scan complete"} {
		if !strings.Contains(screen, want) {
			t.Errorf("final screen missing %q; screen:\n%s\nraw: %q", want, screen, ft.String())
		}
	}
	if strings.ContainsAny(screen, coordBraille) {
		t.Errorf("stale spinner frame survived on the final screen; screen:\n%s", screen)
	}
}

// --- MultiProgress row-count changes with a log line landing between
// renders: the redraw must track rows actually drawn, not the item count.

type coordItem struct{ text string }

func (i coordItem) Render() string { return i.text }

func TestLiveCoordination_MultiProgressRowCountChange(t *testing.T) {
	t.Parallel()

	ft := &coordTerminal{}
	out := live.NewOutput(ft)

	logger := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(out))
	t.Cleanup(func() { _ = logger.Close() })

	mp := live.NewMultiProgress(out)
	mp.Add(coordItem{"item-one"})
	coordWaitUntil(t, ft, func(out string) bool { return strings.Contains(out, "item-one") }, "first multi render")

	logger.Info("log between renders")

	mp.Add(coordItem{"item-two"})
	coordWaitUntil(t, ft, func(out string) bool { return strings.Contains(out, "item-two") }, "second item render")

	// Both rows and the log line between renders must coexist while live.
	screen := coordScreen(ft.String())
	for _, want := range []string{"log between renders", "item-one", "item-two"} {
		if !strings.Contains(screen, want) {
			t.Errorf("live screen missing %q (row-count change clobbered it); screen:\n%s\nraw: %q", want, screen, ft.String())
		}
	}

	// Stop removes the live rows: the log line survives as scrollback.
	mp.Stop()
	screen = coordScreen(ft.String())
	if !strings.Contains(screen, "log between renders") {
		t.Errorf("log line lost after Stop; screen:\n%s\nraw: %q", screen, ft.String())
	}
	if strings.Contains(screen, "item-one") || strings.Contains(screen, "item-two") {
		t.Errorf("live rows survived Stop; screen:\n%s", screen)
	}
}

// --- Non-terminal shared output: normal logs plus one summary line each,
// and not a single cursor escape anywhere.

func TestLiveCoordination_NonTTY_NoCursorEscapes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := live.NewOutput(&buf)

	logger := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(out))
	t.Cleanup(func() { _ = logger.Close() })

	s := live.NewSpinner(out, "scanning")
	pb := live.NewProgressBar(out, 4, "load")
	for range int64(4) {
		pb.Increment(1)
	}

	logger.Info("first log line")
	logger.Info("second log line")
	s.StopWithMessage("scan complete")
	pb.Complete()

	got := buf.String()
	// FORCE_COLOR legitimately styles a non-terminal (SGR, final byte 'm');
	// what must never reach it is cursor MOVEMENT or erase sequences.
	if strings.Contains(got, "\r") {
		t.Errorf("non-TTY shared output received carriage returns: %q", got)
	}
	if seq := firstCursorSequence(got); seq != "" {
		t.Errorf("non-TTY shared output received cursor-control sequence %q: %q", seq, got)
	}
	for _, want := range []string{"first log line", "second log line", "scan complete", "completed in"} {
		if !strings.Contains(got, want) {
			t.Errorf("shared-buffer transcript missing %q; got %q", want, got)
		}
	}
}

// --- Capability forwarding: the coordinator forwards the real destination's
// terminal status; FORCE_COLOR cannot make a wrapped pipe a terminal, and
// styling follows the root colour policy through the same forwarding.

func TestLiveCoordination_CapabilityForwarding(t *testing.T) {
	t.Parallel()

	if !velocity.IsTerminalWriter(live.NewOutput(&coordTerminal{})) {
		t.Error("IsTerminalWriter must forward the coordinator's terminal capability")
	}
	if velocity.IsTerminalWriter(live.NewOutput(&bytes.Buffer{})) {
		t.Error("a coordinator over a plain buffer must not be reported as a terminal")
	}
}

func TestLiveCoordination_ColourThroughCoordinator(t *testing.T) {
	// Colour policy is env-dependent; pin the environment (no t.Parallel).
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")

	ft := &coordTerminal{}
	out := live.NewOutput(ft)
	logger := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(out))
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("colour me")
	got := ft.String()
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("console colour lost through the coordinator despite a terminal destination: %q", got)
	}
}

func TestLiveCoordination_ForceColorStylesButNeverCursorControl(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")

	var buf bytes.Buffer
	out := live.NewOutput(&buf)
	logger := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(out))
	t.Cleanup(func() { _ = logger.Close() })

	s := live.NewSpinner(out, "working")
	logger.Info("styled but static")
	s.StopWithMessage("done")

	got := buf.String()
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("FORCE_COLOR styling should reach a pipe through the coordinator: %q", got)
	}
	// Styling is SGR (final byte 'm'); cursor control would show up as CSI
	// sequences ending in K or A, or bare carriage returns.
	if strings.Contains(got, "\r") {
		t.Errorf("cursor carriage returns leaked onto a pipe: %q", got)
	}
	if seq := firstCursorSequence(got); seq != "" {
		t.Errorf("cursor-control sequence %q emitted on a non-terminal (FORCE_COLOR is colour policy only): %q", seq, got)
	}
}

// firstCursorSequence returns the first CSI sequence in s whose final byte is
// a cursor-control or erase operation (CUU/CUD/CUF/CUB/CHA/CUP, ED/EL, HVP,
// scroll, save/restore), or "". SGR colour (final 'm') is deliberately
// allowed: FORCE_COLOR legitimately styles non-terminals.
func firstCursorSequence(s string) string {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != 0x1b || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j >= len(s) {
			return ""
		}
		if strings.IndexByte("ABCDEFGHJKSTfhnsu", s[j]) >= 0 {
			return s[i : j+1]
		}
		i = j
	}
	return ""
}

// --- Independent sinks: a stalled destination behind one coordinator must
// not block an unrelated logger on another.

type coordGatedWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *coordGatedWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestLiveCoordination_IndependentSinksDoNotBlockEachOther(t *testing.T) {
	t.Parallel()

	gated := &coordGatedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	loggerA := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(live.NewOutput(gated)))
	loggerB := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(&bytes.Buffer{}))
	t.Cleanup(func() {
		close(gated.release)
		_ = loggerA.Close()
		_ = loggerB.Close()
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		loggerA.Info("blocked write")
	}()
	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("logger A never reached its writer")
	}

	bDone := make(chan struct{})
	go func() {
		defer close(bDone)
		loggerB.Info("independent sink")
	}()
	select {
	case <-bDone:
	case <-time.After(2 * time.Second):
		t.Error("logger B blocked while logger A's coordinator destination stalled")
	}
}

// --- Race coverage: the unsafe buffer behind the coordinator carries the
// console writer's log writes as well as widget frames, all serialised.

func TestLiveCoordination_Race_UnsafeBufferBehindCoordinator(t *testing.T) {
	t.Parallel()

	ft := &unsafeTerminal{}
	out := live.NewOutput(ft)
	logger := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(out))

	s := live.NewSpinner(out, "busy")
	pb := live.NewProgressBar(out, 100, "load")
	mp := live.NewMultiProgress(out)
	mp.Add(coordItem{"row"})

	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			logger.Info("concurrent record", velocity.Int("seq", i))
		}(i)
	}
	wg.Add(3)
	go func() { defer wg.Done(); s.StopWithSuccess("spinner done") }()
	go func() { defer wg.Done(); pb.Complete() }()
	go func() { defer wg.Done(); mp.Stop() }()
	wg.Wait()

	if err := logger.Close(); err != nil {
		t.Fatalf("logger close: %v", err)
	}
}

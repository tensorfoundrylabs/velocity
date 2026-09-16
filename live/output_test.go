package live

// Shared-terminal coordination tests (R12). The fake terminal below claims
// terminal capability so Output's cursor-control paths run deterministically
// without a pty, and its buffer is deliberately unsynchronised: every test
// that touches it concurrently does so through an Output, so the race
// detector proves the coordinator serialises all writes.

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// fakeTerminal is an unsafe, unsynchronised terminal-shaped writer. IsTerminal
// makes Output (and the widgets) take the cursor-control paths. Tests that
// inspect it while widgets are live use syncTerminal instead.
type fakeTerminal struct {
	buf bytes.Buffer
}

func (f *fakeTerminal) Write(p []byte) (int, error) { return f.buf.Write(p) }
func (f *fakeTerminal) IsTerminal() bool            { return true }
func (f *fakeTerminal) String() string              { return f.buf.String() }

// syncTerminal guards the same shape with a mutex so tests can snapshot the
// transcript while widget ticker goroutines are still writing.
type syncTerminal struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (f *syncTerminal) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}
func (f *syncTerminal) IsTerminal() bool { return true }
func (f *syncTerminal) String() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.String()
}

// --- Minimal terminal model: replay \r, \n, CSI K (erase line) and CSI nA
// (cursor up n) into the final screen contents. Wide enough for every
// sequence Output and the widgets emit.

type screen struct {
	lines [][]rune
	cols  []int
	row   int
}

func runeWidthTest(r rune) int {
	if r >= 0x1100 && (r <= 0x115f ||
		r >= 0x2e80 && r <= 0xa4cf ||
		r >= 0xac00 && r <= 0xd7a3 ||
		r >= 0xf900 && r <= 0xfaff ||
		r >= 0xfe30 && r <= 0xfe6f ||
		r >= 0xff00 && r <= 0xff60 ||
		r >= 0xffe0 && r <= 0xffe6) {
		return 2
	}
	return 1
}

func (sc *screen) truncate(row, cellCol int) {
	w := 0
	for i, r := range sc.lines[row] {
		if w >= cellCol {
			sc.lines[row] = sc.lines[row][:i]
			return
		}
		w += runeWidthTest(r)
	}
}

// replay turns a raw byte stream into the final screen a terminal would show.
func replay(s string) []string {
	return screenLines(replayCore(s))
}

// replayCore replays s into a full terminal model: contents per row plus the
// final cursor row. Drift tests assert on the cursor row and blank-row growth
// directly; text-survival checks alone cannot see a display walking down the
// screen.
func replayCore(s string) *screen {
	sc := &screen{lines: [][]rune{{}}, cols: []int{0}}
	i := 0
	for i < len(s) {
		b := s[i]
		switch {
		case b == '\r':
			sc.cols[sc.row] = 0
			i++
		case b == '\n':
			sc.lines = append(sc.lines, []rune{})
			sc.cols = append(sc.cols, 0)
			sc.row++
			i++
		case b == 0x1b && i+1 < len(s) && s[i+1] == '[':
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j >= len(s) {
				i = len(s)
				break
			}
			final := s[j]
			params := s[i+2 : j]
			n := 1
			if params != "" {
				_, _ = fmt.Sscanf(params, "%d", &n)
			}
			switch final {
			case 'K':
				sc.truncate(sc.row, sc.cols[sc.row])
			case 'A':
				sc.row -= n
				if sc.row < 0 {
					sc.row = 0
				}
			case 'B':
				// Cursor down: a terminal creates blank rows when moving past
				// the end of the buffer.
				for sc.row+n >= len(sc.lines) {
					sc.lines = append(sc.lines, []rune{})
					sc.cols = append(sc.cols, 0)
				}
				sc.row += n
			}
			i = j + 1
		default:
			r, size := decodeRuneTest(s[i:])
			sc.lines[sc.row] = append(sc.lines[sc.row], r)
			sc.cols[sc.row] += runeWidthTest(r)
			i += size
		}
	}
	return sc
}

func screenLines(sc *screen) []string {
	out := make([]string, len(sc.lines))
	for i, line := range sc.lines {
		out[i] = strings.TrimRight(string(line), " ")
	}
	return out
}

func decodeRuneTest(s string) (rune, int) {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		size = 1
	}
	return r, size
}

func screenJoin(lines []string) string {
	return strings.Join(lines, "\n")
}

const brailleFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

// firstCursorSeq returns the first CSI sequence whose final byte is a cursor
// movement or erase operation, or "". SGR colour (final 'm') is allowed:
// FORCE_COLOR legitimately styles non-terminals.
func firstCursorSeq(s string) string {
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

// --- Capability forwarding ---

func TestOutput_CapabilityForwardsFromDestination(t *testing.T) {
	t.Parallel()

	if !NewOutput(&fakeTerminal{}).IsTerminal() {
		t.Error("Output over a terminal-capable writer must report a terminal")
	}
	if NewOutput(&bytes.Buffer{}).IsTerminal() {
		t.Error("Output over a plain buffer must not pretend to be a terminal")
	}
}

func TestOutput_ForceColorDoesNotGrantCursorControl(t *testing.T) {
	// t.Setenv forbids parallel tests in this file section.
	t.Setenv("FORCE_COLOR", "1")

	if NewOutput(&bytes.Buffer{}).IsTerminal() {
		t.Error("FORCE_COLOR is colour policy; it must not make a buffer terminal-capable for cursor movement")
	}
}

func TestOutput_NoColorKeepsCursorControlOnTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")

	if !NewOutput(&fakeTerminal{}).IsTerminal() {
		t.Error("NO_COLOR is colour policy; it alone must not stop cursor movement on a real terminal")
	}
}

// --- Non-TTY: plain pass-through, no cursor escapes ever ---

func TestOutput_NonTTY_PassThroughNoCursorControl(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	o := NewOutput(&buf)

	s := NewSpinner(o, "scanning")
	s.SetLabel("still scanning")
	pb := NewProgressBar(o, 10, "load")
	pb.Update(5)

	if _, err := o.Write([]byte("log line one\nlog line two\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	s.StopWithMessage("scan complete")
	pb.Complete()

	out := buf.String()
	// Distinguish cursor movement from styling: FORCE_COLOR may legitimately
	// put SGR colour on a non-terminal, but never cursor-control bytes.
	if strings.Contains(out, "\r") {
		t.Errorf("non-TTY Output emitted carriage returns: %q", out)
	}
	if seq := firstCursorSeq(out); seq != "" {
		t.Errorf("non-TTY Output emitted cursor-control sequence %q: %q", seq, out)
	}
	for _, want := range []string{"log line one", "log line two", "scan complete", "completed in"} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q: %q", want, out)
		}
	}
}

// --- Deterministic fake-terminal transcript: multiple widgets, multi-line
// logs, row-count changes, stop ordering ---

type testItem struct{ text string }

func (i testItem) Render() string { return i.text }

func TestOutput_Transcript_LogsWidgetsAndRowCountChanges(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	o := NewOutput(ft)

	s := NewSpinner(o, "scanning")
	mp := NewMultiProgress(o)

	// Initial spinner frame plus a one-row multi block.
	s.render()
	mp.Add(testItem{"item-one"})
	mp.render()

	// Whole multi-line log record through the coordinator.
	_, _ = o.Write([]byte("2026-09-15 [INFO] log arrived mid-spin\nnote line one\nnote line two\n"))

	// Grow the live row count while the log record sits between renders.
	mp.Add(testItem{"item-two"})
	mp.render()
	// Shrink it again: the erase must use rows previously drawn, not the
	// current item count.
	mp.Remove(testItem{"item-two"})
	mp.render()

	// Mid-block single-line log.
	_, _ = o.Write([]byte("log between renders\n"))

	screen := replay(ft.String())
	joined := screenJoin(screen)
	for _, want := range []string{
		"log arrived mid-spin", "note line one", "note line two",
		"log between renders", "item-one",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("live screen missing %q; screen:\n%s\nraw: %q", want, joined, ft.String())
		}
	}

	// A record without a trailing newline must not glue the first live row on.
	_, _ = o.Write([]byte("partial record"))
	screen = replay(ft.String())
	if !strings.Contains(screenJoin(screen), "partial record") {
		t.Errorf("partial record lost from screen: %q", ft.String())
	}
	if !strings.Contains(screenJoin(screen), "item-one") {
		t.Errorf("live rows not redrawn after partial record: %q", ft.String())
	}

	// Stopping the spinner keeps the multi block and all scrollback intact.
	s.StopWithMessage("scan complete")
	joined = screenJoin(replay(ft.String()))
	if !strings.Contains(joined, "scan complete") {
		t.Errorf("final message missing: %q", ft.String())
	}
	if strings.ContainsAny(joined, brailleFrames) {
		t.Errorf("stale spinner frame survived Stop; screen:\n%s", joined)
	}
	if !strings.Contains(joined, "item-one") {
		t.Errorf("remaining widget lost when spinner stopped; screen:\n%s", joined)
	}

	// Stopping the last widget removes every live row: only scrollback stays.
	mp.Stop()
	joined = screenJoin(replay(ft.String()))
	for _, want := range []string{"item-one", "item-two"} {
		if strings.Contains(joined, want) {
			t.Errorf("live rows survived Stop; screen:\n%s", joined)
		}
	}
	for _, want := range []string{"log arrived mid-spin", "log between renders", "scan complete"} {
		if !strings.Contains(joined, want) {
			t.Errorf("scrollback lost after Stop; screen:\n%s", joined)
		}
	}
	if strings.ContainsAny(joined, brailleFrames) {
		t.Errorf("spinner frame survived all stops; screen:\n%s", joined)
	}
}

// --- Race coverage: the unsafe underlying buffer must only ever be touched
// under the Output lock. ---

func TestOutput_Race_UnsafeBufferConcurrentWritesAndDraws(t *testing.T) {
	t.Parallel()

	ft := &fakeTerminal{}
	o := NewOutput(ft)

	mp := NewMultiProgress(o)
	mp.Add(testItem{"row-one"})
	mp.Add(testItem{"row-two"})
	s := NewSpinner(o, "busy")
	pb := NewProgressBar(o, 100, "load")

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = o.Write(fmt.Appendf(nil, "log record %d\nsecond line %d\n", i, i))
		}(i)
	}
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o.draw(o.register(), []string{fmt.Sprintf("live-%d-a", i), fmt.Sprintf("live-%d-b", i)})
		}(i)
	}
	wg.Add(3)
	for _, fn := range []func(){
		func() { defer wg.Done(); s.StopWithMessage("spinner done") },
		func() { defer wg.Done(); pb.Complete() },
		func() { defer wg.Done(); mp.Stop() },
	} {
		go fn()
	}
	wg.Wait()
}

func TestOutput_Race_ConcurrentFinalisersOneMessage(t *testing.T) {
	t.Parallel()

	ft := &fakeTerminal{}
	o := NewOutput(ft)
	s := NewSpinner(o, "working")

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

	out := ft.String()
	finals := 0
	for _, marker := range []string{"plain-done", "success-done", "error-done"} {
		if strings.Contains(out, marker) {
			finals++
		}
	}
	if finals != 1 {
		t.Errorf("concurrent finalisers produced %d final messages, want exactly 1: %q", finals, out)
	}
}

// --- Independent sinks: no global mutex shared across Outputs ---

type gatedWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestOutput_IndependentSinksDoNotBlockEachOther(t *testing.T) {
	t.Parallel()

	gated := &gatedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	blocked := NewOutput(gated)
	free := NewOutput(&bytes.Buffer{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = blocked.Write([]byte("blocked record\n"))
	}()

	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("write never reached the gated destination")
	}

	smallDone := make(chan struct{})
	go func() {
		defer close(smallDone)
		_, _ = free.Write([]byte("independent record\n"))
	}()
	select {
	case <-smallDone:
	case <-time.After(2 * time.Second):
		t.Error("unrelated Output blocked while another Output's destination stalled")
	}

	close(gated.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("blocked write never completed after release")
	}
}

// --- Flush/Sync forwarding keeps the root logger's fatal/close semantics
// intact when output is routed through the coordinator. ---

type flushCounter struct {
	mu      sync.Mutex
	flushes int
}

func (f *flushCounter) Write(p []byte) (int, error) { return len(p), nil }
func (f *flushCounter) IsTerminal() bool            { return false }
func (f *flushCounter) Flush() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
	return nil
}

func TestOutput_ForwardsFlushAndSync(t *testing.T) {
	t.Parallel()

	fc := &flushCounter{}
	o := NewOutput(fc)
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	fc.mu.Lock()
	n := fc.flushes
	fc.mu.Unlock()
	if n != 1 {
		t.Errorf("Flush forwarded %d times, want 1", n)
	}
	if err := o.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

var _ io.Writer = (*Output)(nil)

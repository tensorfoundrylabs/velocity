package live

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

// isTerminalWriter reports whether w's real destination is a terminal. Live
// cursor control keys off this alone: NO_COLOR and FORCE_COLOR are colour
// policy (the root package resolves them for styling) and neither makes a
// pipe safe for cursor movement nor stops cursor movement on a real terminal.
// Wrappers that know their destination's capability — Output does — forward
// it by implementing interface{ IsTerminal() bool }, so wrapping os.Stdout
// does not lose detection while a wrapped pipe still reports false.
func isTerminalWriter(w io.Writer) bool {
	if detector, ok := w.(interface{ IsTerminal() bool }); ok {
		return detector.IsTerminal()
	}
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: uintptr fd fits in int on all supported platforms
	}
	return false
}

// sink is the resolved output wiring shared by every widget: either a shared
// Output coordinator (out != nil, with a registration handle) or a plain
// standalone writer.
type sink struct {
	writer io.Writer
	out    *Output
	state  *outState
	isTTY  bool
}

func resolveSink(w io.Writer) sink {
	if o, ok := w.(*Output); ok {
		return sink{writer: o, out: o, state: o.register(), isTTY: o.IsTerminal()}
	}
	return sink{writer: w, isTTY: isTerminalWriter(w)}
}

// buildClear appends the sequence that erases rows terminal rows and leaves
// the cursor at column 0 of the FIRST live row — the same first-row invariant
// the coordinator holds, so repeated standalone repaints do not walk the
// display down the screen. Callers own any locking.
func buildClear(b *strings.Builder, rows int) {
	writeClearSeq(b, rows)
}

// buildLines appends lines each terminated by a newline except the last,
// which is left open so the cursor parks at its end. Returns the number of
// rows painted. Callers own any locking.
func buildLines(b *strings.Builder, lines []string) int {
	for i, line := range lines {
		b.WriteString(line)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return len(lines)
}

// ProgressBar displays progress for long-running operations.
// Thread-safe for concurrent updates while logging continues.
//
// When the destination is not a terminal (piped output, CI, log files),
// per-tick renders are suppressed — no render goroutine is started at all —
// and a single summary line is written on Complete().
type ProgressBar struct {
	started    time.Time
	lastDraw   time.Time
	writer     io.Writer
	out        *Output
	state      *outState
	done       chan struct{}
	renderDone chan struct{}
	finDone    chan struct{}
	label      string
	total      int64
	current    int64
	width      int
	mu         sync.Mutex
	active     atomic.Bool
	noOp       bool
	isTTY      bool
	completed  bool
}

func NewProgressBar(w io.Writer, total int64, label string) *ProgressBar {
	if w == nil {
		return &ProgressBar{
			noOp:  true,
			total: total,
			done:  make(chan struct{}),
		}
	}

	if total < 0 {
		total = 0
	}

	s := resolveSink(w)
	pb := &ProgressBar{
		writer:  s.writer,
		out:     s.out,
		state:   s.state,
		total:   total,
		label:   label,
		width:   40,
		started: time.Now(),
		done:    make(chan struct{}),
		finDone: make(chan struct{}),
		isTTY:   s.isTTY,
	}
	pb.active.Store(true)

	// Static non-terminal output has nothing to animate: skip the ticker
	// goroutine entirely, Complete() writes the one summary line directly.
	if pb.isTTY {
		pb.renderDone = make(chan struct{})
		go pb.renderLoop()
	}

	return pb
}

func (pb *ProgressBar) Update(current int64) {
	if pb == nil || pb.noOp {
		return
	}

	if current < 0 {
		current = 0
	}
	if current > pb.total {
		current = pb.total
	}

	pb.mu.Lock()
	pb.current = current
	pb.mu.Unlock()
}

func (pb *ProgressBar) Increment(delta int64) {
	if pb == nil || pb.noOp {
		return
	}

	pb.mu.Lock()
	pb.current += delta
	if pb.current < 0 {
		pb.current = 0
	}
	if pb.current > pb.total {
		pb.current = pb.total
	}
	pb.mu.Unlock()
}

func (pb *ProgressBar) SetLabel(label string) {
	if pb == nil || pb.noOp {
		return
	}

	pb.mu.Lock()
	pb.label = label
	pb.mu.Unlock()
}

// Complete finalises the bar exactly once: concurrent callers all wait for
// the same completion output. On a terminal the final frame is promoted to a
// permanent line; on other destinations exactly one summary line is written
// even when the total was reached (or is zero) many ticks earlier.
func (pb *ProgressBar) Complete() {
	if pb == nil || pb.noOp {
		return
	}

	if pb.active.CompareAndSwap(true, false) {
		close(pb.done)
		// Join the render goroutine before emitting anything: a tick
		// admitted before the CAS may still be mid-render, and its frame
		// must land before the final line, never after it. Waiting holds no
		// lock the renderer needs.
		if pb.renderDone != nil {
			<-pb.renderDone
		}

		pb.mu.Lock()
		pb.current = pb.total
		pb.completed = true
		pb.mu.Unlock()

		switch {
		case pb.out != nil && pb.isTTY:
			pb.out.remove(pb.state, []string{pb.frameLine()})
		case pb.out != nil:
			pb.out.remove(pb.state, []string{pb.summaryLine()})
		case pb.isTTY:
			// Final frame in place, then finish the line so the bar stays
			// visible as scrollback.
			pb.render()
			_, _ = fmt.Fprintln(pb.writer)
		default:
			_, _ = fmt.Fprintln(pb.writer, pb.summaryLine())
		}

		close(pb.finDone)
	} else {
		// A concurrent Complete won; wait until its output has finished so
		// no caller can observe half-written terminal state.
		<-pb.finDone
	}
}

// frameLine renders the current bar as a single frame line. Callers must not
// hold pb.mu.
func (pb *ProgressBar) frameLine() string {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	var percent float64
	if pb.total > 0 {
		percent = float64(pb.current) / float64(pb.total) * 100
	}
	elapsed := time.Since(pb.started)
	filled := min(int(float64(pb.width)*percent/100), pb.width)

	var eta time.Duration
	if pb.current > 0 && pb.current < pb.total {
		rate := float64(pb.current) / elapsed.Seconds()
		remaining := float64(pb.total - pb.current)
		eta = time.Duration(remaining/rate) * time.Second
	}

	var bar strings.Builder
	if pb.label != "" {
		bar.WriteString(pb.label)
		bar.WriteString(" ")
	}
	bar.WriteString("[")
	for i := range pb.width {
		if i < filled {
			bar.WriteString("█")
		} else {
			bar.WriteString("░")
		}
	}
	bar.WriteString("] ")
	fmt.Fprintf(&bar, "%3.0f%% ", percent)
	fmt.Fprintf(&bar, "(%d/%d) ", pb.current, pb.total)
	if pb.current >= pb.total {
		fmt.Fprintf(&bar, "completed in %s", formatDuration(elapsed))
	} else if eta > 0 {
		fmt.Fprintf(&bar, "ETA: %s", formatDuration(eta))
	}
	return bar.String()
}

// summaryLine renders the single non-terminal completion summary.
func (pb *ProgressBar) summaryLine() string {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	elapsed := time.Since(pb.started)
	if pb.label != "" {
		return fmt.Sprintf("%s: completed in %s", pb.label, formatDuration(elapsed))
	}
	return "completed in " + formatDuration(elapsed)
}

// render draws one in-place frame. Only reached on a terminal; non-terminal
// bars have no render loop and Complete writes the summary directly.
func (pb *ProgressBar) render() {
	line := pb.frameLine()
	if pb.out != nil {
		pb.out.draw(pb.state, []string{line})
		return
	}
	var b strings.Builder
	b.WriteString("\r\x1b[K")
	b.WriteString(line)
	_, _ = fmt.Fprint(pb.writer, b.String())
	pb.mu.Lock()
	pb.lastDraw = time.Now()
	pb.mu.Unlock()
}

func (pb *ProgressBar) renderLoop() {
	defer close(pb.renderDone)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pb.done:
			return
		case <-ticker.C:
			if pb.active.Load() {
				pb.render()
			}
		}
	}
}

// Spinner displays an animated spinner for indeterminate progress.
// Thread-safe and works concurrently with logging.
//
// When the destination is not a terminal, frames are never drawn — no render
// goroutine is started — and Stop/StopWith* still emit their final line.
type Spinner struct {
	writer     io.Writer
	out        *Output
	state      *outState
	done       chan struct{}
	renderDone chan struct{}
	finDone    chan struct{}
	label      string
	frames     []string
	current    int
	mu         sync.Mutex
	active     atomic.Bool
	noOp       bool
	isTTY      bool
}

func NewSpinner(w io.Writer, label string) *Spinner {
	if w == nil {
		return &Spinner{
			noOp: true,
			done: make(chan struct{}),
		}
	}

	s := resolveSink(w)
	sp := &Spinner{
		writer:  s.writer,
		out:     s.out,
		state:   s.state,
		label:   label,
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		done:    make(chan struct{}),
		finDone: make(chan struct{}),
		isTTY:   s.isTTY,
	}
	sp.active.Store(true)

	if sp.isTTY {
		sp.renderDone = make(chan struct{})
		go sp.renderLoop()
	}

	return sp
}

func (s *Spinner) SetLabel(label string) {
	if s == nil || s.noOp {
		return
	}

	s.mu.Lock()
	s.label = label
	s.mu.Unlock()
}

// Stop erases the spinner. Safe to call concurrently: at most one caller
// performs the finalisation and every other caller waits for it to finish.
func (s *Spinner) Stop() {
	s.finalise("", false)
}

func (s *Spinner) StopWithMessage(message string) {
	if s == nil {
		fmt.Println(message)
		return
	}
	s.finalise(message, true)
}

func (s *Spinner) StopWithSuccess(message string) {
	if s == nil {
		fmt.Printf("✅ %s\n", message)
		return
	}
	s.finalise("✅ "+message, true)
}

func (s *Spinner) StopWithError(message string) {
	if s == nil {
		fmt.Printf("❌ %s\n", message)
		return
	}
	s.finalise("❌ "+message, true)
}

// finalise performs the single terminal transition for this spinner: stop
// admitting renders, join the render goroutine, remove the live rows, and
// emit the optional final message through the same output synchronisation as
// frames. The CAS winner does the work; concurrent finaliser calls wait on
// finDone so none of them return while output is still in flight.
func (s *Spinner) finalise(finalLine string, hasFinal bool) {
	if s == nil || s.noOp {
		return
	}

	if s.active.CompareAndSwap(true, false) {
		close(s.done)
		// A render admitted before the CAS may still be writing its frame;
		// join the goroutine first so the frame lands before the erase and
		// never after it. No lock is held while waiting — render() needs
		// both the widget mutex and the output lock.
		if s.renderDone != nil {
			<-s.renderDone
		}

		switch {
		case s.out != nil:
			if hasFinal {
				s.out.remove(s.state, []string{finalLine})
			} else {
				s.out.remove(s.state, nil)
			}
		case s.isTTY:
			// Only erase on a terminal; non-terminals never drew a frame.
			_, _ = fmt.Fprint(s.writer, "\r\x1b[K")
			if hasFinal {
				_, _ = fmt.Fprintln(s.writer, finalLine)
			}
		case hasFinal:
			_, _ = fmt.Fprintln(s.writer, finalLine)
		}

		close(s.finDone)
	} else {
		<-s.finDone
	}
}

func (s *Spinner) render() {
	// Snapshot the frame under the mutex, then draw outside it: state capture
	// and terminal I/O stay separate so neither lock is held longer than
	// needed.
	s.mu.Lock()
	line := fmt.Sprintf("%s %s", s.frames[s.current], s.label)
	s.current = (s.current + 1) % len(s.frames)
	s.mu.Unlock()

	if s.out != nil {
		s.out.draw(s.state, []string{line})
		return
	}
	_, _ = fmt.Fprint(s.writer, "\r\x1b[K")
	_, _ = fmt.Fprint(s.writer, line)
}

func (s *Spinner) renderLoop() {
	defer close(s.renderDone)
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	// The first render is guarded by the active flag: an immediate Stop may
	// have already won the CAS, in which case no frame is drawn at all.
	if s.active.Load() {
		s.render()
	}

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if s.active.Load() {
				s.render()
			}
		}
	}
}

// SpinnerStyle selects the animation frame set.
type SpinnerStyle int

const (
	SpinnerStyleBraille SpinnerStyle = iota
	SpinnerStyleDots
	SpinnerStyleArrows
	SpinnerStyleBounce
	SpinnerStyleBar
)

func (s *Spinner) SetStyle(style SpinnerStyle) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch style {
	case SpinnerStyleBraille:
		s.frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	case SpinnerStyleDots:
		s.frames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	case SpinnerStyleArrows:
		s.frames = []string{"←", "↖", "↑", "↗", "→", "↘", "↓", "↙"}
	case SpinnerStyleBounce:
		s.frames = []string{"⠁", "⠂", "⠄", "⡀", "⢀", "⠠", "⠐", "⠈"}
	case SpinnerStyleBar:
		s.frames = []string{"|", "/", "-", "\\"}
	}
	// The new set may be shorter than the old one; an index left over from
	// the previous set would panic the render goroutine on its next tick.
	if s.current >= len(s.frames) {
		s.current = 0
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour //nolint:durationcheck // h is already a time.Duration from division
	m := d / time.Minute
	d -= m * time.Minute //nolint:durationcheck // m is already a time.Duration from division
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// MultiProgress manages multiple progress bars or spinners simultaneously.
//
// When the destination is not a terminal, frame renders are suppressed — no
// render goroutine is started — so pipes and log files receive no cursor
// movement.
type MultiProgress struct {
	writer     io.Writer
	out        *Output
	state      *outState
	done       chan struct{}
	renderDone chan struct{}
	finDone    chan struct{}
	items      []ProgressItem
	rowsDrawn  int
	mu         sync.Mutex
	active     atomic.Bool
	isTTY      bool
}

// ProgressItem is implemented by types that can render themselves as a progress line.
type ProgressItem interface {
	Render() string
}

func NewMultiProgress(w io.Writer) *MultiProgress {
	s := sink{}
	if w != nil {
		s = resolveSink(w)
	}
	mp := &MultiProgress{
		writer:  s.writer,
		out:     s.out,
		state:   s.state,
		items:   make([]ProgressItem, 0),
		done:    make(chan struct{}),
		finDone: make(chan struct{}),
		isTTY:   s.isTTY,
	}
	mp.active.Store(true)

	if mp.isTTY {
		mp.renderDone = make(chan struct{})
		go mp.renderLoop()
	}

	return mp
}

func (mp *MultiProgress) Add(item ProgressItem) {
	mp.mu.Lock()
	mp.items = append(mp.items, item)
	mp.mu.Unlock()
}

func (mp *MultiProgress) Remove(item ProgressItem) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for i, it := range mp.items {
		if it == item {
			mp.items = append(mp.items[:i], mp.items[i+1:]...)
			break
		}
	}
}

// Stop erases the live area and deregisters the widget before returning.
// Concurrent callers all wait for the same completion output.
func (mp *MultiProgress) Stop() {
	if mp.active.CompareAndSwap(true, false) {
		close(mp.done)
		if mp.renderDone != nil {
			<-mp.renderDone
		}

		if mp.out != nil {
			mp.out.remove(mp.state, nil)
		} else if mp.isTTY && mp.writer != nil {
			mp.mu.Lock()
			rows := mp.rowsDrawn
			mp.rowsDrawn = 0
			mp.mu.Unlock()
			var b strings.Builder
			buildClear(&b, rows)
			// buildClear returns to the first erased row; park the cursor one
			// row BELOW the erased block so later output starts on a fresh
			// line instead of overwriting the blank rows.
			if rows > 0 {
				fmt.Fprintf(&b, "\x1b[%dB", rows)
			}
			_, _ = fmt.Fprint(mp.writer, b.String())
		}

		close(mp.finDone)
	} else {
		<-mp.finDone
	}
}

// render repaints the whole block. Item render callbacks run under the widget
// mutex only — never under the Output lock — and the erase uses the row count
// actually drawn previously, so adding or removing items (or a log line
// landing between renders through a shared Output) cannot make the repaint
// start above the block and clobber scrollback.
func (mp *MultiProgress) render() {
	mp.mu.Lock()
	if len(mp.items) == 0 && mp.rowsDrawn == 0 {
		mp.mu.Unlock()
		return
	}
	// Capture item lines under the widget lock; item.Render is user code.
	lines := make([]string, 0, len(mp.items))
	for _, item := range mp.items {
		lines = append(lines, item.Render())
	}
	prev := mp.rowsDrawn
	mp.rowsDrawn = len(lines)
	mp.mu.Unlock()

	if mp.out != nil {
		mp.out.draw(mp.state, lines)
		return
	}

	var b strings.Builder
	buildClear(&b, prev)
	buildLines(&b, lines)
	// No trailing newline: the cursor parks at the end of the last line, the
	// same invariant buildClear expects, matching the Output-coordinated path.
	_, _ = fmt.Fprint(mp.writer, b.String())
}

func (mp *MultiProgress) renderLoop() {
	defer close(mp.renderDone)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-mp.done:
			return
		case <-ticker.C:
			if mp.active.Load() {
				mp.render()
			}
		}
	}
}

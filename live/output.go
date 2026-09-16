package live

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

// Output coordinates log records and live widgets (ProgressBar, Spinner,
// MultiProgress) that share one terminal destination.
//
// Construct it once and pass the SAME object to the logger (via
// velocity.WithConsoleOutput) and to every widget constructor. A log write
// through Write clears the active live rows, writes the whole record, then
// redraws the live rows, so records never glue themselves onto spinner frames
// or clobber progress bars. Widgets draw through the same lock, so frames and
// final messages cannot interleave with records either.
//
// Ordinary io.Writer constructors keep working standalone; they simply do not
// coordinate, and any raw write to the underlying writer bypassing this object
// is explicitly outside the coordination guarantee.
//
// There is no global registry: each Output serialises only the widgets and
// loggers wired to it, so unrelated sinks never block each other.
type Output struct {
	dst io.Writer

	// states are the registered widgets in draw order (top of the screen
	// first). Each entry caches the last lines drawn for that widget so a
	// redraw never calls back into widget code while the lock is held.
	states []*outState
	// rows is the number of terminal rows the live area currently occupies.
	rows int
	mu   sync.Mutex

	isTerm bool
}

// outState is one widget's registration with an Output. lines is owned by the
// Output once handed over via draw; widgets replace it wholesale and never
// mutate a slice they have passed in.
type outState struct {
	lines []string
}

// NewOutput wraps w as a shared terminal-output coordinator. w must not be
// nil. Terminal detection is taken from the real destination: an *os.File is
// probed with term.IsTerminal, and any writer that knows its own capability
// can forward it by implementing interface{ IsTerminal() bool }. A wrapped
// pipe therefore stays non-terminal — the coordinator never pretends
// otherwise, and colour environment variables (NO_COLOR / FORCE_COLOR) do not
// change cursor-control capability, only colour policy, which the root
// package resolves independently.
func NewOutput(w io.Writer) *Output {
	if w == nil {
		w = io.Discard
	}
	o := &Output{dst: w}
	if detector, ok := w.(interface{ IsTerminal() bool }); ok {
		o.isTerm = detector.IsTerminal()
	} else if f, ok := w.(*os.File); ok {
		o.isTerm = term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: uintptr fd fits in int on all supported platforms
	}
	return o
}

// IsTerminal reports whether the real destination is a terminal. It is
// immutable after construction, so it is safe to read without the lock.
func (o *Output) IsTerminal() bool {
	return o.isTerm
}

// Write emits one whole (possibly multi-line) log record. When live rows are
// active on a terminal it clears them, writes the record, and redraws the
// rows in a single serialised operation. Without live rows — or on a
// non-terminal destination — it is a plain pass-through write with no cursor
// control, so non-TTY transcripts stay free of escape sequences.
func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.rows == 0 || !o.isTerm {
		return o.dst.Write(p)
	}

	o.clearRowsLocked()
	n, err := o.dst.Write(p)
	if err != nil {
		// The live area was cleared and not redrawn; drop the row count so
		// the next widget draw rebuilds the screen from scratch instead of
		// moving the cursor above rows that no longer exist.
		o.rows = 0
		return n, err
	}
	if len(p) > 0 && p[len(p)-1] != '\n' {
		// Redraws assume the cursor sits at column 0 of a fresh row. Records
		// are whole lines by contract, so terminate anything partial rather
		// than letting the first live row glue onto the record.
		if _, err = o.dst.Write([]byte{'\n'}); err != nil {
			o.rows = 0
			return n, err
		}
	}
	o.redrawLocked()
	return n, nil
}

// Flush forwards to the destination when it is buffered, so the root logger's
// fatal/close flush semantics survive being routed through the coordinator.
func (o *Output) Flush() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if f, ok := o.dst.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

// Sync forwards best-effort file synchronisation, mirroring Flush.
func (o *Output) Sync() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.dst.(interface{ Sync() error }); ok {
		return s.Sync()
	}
	return nil
}

// register adds a widget to the live area and returns its state handle.
// Called by the widget constructors in this package.
func (o *Output) register() *outState {
	st := &outState{}
	o.mu.Lock()
	o.states = append(o.states, st)
	o.mu.Unlock()
	return st
}

// draw replaces the widget's lines with lines and repaints the whole live
// area. The caller must have captured its state (built the lines) before
// calling, and must not retain or mutate lines afterwards: ownership passes
// to the Output. On a non-terminal destination this is a no-op — static
// non-TTY output never repaints.
func (o *Output) draw(st *outState, lines []string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.isTerm {
		return
	}
	if len(lines) == 0 && len(st.lines) == 0 {
		return
	}
	st.lines = lines
	o.clearRowsLocked()
	o.redrawLocked()
}

// remove takes the widget out of the live area before returning: previous
// rows are cleared, the optional final lines are written as permanent
// scrollback (one per row), and the remaining widgets are redrawn beneath.
func (o *Output) remove(st *outState, final []string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for i, s := range o.states {
		if s == st {
			o.states = append(o.states[:i], o.states[i+1:]...)
			break
		}
	}

	if !o.isTerm {
		for _, line := range final {
			_, _ = fmt.Fprintln(o.dst, line)
		}
		return
	}

	o.clearRowsLocked()
	o.rows = 0
	for _, line := range final {
		_, _ = fmt.Fprintln(o.dst, line)
	}
	o.redrawLocked()
}

// clearRowsLocked erases the live area and leaves the cursor at column 0 of
// its FIRST row, so a redraw starts exactly where the previous repaint did:
// repeated repaints must not walk the display down the screen accumulating
// blank scrollback. The caller's cursor is at the END of the last live row
// (the invariant redrawLocked leaves behind), so reaching the first live row
// means moving up rows-1, not rows. Caller holds o.mu.
func (o *Output) clearRowsLocked() {
	if o.rows <= 0 {
		return
	}
	var b strings.Builder
	writeClearSeq(&b, o.rows)
	_, _ = o.dst.Write([]byte(b.String()))
}

// writeClearSeq appends the sequence that erases rows terminal rows. The
// cursor is expected at the end of the last live row and ends at column 0 of
// the first live row: up rows-1, erase each row moving down, then back up
// rows-1 to the first row. Shared by the coordinator and the standalone
// MultiProgress clearing path so both hold the same first-row invariant.
func writeClearSeq(b *strings.Builder, rows int) {
	if rows <= 0 {
		return
	}
	if rows > 1 {
		fmt.Fprintf(b, "\x1b[%dA", rows-1)
	}
	for i := range rows {
		b.WriteString("\r\x1b[K")
		if i < rows-1 {
			b.WriteString("\n")
		}
	}
	if rows > 1 {
		fmt.Fprintf(b, "\x1b[%dA", rows-1)
	}
}

// redrawLocked paints every registered widget's cached lines and leaves the
// cursor at the end of the last live row (no trailing newline), which is the
// position clearRowsLocked expects. Caller holds o.mu.
func (o *Output) redrawLocked() {
	rows := 0
	last := ""
	for _, st := range o.states {
		for _, line := range st.lines {
			if rows > 0 {
				_, _ = fmt.Fprintln(o.dst, last)
			}
			last = line
			rows++
		}
	}
	if rows > 0 {
		_, _ = fmt.Fprint(o.dst, last)
	}
	o.rows = rows
}

package live

// Vertical-stability regression tests (finish review F3). Clearing and
// redrawing must agree on a first-row cursor invariant: repeated repaints of
// a multi-row display must not walk the display down the screen or
// accumulate blank scrollback. Assertions run against a terminal model that
// tracks the cursor row and blank-row growth — text survival alone cannot
// see this defect. All sequences are driven by explicit draw/render calls,
// so there are no sleeps and no ticker races.

import (
	"fmt"
	"strings"
	"testing"
)

// driftState summarises a transcript: cursor row, total rows and non-blank
// rows on screen.
type driftState struct {
	cursorRow int
	totalRows int
	textRows  int
}

func driftCheck(t *testing.T, transcript string) driftState {
	t.Helper()
	sc := replayCore(transcript)
	lines := screenLines(sc)
	st := driftState{cursorRow: sc.row, totalRows: len(lines)}
	for _, line := range lines {
		if line != "" {
			st.textRows++
		}
	}
	return st
}

func assertDrift(t *testing.T, transcript string, wantCursor, wantText int, context string) {
	t.Helper()
	st := driftCheck(t, transcript)
	if st.cursorRow != wantCursor {
		t.Errorf("%s: cursor at row %d, want %d; screen:\n%s\nraw: %q", context, st.cursorRow, wantCursor, strings.Join(screenLines(replayCore(transcript)), "\n"), transcript)
	}
	if st.textRows != wantText {
		t.Errorf("%s: %d non-blank rows on screen, want %d; screen:\n%s\nraw: %q", context, st.textRows, wantText, strings.Join(screenLines(replayCore(transcript)), "\n"), transcript)
	}
}

// Promotion of the independent review's reproduction: a two-row display
// redrawn repeatedly must hold its vertical position.
func TestLiveNoDrift_TwoRowDisplayRepeatedRedraws(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	o := NewOutput(ft)
	a, b := o.register(), o.register()
	o.draw(a, []string{"first"})
	o.draw(b, []string{"second"})

	assertDrift(t, ft.String(), 1, 2, "two rows established")

	for i := range 6 {
		o.draw(a, []string{fmt.Sprintf("first-%d", i)})
		assertDrift(t, ft.String(), 1, 2, fmt.Sprintf("repaint %d", i+1))
	}
}

// A four-row display (drift of rows-1 = 3 per repaint before the fix), then
// grow and shrink transitions, all holding exact positions with no blank-row
// growth.
func TestLiveNoDrift_GrowShrinkMatrix(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	o := NewOutput(ft)
	a, b := o.register(), o.register()

	// Four rows: cursor at row 3.
	o.draw(a, []string{"alpha"})
	o.draw(b, []string{"beta-1", "beta-2", "beta-3"})
	assertDrift(t, ft.String(), 3, 4, "four rows established")

	for i := range 5 {
		o.draw(a, []string{fmt.Sprintf("alpha-%d", i)})
		assertDrift(t, ft.String(), 3, 4, fmt.Sprintf("four-row repaint %d", i+1))
	}

	// Grow to five rows: the new row extends the display; no scrollback
	// appears above and nothing moves down.
	o.draw(b, []string{"beta-1", "beta-2", "beta-3", "beta-4"})
	assertDrift(t, ft.String(), 4, 5, "grown to five rows")
	o.draw(a, []string{"alpha-x"})
	assertDrift(t, ft.String(), 4, 5, "five-row repaint")

	// Shrink back to two rows: the vacated rows are erased, cursor rises.
	o.draw(b, []string{"beta-solo"})
	assertDrift(t, ft.String(), 1, 2, "shrunk to two rows")
	for i := range 3 {
		o.draw(a, []string{fmt.Sprintf("alpha-%d", i)})
		assertDrift(t, ft.String(), 1, 2, fmt.Sprintf("shrunk repaint %d", i+1))
	}
}

// A log record between repaints scrolls the live area down by exactly the
// record's own height — once — and subsequent repaints hold the new
// position instead of continuing to sink.
func TestLiveNoDrift_LogInsertBetweenRepaints(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	o := NewOutput(ft)
	a, b := o.register(), o.register()
	o.draw(a, []string{"alpha"})
	o.draw(b, []string{"beta"})
	assertDrift(t, ft.String(), 1, 2, "two rows established")

	_, _ = o.Write([]byte("log record one\n"))
	assertDrift(t, ft.String(), 2, 3, "one-line log inserted")

	for i := range 4 {
		o.draw(a, []string{fmt.Sprintf("alpha-%d", i)})
		assertDrift(t, ft.String(), 2, 3, fmt.Sprintf("repaint after log %d", i+1))
	}

	// Multi-line record: area sinks by its height, exactly once.
	_, _ = o.Write([]byte("note one\nnote two\n"))
	assertDrift(t, ft.String(), 4, 5, "two-line log inserted")
	o.draw(b, []string{"beta-2"})
	assertDrift(t, ft.String(), 4, 5, "repaint after multi-line log")
}

// Removing a widget writes its final line as scrollback at the position the
// live area held; the remaining widget stays put and later repaints do not
// drift.
func TestLiveNoDrift_WidgetRemoval(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	o := NewOutput(ft)
	a, b := o.register(), o.register()
	o.draw(a, []string{"alpha"})
	o.draw(b, []string{"beta-1", "beta-2"})
	assertDrift(t, ft.String(), 2, 3, "three rows established")

	// The final line becomes scrollback at the block's top row; the remaining
	// widget moves up into the freed space instead of the block leaving a
	// hole, and repaints hold that position.
	o.remove(b, []string{"beta-final"})
	assertDrift(t, ft.String(), 1, 2, "widget removed with final line")

	for i := range 3 {
		o.draw(a, []string{fmt.Sprintf("alpha-%d", i)})
		assertDrift(t, ft.String(), 1, 2, fmt.Sprintf("repaint after removal %d", i+1))
	}

	// Removing the last widget erases its row and leaves the cursor there;
	// the scrollback written before it survives.
	o.remove(a, nil)
	assertDrift(t, ft.String(), 1, 1, "last widget removed")
}

// The standalone MultiProgress path (its own clearing helper) holds the same
// invariant across repeated renders, grow and shrink. Constructed without a
// ticker goroutine so render sequencing is fully deterministic.
func TestMultiProgressNoDrift_Standalone(t *testing.T) {
	t.Parallel()

	ft := &syncTerminal{}
	mp := &MultiProgress{writer: ft, done: make(chan struct{}), finDone: make(chan struct{}), isTTY: true}
	mp.active.Store(true)

	mp.Add(testItem{"row-one"})
	mp.Add(testItem{"row-two"})
	mp.render()
	assertDrift(t, ft.String(), 1, 2, "standalone two rows established")

	for i := range 5 {
		mp.mu.Lock()
		mp.items[0] = testItem{fmt.Sprintf("row-one-%d", i)}
		mp.mu.Unlock()
		mp.render()
		assertDrift(t, ft.String(), 1, 2, fmt.Sprintf("standalone repaint %d", i+1))
	}

	// Grow to three rows.
	mp.Add(testItem{"row-three"})
	mp.render()
	assertDrift(t, ft.String(), 2, 3, "standalone grown to three rows")
	mp.render()
	assertDrift(t, ft.String(), 2, 3, "standalone three-row repaint")

	// Shrink to one row: vacated rows erased, cursor rises.
	mp.Remove(testItem{"row-two"})
	mp.Remove(testItem{"row-three"})
	mp.render()
	assertDrift(t, ft.String(), 0, 1, "standalone shrunk to one row")
	mp.render()
	assertDrift(t, ft.String(), 0, 1, "standalone shrunk repaint")

	// Stop erases the block and parks the cursor one row below it: with one
	// drawn row the cursor lands on row 1, and the screen keeps exactly the
	// scrollback that existed before.
	mp.Stop()
	assertDrift(t, ft.String(), 1, 0, "standalone stop parks below erased block")
}

// Package live provides stateful terminal UI primitives: progress bars, spinners,
// and multi-progress displays. These types own goroutines and have explicit lifecycle
// (Stop/Complete), which is why they live apart from the static Renderables in the
// root package.
//
// # Shared output coordination
//
// NewOutput wraps a writer as a shared coordinator for one terminal. Pass the
// SAME *Output to velocity.WithConsoleOutput and to the widget constructors: a
// log record then clears the active live rows, writes the whole record, and
// redraws the live rows in one serialised operation, so records never glue
// themselves onto spinner frames or clobber progress bars. Ordinary io.Writer
// constructors keep working standalone; they simply do not coordinate, and raw
// writes that bypass the Output are outside the coordination guarantee. There
// is no global registry; each Output serialises only what was wired to it.
//
// # TTY awareness
//
// Cursor-control capability is taken from the real destination: an *os.File is
// probed directly, and a writer that knows its own capability can forward it by
// implementing interface{ IsTerminal() bool }. Colour environment variables do
// not change this: FORCE_COLOR can style a non-terminal but never grants
// cursor movement, and NO_COLOR alone does not stop cursor movement on a real
// terminal (it is a colour convention, not a trust or cursor policy).
//
// When the destination is not a terminal (piped output, redirected stdout, CI
// runners):
//   - ProgressBar suppresses per-tick renders; Complete() emits a single summary line.
//   - Spinner suppresses per-frame renders; Stop/StopWithMessage still print their message.
//   - MultiProgress suppresses all renders; Stop() is a no-op for display cleanup.
//
// This prevents \r, \033[K, and cursor movement sequences from appearing in log files
// or aggregated output streams.
package live

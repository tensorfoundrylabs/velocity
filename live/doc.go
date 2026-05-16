// Package live provides stateful terminal UI primitives: progress bars, spinners,
// and multi-progress displays. These types own goroutines and have explicit lifecycle
// (Stop/Complete), which is why they live apart from the static Renderables in the
// root package.
//
// # TTY awareness
//
// All types detect whether their writer is a real terminal at construction time.
// When the writer is not a terminal (piped output, redirected stdout, CI runners):
//   - ProgressBar suppresses per-tick renders; Complete() emits a single summary line.
//   - Spinner suppresses per-frame renders; Stop/StopWithMessage still print their message.
//   - MultiProgress suppresses all renders; Stop() is a no-op for display cleanup.
//
// This prevents \r, \033[K, and cursor movement sequences from appearing in log files
// or aggregated output streams.
package live

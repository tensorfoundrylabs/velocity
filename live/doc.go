// Package live provides stateful terminal UI primitives: progress bars, spinners,
// and multi-progress displays. These types own goroutines and have explicit lifecycle
// (Stop/Complete), which is why they live apart from the static Renderables in the
// root package.
package live

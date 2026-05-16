// Package velocity is a high-performance structured logging library for Go CLI applications.
//
// Velocity provides rich terminal output with themed colours, structured fields,
// tables, status items, group blocks, continuation output, hyperlinks, and JSON —
// all with zero-allocation field encoding on hot paths.
//
// Quick start:
//
//	log := velocity.New(velocity.WithDevelopment())
//	log.Info("server started", velocity.String("addr", ":8080"))
//
// Preset options cover the common scenarios:
//
//	log := velocity.New(velocity.WithDevelopment())   // coloured console, debug level
//	log := velocity.New(velocity.WithProduction())    // JSON to stderr, info level
//	log := velocity.New(velocity.WithContainer())     // JSON to stdout, info level
//	log := velocity.New(velocity.WithTesting(t))      // writes via t.Log; cleans up on exit
//	log := velocity.New(velocity.WithNop())           // discards all output
//
// Renderables (Box, Table, Tree, Banner, KeyValue, SystemInfo) live in the root
// package and are rendered via Logger convenience methods or the standalone Pretty facade.
// Stateful animated types (Spinner, ProgressBar) live in velocity/live.
// The slog bridge lives in velocity/slogbridge.
package velocity

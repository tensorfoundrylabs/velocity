// Package pretty provides styled terminal output utilities for CLI applications.
// Boxes, panels, banners, tables, trees, progress bars, and spinners.
//
// When a velocity.Logger is available, use NewFromLogger to route output through
// the logger's console writer — this keeps pretty output serialised with log lines
// and indented to align with the message column.
package pretty

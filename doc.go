// Package velocity is a high-performance structured logging library for Go CLI applications.
//
// Velocity provides rich terminal output with themed colours, structured fields,
// tree displays, tables, progress indicators, and JSON output — all with
// zero-allocation field encoding on hot paths.
//
// Quick start:
//
//	log := velocity.New(os.Stdout)
//	log.Info("server started", velocity.String("addr", ":8080"))
//
// For more control, use the builder or functional options:
//
//	log := velocity.NewWithOptions(
//	    velocity.WithLevel(velocity.LevelDebug),
//	    velocity.WithTheme(velocity.ThemeDracula),
//	)
//
// Velocity includes five presets for common scenarios:
//
//   - PresetDevelopment: verbose, coloured console output
//   - PresetProduction: structured JSON, info level and above
//   - PresetContainer: JSON with container-friendly defaults
//   - PresetTesting: minimal output for test harnesses
//   - PresetHighPerformance: sampling and ring-buffer batching
//
// Rich terminal output from velocity/pretty can be mixed with log lines via
// the Renderable interface. Logger.Render writes indented output aligned with
// the message column; Logger.RenderRaw writes flush-left. Logger.Newline inserts
// a blank line under the same mutex as log calls to prevent interleaving.
package velocity

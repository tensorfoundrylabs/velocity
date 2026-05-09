// continuation demonstrates Logger.Continue for multi-line output anchored to
// a single structured log entry.
//
// The canonical use case is a server startup block where the listening address,
// dashboard URL, and stop instruction should all be grouped under one timestamped
// INFO entry rather than scattered across three separate log lines.
//
// Run directly for a TTY console with coloured │ glyph:
//
//	go run ./examples/continuation
//
// Pipe to see the non-TTY plain form (same glyph, no colour):
//
//	go run ./examples/continuation | cat
//
// Add -json to write structured output alongside the console output:
//
//	go run ./examples/continuation -json
package main

import (
	"flag"
	"os"
	"time"

	velocity "github.com/tensorfoundrylabs/velocity"
)

func main() {
	jsonOut := flag.Bool("json", false, "also write JSON to continuation.log")
	flag.Parse()

	opts := []velocity.Option{
		velocity.WithDevelopment(),
	}
	if *jsonOut {
		f, err := os.Create("continuation.log")
		if err != nil {
			panic(err)
		}
		defer func() {
			if err := f.Close(); err != nil {
				panic(err)
			}
		}()
		opts = append(opts,
			velocity.WithStructuredOutput(f),
			velocity.WithStructuredLevel(velocity.LevelDebug),
		)
	}

	log := velocity.New(opts...)
	defer func() {
		if err := log.Close(); err != nil {
			panic(err)
		}
	}()

	// --- Server startup block ---
	// The primary INFO line records the event in the structured pipeline.
	// Continuation lines carry the human-readable context (URL, keybind) without
	// polluting the structured log with ad-hoc fields.
	log.Continue(velocity.LevelInfo, "HTTP server listening",
		"Available at: "+velocity.Hyperlink("http://localhost:8080", "http://localhost:8080"),
		"Metrics:      "+velocity.Hyperlink("http://localhost:9090/metrics", "http://localhost:9090/metrics"),
		"Press Ctrl+C to stop",
	)

	log.Newline()

	// --- Error context block ---
	// Failed operations often benefit from inline context: the query that failed,
	// the connection details, and a suggested action — all tied to one ERROR entry.
	log.Continue(velocity.LevelError, "Database query failed",
		"Query:      SELECT * FROM users WHERE active = true",
		"Connection: db.internal:5432",
		"Try:        check DB connectivity with `pg_isready -h db.internal`",
	)

	log.Newline()

	// --- MOTD-style startup block ---
	// A service that displays its version and environment at launch. Continuation
	// lets this be a proper log entry (timestamped, levelled) rather than a
	// fmt.Println block that bypasses the structured pipeline.
	buildTime := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	log.Continue(velocity.LevelInfo, "Velocity example service starting",
		"Version:    v2.0.0-dev",
		"Built:      "+buildTime.Format(time.RFC3339),
		"Go:         1.24.3",
		"Environment: development",
	)

	log.Newline()

	// --- Single continuation line ---
	// Works with just one additional line; no special case needed by the caller.
	log.Continue(velocity.LevelWarn, "Rate limit approaching",
		"Current: 950/1000 requests per minute",
	)

	log.Newline()

	// --- No continuation lines ---
	// When called with no lines, Continue behaves like a normal log call.
	// Useful when continuation lines are conditionally computed.
	log.Continue(velocity.LevelDebug, "Cache warm-up complete")
}

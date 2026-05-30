// component-logging demonstrates the opt-in inline-indicator feature using a
// trimmed simulation of FoundryOS's Fleet/Scout/Relay service topology.
//
// Three services run concurrently in production; here they are interleaved in a
// deterministic sequence that exercises every indicator type:
//
//   - Component bar:  coloured name + muted │ prefix on every line
//   - Count:          (N) after the message  (field "count")
//   - Timing:         [⏱ Xs] suffix          (fields "startup_ms", "startup_time", "stop_ms", "shutdown_ms")
//   - State arrows:   from → to              (fields "old_state"/"new_state", "prev_state"/"next_state")
//
// Run directly for a TTY console with colour:
//
//	go run ./examples/component-logging
//
// Set FORCE_COLOR=1 on Windows terminals that proxy stdout through a pipe:
//
//	FORCE_COLOR=1 go run ./examples/component-logging
//
// The second section re-emits a subset of the same entries through a plain JSON
// writer to demonstrate the JSON-parity guarantee: every promoted field that was
// folded into the pretty header still appears fully expanded in the JSON record.
package main

import (
	"bytes"
	"os"

	velocity "github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	// --- Pretty console logger (the FoundryOS operator view) ---
	//
	// WithComponentStyling enables the full indicator set with sensible defaults:
	// component field "component", count field "count", state pairs old/new and
	// prev/next. Timing fields are explicitly listed because their names are
	// application-specific.
	// WithFieldDisplayMode(FieldDisplayTree) puts remaining fields under a │-tree,
	// matching how FoundryOS actually renders its multiplexed service stream.
	console := velocity.New(
		velocity.WithDevelopment(),
		velocity.WithComponentStyling(),
		// velocity.WithComponentColumnWidth(5),
		velocity.WithTimingFields("startup_ms", "startup_time", "stop_ms", "shutdown_ms"),
		velocity.WithFieldDisplayMode(velocity.FieldDisplayTree),
		velocity.WithInlineGlyphs(true),
	)
	defer func() {
		if err := console.Close(); err != nil {
			panic(err)
		}
	}()

	// One child logger per service. WithComponent stamps every entry with
	// component="<name>" so the indicator picks it up automatically.
	fleet := console.WithComponent("Fleet")
	scout := console.WithComponent("Scout")
	relay := console.WithComponent("Relay")

	// --- Startup sequence ---

	scout.Info("service started",
		velocity.Int("startup_ms", 2000),
		velocity.String("name", "discovery"),
		velocity.Int("phase", 0),
	)

	fleet.Info("service started",
		velocity.Duration("startup_time", 1500*1000*1000), // 1.5 s as nanoseconds
		velocity.String("mode", "primary"),
	)

	relay.Info("service started",
		velocity.Int("startup_ms", 800),
		velocity.String("listen", ":7400"),
	)

	relay.Info("registered upstream peers",
		velocity.Int("count", 3),
		velocity.String("region", "ap-southeast-2"),
	)

	fleet.Info("source connected",
		velocity.String("old_state", "disconnected"),
		velocity.String("new_state", "connected"),
		velocity.String("source_id", "relay-CODY-RYZEN"),
	)

	scout.Info("discovery complete",
		velocity.Int("count", 12),
		velocity.Int("startup_ms", 3200),
	)

	// --- Steady-state ---

	fleet.Info("health check passed",
		velocity.String("prev_state", "degraded"),
		velocity.String("next_state", "healthy"),
	)

	relay.Info("stopping services",
		velocity.Int("count", 4),
	)

	scout.Warn("circuit breaker opened",
		velocity.String("old_state", "closed"),
		velocity.String("new_state", "open"),
		velocity.String("endpoint", "192.168.0.181:8010"),
	)

	fleet.Info("Source state transition",
		velocity.String("prev_state", "connected"),
		velocity.String("next_state", "stale"),
		velocity.String("source_id", "relay-CODY-RYZEN"),
	)

	// --- Shutdown sequence ---

	scout.Info("stopping",
		velocity.Int("stop_ms", 120),
	)

	relay.Info("flushed pending messages",
		velocity.Int("count", 7),
		velocity.Int("stop_ms", 45),
	)

	fleet.Info("Fleet has shutdown",
		velocity.Int("shutdown_ms", 310),
	)

	// --- JSON parity demonstration ---
	//
	// Re-emit a representative subset through a JSON-only logger to prove parity:
	// every field that was promoted to the pretty header (component, count, timing,
	// state arrows) still appears fully expanded in the JSON record. The pretty
	// console writer folds them into compact indicators; the JSON writer ignores
	// the indicator config entirely and emits them as plain fields.

	console.Newline()
	console.Info("--- JSON parity (same entries, structured writer) ---")
	console.Newline()

	var jsonBuf bytes.Buffer
	jsonLog := velocity.New(
		velocity.WithProduction(),
		velocity.WithStructuredOutput(&jsonBuf),
		velocity.WithStructuredLevel(velocity.LevelDebug),
		// Intentionally no WithComponentStyling — JSON is untouched regardless.
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithLevel(velocity.LevelOff),
	)
	defer func() {
		if err := jsonLog.Close(); err != nil {
			panic(err)
		}
	}()

	jFleet := jsonLog.WithComponent("Fleet")
	jScout := jsonLog.WithComponent("Scout")

	jScout.Info("service started",
		velocity.Int("startup_ms", 2000),
		velocity.String("name", "discovery"),
		velocity.Int("phase", 0),
	)

	jFleet.Info("source connected",
		velocity.String("old_state", "disconnected"),
		velocity.String("new_state", "connected"),
		velocity.String("source_id", "relay-CODY-RYZEN"),
	)

	jScout.Warn("circuit breaker opened",
		velocity.String("old_state", "closed"),
		velocity.String("new_state", "open"),
		velocity.String("endpoint", "192.168.0.181:8010"),
	)

	// Flush the JSON buffer to stdout so it is visible after the pretty section.
	if _, err := os.Stdout.Write(jsonBuf.Bytes()); err != nil {
		panic(err)
	}
}

// Notify channel example. Demonstrates the ephemeral operator output pattern
// used by alloy for onboarding URLs — messages that must reach the operator
// regardless of log level, writer configuration, or sampling settings.
//
// Notify bypasses the structured pipeline entirely: no level check, no sampler,
// no JSON writer, no MultiWriter fan-out. The console writer mutex is shared
// so log lines and Notify output cannot interleave on a shared terminal.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	log := velocity.New(
		velocity.WithDevelopment(),
		velocity.WithConsoleOutput(os.Stdout),
	)
	defer func() { _ = log.Close() }()

	// Simulate a bootstrap URL that the operator must open to complete setup.
	// In alloy this came from 5+ raw fmt.Fprintf(os.Stderr, ...) calls scattered
	// across server.go. NotifyBox collapses that into one intentional call site.
	token := "abc123xyz"
	setupURL := "https://example.tensorfoundry.io/setup?token=" + token

	// NotifyBox renders to stderr (default) with a visible border so the URL
	// stands out even when the terminal is flooded with log output.
	log.NotifyBox(velocity.NewBox(
		"Setup not complete",
		fmt.Sprintf("Open this URL to finish configuring your instance:\n\n  %s\n\nThe URL expires in 15 minutes.", setupURL),
		velocity.ThemeNightOwl,
	))

	// Regular structured log — goes through the normal pipeline (console stdout
	// in development mode), not to the notify destination.
	log.Info("server starting", velocity.String("version", "2.0.0"))

	// Simulate some work.
	time.Sleep(10 * time.Millisecond)
	log.Info("listening", velocity.String("addr", ":8080"))

	// NotifyLines is the lighter form — useful for simple multi-line operator
	// messages without the bordered box treatment.
	log.NotifyLines(
		"",
		"  Reminder: setup URL expires soon.",
		"  "+setupURL,
		"",
	)

	// Notify with format string — the most direct form for a single line.
	log.Notify("\n  Setup complete? Run: tensorfoundry validate --token %s\n\n", token)

	log.Info("example complete")
}

// Package main demonstrates dual-output logging: coloured console for humans
// and newline-delimited JSON for log aggregators. This is the typical setup
// for a production service that needs both readable local output and
// machine-parseable logs for tools like Loki or Elasticsearch.
package main

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity"
)

func main() {
	// Write JSON to a temp file so we can read it back and show the structure.
	jsonFile, err := os.CreateTemp("", "velocity-*.jsonl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp file: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.Remove(jsonFile.Name()) }()
	defer func() { _ = jsonFile.Close() }()

	// Build a logger with console output to stdout and JSON to the temp file.
	// WithCaller adds the source file and line number to every JSON entry,
	// which is invaluable when tailing logs in production.
	log := velocity.NewWithOptions(
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithLevel(velocity.LevelDebug),
		velocity.WithStructuredOutput(jsonFile),
		velocity.WithStructuredLevel(velocity.LevelInfo),
		velocity.WithCaller(true),
		velocity.WithTimeFormat(time.RFC3339),
	)

	fmt.Println("=== Console output (what an operator sees) ===")
	fmt.Println()

	// These entries go to both the console (coloured) and the JSON file.
	// Debug is suppressed in JSON because StructuredLevel is Info.
	log.Debug("debug not written to JSON", velocity.String("note", "filtered by structured level"))

	log.Info("application started",
		velocity.String("version", "1.3.0"),
		velocity.String("env", "production"),
	)

	log.Warn("configuration value missing, using default",
		velocity.String("key", "max_connections"),
		velocity.Int("default", 100),
	)

	log.Error("upstream service degraded",
		velocity.String("service", "payments-api"),
		velocity.Int("error_rate_pct", 12),
		velocity.Duration("p99_latency", 3200*time.Millisecond),
	)

	log.Info("request complete",
		velocity.String("method", "POST"),
		velocity.String("path", "/api/orders"),
		velocity.Int("status", 201),
		velocity.Float64("duration_ms", 45.7),
		velocity.Bool("cached", false),
	)

	// Make sure everything is flushed before we read the file back.
	_ = jsonFile.Sync()

	fmt.Println()
	fmt.Println("=== JSON output (what the log aggregator sees) ===")
	fmt.Println()

	// Seek back to the start so we can read what was written.
	if _, err = jsonFile.Seek(0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "seek failed: %v\n", err)
		return
	}

	scanner := bufio.NewScanner(jsonFile)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
	}
}

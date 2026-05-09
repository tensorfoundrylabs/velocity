// Package main demonstrates velocity's table rendering for structured
// terminal output. Tables auto-size columns and handle ANSI colour codes
// in cell content (e.g. from StatusFormatter) without breaking alignment.
package main

import (
	"fmt"
	"os"

	"github.com/tensorfoundrylabs/velocity"
)

func main() {
	log := velocity.NewWithOptions(
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithTheme(velocity.ThemeNightOwl),
		velocity.WithLevel(velocity.LevelDebug),
	)

	sf := log.Status()
	theme := velocity.ThemeNightOwl

	fmt.Println("=== Pretty Table ===")
	fmt.Println()
	log.Info("service health check results")
	log.RenderRaw(velocity.NewTable(
		[]string{"Service", "Status", "Latency", "Region"},
		[][]string{
			{"auth-api", sf.Okay("HEALTHY"), "12ms", "us-east-1"},
			{"payments", sf.Okay("HEALTHY"), "45ms", "us-east-1"},
			{"search", sf.Warn("DEGRADED"), "380ms", "eu-west-1"},
			{"notifications", sf.Fail("DOWN"), "-", "ap-southeast-2"},
			{"analytics", sf.Okay("HEALTHY"), "28ms", "us-west-2"},
		},
		theme,
	))
	log.Newline()

	fmt.Println("=== GPU Node Table ===")
	fmt.Println()
	log.RenderRaw(velocity.NewTable(
		[]string{"Node", "GPU", "Memory", "Utilisation", "Temperature"},
		[][]string{
			{"node-0", "A100 80GB", "72.3 / 80.0 GB", sf.Okay("89%"), "68C"},
			{"node-1", "A100 80GB", "65.1 / 80.0 GB", sf.Okay("81%"), "65C"},
			{"node-2", "A100 80GB", "78.9 / 80.0 GB", sf.Warn("98%"), "82C"},
			{"node-3", "A100 80GB", "0.0 / 80.0 GB", sf.Fail("0%"), "34C"},
		},
		theme,
	))
	log.Newline()

	// Tables work without colour too. velocity.NewPretty(os.Stdout, nil) demonstrates
	// the standalone constructor without a logger.
	fmt.Println("=== Plain Table (no theme, no colour) ===")
	fmt.Println()
	log.RenderRaw(velocity.NewTable(
		[]string{"Endpoint", "Method", "Calls/sec", "P99"},
		[][]string{
			{"/v1/chat/completions", "POST", "1,240", "89ms"},
			{"/v1/embeddings", "POST", "3,800", "12ms"},
			{"/v1/models", "GET", "450", "3ms"},
			{"/health", "GET", "10,000", "1ms"},
		},
		nil,
	))
	log.Newline()

	// Wide table with many columns. Columns auto-size to content.
	fmt.Println("=== Wide Table (auto-sized columns) ===")
	fmt.Println()
	log.RenderRaw(velocity.NewTable(
		[]string{"PID", "User", "CPU%", "Mem%", "VSZ", "RSS", "TTY", "Stat", "Command"},
		[][]string{
			{"1", "root", "0.0", "0.1", "168k", "12k", "?", "Ss", "/sbin/init"},
			{"842", "vllm", "98.2", "45.3", "48G", "36G", "?", "Sl", "python -m vllm.entrypoints.api_server"},
			{"1204", "nginx", "0.3", "0.2", "32M", "8M", "?", "S", "nginx: worker process"},
			{"1891", "prometheus", "1.2", "0.8", "256M", "64M", "?", "Sl", "/usr/bin/prometheus"},
		},
		theme,
	))
	log.Newline()

	// Use log.Render to nest a small table under a related log line. The table
	// indents to the message column, visually grouping with the entry above.
	// Best for narrow tables; wide tables still want RenderRaw to avoid wrapping.
	fmt.Println("=== Indented Table (under a log line via log.Render) ===")
	fmt.Println()
	log.Info("migrations applied", velocity.Int("count", 3))
	log.Render(velocity.NewTable(
		[]string{"Migration", "Duration", "Status"},
		[][]string{
			{"001_initial_schema.sql", "5ms", sf.Okay("OK")},
			{"002_webhooks.sql", "2ms", sf.Okay("OK")},
			{"003_model_access.sql", "3ms", sf.Okay("OK")},
		},
		theme,
	))
}

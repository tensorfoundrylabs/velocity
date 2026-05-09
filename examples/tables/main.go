// Package main demonstrates velocity's table rendering for structured
// terminal output. Tables auto-size columns and handle ANSI colour codes
// in cell content without breaking alignment.
package main

import (
	"fmt"
	"os"

	"github.com/tensorfoundrylabs/velocity"
)

func main() {
	log := velocity.New(
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithTheme(velocity.ThemeNightOwl),
		velocity.WithLevel(velocity.LevelDebug),
	)

	// log.Style() returns the active theme. Use its colour fields directly
	// to produce ANSI-coloured cell content. Phase 2 will add Theme.Format(slot, s)
	// as a cleaner API for this pattern.
	style := log.Style()
	ok := func(s string) string { return style.StatusOKColour.ANSI(true) + s + velocity.Reset }
	warn := func(s string) string { return style.StatusWarnColour.ANSI(true) + s + velocity.Reset }
	fail := func(s string) string { return style.StatusFailColour.ANSI(true) + s + velocity.Reset }
	theme := velocity.ThemeNightOwl

	fmt.Println("=== Pretty Table ===")
	fmt.Println()
	log.Info("service health check results")
	log.RenderRaw(velocity.NewTable(
		[]string{"Service", "Status", "Latency", "Region"},
		[][]string{
			{"auth-api", ok("HEALTHY"), "12ms", "us-east-1"},
			{"payments", ok("HEALTHY"), "45ms", "us-east-1"},
			{"search", warn("DEGRADED"), "380ms", "eu-west-1"},
			{"notifications", fail("DOWN"), "-", "ap-southeast-2"},
			{"analytics", ok("HEALTHY"), "28ms", "us-west-2"},
		},
		theme,
	))
	log.Newline()

	fmt.Println("=== GPU Node Table ===")
	fmt.Println()
	log.RenderRaw(velocity.NewTable(
		[]string{"Node", "GPU", "Memory", "Utilisation", "Temperature"},
		[][]string{
			{"node-0", "A100 80GB", "72.3 / 80.0 GB", ok("89%"), "68C"},
			{"node-1", "A100 80GB", "65.1 / 80.0 GB", ok("81%"), "65C"},
			{"node-2", "A100 80GB", "78.9 / 80.0 GB", warn("98%"), "82C"},
			{"node-3", "A100 80GB", "0.0 / 80.0 GB", fail("0%"), "34C"},
		},
		theme,
	))
	log.Newline()

	// Tables work without colour too.
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

	// log.Table is the convenience form: it calls log.Style() for the theme and
	// routes through Logger.Render, so the table indents to the message column.
	// Equivalent to log.Render(velocity.NewTable(..., log.Style())) but shorter.
	fmt.Println("=== Indented Table (under a log line via log.Table) ===")
	fmt.Println()
	log.Info("migrations applied", velocity.Int("count", 3))
	log.Table(
		[]string{"Migration", "Duration", "Status"},
		[][]string{
			{"001_initial_schema.sql", "5ms", ok("OK")},
			{"002_webhooks.sql", "2ms", ok("OK")},
			{"003_model_access.sql", "3ms", ok("OK")},
		},
	)
}

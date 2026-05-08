// Package main demonstrates velocity's table rendering for structured
// terminal output. Tables auto-size columns and handle ANSI colour codes
// in cell content (e.g. from StatusFormatter) without breaking alignment.
package main

import (
	"os"

	"github.com/tensorfoundrylabs/velocity"
	"github.com/tensorfoundrylabs/velocity/pretty"
)

func main() {
	log := velocity.NewWithOptions(
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithTheme(velocity.ThemeNightOwl),
		velocity.WithLevel(velocity.LevelDebug),
	)

	sf := log.Status()
	p := pretty.NewFromLogger(log)

	log.Info("=== Pretty Table ===")
	log.Newline()
	log.Info("service health check results")
	log.Render(p.NewTable(
		[]string{"Service", "Status", "Latency", "Region"},
		[][]string{
			{"auth-api", sf.Okay("HEALTHY"), "12ms", "us-east-1"},
			{"payments", sf.Okay("HEALTHY"), "45ms", "us-east-1"},
			{"search", sf.Warn("DEGRADED"), "380ms", "eu-west-1"},
			{"notifications", sf.Fail("DOWN"), "-", "ap-southeast-2"},
			{"analytics", sf.Okay("HEALTHY"), "28ms", "us-west-2"},
		},
	))
	log.Newline()

	log.Info("=== GPU Node Table ===")
	log.Newline()
	log.Render(p.NewTable(
		[]string{"Node", "GPU", "Memory", "Utilisation", "Temperature"},
		[][]string{
			{"node-0", "A100 80GB", "72.3 / 80.0 GB", sf.Okay("89%"), "68C"},
			{"node-1", "A100 80GB", "65.1 / 80.0 GB", sf.Okay("81%"), "65C"},
			{"node-2", "A100 80GB", "78.9 / 80.0 GB", sf.Warn("98%"), "82C"},
			{"node-3", "A100 80GB", "0.0 / 80.0 GB", sf.Fail("0%"), "34C"},
		},
	))
	log.Newline()

	// Tables work without colour too. pretty.New(os.Stdout, nil) demonstrates
	// the standalone constructor without a logger or theme.
	log.Info("=== Plain Table (no theme, no colour) ===")
	log.Newline()
	plain := pretty.New(os.Stdout, nil)
	plain.Table(
		[]string{"Endpoint", "Method", "Calls/sec", "P99"},
		[][]string{
			{"/v1/chat/completions", "POST", "1,240", "89ms"},
			{"/v1/embeddings", "POST", "3,800", "12ms"},
			{"/v1/models", "GET", "450", "3ms"},
			{"/health", "GET", "10,000", "1ms"},
		},
	)
	log.Newline()

	// Wide table with many columns. Columns auto-size to content.
	log.Info("=== Wide Table (auto-sized columns) ===")
	log.Newline()
	log.Render(p.NewTable(
		[]string{"PID", "User", "CPU%", "Mem%", "VSZ", "RSS", "TTY", "Stat", "Command"},
		[][]string{
			{"1", "root", "0.0", "0.1", "168k", "12k", "?", "Ss", "/sbin/init"},
			{"842", "vllm", "98.2", "45.3", "48G", "36G", "?", "Sl", "python -m vllm.entrypoints.api_server"},
			{"1204", "nginx", "0.3", "0.2", "32M", "8M", "?", "S", "nginx: worker process"},
			{"1891", "prometheus", "1.2", "0.8", "256M", "64M", "?", "Sl", "/usr/bin/prometheus"},
		},
	))
}

// Package main demonstrates velocity's pretty-printing utilities for building
// rich CLI tool output. Think deployment scripts, migration runners, or any
// tool where you want structure and colour rather than a wall of log lines.
package main

import (
	"fmt"
	"os"

	"github.com/tensorfoundrylabs/velocity"
)

func main() {
	log := velocity.New(
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithLevel(velocity.LevelDebug),
		velocity.WithTheme(velocity.ThemeNightOwl),
	)

	p := velocity.NewPretty(os.Stdout, velocity.ThemeNightOwl)

	// Banner shows the tool name using the double-border box built into the logger.
	// Great for the splash screen at startup.
	ascii := []string{
		"  ____             _              ",
		" |  _ \\  ___ _ __ | | ___  _   _ ",
		" | | | |/ _ \\ '_ \\| |/ _ \\| | | |",
		" | |_| |  __/ |_) | | (_) | |_| |",
		" |____/ \\___| .__/|_|\\___/ \\__, |",
		"            |_|            |___/ ",
	}
	banner := velocity.CreateBanner("Deploy", "4.2.0", "https://deploy.example.com", ascii)
	fmt.Print(banner)

	// Section headers make it easy to scan a long run's output.
	p.Section("Pre-flight Checks")

	// Theme.Format(slot, s) colours cell content using semantic slots.
	// The theme handles all ANSI construction; call sites stay readable.
	style := log.Style()
	statusOK := style.Format(velocity.SlotStatusOK, "OK")
	statusWarn := style.Format(velocity.SlotStatusWarn, "WARN (non-prod)")
	statusFail := style.Format(velocity.SlotStatusFail, "FAIL")
	fmt.Printf("  %-30s %s\n", "Docker daemon reachable:", statusOK)
	fmt.Printf("  %-30s %s\n", "Registry credentials:", statusOK)
	fmt.Printf("  %-30s %s\n", "Kubernetes context:", statusWarn)
	fmt.Printf("  %-30s %s\n", "Staging namespace exists:", statusOK)
	fmt.Printf("  %-30s %s\n", "Production namespace:", statusFail)

	p.Section("Environment Info")

	// SystemInfo is a compact block for key-value pairs under a title.
	// Perfect for printing build metadata or runtime configuration at startup.
	p.SystemInfo(&velocity.SystemInfoData{
		Title:   "Deploy Tool",
		Version: "4.2.0",
		Fields: []velocity.KeyValuePair{
			{Key: "Target cluster", Value: "k8s-staging-au-east-1"},
			{Key: "Namespace", Value: "app-staging"},
			{Key: "Image", Value: "registry.example.com/app:v4.2.0"},
			{Key: "Replicas", Value: "3"},
			{Key: "Strategy", Value: "RollingUpdate (maxSurge 1)"},
		},
	})

	p.Section("Build Artefacts")

	// Box draws a bordered frame around any text. Handy for highlighting
	// important output like a release note or a configuration summary.
	p.Box("Release Notes v4.2.0",
		"- Added blue-green deployment support\n"+
			"- Improved rollback detection speed by 40%\n"+
			"- Fixed race condition in health check poller\n"+
			"- Updated base image to Alpine 3.21")

	p.Section("Deployment Plan")

	// Bullet supports nested indentation. Level 0 uses a solid bullet,
	// level 1 uses a hollow one, and so on.
	p.Bullet(0, "Build and push Docker image")
	p.Bullet(1, "Build: docker buildx build --platform linux/amd64")
	p.Bullet(1, "Push: registry.example.com/app:v4.2.0")
	p.Bullet(0, "Update Kubernetes manifests")
	p.Bullet(1, "Patch deployment image tag")
	p.Bullet(1, "Apply resource quota changes")
	p.Bullet(0, "Run smoke tests against staging")
	p.Bullet(1, "GET /health -> expect 200")
	p.Bullet(1, "POST /api/ping -> expect 200")

	p.Section("Service Configuration")

	// KeyValue is a simple two-column display for name-value pairs. Lighter
	// than SystemInfo when you don't need the title block.
	p.KeyValue("Memory limit", "512Mi")
	p.KeyValue("CPU request", "250m")
	p.KeyValue("CPU limit", "1000m")
	p.KeyValue("Liveness probe", "/health (delay 10s, period 15s)")
	p.KeyValue("Readiness probe", "/ready (delay 5s, period 10s)")

	p.Section("Rollout History")

	// Table renders aligned columns with header and row separators.
	// Column widths auto-fit to the widest cell in each column.
	p.Table(
		[]string{"Version", "Deployed At", "Author", "Status"},
		[][]string{
			{"v4.1.3", "2026-03-28 09:14", "ci-bot", "stable"},
			{"v4.2.0-rc1", "2026-04-01 14:22", "alice", "rolled back"},
			{"v4.2.0", "2026-04-04 10:00", "ci-bot", "in progress"},
		},
	)

	p.Section("Service Dependency Graph")

	// Tree shows hierarchical relationships. Each TreeItem can have children,
	// and velocity draws the connecting lines automatically.
	p.Tree([]velocity.TreeItem{
		{
			Key: "app (v4.2.0)",
			Children: []velocity.TreeItem{
				{
					Key: "postgres (primary)",
					Children: []velocity.TreeItem{
						{Key: "max_connections", Value: 200},
						{Key: "pool_size", Value: 20},
					},
				},
				{
					Key: "redis (cache)",
					Children: []velocity.TreeItem{
						{Key: "eviction_policy", Value: "allkeys-lru"},
						{Key: "max_memory", Value: "256mb"},
					},
				},
				{
					Key: "payments-api (external)",
					Children: []velocity.TreeItem{
						{Key: "timeout", Value: "5s"},
						{Key: "retries", Value: 3},
					},
				},
			},
		},
	})

	p.Section("Panel Example")

	// Panel draws a simpler bordered block with a title bar.
	// Good for multi-line notices that need to stand out.
	p.Panel("Deployment Notice",
		"This deployment will cause a brief disruption to the payments service.\n"+
			"Estimated downtime: 0 seconds (rolling update with readiness gates).\n"+
			"On-call: alice@example.com | Incident channel: #incidents")

	log.Newline()
	log.Info("pre-flight complete, starting deployment")
	log.Warn("production namespace check failed, continuing with staging only")
	log.Info("deployment finished",
		velocity.String("status", "success"),
		velocity.String("environment", "staging"),
	)
}

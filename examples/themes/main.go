// Package main cycles through velocity's four built-in themes so you can see
// how each one styles the different log levels. Run this in a terminal that
// supports 256-colour or true-colour output for the full effect.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity"
)

func main() {
	themes := []*velocity.Theme{
		velocity.ThemeNightOwl,
		velocity.ThemeSolarized,
		velocity.ThemeDracula,
		velocity.ThemeNord,
	}

	for _, theme := range themes {
		showTheme(theme)
	}
}

func showTheme(theme *velocity.Theme) {
	fmt.Printf("\n--- Theme: %s ---\n\n", theme.Name)

	log := velocity.New(
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithLevel(velocity.LevelDebug),
		velocity.WithTheme(theme),
	)

	// Show all four everyday levels so the colour contrast is obvious.
	log.Debug("initialising subsystems", velocity.String("phase", "boot"))
	log.Info("service started", velocity.String("addr", ":9090"))
	log.Warn("memory pressure detected", velocity.Int("used_mb", 780))
	log.Error("health check failed", velocity.String("target", "db.internal"))

	// Detailed() returns a child that forces tree-format, showing how each
	// theme colours key names and values separately.
	log.Detailed().Info("deployment complete",
		velocity.String("environment", "staging"),
		velocity.String("version", "3.1.0"),
		velocity.Int("instances", 5),
		velocity.Duration("rollout", 32*time.Second),
		velocity.Bool("canary", false),
	)
}

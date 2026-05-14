// Package main cycles through velocity's built-in themes and demonstrates
// Theme.Format(slot, s) for each semantic style slot. Run in a terminal
// with 256-colour or true-colour support for the full effect.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	themes := []*velocity.Theme{
		velocity.ThemeNightOwl,
		velocity.ThemeSolarized,
		velocity.ThemeDracula,
		velocity.ThemeNord,
		velocity.ThemeMono,
	}

	for _, theme := range themes {
		showTheme(theme)
	}
}

func showTheme(theme *velocity.Theme) {
	fmt.Printf("\n--- Theme: %s ---\n\n", theme.Name())

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

	// log.Style() returns the active palette when colour is enabled (TTY or
	// FORCE_COLOR), or ThemeMono when NO_COLOR / piped — so Format and Wrap
	// calls are always colour-aware without manual env-var checks.
	style := log.Style()

	// Theme.Format(slot, s) — semantic colouring without raw ANSI.
	// Each slot has a well-defined role across all built-in themes.
	fmt.Printf("\n  Style slots:\n")
	fmt.Printf("    %s\n", style.Format(velocity.SlotGood, "SlotGood — success / positive outcome"))
	fmt.Printf("    %s\n", style.Format(velocity.SlotBad, "SlotBad  — error / failure"))
	fmt.Printf("    %s\n", style.Format(velocity.SlotWarn, "SlotWarn — warning / degraded"))
	fmt.Printf("    %s\n", style.Format(velocity.SlotInfo, "SlotInfo — informational"))
	fmt.Printf("    %s\n", style.Format(velocity.SlotMuted, "SlotMuted — secondary / de-emphasised"))
	fmt.Printf("    %s\n", style.Format(velocity.SlotStrong, "SlotStrong — emphasis"))
	fmt.Printf("    %s\n", style.Format(velocity.SlotHeading, "SlotHeading — section headings"))
	fmt.Printf("    %s\n", style.Format(velocity.SlotEndpoint, "SlotEndpoint — service/URL labels"))
	fmt.Printf("    %s\n", style.Format(velocity.SlotTableHeader, "SlotTableHeader — column headers"))

	// Status badge demonstration using Wrap for prefix/suffix embedding.
	okPfx, okSfx := style.Wrap(velocity.SlotStatusOK)
	warnPfx, warnSfx := style.Wrap(velocity.SlotStatusWarn)
	failPfx, failSfx := style.Wrap(velocity.SlotStatusFail)
	infoPfx, infoSfx := style.Wrap(velocity.SlotStatusInfo)
	fmt.Printf("\n  Status slots (via Wrap):\n")
	fmt.Printf("    %s[OKAY]%s  %s[WARN]%s  %s[FAIL]%s  %s[INFO]%s\n",
		okPfx, okSfx, warnPfx, warnSfx, failPfx, failSfx, infoPfx, infoSfx)
}

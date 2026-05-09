// Custom theme example. Shows how to define your own colour palette
// and have it flow through the entire velocity stack: log lines,
// pretty output, status indicators, and tables.
//
// This one is a cyberpunk theme. Hot pinks, electric blues, neon greens.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity"
)

// ThemeCyberpunk is a neon-on-dark palette inspired by Night City.
var ThemeCyberpunk = cyberpunkTheme()

func cyberpunkTheme() *velocity.Theme {
	t := &velocity.Theme{
		Name: "Cyberpunk",

		// Log levels: each gets a distinct neon tone.
		DebugColour: velocity.RGB(0x8B, 0x5C, 0xF6), // purple
		InfoColour:  velocity.RGB(0x00, 0xD4, 0xFF), // electric blue
		WarnColour:  velocity.RGB(0xFF, 0xE6, 0x00), // neon yellow
		ErrorColour: velocity.RGB(0xFF, 0x00, 0x6E), // hot pink
		FatalColour: velocity.RGB(0xFF, 0x00, 0x00), // red

		// Chrome: the structural bits around your log messages.
		TimestampColour: velocity.RGB(0x5A, 0x5A, 0x7A), // dim steel
		MessageColour:   velocity.RGB(0xE0, 0xE0, 0xFF), // cool white
		FieldKeyColour:  velocity.RGB(0x00, 0xFF, 0xAA), // neon green
		FieldValColour:  velocity.RGB(0xCC, 0xCC, 0xEE), // soft lavender
		ErrorValColour:  velocity.RGB(0xFF, 0x00, 0x6E), // hot pink (matches error)

		// Status indicators for tables and operation results.
		StatusOKColour:   velocity.RGB(0x00, 0xFF, 0xAA), // neon green
		StatusFailColour: velocity.RGB(0xFF, 0x00, 0x6E), // hot pink
		StatusWarnColour: velocity.RGB(0xFF, 0xE6, 0x00), // neon yellow
		StatusInfoColour: velocity.RGB(0x00, 0xD4, 0xFF), // electric blue

		// Table headers.
		TableHeader: velocity.RGB(0xBB, 0x86, 0xFC), // bright purple
	}

	// Pre-compute ANSI escape sequences so they aren't generated per log line.
	t.Cache()

	return t
}

func main() {
	// Wire up the theme through the logger. Every writer and formatter
	// inherits it automatically.
	log := velocity.New(
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithTheme(ThemeCyberpunk),
		velocity.WithLevel(velocity.LevelDebug),
	)

	fmt.Println("=== Cyberpunk Theme ===")
	fmt.Println()

	// All log levels pick up the theme colours.
	log.Debug("neural link initialised", velocity.String("interface", "BCI-7"))
	log.Info("connected to net", velocity.String("node", "arasaka-tower"), velocity.Int("latency_ms", 3))
	log.Warn("ICE detected on subnet", velocity.String("subnet", "10.77.0.0/16"), velocity.Float64("threat", 0.82))
	log.Error("intrusion countermeasure triggered", velocity.String("target", "db-vault-03"), velocity.Duration("lockout", 30*time.Second))

	log.Newline()

	// Detailed() child forces tree mode, same colours as the parent theme.
	log.Detailed().Info("system status",
		velocity.String("cpu", "Arasaka X9-R"),
		velocity.Int("cores", 128),
		velocity.Float64("clock_ghz", 5.8),
		velocity.Bool("overclocked", true),
		velocity.String("cooling", "liquid nitrogen"),
	)

	log.Newline()

	// Pretty output routes through the logger so it serialises under the same mutex
	// and inherits the theme automatically.
	p := velocity.NewPrettyFromLogger(log)

	p.Section("Mission Briefing")

	p.Box("Operation Blackout",
		"Infiltrate Militech's northern data centre.\n"+
			"Extract the Relic prototype schematics.\n"+
			"Exfil via AV on the roof. 4 minute window.")

	p.KeyValue("Difficulty", "Very Hard")
	p.KeyValue("Reward", "50,000 eddies")
	p.KeyValue("Fixer", "Rogue Amendiares")

	log.Newline()

	// log.Style() returns the active theme. Use its ANSI codes directly to
	// colour table cell content. Phase 2 adds Theme.Format(slot, s) as a
	// cleaner API; this is the Phase 1 idiom.
	style := log.Style()
	colour := func(c velocity.Colour, s string) string { return c.ANSI(true) + s + velocity.Reset }

	p.Table(
		[]string{"Implant", "Status", "Integrity"},
		[][]string{
			{"Kiroshi Optics Mk.3", colour(style.StatusOKColour, "ONLINE"), "98%"},
			{"Mantis Blades", colour(style.StatusOKColour, "ONLINE"), "100%"},
			{"Sandevistan Mk.4", colour(style.StatusWarnColour, "DEGRADED"), "67%"},
			{"Monowire", colour(style.StatusFailColour, "OFFLINE"), "12%"},
		},
	)

	log.Newline()

	// Tree display with the theme. velocity.NewTree is the canonical constructor;
	// p.Tree is sugar that calls Render immediately.
	p.Tree([]velocity.TreeItem{
		{
			Key: "netrunner-loadout",
			Children: []velocity.TreeItem{
				{Key: "deck", Value: "Tetratronic Rippler Mk.4"},
				{Key: "quickhacks", Children: []velocity.TreeItem{
					{Key: "contagion", Value: "legendary"},
					{Key: "short circuit", Value: "epic"},
					{Key: "system reset", Value: "rare"},
				}},
				{Key: "ram", Value: "24 units"},
				{Key: "buffer_size", Value: 8},
			},
		},
	})

	log.Newline()

	// Child loggers inherit the theme through the parent's writer.
	mission := log.With(velocity.String("op", "blackout"), velocity.String("agent", "V"))
	mission.Info("starting infiltration", velocity.String("entry_point", "ventilation shaft"))
	mission.Warn("guard patrol detected", velocity.Int("hostiles", 3), velocity.Duration("eta", 45*time.Second))
	mission.Info("relic acquired", velocity.Bool("detected", false))

	log.Newline()
	p.Success("Mission complete. Get paid, choom.")
}

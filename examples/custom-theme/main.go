// Custom theme example. Shows how to define your own colour palette using NewTheme
// and ThemeOption, then use Theme.Format(slot, s) to colour arbitrary output.
//
// This one is a cyberpunk theme. Hot pinks, electric blues, neon greens.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
)

// ThemeCyberpunk is a neon-on-dark palette inspired by Night City.
var ThemeCyberpunk = velocity.NewTheme("Cyberpunk",
	// Log levels: each gets a distinct neon tone.
	velocity.WithLevelColours(
		velocity.RGB(0x8B, 0x5C, 0xF6), // debug: purple
		velocity.RGB(0x00, 0xD4, 0xFF), // info: electric blue
		velocity.RGB(0xFF, 0xE6, 0x00), // warn: neon yellow
		velocity.RGB(0xFF, 0x00, 0x6E), // error: hot pink
		velocity.RGB(0xFF, 0x00, 0x00), // fatal: red
	),
	// Chrome: the structural bits around your log messages.
	velocity.WithTimestampColour(velocity.RGB(0x5A, 0x5A, 0x7A)), // dim steel
	velocity.WithMessageColour(velocity.RGB(0xE0, 0xE0, 0xFF)),   // cool white
	velocity.WithFieldColours(
		velocity.RGB(0x00, 0xFF, 0xAA), // key: neon green
		velocity.RGB(0xCC, 0xCC, 0xEE), // value: soft lavender
		velocity.RGB(0xFF, 0x00, 0x6E), // error value: hot pink
	),
	// Status and semantic slots for use with Theme.Format.
	velocity.WithStyleSlot(velocity.SlotStatusOK, velocity.RGB(0x00, 0xFF, 0xAA)),
	velocity.WithStyleSlot(velocity.SlotStatusFail, velocity.RGB(0xFF, 0x00, 0x6E)),
	velocity.WithStyleSlot(velocity.SlotStatusWarn, velocity.RGB(0xFF, 0xE6, 0x00)),
	velocity.WithStyleSlot(velocity.SlotStatusInfo, velocity.RGB(0x00, 0xD4, 0xFF)),
	velocity.WithStyleSlot(velocity.SlotTableHeader, velocity.RGB(0xBB, 0x86, 0xFC)),
	velocity.WithStyleSlot(velocity.SlotGood, velocity.RGB(0x00, 0xFF, 0xAA)),
	velocity.WithStyleSlot(velocity.SlotBad, velocity.RGB(0xFF, 0x00, 0x6E)),
	velocity.WithStyleSlot(velocity.SlotMuted, velocity.RGB(0x5A, 0x5A, 0x7A)),
)

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

	// Theme.Format(slot, s) is the v2 way to colour cell content.
	// No raw ANSI construction needed; the theme handles escape codes.
	style := log.Style()
	p.Table(
		[]string{"Implant", "Status", "Integrity"},
		[][]string{
			{"Kiroshi Optics Mk.3", style.Format(velocity.SlotStatusOK, "ONLINE"), "98%"},
			{"Mantis Blades", style.Format(velocity.SlotStatusOK, "ONLINE"), "100%"},
			{"Sandevistan Mk.4", style.Format(velocity.SlotStatusWarn, "DEGRADED"), "67%"},
			{"Monowire", style.Format(velocity.SlotStatusFail, "OFFLINE"), "12%"},
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

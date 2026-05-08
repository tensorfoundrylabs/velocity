package velocity

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheme_Cache_Idempotent confirms that calling Cache() twice produces identical
// cached strings. Entry points rely on this to safely re-cache already-cached themes.
func TestTheme_Cache_Idempotent(t *testing.T) {
	t.Parallel()

	theme := &Theme{
		DebugColour:     RGB(0xC7, 0x92, 0xEA),
		InfoColour:      RGB(0x82, 0xAA, 0xFF),
		WarnColour:      RGB(0xFF, 0xCB, 0x6B),
		ErrorColour:     RGB(0xFF, 0x55, 0x72),
		FatalColour:     RGB(0xFF, 0x00, 0x00),
		TimestampColour: RGB(0x7E, 0x8E, 0xA6),
		MessageColour:   RGB(0xE0, 0xE0, 0xE0),
		FieldKeyColour:  RGB(0x7E, 0x8E, 0xA6),
		FieldValColour:  RGB(0xD3, 0xD3, 0xD3),
		ErrorValColour:  RGB(0xFF, 0x55, 0x72),
		TableHeader:     RGB(0x7F, 0xD3, 0xFF),
	}

	theme.Cache()
	first := *theme // copy all fields after first cache

	theme.Cache()
	second := *theme // copy after second cache

	if first.cachedTimestampFg != second.cachedTimestampFg {
		t.Errorf("cachedTimestampFg changed: %q vs %q", first.cachedTimestampFg, second.cachedTimestampFg)
	}
	if first.cachedMessageFg != second.cachedMessageFg {
		t.Errorf("cachedMessageFg changed: %q vs %q", first.cachedMessageFg, second.cachedMessageFg)
	}
	if first.cachedFieldKeyFg != second.cachedFieldKeyFg {
		t.Errorf("cachedFieldKeyFg changed: %q vs %q", first.cachedFieldKeyFg, second.cachedFieldKeyFg)
	}
	if first.cachedFieldValFg != second.cachedFieldValFg {
		t.Errorf("cachedFieldValFg changed: %q vs %q", first.cachedFieldValFg, second.cachedFieldValFg)
	}
	if first.cachedErrorValFg != second.cachedErrorValFg {
		t.Errorf("cachedErrorValFg changed: %q vs %q", first.cachedErrorValFg, second.cachedErrorValFg)
	}
	if first.cachedTableHeaderFg != second.cachedTableHeaderFg {
		t.Errorf("cachedTableHeaderFg changed: %q vs %q", first.cachedTableHeaderFg, second.cachedTableHeaderFg)
	}
	if first.cachedInfoColourFg != second.cachedInfoColourFg {
		t.Errorf("cachedInfoColourFg changed: %q vs %q", first.cachedInfoColourFg, second.cachedInfoColourFg)
	}
	for lvl := LevelDebug; lvl <= LevelOff; lvl++ {
		if first.cachedLevelFg[lvl] != second.cachedLevelFg[lvl] {
			t.Errorf("cachedLevelFg[%d] changed: %q vs %q", lvl, first.cachedLevelFg[lvl], second.cachedLevelFg[lvl])
		}
	}
}

// TestLogger_SetTheme_CachesUncachedTheme verifies that a user-defined theme constructed via
// struct literal (no explicit Cache() call) produces ANSI-coloured output after passing through
// SetTheme. ensureCached clones and caches the theme internally, so the writer has fully
// populated colour codes even though the caller never called Cache().
func TestLogger_SetTheme_CachesUncachedTheme(t *testing.T) {
	t.Parallel()

	// Build a custom theme without calling Cache() — simulates what a user does.
	customTheme := &Theme{
		Name:            "Custom",
		DebugColour:     RGB(0x11, 0x22, 0x33),
		InfoColour:      RGB(0x44, 0x55, 0x66),
		WarnColour:      RGB(0x77, 0x88, 0x99),
		ErrorColour:     RGB(0xAA, 0xBB, 0xCC),
		FatalColour:     RGB(0xDD, 0xEE, 0xFF),
		TimestampColour: RGB(0x10, 0x20, 0x30),
		MessageColour:   RGB(0x40, 0x50, 0x60),
		FieldKeyColour:  RGB(0x70, 0x80, 0x90),
		FieldValColour:  RGB(0xA0, 0xB0, 0xC0),
		ErrorValColour:  RGB(0xD0, 0xE0, 0xF0),
		TableHeader:     RGB(0x12, 0x34, 0x56),
	}

	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.StructuredOutput = nil
	cfg.ConsoleTheme = ThemeNightOwl // start with a known good theme

	log := NewWithConfig(cfg)
	log.SetTheme(customTheme) // must auto-cache the theme internally

	// Log something and confirm ANSI escape codes appear in output.
	// If the theme were not cached the colour strings would be empty and no ANSI escapes written.
	log.Info("testing custom theme")
	out := buf.String()

	if !strings.Contains(out, "\033[") {
		t.Errorf("expected ANSI escape codes in output for custom theme, got: %q", out)
	}
}

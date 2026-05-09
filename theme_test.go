package velocity

import (
	"bytes"
	"strings"
	"sync"
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

	// Snapshot field values after the first call.
	tsAfterFirst := theme.cachedTimestampFg
	msgAfterFirst := theme.cachedMessageFg
	keyAfterFirst := theme.cachedFieldKeyFg
	valAfterFirst := theme.cachedFieldValFg
	errAfterFirst := theme.cachedErrorValFg
	tblAfterFirst := theme.cachedTableHeaderFg
	infoAfterFirst := theme.cachedInfoColourFg
	lvlAfterFirst := theme.cachedLevelFg

	// Second call must be a no-op — sync.Once prevents re-execution.
	theme.Cache()

	if theme.cachedTimestampFg != tsAfterFirst {
		t.Errorf("cachedTimestampFg changed: %q vs %q", tsAfterFirst, theme.cachedTimestampFg)
	}
	if theme.cachedMessageFg != msgAfterFirst {
		t.Errorf("cachedMessageFg changed: %q vs %q", msgAfterFirst, theme.cachedMessageFg)
	}
	if theme.cachedFieldKeyFg != keyAfterFirst {
		t.Errorf("cachedFieldKeyFg changed: %q vs %q", keyAfterFirst, theme.cachedFieldKeyFg)
	}
	if theme.cachedFieldValFg != valAfterFirst {
		t.Errorf("cachedFieldValFg changed: %q vs %q", valAfterFirst, theme.cachedFieldValFg)
	}
	if theme.cachedErrorValFg != errAfterFirst {
		t.Errorf("cachedErrorValFg changed: %q vs %q", errAfterFirst, theme.cachedErrorValFg)
	}
	if theme.cachedTableHeaderFg != tblAfterFirst {
		t.Errorf("cachedTableHeaderFg changed: %q vs %q", tblAfterFirst, theme.cachedTableHeaderFg)
	}
	if theme.cachedInfoColourFg != infoAfterFirst {
		t.Errorf("cachedInfoColourFg changed: %q vs %q", infoAfterFirst, theme.cachedInfoColourFg)
	}
	for lvl := LevelDebug; lvl <= LevelOff; lvl++ {
		if theme.cachedLevelFg[lvl] != lvlAfterFirst[lvl] {
			t.Errorf("cachedLevelFg[%d] changed: %q vs %q", lvl, lvlAfterFirst[lvl], theme.cachedLevelFg[lvl])
		}
	}
}

// TestLogger_SetTheme_CachesUncachedTheme verifies that a user-defined theme constructed via
// struct literal (no explicit Cache() call) produces ANSI-coloured output after passing through
// SetTheme. EnsureCached populates the theme in-place via sync.Once, so the writer has fully
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
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.StructuredOutput = nil
	cfg.ConsoleTheme = ThemeNightOwl // start with a known good theme

	log := newFromConfig(cfg)
	log.SetTheme(customTheme) // must auto-cache the theme internally

	// Log something and confirm ANSI escape codes appear in output.
	// If the theme were not cached the colour strings would be empty and no ANSI escapes written.
	log.Info("testing custom theme")
	out := buf.String()

	if !strings.Contains(out, "\033[") {
		t.Errorf("expected ANSI escape codes in output for custom theme, got: %q", out)
	}
}

// TestTheme_Cache_PreservesPointerIdentity verifies that EnsureCached returns the original
// pointer — not a clone. The sync.Once refactor makes in-place mutation safe, so callers
// that compare theme pointers for identity (e.g. logger_settheme_test.go) must not break.
func TestTheme_Cache_PreservesPointerIdentity(t *testing.T) {
	t.Parallel()

	original := &Theme{
		MessageColour: RGB(0xE0, 0xE0, 0xE0),
		InfoColour:    RGB(0x82, 0xAA, 0xFF),
	}

	got := original.EnsureCached()
	if got != original {
		t.Errorf("EnsureCached returned a different pointer: want %p, got %p", original, got)
	}
}

// TestTheme_Cache_Concurrent verifies that Cache() called from many goroutines simultaneously
// produces no data race. Correctness is validated by the -race flag; this test exercises the path.
func TestTheme_Cache_Concurrent(t *testing.T) {
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

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			theme.Cache()
			// Read a cached field to exercise the memory model under -race.
			_ = theme.CachedMessageFg()
		}()
	}

	wg.Wait()

	if theme.CachedMessageFg() == "" {
		t.Error("expected CachedMessageFg to be populated after concurrent Cache() calls")
	}
}

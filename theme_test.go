package velocity

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewTheme_Name confirms the name accessor returns what was passed to NewTheme.
func TestNewTheme_Name(t *testing.T) {
	t.Parallel()

	th := NewTheme("Test Theme")
	if got := th.Name(); got != "Test Theme" {
		t.Errorf("Name() = %q, want %q", got, "Test Theme")
	}
}

// TestNewTheme_NilName confirms nil theme returns empty string from Name.
func TestNewTheme_NilName(t *testing.T) {
	t.Parallel()

	var th *Theme
	if got := th.Name(); got != "" {
		t.Errorf("nil.Name() = %q, want empty", got)
	}
}

// TestThemeMono_Format confirms ThemeMono (no colour options) returns input unchanged.
func TestThemeMono_Format(t *testing.T) {
	t.Parallel()

	const input = "hello"
	for _, slot := range []StyleSlot{SlotGood, SlotBad, SlotWarn, SlotStatusOK, SlotTableHeader} {
		got := ThemeMono.Format(slot, input)
		if got != input {
			t.Errorf("ThemeMono.Format(%d, %q) = %q, want input unchanged", slot, input, got)
		}
	}
}

// TestTheme_Format_EmitsANSI confirms Format wraps with ANSI codes on a coloured theme.
func TestTheme_Format_EmitsANSI(t *testing.T) {
	t.Parallel()

	th := NewTheme("test",
		WithStyleSlot(SlotGood, RGB(0x00, 0xFF, 0x00)),
	)

	got := th.Format(SlotGood, "OK")
	if !strings.HasPrefix(got, "\033[") {
		t.Errorf("Format() did not emit ANSI prefix: %q", got)
	}
	if !strings.Contains(got, "OK") {
		t.Errorf("Format() dropped the content: %q", got)
	}
	if !strings.HasSuffix(got, Reset) {
		t.Errorf("Format() did not append Reset: %q", got)
	}
}

// TestTheme_Format_UnsetSlot confirms Format returns input unchanged for a slot with no colour.
func TestTheme_Format_UnsetSlot(t *testing.T) {
	t.Parallel()

	// Only SlotGood is set; SlotBad should passthrough.
	th := NewTheme("partial",
		WithStyleSlot(SlotGood, RGB(0x00, 0xFF, 0x00)),
	)

	in := "FAIL"
	got := th.Format(SlotBad, in)
	if got != in {
		t.Errorf("Format() for unset slot = %q, want %q", got, in)
	}
}

// TestTheme_Format_NilTheme confirms nil theme returns input unchanged.
func TestTheme_Format_NilTheme(t *testing.T) {
	t.Parallel()

	const input = "hello"
	var th *Theme
	got := th.Format(SlotGood, input)
	if got != input {
		t.Errorf("nil.Format() = %q, want %q", got, input)
	}
}

// TestTheme_Wrap_EmitsCodesForSetSlot confirms Wrap returns non-empty prefix/suffix for a set slot.
func TestTheme_Wrap_EmitsCodesForSetSlot(t *testing.T) {
	t.Parallel()

	th := NewTheme("test",
		WithStyleSlot(SlotStatusOK, RGB(0x80, 0xD4, 0xAA)),
	)

	prefix, suffix := th.Wrap(SlotStatusOK)
	if prefix == "" {
		t.Error("Wrap() prefix is empty for set slot")
	}
	if suffix != Reset {
		t.Errorf("Wrap() suffix = %q, want %q", suffix, Reset)
	}
}

// TestTheme_Wrap_EmptyForUnsetSlot confirms Wrap returns empty strings for an unset slot.
func TestTheme_Wrap_EmptyForUnsetSlot(t *testing.T) {
	t.Parallel()

	th := NewTheme("partial",
		WithStyleSlot(SlotGood, RGB(0x00, 0xFF, 0x00)),
	)

	prefix, suffix := th.Wrap(SlotBad)
	if prefix != "" || suffix != "" {
		t.Errorf("Wrap() for unset slot = (%q, %q), want both empty", prefix, suffix)
	}
}

// TestTheme_Wrap_NilTheme confirms nil theme returns empty strings from Wrap.
func TestTheme_Wrap_NilTheme(t *testing.T) {
	t.Parallel()

	var th *Theme
	prefix, suffix := th.Wrap(SlotGood)
	if prefix != "" || suffix != "" {
		t.Errorf("nil.Wrap() = (%q, %q), want both empty", prefix, suffix)
	}
}

// TestTheme_Stylish_BufferIsNotTTY confirms Stylish returns false for a bytes.Buffer.
func TestTheme_Stylish_BufferIsNotTTY(t *testing.T) {
	t.Parallel()

	th := ThemeNightOwl
	if th.Stylish(&bytes.Buffer{}) {
		t.Error("Stylish(bytes.Buffer) should return false — buffer is not a TTY")
	}
}

// TestWithLevelColours_SetsAllLevels confirms WithLevelColours populates all five levels.
func TestWithLevelColours_SetsAllLevels(t *testing.T) {
	t.Parallel()

	th := NewTheme("levels",
		WithLevelColours(
			RGB(0x11, 0x11, 0x11), // debug
			RGB(0x22, 0x22, 0x22), // info
			RGB(0x33, 0x33, 0x33), // warn
			RGB(0x44, 0x44, 0x44), // error
			RGB(0x55, 0x55, 0x55), // fatal
		),
	)

	for _, lvl := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal} {
		code := th.cachedLevelCode(lvl)
		if code == "" {
			t.Errorf("level %v has empty ANSI code", lvl)
		}
		if !strings.HasPrefix(code, "\033[") {
			t.Errorf("level %v code does not start with ESC: %q", lvl, code)
		}
	}
}

// TestWithLevelColour_Single confirms WithLevelColour sets exactly one level.
func TestWithLevelColour_Single(t *testing.T) {
	t.Parallel()

	th := NewTheme("single",
		WithLevelColour(LevelError, RGB(0xFF, 0x55, 0x72)),
	)

	code := th.cachedLevelCode(LevelError)
	if code == "" {
		t.Error("LevelError code is empty after WithLevelColour")
	}
	// Other levels should be empty.
	if th.cachedLevelCode(LevelDebug) != "" {
		t.Error("LevelDebug should be empty when not set")
	}
}

// TestBuiltInThemes_HaveAllSlots confirms each built-in theme populates the key slots.
func TestBuiltInThemes_HaveAllSlots(t *testing.T) {
	t.Parallel()

	themes := []*Theme{ThemeNightOwl, ThemeSolarized, ThemeDracula, ThemeNord}
	criticalSlots := []StyleSlot{SlotStatusOK, SlotStatusFail, SlotStatusWarn, SlotStatusInfo, SlotTableHeader}

	for _, th := range themes {
		for _, slot := range criticalSlots {
			code := th.slotCode(slot)
			if code == "" {
				t.Errorf("theme %q: slot %d has no ANSI code", th.Name(), slot)
			}
		}
	}
}

// TestBuiltInThemes_LevelCodesPresent confirms all built-in themes have level codes.
func TestBuiltInThemes_LevelCodesPresent(t *testing.T) {
	t.Parallel()

	themes := []*Theme{ThemeNightOwl, ThemeSolarized, ThemeDracula, ThemeNord}
	for _, th := range themes {
		for _, lvl := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal} {
			if th.cachedLevelCode(lvl) == "" {
				t.Errorf("theme %q: level %v has no ANSI code", th.Name(), lvl)
			}
		}
	}
}

// TestLogger_SetTheme_WithNewTheme verifies that a user-defined theme built via
// NewTheme produces ANSI-coloured output after passing through SetTheme.
func TestLogger_SetTheme_WithNewTheme(t *testing.T) {
	t.Parallel()

	customTheme := NewTheme("Custom",
		WithLevelColours(
			RGB(0x11, 0x22, 0x33),
			RGB(0x44, 0x55, 0x66),
			RGB(0x77, 0x88, 0x99),
			RGB(0xAA, 0xBB, 0xCC),
			RGB(0xDD, 0xEE, 0xFF),
		),
		WithTimestampColour(RGB(0x10, 0x20, 0x30)),
		WithMessageColour(RGB(0x40, 0x50, 0x60)),
		WithFieldColours(RGB(0x70, 0x80, 0x90), RGB(0xA0, 0xB0, 0xC0), RGB(0xD0, 0xE0, 0xF0)),
		WithStyleSlot(SlotTableHeader, RGB(0x12, 0x34, 0x56)),
	)

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.StructuredOutput = nil
	cfg.ConsoleTheme = ThemeNightOwl

	log := newFromConfig(cfg)
	log.SetTheme(customTheme)

	log.Info("testing custom theme")
	out := buf.String()

	if !strings.Contains(out, "\033[") {
		t.Errorf("expected ANSI escape codes in output for custom theme, got: %q", out)
	}
}

// TestTheme_Format_AllBuiltInSlots exercises Format for every named slot on NightOwl.
func TestTheme_Format_AllBuiltInSlots(t *testing.T) {
	t.Parallel()

	th := ThemeNightOwl
	slots := []StyleSlot{
		SlotGood, SlotBad, SlotWarn, SlotInfo, SlotMuted, SlotStrong, SlotHeading,
		SlotEndpoint, SlotHyperlink, SlotContinuation, SlotCount, SlotSecure,
		SlotStatusOK, SlotStatusFail, SlotStatusWarn, SlotStatusInfo, SlotTableHeader,
	}

	for _, slot := range slots {
		got := th.Format(slot, "text")
		// Every slot must contain the original text.
		if !strings.Contains(got, "text") {
			t.Errorf("slot %d: Format dropped content: %q", slot, got)
		}
		// Every slot on NightOwl should have a colour code.
		if !strings.HasPrefix(got, "\033[") {
			t.Errorf("slot %d: Format did not emit ANSI prefix: %q", slot, got)
		}
	}
}

// TestNoColourTheme_Format confirms the package-level noColourTheme returns input unchanged.
func TestNoColourTheme_Format(t *testing.T) {
	t.Parallel()

	got := noColourTheme.Format(SlotStatusOK, "OK")
	if got != "OK" {
		t.Errorf("noColourTheme.Format() = %q, want %q", got, "OK")
	}
}

// TestTheme_CachedFieldKeyFg_Empty confirms Mono theme returns empty cached fields.
func TestTheme_CachedFieldKeyFg_Empty(t *testing.T) {
	t.Parallel()

	if got := ThemeMono.CachedFieldKeyFg(); got != "" {
		t.Errorf("ThemeMono.CachedFieldKeyFg() = %q, want empty", got)
	}
}

// TestTheme_CachedTableHeaderFg_NightOwl confirms NightOwl has a non-empty table header code.
func TestTheme_CachedTableHeaderFg_NightOwl(t *testing.T) {
	t.Parallel()

	if got := ThemeNightOwl.CachedTableHeaderFg(); got == "" {
		t.Error("ThemeNightOwl.CachedTableHeaderFg() is empty")
	}
}

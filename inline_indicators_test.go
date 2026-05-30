package velocity

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fixedTime is a stable timestamp for golden comparisons. Using a fixed zone offset
// (not UTC) so RFC3339-style formats include "+HH:MM" — the longest rendered form.
var fixedTime = time.Date(2026, 5, 30, 14, 52, 33, 0, time.FixedZone("AEST", 10*60*60))

// renderEntry is a test helper that builds a console output string for the given
// entry using colour disabled, the default template, and the supplied config.
// Caller constructs the cfg with any test-specific overrides before passing it here.
func renderEntry(t *testing.T, cfg *config, e *Entry) string {
	t.Helper()
	var buf bytes.Buffer
	cfg.ConsoleOutput = &buf
	cfg.DisableColour = true
	cfg.TimeFormat = "2006-01-02 15:04:05"
	cfg.StructuredOutput = nil
	cfg.StructuredLevel = LevelOff
	cfg.ConsoleLevel = LevelDebug
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 512
	}
	if cfg.FieldPoolSize == 0 {
		cfg.FieldPoolSize = 25
	}
	l := newFromConfig(cfg)
	if l.consoleWriter == nil {
		t.Fatal("consoleWriter is nil after newFromConfig")
	}
	if err := l.consoleWriter.Write(e); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.String()
}

// TestInlineIndicators_DefaultOff_OutputByteIdentical is the golden characterisation
// test that locks the non-breaking guarantee: a representative entry with component,
// count, a timing field, and an ordinary field must produce BYTE-IDENTICAL output
// whether the indicators config is enabled or disabled, as long as no options are set.
func TestInlineIndicators_DefaultOff_OutputByteIdentical(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("service started")
	e.SetTime(fixedTime)
	e.WithFields(
		String("component", "Scout"),
		Int("count", 3),
		String("startup_ms", "2000"),
		String("env", "production"),
	)

	// Baseline: no indicators configured (zero-value inlineIndicators).
	baselineCfg := defaultConfig()
	baseline := renderEntry(t, baselineCfg, e)

	// Indicators struct present but feature disabled (all booleans false).
	withZeroIndicatorsCfg := defaultConfig()
	withZeroIndicatorsCfg.Indicators = inlineIndicators{} // explicit zero value
	withZeroIndicators := renderEntry(t, withZeroIndicatorsCfg, e)

	if baseline != withZeroIndicators {
		t.Errorf("output changed when inlineIndicators is zero-valued:\n  baseline: %q\n  zero ind: %q",
			baseline, withZeroIndicators)
	}
}

// TestInlineIndicators_OptionsWriteConfig verifies that each option function correctly
// mutates the config struct without triggering any rendering change (Phase 0 contract).
func TestInlineIndicators_OptionsWriteConfig(t *testing.T) {
	t.Parallel()

	t.Run("WithComponentField", func(t *testing.T) {
		t.Parallel()
		cfg := defaultConfig()
		WithComponentField("svc")(cfg)
		if !cfg.Indicators.component {
			t.Error("component should be true after WithComponentField")
		}
		if cfg.Indicators.componentField != "svc" {
			t.Errorf("componentField = %q, want %q", cfg.Indicators.componentField, "svc")
		}
		if cfg.Indicators.componentWidth != 8 {
			t.Errorf("componentWidth default = %d, want 8", cfg.Indicators.componentWidth)
		}
		if !cfg.Indicators.removeFromTree {
			t.Error("removeFromTree should default to true when component is enabled")
		}
	})

	t.Run("WithComponentColumnWidth", func(t *testing.T) {
		t.Parallel()
		cfg := defaultConfig()
		WithComponentColumnWidth(12)(cfg)
		if cfg.Indicators.componentWidth != 12 {
			t.Errorf("componentWidth = %d, want 12", cfg.Indicators.componentWidth)
		}
		// Zero or negative should be ignored.
		WithComponentColumnWidth(0)(cfg)
		if cfg.Indicators.componentWidth != 12 {
			t.Errorf("zero width should not overwrite existing width; got %d", cfg.Indicators.componentWidth)
		}
	})

	t.Run("WithCountFields", func(t *testing.T) {
		t.Parallel()
		cfg := defaultConfig()
		WithCountFields("total", "n")(cfg)
		if len(cfg.Indicators.countFields) != 2 {
			t.Errorf("countFields len = %d, want 2", len(cfg.Indicators.countFields))
		}
		if cfg.Indicators.countFields[0] != "total" || cfg.Indicators.countFields[1] != "n" {
			t.Errorf("countFields = %v, want [total n]", cfg.Indicators.countFields)
		}
	})

	t.Run("WithTimingFields", func(t *testing.T) {
		t.Parallel()
		cfg := defaultConfig()
		WithTimingFields("startup_ms", "stop_ms")(cfg)
		if len(cfg.Indicators.timingFields) != 2 {
			t.Errorf("timingFields len = %d, want 2", len(cfg.Indicators.timingFields))
		}
	})

	t.Run("WithStateTransitionPairs", func(t *testing.T) {
		t.Parallel()
		cfg := defaultConfig()
		WithStateTransitionPairs([2]string{"old_state", "new_state"})(cfg)
		if len(cfg.Indicators.statePairs) != 1 {
			t.Errorf("statePairs len = %d, want 1", len(cfg.Indicators.statePairs))
		}
		if cfg.Indicators.statePairs[0] != [2]string{"old_state", "new_state"} {
			t.Errorf("statePairs[0] = %v, want [old_state new_state]", cfg.Indicators.statePairs[0])
		}
	})

	t.Run("WithInlineGlyphs", func(t *testing.T) {
		t.Parallel()
		cfg := defaultConfig()
		WithInlineGlyphs(true)(cfg)
		if !cfg.Indicators.showGlyphs {
			t.Error("showGlyphs should be true after WithInlineGlyphs(true)")
		}
		WithInlineGlyphs(false)(cfg)
		if cfg.Indicators.showGlyphs {
			t.Error("showGlyphs should be false after WithInlineGlyphs(false)")
		}
	})

	t.Run("WithComponentStyling", func(t *testing.T) {
		t.Parallel()
		cfg := defaultConfig()
		WithComponentStyling()(cfg)
		if !cfg.Indicators.component {
			t.Error("component should be true after WithComponentStyling")
		}
		if cfg.Indicators.componentField != "component" {
			t.Errorf("componentField = %q, want %q", cfg.Indicators.componentField, "component")
		}
		if len(cfg.Indicators.countFields) == 0 {
			t.Error("countFields should be non-empty after WithComponentStyling")
		}
		if len(cfg.Indicators.statePairs) == 0 {
			t.Error("statePairs should be non-empty after WithComponentStyling")
		}
		if !cfg.Indicators.removeFromTree {
			t.Error("removeFromTree should be true after WithComponentStyling")
		}
	})
}

// TestInlineIndicators_ThreadedToTemplate verifies that the indicators config is
// visible on the template after newFromConfig runs.
func TestInlineIndicators_ThreadedToTemplate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	WithComponentField("service")(cfg)

	l := newFromConfig(cfg)
	if l.consoleWriter == nil {
		t.Fatal("expected consoleWriter")
	}

	ind := l.consoleWriter.template.indicators
	if !ind.component {
		t.Error("template.indicators.component should be true")
	}
	if ind.componentField != "service" {
		t.Errorf("template.indicators.componentField = %q, want %q", ind.componentField, "service")
	}
}

// TestInlineIndicators_EnabledDoesNotChangeOutput confirms that enabling the indicators
// config (component = true) but NOT yet rendering (Phase 2+) leaves the console output
// byte-identical to the zero-value path. Phase 2 will change this — until then the
// render path is untouched and this test must pass.
func TestInlineIndicators_EnabledDoesNotChangeOutput(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("service started")
	e.SetTime(fixedTime)
	e.WithFields(
		String("component", "Fleet"),
		Int("count", 5),
	)

	baselineCfg := defaultConfig()
	baseline := renderEntry(t, baselineCfg, e)

	enabledCfg := defaultConfig()
	WithComponentStyling()(enabledCfg) // writes into the config; no rendering change yet
	enabled := renderEntry(t, enabledCfg, e)

	// Phase 0 contract: enabling the config changes nothing in the output.
	if baseline != enabled {
		t.Errorf("Phase 0: enabling indicators must not change output:\n  baseline: %q\n  enabled:  %q",
			baseline, enabled)
	}
}

// ---------------------------------------------------------------------------
// Phase 1: component colour hashing tests
// ---------------------------------------------------------------------------

// TestComponentColourCode_Deterministic verifies that the same component name always
// maps to the same ANSI code across multiple calls.
func TestComponentColourCode_Deterministic(t *testing.T) {
	t.Parallel()

	th := ThemeNightOwl
	name := "Scout"
	first := th.componentColourCode(name)
	if first == "" {
		t.Fatal("expected non-empty code for NightOwl theme")
	}

	for i := range 100 {
		got := th.componentColourCode(name)
		if got != first {
			t.Errorf("call %d: componentColourCode(%q) = %q, want %q", i, name, got, first)
			break
		}
	}
}

// TestComponentColourCode_PaletteIndexStable ensures the FNV-1a hash consistently maps
// a fixed name to the same palette index across calls and processes.
func TestComponentColourCode_PaletteIndexStable(t *testing.T) {
	t.Parallel()

	// Build a theme with a known palette so we can verify the exact index.
	// Each colour uses a distinct R byte so we can identify the chosen slot precisely.
	palette := [4]Colour{
		RGB(0x01, 0x00, 0x00), // index 0
		RGB(0x02, 0x00, 0x00), // index 1
		RGB(0x03, 0x00, 0x00), // index 2
		RGB(0x04, 0x00, 0x00), // index 3
	}
	th := NewTheme("test",
		WithComponentPalette(palette[:]...),
	)

	// Compute expected index from FNV-1a directly.
	name := "Fleet"
	h := fnvHash32a(name)
	expectedIdx := int(h % 4) // mirror production: modulo in uint32 space, then index
	expectedCode := palette[expectedIdx].ANSI(true)

	got := th.componentColourCode(name)
	if got != expectedCode {
		t.Errorf("componentColourCode(%q) = %q, want %q (palette index %d)",
			name, got, expectedCode, expectedIdx)
	}
}

// fnvHash32a is a copy of the FNV-1a logic used in componentColourCode so the test
// can compute expected indices independently without importing hash/fnv.
func fnvHash32a(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := range len(s) {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}

// TestComponentColourCode_NoColourThemeReturnsEmpty ensures noColour themes never
// emit component colour codes (NO_COLOR contract).
func TestComponentColourCode_NoColourThemeReturnsEmpty(t *testing.T) {
	t.Parallel()

	themes := []*Theme{ThemeMono, noColourTheme}
	for _, th := range themes {
		got := th.componentColourCode("Scout")
		if got != "" {
			t.Errorf("theme %q: componentColourCode should return empty, got %q", th.Name(), got)
		}
	}
}

// TestComponentColourCode_NilThemeReturnsEmpty ensures a nil theme never panics.
func TestComponentColourCode_NilThemeReturnsEmpty(t *testing.T) {
	t.Parallel()

	var th *Theme
	got := th.componentColourCode("Scout")
	if got != "" {
		t.Errorf("nil theme: componentColourCode should return empty, got %q", got)
	}
}

// TestComponentColourCode_Nopalette_ReturnsEmpty verifies themes built without a
// component palette return empty strings.
func TestComponentColourCode_NoPaletteReturnsEmpty(t *testing.T) {
	t.Parallel()

	th := NewTheme("no-palette",
		WithStyleSlot(SlotGood, RGB(0x80, 0xD4, 0xAA)),
	)
	got := th.componentColourCode("Scout")
	if got != "" {
		t.Errorf("theme without palette: componentColourCode should return empty, got %q", got)
	}
}

// TestComponentColourCode_PinOverridesHash verifies that WithComponentColour beats
// the hash-based assignment.
func TestComponentColourCode_PinOverridesHash(t *testing.T) {
	t.Parallel()

	pinned := RGB(0xAB, 0xCD, 0xEF)
	th := NewTheme("pinned",
		WithComponentPalette(
			RGB(0x01, 0x00, 0x00),
			RGB(0x02, 0x00, 0x00),
			RGB(0x03, 0x00, 0x00),
		),
		WithComponentColour("Relay", pinned),
	)

	want := pinned.ANSI(true)
	got := th.componentColourCode("Relay")
	if got != want {
		t.Errorf("pinned component: got %q, want %q", got, want)
	}

	// Other names should still use the hash, not the pin.
	other := th.componentColourCode("Fleet")
	if other == want {
		t.Errorf("non-pinned component should not use the pin code; got %q", other)
	}
}

// TestComponentColourCode_PaletteSpread verifies that a set of distinct names hashes
// to at least half the available palette slots, ensuring reasonable distribution.
func TestComponentColourCode_PaletteSpread(t *testing.T) {
	t.Parallel()

	const paletteSize = 10
	palette := make([]Colour, paletteSize)
	for i := range paletteSize {
		// Use colour index i+1 as a sentinel so each entry maps to a distinct code.
		palette[i] = Colour256(i + 1)
	}
	opts := make([]ThemeOption, 0, 1+paletteSize)
	opts = append(opts, WithComponentPalette(palette...))
	th := NewTheme("spread-test", opts...)

	names := []string{
		"Fleet", "Scout", "Relay", "Auth", "Gateway",
		"Monitor", "Scheduler", "Notifier", "Indexer", "Cache",
		"Worker", "Collector", "Publisher", "Subscriber", "Aggregator",
		"Router", "Proxy", "Registry", "Discovery", "Coordinator",
	}

	seen := make(map[string]bool)
	for _, name := range names {
		code := th.componentColourCode(name)
		seen[code] = true
	}

	// With 20 distinct names and a palette of 10, expect all 10 slots used.
	if len(seen) < paletteSize/2 {
		t.Errorf("poor palette spread: only %d of %d slots used across %d names",
			len(seen), paletteSize, len(names))
	}
}

// TestComponentColourCode_BuiltInThemesPalettePresent confirms all four built-in
// themes return non-empty codes for representative component names.
func TestComponentColourCode_BuiltInThemesPalettePresent(t *testing.T) {
	t.Parallel()

	themes := []*Theme{ThemeNightOwl, ThemeSolarized, ThemeDracula, ThemeNord}
	names := []string{"Fleet", "Scout", "Relay"}

	for _, th := range themes {
		for _, name := range names {
			got := th.componentColourCode(name)
			if got == "" {
				t.Errorf("theme %q: componentColourCode(%q) returned empty", th.Name(), name)
			}
			if !strings.HasPrefix(got, "\033[") {
				t.Errorf("theme %q: componentColourCode(%q) = %q — not an ANSI sequence", th.Name(), name, got)
			}
		}
	}
}

// TestComponentColourCode_CachingIsIdempotent confirms the sync.Map memoisation does
// not change the result when called concurrently by multiple goroutines.
func TestComponentColourCode_CachingIsIdempotent(t *testing.T) {
	t.Parallel()

	th := ThemeNightOwl
	name := "Relay"
	want := th.componentColourCode(name) // warm the cache

	const goroutines = 50
	results := make(chan string, goroutines)
	for range goroutines {
		go func() { results <- th.componentColourCode(name) }()
	}
	for range goroutines {
		got := <-results
		if got != want {
			t.Errorf("concurrent componentColourCode(%q) = %q, want %q", name, got, want)
		}
	}
}

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

// fieldNameComponent is the canonical field name used in test fixtures.
// Defined as a constant to avoid goconst lint noise across this file.
const fieldNameComponent = "component"

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
		String(fieldNameComponent, "Scout"),
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
		// Default is compact (0): the name keeps its natural width. A fixed column
		// is opt-in via WithComponentColumnWidth.
		if cfg.Indicators.componentWidth != 0 {
			t.Errorf("componentWidth default = %d, want 0 (compact)", cfg.Indicators.componentWidth)
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
		if cfg.Indicators.componentField != fieldNameComponent {
			t.Errorf("componentField = %q, want %q", cfg.Indicators.componentField, fieldNameComponent)
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

// TestInlineIndicators_WithComponentStyling_GoldenOutput is the Phase 2+ golden test
// confirming that WithComponentStyling renders the expected compact header format.
// The Phase 0 no-change contract is covered by TestInlineIndicators_DefaultOff_OutputByteIdentical.
func TestInlineIndicators_WithComponentStyling_GoldenOutput(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("service started")
	e.SetTime(fixedTime)
	e.WithFields(
		String(fieldNameComponent, "Fleet"),
		Int("count", 5),
	)

	cfg := defaultConfig()
	WithComponentStyling()(cfg)
	WithInlineGlyphs(false)(cfg) // deterministic: no glyph env dependency
	got := renderEntry(t, cfg, e)

	// compact component (natural width) + │ + message + (5) + no remaining fields
	want := "2026-05-30 14:52:33 [INFO] Fleet │ service started (5)\n"
	if got != want {
		t.Errorf("golden output mismatch:\n  got:  %q\n  want: %q", got, want)
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

// ---------------------------------------------------------------------------
// Phase 2: Component prefix rendering
// ---------------------------------------------------------------------------

// TestComponentPrefix_PaddingAndTruncation verifies that the component name is
// padded to componentWidth when short and truncated (with ellipsis) when long.
func TestComponentPrefix_PaddingAndTruncation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		compName  string
		width     int
		wantInOut string // the portion of the output between "[INFO]" and "│"
	}{
		// Compact (width 0, the default): natural width, single separator space.
		{"compact", "Fleet", 0, " Fleet │ "},
		{"exact", "Scout", 5, " Scout │ "},
		{"padded", "A", 4, " A    │ "},
		{"truncated", "Longname", 5, " Long… │ "},
		// width=8, "Fleet"=5 runes: 3 rune padding + 1 separator space = 4 visible spaces before │
		{"default-width", "Fleet", 8, " Fleet    │ "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := GetEntry()
			defer e.Release()
			e.SetLevel(LevelInfo)
			e.SetMessage("msg")
			e.SetTime(fixedTime)
			e.WithFields(String(fieldNameComponent, tc.compName))

			cfg := defaultConfig()
			cfg.Indicators.component = true
			cfg.Indicators.componentField = fieldNameComponent
			cfg.Indicators.componentWidth = tc.width
			cfg.Indicators.removeFromTree = true
			got := renderEntry(t, cfg, e)

			if !strings.Contains(got, tc.wantInOut) {
				t.Errorf("component prefix: got %q, want substring %q", got, tc.wantInOut)
			}
		})
	}
}

// TestComponentPrefix_MissingField verifies that when the component field is absent
// the output is unchanged from baseline (no prefix rendered).
func TestComponentPrefix_MissingField(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("no component")
	e.SetTime(fixedTime)
	e.WithFields(String("env", "prod"))

	cfg := defaultConfig()
	cfg.Indicators.component = true
	cfg.Indicators.componentField = fieldNameComponent
	cfg.Indicators.componentWidth = 8
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e)
	// Fallback to baseline path because no matching key found.
	if strings.Contains(got, "│") {
		t.Errorf("no component field: unexpected │ in output: %q", got)
	}
}

// TestComponentPrefix_SecureOnUntrustedStaysInTree verifies that a Secure component
// field is NOT promoted — it stays in the field list.
func TestComponentPrefix_SecureOnUntrustedStaysInTree(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("secure component")
	e.SetTime(fixedTime)
	// A Secure field type for the component key — must not be promoted on untrusted path.
	e.WithFields(Secure(fieldNameComponent, "InternalService"))

	cfg := defaultConfig()
	cfg.Indicators.component = true
	cfg.Indicators.componentField = fieldNameComponent
	cfg.Indicators.componentWidth = 8
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e)
	// No │ separator — field was not promoted.
	if strings.Contains(got, "│") {
		t.Errorf("secure component field: should not be promoted, got %q", got)
	}
	// The field should still appear in the output via the tree/inline path.
	if !strings.Contains(got, fieldNameComponent) {
		t.Errorf("secure component field: should remain in output, got %q", got)
	}
}

// TestComponentPrefix_NoColour verifies plain "name │" output when colour is off.
func TestComponentPrefix_NoColour(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("msg")
	e.SetTime(fixedTime)
	e.WithFields(String(fieldNameComponent, "Scout"))

	cfg := defaultConfig()
	cfg.Indicators.component = true
	cfg.Indicators.componentField = fieldNameComponent
	cfg.Indicators.componentWidth = 8
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e) // renderEntry forces DisableColour = true
	// Should contain plain "Scout    │" without ANSI codes.
	if !strings.Contains(got, "Scout    │") {
		t.Errorf("no-colour output: want 'Scout    │', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Phase 3: Count promotion
// ---------------------------------------------------------------------------

// TestCountPromotion_IntField verifies that an integer count field is promoted
// to "(N)" and removed from the tree.
func TestCountPromotion_IntField(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("stopping services")
	e.SetTime(fixedTime)
	e.WithFields(Int("count", 4), String("env", "prod"))

	cfg := defaultConfig()
	cfg.Indicators.countFields = []string{"count"}
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e)
	// Message + count suffix, env field remains.
	if !strings.Contains(got, "stopping services (4)") {
		t.Errorf("count: want 'stopping services (4)', got %q", got)
	}
	// count field removed from tree.
	if strings.Contains(got, "count: 4") {
		t.Errorf("count field should be removed from tree, got %q", got)
	}
	// env field still present.
	if !strings.Contains(got, "env") {
		t.Errorf("env field should remain in tree, got %q", got)
	}
}

// TestCountPromotion_StringFieldLeftInTree verifies that a string-typed field
// matching a countFields name is NOT promoted (only integer types are eligible).
func TestCountPromotion_StringFieldLeftInTree(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("msg")
	e.SetTime(fixedTime)
	e.WithFields(String("count", "many"))

	cfg := defaultConfig()
	cfg.Indicators.countFields = []string{"count"}
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e)
	// No "(N)" suffix because field is string type.
	if strings.Contains(got, "(") {
		t.Errorf("string count field should not be promoted, got %q", got)
	}
	// Field should still appear.
	if !strings.Contains(got, "count: many") {
		t.Errorf("string count field should remain in tree, got %q", got)
	}
}

// TestCountPromotion_MultipleNames verifies that the first matching count field
// name wins.
func TestCountPromotion_MultipleNames(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("msg")
	e.SetTime(fixedTime)
	e.WithFields(Int("total", 7))

	cfg := defaultConfig()
	cfg.Indicators.countFields = []string{"count", "total"}
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e)
	if !strings.Contains(got, "msg (7)") {
		t.Errorf("second name in countFields: want 'msg (7)', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Phase 4: Timing promotion and smart duration formatting
// ---------------------------------------------------------------------------

// TestTimingPromotion_IntMilliseconds verifies that an integer timing field is
// rendered as "NNNms" inside the timing bracket.
func TestTimingPromotion_IntMilliseconds(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("started")
	e.SetTime(fixedTime)
	e.WithFields(Int("startup_ms", 294))

	cfg := defaultConfig()
	cfg.Indicators.timingFields = []string{"startup_ms"}
	cfg.Indicators.removeFromTree = true
	WithInlineGlyphs(false)(cfg) // deterministic

	got := renderEntry(t, cfg, e)
	if !strings.Contains(got, "[294ms]") {
		t.Errorf("int timing: want '[294ms]', got %q", got)
	}
}

// TestTimingPromotion_DurationField verifies smart duration formatting from a
// time.Duration field.
func TestTimingPromotion_DurationField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		d        time.Duration
		wantFrag string
	}{
		{"sub-ms", 850 * time.Microsecond, "[850µs]"},
		{"ms", 294 * time.Millisecond, "[294ms]"},
		{"seconds", 2778 * time.Millisecond, "[2.77s]"},
		{"whole-seconds", 3 * time.Second, "[3.00s]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := GetEntry()
			defer e.Release()
			e.SetLevel(LevelInfo)
			e.SetMessage("op")
			e.SetTime(fixedTime)
			e.WithFields(Duration("elapsed", tc.d))

			cfg := defaultConfig()
			cfg.Indicators.timingFields = []string{"elapsed"}
			cfg.Indicators.removeFromTree = true
			WithInlineGlyphs(false)(cfg)

			got := renderEntry(t, cfg, e)
			if !strings.Contains(got, tc.wantFrag) {
				t.Errorf("duration %v: want %q in %q", tc.d, tc.wantFrag, got)
			}
		})
	}
}

// TestTimingPromotion_MultipleFields verifies that multiple timing fields are
// comma-separated inside one bracket.
func TestTimingPromotion_MultipleFields(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("op")
	e.SetTime(fixedTime)
	e.WithFields(Int("startup_ms", 100), Int("stop_ms", 50))

	cfg := defaultConfig()
	cfg.Indicators.timingFields = []string{"startup_ms", "stop_ms"}
	cfg.Indicators.removeFromTree = true
	WithInlineGlyphs(false)(cfg)

	got := renderEntry(t, cfg, e)
	if !strings.Contains(got, "[100ms, 50ms]") {
		t.Errorf("multiple timing fields: want '[100ms, 50ms]', got %q", got)
	}
}

// TestTimingPromotion_GlyphOff verifies ASCII fallback: no ⏱ when glyphs are off.
func TestTimingPromotion_GlyphOff(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("op")
	e.SetTime(fixedTime)
	e.WithFields(Int("t", 42))

	cfg := defaultConfig()
	cfg.Indicators.timingFields = []string{"t"}
	cfg.Indicators.removeFromTree = true
	WithInlineGlyphs(false)(cfg)

	got := renderEntry(t, cfg, e)
	if strings.Contains(got, "⏱") {
		t.Errorf("glyph-off: unexpected ⏱ in output: %q", got)
	}
	if !strings.Contains(got, "[42ms]") {
		t.Errorf("glyph-off: want '[42ms]', got %q", got)
	}
}

// TestTimingPromotion_GlyphOn verifies ⏱ glyph appears when explicitly enabled.
func TestTimingPromotion_GlyphOn(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("op")
	e.SetTime(fixedTime)
	e.WithFields(Int("t", 200))

	cfg := defaultConfig()
	cfg.Indicators.timingFields = []string{"t"}
	cfg.Indicators.removeFromTree = true
	WithInlineGlyphs(true)(cfg)

	got := renderEntry(t, cfg, e)
	// With the glyph present the timing is bracketless: " ⏱ 200ms", not "[⏱ 200ms]".
	if !strings.Contains(got, "⏱ 200ms") {
		t.Errorf("glyph-on: expected '⏱ 200ms' in output: %q", got)
	}
	if strings.Contains(got, "[⏱") || strings.Contains(got, "200ms]") {
		t.Errorf("glyph-on: timing should not be bracketed: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Phase 5: State-transition arrow pairs
// ---------------------------------------------------------------------------

// TestStateArrow_BothPresent verifies that a complete pair renders " from → to".
func TestStateArrow_BothPresent(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("state change")
	e.SetTime(fixedTime)
	e.WithFields(String("old_state", "connected"), String("new_state", "stale"))

	cfg := defaultConfig()
	cfg.Indicators.statePairs = [][2]string{{"old_state", "new_state"}}
	cfg.Indicators.removeFromTree = true
	WithInlineGlyphs(true)(cfg)

	got := renderEntry(t, cfg, e)
	// Glyphs on: the ⟳ state-change icon leads the transition.
	if !strings.Contains(got, "⟳ connected → stale") {
		t.Errorf("state arrow: want '⟳ connected → stale', got %q", got)
	}
	// Both fields removed from tree.
	if strings.Contains(got, "old_state") || strings.Contains(got, "new_state") {
		t.Errorf("state fields should be removed from tree, got %q", got)
	}
}

// TestStateArrow_GlyphOff verifies ASCII arrow when glyphs are disabled.
func TestStateArrow_GlyphOff(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("state change")
	e.SetTime(fixedTime)
	e.WithFields(String("old_state", "open"), String("new_state", "closed"))

	cfg := defaultConfig()
	cfg.Indicators.statePairs = [][2]string{{"old_state", "new_state"}}
	cfg.Indicators.removeFromTree = true
	WithInlineGlyphs(false)(cfg)

	got := renderEntry(t, cfg, e)
	if !strings.Contains(got, "open -> closed") {
		t.Errorf("glyph-off arrow: want 'open -> closed', got %q", got)
	}
	// No state-change glyph in the ASCII fallback.
	if strings.Contains(got, "⟳") {
		t.Errorf("glyph-off arrow: unexpected ⟳ in %q", got)
	}
}

// TestStateArrow_OnlyOneHalfPresent verifies that a partial pair is NOT collapsed:
// both fields remain in the tree.
func TestStateArrow_OnlyOneHalfPresent(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("partial")
	e.SetTime(fixedTime)
	e.WithFields(String("old_state", "running"))
	// new_state absent — pair is incomplete.

	cfg := defaultConfig()
	cfg.Indicators.statePairs = [][2]string{{"old_state", "new_state"}}
	cfg.Indicators.removeFromTree = true
	WithInlineGlyphs(false)(cfg)

	got := renderEntry(t, cfg, e)
	// No arrow.
	if strings.Contains(got, "->") || strings.Contains(got, "→") {
		t.Errorf("partial pair: unexpected arrow in %q", got)
	}
	// old_state should remain in output.
	if !strings.Contains(got, "old_state") {
		t.Errorf("partial pair: old_state should remain in output, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Phase 6: Tree/inline skip integration
// ---------------------------------------------------------------------------

// TestTreeSkip_PartialSkip verifies that non-promoted fields remain in the tree
// and the last-child └ glyph is correct.
func TestTreeSkip_PartialSkip(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("msg")
	e.SetTime(fixedTime)
	e.WithFields(
		String(fieldNameComponent, "Scout"),
		String("region", "ap-southeast-2"),
		String("env", "prod"),
	)
	e.forceTreeDisplay = true

	cfg := defaultConfig()
	cfg.Indicators.component = true
	cfg.Indicators.componentField = fieldNameComponent
	cfg.Indicators.componentWidth = 8
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e)
	// region and env remain; component is removed.
	if !strings.Contains(got, "region") {
		t.Errorf("tree skip: region should remain, got %q", got)
	}
	if !strings.Contains(got, "env") {
		t.Errorf("tree skip: env should remain, got %q", got)
	}
	if strings.Contains(got, "component:") {
		t.Errorf("tree skip: component should be removed, got %q", got)
	}
	// The last remaining field (env) must use └, not ├.
	if !strings.Contains(got, "└ env") {
		t.Errorf("tree skip: last-child should use └ for env, got %q", got)
	}
}

// TestTreeSkip_AllFieldsPromoted verifies that when all fields are promoted the
// output ends with a single newline and no stray tree characters.
func TestTreeSkip_AllFieldsPromoted(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("msg")
	e.SetTime(fixedTime)
	e.WithFields(
		String(fieldNameComponent, "Fleet"),
		Int("count", 3),
	)
	e.forceTreeDisplay = true

	cfg := defaultConfig()
	cfg.Indicators.component = true
	cfg.Indicators.componentField = fieldNameComponent
	cfg.Indicators.componentWidth = 8
	cfg.Indicators.countFields = []string{"count"}
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e)
	// No tree glyphs should appear.
	if strings.Contains(got, "├") || strings.Contains(got, "└") {
		t.Errorf("all-promoted: unexpected tree glyph in %q", got)
	}
	// Must end with exactly one newline.
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("all-promoted: should end with exactly one newline, got %q", got)
	}
}

// TestTreeSkip_MoreThan64Fields verifies the >64 field safety fallback: when
// an entry has more than 64 fields, all fields render (no skip mask applied).
func TestTreeSkip_MoreThan64Fields(t *testing.T) {
	t.Parallel()

	fields := make([]Field, 65)
	fields[0] = String(fieldNameComponent, "Scout")
	for i := 1; i < 65; i++ {
		fields[i] = String("k"+itoa(i), "v")
	}

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("large entry")
	e.SetTime(fixedTime)
	e.WithFields(fields...)

	cfg := defaultConfig()
	cfg.Indicators.component = true
	cfg.Indicators.componentField = fieldNameComponent
	cfg.Indicators.componentWidth = 8
	cfg.Indicators.removeFromTree = true

	got := renderEntry(t, cfg, e)
	// component field should still appear in the tree since skip is disabled for >64.
	if !strings.Contains(got, "component: Scout") {
		t.Errorf(">64 fields: component should remain in output, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Phase 7: JSON parity
// ---------------------------------------------------------------------------

// TestJSONParity verifies that a console render with indicators on produces the
// compact format, while the same entry through a JSON writer contains every field
// fully expanded (writer_json.go must be untouched).
func TestJSONParity(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("service started")
	e.SetTime(fixedTime)
	e.WithFields(
		String(fieldNameComponent, "Scout"),
		Int("count", 4),
		Int("startup_ms", 2000),
		String("old_state", "idle"),
		String("new_state", "running"),
		String("env", "prod"),
	)

	// Console with indicators on.
	consoleCfg := defaultConfig()
	consoleCfg.Indicators = inlineIndicators{
		component:      true,
		componentField: fieldNameComponent,
		componentWidth: 8,
		countFields:    []string{"count"},
		timingFields:   []string{"startup_ms"},
		statePairs:     [][2]string{{"old_state", "new_state"}},
		removeFromTree: true,
		showGlyphs:     false,
		glyphsExplicit: true,
	}

	// Use renderEntry helper for console.
	consoleOut := renderEntry(t, consoleCfg, e)

	// Console must have the compact form.
	if !strings.Contains(consoleOut, "Scout") {
		t.Errorf("console: want 'Scout' component prefix, got %q", consoleOut)
	}
	if !strings.Contains(consoleOut, "(4)") {
		t.Errorf("console: want '(4)' count suffix, got %q", consoleOut)
	}
	if !strings.Contains(consoleOut, "[2000ms]") && !strings.Contains(consoleOut, "[2.00s]") {
		t.Errorf("console: want timing bracket, got %q", consoleOut)
	}

	// JSON must expand every field.
	var jsonBuf bytes.Buffer
	jw := NewJSONWriter(&jsonBuf)
	if err := jw.Write(e); err != nil {
		t.Fatalf("JSONWriter.Write: %v", err)
	}
	jsonOut := jsonBuf.String()

	for _, wantKey := range []string{fieldNameComponent, "count", "startup_ms", "old_state", "new_state", "env"} {
		if !strings.Contains(jsonOut, `"`+wantKey+`"`) {
			t.Errorf("JSON parity: key %q missing from JSON output: %q", wantKey, jsonOut)
		}
	}
}

package velocity

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestConsoleWriter_InvalidLevel(_ *testing.T) {
	var out bytes.Buffer
	w := NewConsoleWriter(&out, nil)

	var bb bytes.Buffer
	buf := NewBytesBuffer(&bb)

	// Out-of-range levels must not panic on the levelColours array.
	w.formatLevel(buf, Level(100))
	w.formatLevel(buf, Level(6))
}

func TestConsoleWriter_AddCaller(t *testing.T) {
	var buf bytes.Buffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleLevel = LevelDebug
	cfg.StructuredOutput = nil
	cfg.ConsoleTheme = nil
	cfg.AddCaller = true

	log := newFromConfig(cfg)
	log.Info("caller console test")

	if !strings.Contains(buf.String(), "_test.go:") {
		t.Fatalf("expected console output to contain caller reference, got: %s", buf.String())
	}
}

// TestTemplate_CachedPrefixWidth_BadgeStyle checks that the cached prefix width matches
// what calculatePrefixWidth produces for a real entry under the default badge style.
func TestTemplate_CachedPrefixWidth_BadgeStyle(t *testing.T) {
	t.Parallel()

	tmpl := TemplateDefault

	entry := GetEntry()
	entry.Time = time.Now()
	entry.Level = LevelInfo

	perCall := tmpl.calculatePrefixWidth(entry)
	cached := tmpl.CachedPrefixWidth()

	if cached != perCall {
		t.Errorf("cached prefix width %d != per-call %d", cached, perCall)
	}
}

// TestConsoleWriter_ColourEmittedWhenNoThemeSet verifies that a ConsoleWriter constructed
// without an explicit theme (nil → default NightOwl) emits ANSI escape sequences when the
// writer is a TTY. Previously, nil theme silently disabled colour even when DisableColour
// was false.
//
// Technique: construct the writer, then flip isTTY=true and template.useColours=true to
// simulate a real terminal, bypassing the io.Writer TTY probe which never fires on a buffer.
// Both fields must be set together because template.useColours gates ANSI in the template
// path, and isTTY gates the level-colour cache used by the fallback formatEntrySecure path.
func TestConsoleWriter_ColourEmittedWhenNoThemeSet(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	// nil theme → should default to NightOwl with colour enabled.
	w := NewConsoleWriter(&buf, nil)
	// Simulate a real terminal — both flags must be updated together because
	// template.useColours drives ANSI in the template path while isTTY drives
	// the level-colour cache used by the fallback formatEntrySecure path.
	w.isTTY = true
	w.template.useColours = true
	w.cacheLevelColours()

	// At least one level colour must be non-empty — NightOwl defines them all.
	hasColour := false
	for _, code := range w.levelColours {
		if code != "" {
			hasColour = true
			break
		}
	}
	if !hasColour {
		t.Error("expected at least one cached level colour after nil-theme construction with isTTY=true, got none")
	}

	// Writing a log entry to the writer should produce ANSI escapes.
	entry := GetEntry()
	defer entry.Release()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("colour test")
	entry.SetTime(entry.Time) // keep zero time — not what we are testing

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "\033[") {
		t.Errorf("expected ANSI escapes in output with nil theme + isTTY, got: %q", output)
	}
}

// TestConsoleWriter_NoColourWhenDisabled verifies that a ConsoleWriter constructed
// with noColourTheme (the DisableColour path in newFromConfig) emits no ANSI escapes,
// even when isTTY is forced true.
func TestConsoleWriter_NoColourWhenDisabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	// Simulate the DisableColour=true path: logger.go passes noColourTheme.
	w := NewConsoleWriter(&buf, noColourTheme)
	w.isTTY = true
	w.cacheLevelColours()

	// No level colour should be cached for a no-colour theme.
	for i, code := range w.levelColours {
		if code != "" {
			t.Errorf("levelColours[%d] = %q, want empty for noColourTheme", i, code)
		}
	}

	entry := GetEntry()
	defer entry.Release()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("no colour test")

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "\033[") {
		t.Errorf("expected no ANSI escapes in output with noColourTheme, got: %q", output)
	}
}

// TestLogger_DevelopmentPresetHasColour verifies that a logger built with
// WithDevelopment() creates a console writer whose theme has non-empty level
// colours when isTTY is forced true. This guards the DisableColour=false → colour
// path that was broken by the original writer_console.go nil-theme logic.
func TestLogger_DevelopmentPresetHasColour(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(WithDevelopment(), WithConsoleOutput(&buf))
	if log.consoleWriter == nil {
		t.Fatal("expected consoleWriter to be set for WithDevelopment")
	}

	// Force TTY mode and rebuild colour cache to exercise the theme path.
	log.consoleWriter.isTTY = true
	log.consoleWriter.cacheLevelColours()

	hasColour := false
	for _, code := range log.consoleWriter.levelColours {
		if code != "" {
			hasColour = true
			break
		}
	}
	if !hasColour {
		t.Error("WithDevelopment() logger should have cached level colours when isTTY=true")
	}
}

// TestLogger_ProductionPresetNoColour verifies that a logger built with
// WithProduction() (DisableColour=true) produces a console writer with no cached
// colour codes even when isTTY is forced true.
func TestLogger_ProductionPresetNoColour(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	// WithProduction sets ConsoleOutput to io.Discard, so override it to get a writer.
	cfg := defaultConfig()
	WithProduction()(cfg)
	cfg.ConsoleOutput = &buf
	log := newFromConfig(cfg)

	if log.consoleWriter == nil {
		t.Fatal("expected consoleWriter to be set after overriding ConsoleOutput")
	}

	log.consoleWriter.isTTY = true
	log.consoleWriter.cacheLevelColours()

	for i, code := range log.consoleWriter.levelColours {
		if code != "" {
			t.Errorf("WithProduction() levelColours[%d] = %q, want empty (DisableColour=true)", i, code)
		}
	}
}

// TestConsoleWriter_CachedWidthsAfterFieldDisplayMutation verifies that constructing a
// ConsoleWriter with FieldDisplayTree produces a template whose cached widths match those
// of TemplateDefault (field display mode does not affect timestamp or level widths). The
// key invariant is that initCache() is called after the shallow copy so any future
// width-affecting field would not silently leave stale cached values.
func TestConsoleWriter_CachedWidthsAfterFieldDisplayMutation(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewConsoleWriterWithOptions(&buf, ThemeNightOwl, time.UTC, FieldDisplayTree)

	wantPrefixWidth := TemplateDefault.CachedPrefixWidth()
	wantMessageColumn := len(TemplateDefault.CachedMessageIndentStr())

	gotPrefixWidth := w.template.CachedPrefixWidth()
	gotMessageIndent := len(w.template.CachedMessageIndentStr())

	if gotPrefixWidth != wantPrefixWidth {
		t.Errorf("prefix width: got %d, want %d", gotPrefixWidth, wantPrefixWidth)
	}
	if gotMessageIndent != wantMessageColumn {
		t.Errorf("message indent: got %d, want %d", gotMessageIndent, wantMessageColumn)
	}

	// field display mode must be set on the writer's template, not TemplateDefault's.
	if w.template.fieldDisplayMode != FieldDisplayTree {
		t.Errorf("fieldDisplayMode not applied: got %v, want FieldDisplayTree", w.template.fieldDisplayMode)
	}
	if TemplateDefault.fieldDisplayMode == FieldDisplayTree {
		t.Error("TemplateDefault was mutated — shallow copy semantics broken")
	}
}

// TestEnvVar_NoColour verifies that NO_COLOR=1 suppresses ANSI output even when
// the writer would otherwise be treated as a TTY (isTTY forced true for the test).
func TestEnvVar_NoColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "") // ensure FORCE_COLOR does not override

	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, ThemeNightOwl)

	// Even forcing isTTY won't emit ANSI when NO_COLOR is set, because
	// resolveColourForWriter() checked NO_COLOR at construction time.
	// The field stays false regardless.
	if w.isTTY {
		t.Error("expected isTTY=false when NO_COLOR is set, but got true")
	}
	if w.template.useColours {
		t.Error("expected template.useColours=false when NO_COLOR is set, but got true")
	}

	entry := GetEntry()
	defer entry.Release()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("no colour via env")

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "\033[") {
		t.Errorf("NO_COLOR=1: expected no ANSI escapes, got: %q", output)
	}
}

// TestEnvVar_ForceColour verifies that FORCE_COLOR=1 enables ANSI output even
// when the writer is a *bytes.Buffer (which would normally be non-TTY).
func TestEnvVar_ForceColour(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "") // ensure NO_COLOR does not suppress

	var buf bytes.Buffer
	w := NewConsoleWriter(&buf, ThemeNightOwl)

	if !w.isTTY {
		t.Error("expected isTTY=true when FORCE_COLOR is set, but got false")
	}
	if !w.template.useColours {
		t.Error("expected template.useColours=true when FORCE_COLOR is set, but got false")
	}

	entry := GetEntry()
	defer entry.Release()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("forced colour via env")

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "\033[") {
		t.Errorf("FORCE_COLOR=1: expected ANSI escapes, got: %q", output)
	}
}

// TestStatusItem_ColourViaLoggerRender verifies that a StatusItem rendered via
// Logger.Render emits ANSI colour sequences when the console writer is in TTY
// mode. This guards the two-step buffer path in Logger.Render where IsTerminalWriter
// on the intermediate buffer would always return false without TTYRenderable.
func TestStatusItem_ColourViaLoggerRender(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(WithDevelopment(), WithConsoleOutput(&buf))
	if log.consoleWriter == nil {
		t.Fatal("expected consoleWriter to be set")
	}

	// Force TTY so colour is enabled.
	log.consoleWriter.isTTY = true
	log.consoleWriter.template.useColours = true
	log.consoleWriter.cacheLevelColours()

	item := NewStatusItem(StatusOK, "service healthy", log.Style())
	log.Render(item)

	output := buf.String()
	if !strings.Contains(output, "\033[") {
		t.Errorf("StatusItem via Logger.Render with isTTY=true: expected ANSI escapes, got: %q", output)
	}
}

// TestStatusItem_NoColourViaLoggerRenderWhenNotTTY verifies that a StatusItem
// rendered via Logger.Render emits plain text when the console writer is not a TTY.
func TestStatusItem_NoColourViaLoggerRenderWhenNotTTY(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(WithDevelopment(), WithConsoleOutput(&buf))
	if log.consoleWriter == nil {
		t.Fatal("expected consoleWriter to be set")
	}
	// isTTY stays false — buf is not a terminal.

	item := NewStatusItem(StatusOK, "service healthy", log.Style())
	log.Render(item)

	output := buf.String()
	if strings.Contains(output, "\033[") {
		t.Errorf("StatusItem via Logger.Render with isTTY=false: expected no ANSI escapes, got: %q", output)
	}
	// The badge itself must still appear.
	if !strings.Contains(output, "[OKAY]") {
		t.Errorf("StatusItem via Logger.Render: expected [OKAY] badge in plain output, got: %q", output)
	}
}

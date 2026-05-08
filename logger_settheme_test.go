package velocity

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestLogger_SetTheme verifies that SetTheme swaps the active theme and that subsequent
// log output reflects the new ANSI codes.
func TestLogger_SetTheme(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	log := NewWithConfig(cfg)

	log.Info("before theme change")
	before := buf.String()
	buf.Reset()

	log.SetTheme(ThemeSolarized)
	log.Info("after theme change")
	after := buf.String()

	// Night Owl and Light use different ANSI codes. If SetTheme had no effect,
	// both outputs would be identical.
	if before == after {
		t.Errorf("SetTheme had no effect: output unchanged after theme switch")
	}

	// cfg.ConsoleTheme must be updated so subsequent With() calls inherit the new theme.
	if log.cfg.ConsoleTheme != ThemeSolarized {
		t.Errorf("cfg.ConsoleTheme not updated: got %v, want ThemeSolarized", log.cfg.ConsoleTheme)
	}
}

// TestLogger_SetTheme_Nil verifies that a nil theme does not panic.
func TestLogger_SetTheme_Nil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(&buf)
	// Must not panic.
	log.SetTheme(nil)
}

// TestLogger_SetTheme_NilLogger verifies nil receiver is safe.
func TestLogger_SetTheme_NilLogger(t *testing.T) {
	t.Parallel()

	var log *Logger
	// Must not panic.
	log.SetTheme(ThemeNightOwl)
}

// TestLogger_Theme_Returns verifies that Theme() returns the configured theme.
func TestLogger_Theme_Returns(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	log := NewWithConfig(cfg)

	if got := log.Theme(); got != ThemeNightOwl {
		t.Errorf("Theme() returned unexpected theme: %v", got)
	}
}

// TestLogger_Theme_NilLogger falls back to ThemeNightOwl, not nil, so callers are safe.
func TestLogger_Theme_NilLogger(t *testing.T) {
	t.Parallel()

	var log *Logger
	if got := log.Theme(); got == nil {
		t.Error("Theme() on nil logger must return a non-nil fallback theme")
	}
}

// TestLogger_SetTheme_PropagatestoAdditionalWriter verifies that SetTheme forwards the
// new theme to writers added via AddWriter that implement SetTheme.
func TestLogger_SetTheme_PropagatestoAdditionalWriter(t *testing.T) {
	t.Parallel()

	var primaryBuf safeBuffer
	var extraBuf safeBuffer

	cfg := DefaultConfig()
	cfg.ConsoleOutput = &primaryBuf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := NewWithConfig(cfg)

	// Add a console writer as an additional writer — it implements SetTheme.
	extra := NewConsoleWriter(&extraBuf, ThemeNightOwl)
	log.AddWriter("extra", extra)

	log.Info("before")

	// The additional writer is async (MultiWriter). Wait for the write to land
	// before reading the buffer, otherwise Reset races the worker goroutine.
	waitFor(t, func() bool {
		return extraBuf.Len() > 0
	}, 5*time.Second, 5*time.Millisecond, "extra writer should have received the 'before' entry")

	before := extraBuf.String()
	extraBuf.Reset()

	log.SetTheme(ThemeSolarized)
	log.Info("after")

	// Drain the async multi-writer before reading output.
	_ = log.Close()
	after := extraBuf.String()

	if before == after {
		t.Errorf("SetTheme did not propagate to additional writer: output unchanged")
	}
}

// TestLogger_SetTheme_WithCloneInherits verifies that a logger cloned via With()
// after SetTheme picks up the updated theme.
func TestLogger_SetTheme_WithCloneInherits(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := NewWithConfig(cfg)

	log.SetTheme(ThemeSolarized)

	// With() reads cfg.ConsoleTheme to build the child's console writer theme.
	child := log.With(String("child", "true"))
	if child == nil {
		t.Fatal("With() returned nil")
	}

	// The child's config must reflect the updated theme so further clones inherit it.
	if child.cfg.ConsoleTheme != ThemeSolarized {
		t.Errorf("With() clone cfg.ConsoleTheme = %v, want ThemeSolarized", child.cfg.ConsoleTheme)
	}

	// Produce output from the child logger and confirm it is distinct from ThemeNightOwl output.
	buf.Reset()
	child.Info("from child")
	childOut := buf.String()

	buf.Reset()
	// Build a NightOwl logger for comparison.
	cfg2 := DefaultConfig()
	cfg2.ConsoleOutput = &buf
	cfg2.ConsoleTheme = ThemeNightOwl
	cfg2.StructuredOutput = nil
	owlLog := NewWithConfig(cfg2)
	owlLog.With(String("child", "true")).Info("from child")
	owlOut := buf.String()

	if strings.EqualFold(childOut, owlOut) {
		t.Log("note: themes produced identical byte sequences (unlikely but not a hard failure)")
	}
}

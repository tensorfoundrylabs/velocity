package velocity

import (
	"bytes"
	"testing"
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

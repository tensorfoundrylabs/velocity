package velocity

import (
	"os"
	"testing"
)

func TestIntegration(_ *testing.T) {
	log := New(WithDevelopment())

	log.Debug("Debug message")
	log.Info("Info message")
	log.Warn("Warning message")
	log.Error("Error message")

	log.Info(
		"Server started",
		String("addr", ":8080"),
		Int("pid", os.Getpid()),
		Bool("tls", true),
	)

	themes := []*Theme{
		ThemeNightOwl,
		ThemeSolarized,
		ThemeDracula,
		ThemeNord,
	}

	for _, theme := range themes {
		log := New(
			WithConsoleOutput(os.Stdout),
			WithTheme(theme),
			WithLevel(LevelInfo),
		)
		log.Info("Testing theme", String("theme", theme.Name()))
	}
}

func TestMultiWriterIntegration(t *testing.T) {
	multiWriter := NewMultiWriter()

	if multiWriter == nil {
		t.Fatal("Failed to create multi-writer")
	}
}

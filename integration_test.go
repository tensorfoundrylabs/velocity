package velocity

import (
	"os"
	"testing"
)

func TestIntegration(_ *testing.T) {
	// Test basic development logger
	log := NewDevelopment()

	log.Debug("Debug message")
	log.Info("Info message")
	log.Warn("Warning message")
	log.Error("Error message")

	// Test with fields
	log.Info("Server started",
		String("addr", ":8080"),
		Int("pid", os.Getpid()),
		Bool("tls", true),
	)

	// Test themes
	themes := []*Theme{
		ThemeNightOwl,
		ThemeSolarized,
		ThemeDracula,
		ThemeNord,
	}

	for _, theme := range themes {
		log := NewWithOptions(
			WithConsoleOutput(os.Stdout),
			WithTheme(theme),
			WithLevel(LevelInfo),
		)
		log.Info("Testing theme", String("theme", theme.Name))
	}
}

func TestMultiWriterIntegration(t *testing.T) {
	// Multi-writer enables simultaneous output to different destinations,
	// essential for maintaining human-readable console logs while preserving
	// structured data for monitoring systems
	multiWriter := NewMultiWriter()

	if multiWriter == nil {
		t.Fatal("Failed to create multi-writer")
	}
}

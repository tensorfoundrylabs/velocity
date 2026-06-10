package velocity

import (
	"bytes"
	"strings"
	"testing"
)

// TestWithLevels_SetsBothFields verifies that applying WithLevels sets both
// ConsoleLevel and StructuredLevel on the underlying config.
func TestWithLevels_SetsBothFields(t *testing.T) {
	t.Parallel()

	log := New(WithDevelopment(), WithLevels(LevelWarn))

	if log.cfg.ConsoleLevel != LevelWarn {
		t.Errorf("ConsoleLevel: want %v, got %v", LevelWarn, log.cfg.ConsoleLevel)
	}
	if log.cfg.StructuredLevel != LevelWarn {
		t.Errorf("StructuredLevel: want %v, got %v", LevelWarn, log.cfg.StructuredLevel)
	}
}

// TestWithLevels_GatesConsoleOutput verifies that a sub-threshold entry does not
// appear in console output after WithLevels raises the threshold.
func TestWithLevels_GatesConsoleOutput(t *testing.T) {
	t.Parallel()

	var console bytes.Buffer
	log := New(
		WithDevelopment(),
		WithConsoleOutput(&console),
		WithLevels(LevelWarn),
	)
	defer func() { _ = log.Close() }()

	log.Info("should be suppressed")
	log.Warn("should appear")

	out := console.String()
	if strings.Contains(out, "should be suppressed") {
		t.Errorf("Info entry was not gated by WithLevels(Warn) on console: %s", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Errorf("Warn entry missing from console output: %s", out)
	}
}

// TestWithLevels_GatesStructuredOutput verifies that a sub-threshold entry does
// not reach structured (JSON) output after WithLevels raises the threshold.
func TestWithLevels_GatesStructuredOutput(t *testing.T) {
	t.Parallel()

	var structured bytes.Buffer
	log := New(
		WithProduction(),
		WithStructuredOutput(&structured),
		WithLevels(LevelWarn),
	)
	defer func() { _ = log.Close() }()

	log.Info("should be suppressed")
	log.Warn("should appear")

	out := structured.String()
	if strings.Contains(out, "should be suppressed") {
		t.Errorf("Info entry was not gated by WithLevels(Warn) on structured output: %s", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Errorf("Warn entry missing from structured output: %s", out)
	}
}

// TestWithLevels_ProductionBothLevels is the canonical footgun scenario: a
// production logger where both thresholds must move together.
func TestWithLevels_ProductionBothLevels(t *testing.T) {
	t.Parallel()

	log := New(WithProduction(), WithLevels(LevelWarn))

	if log.cfg.ConsoleLevel != LevelWarn {
		t.Errorf("ConsoleLevel: want Warn, got %v", log.cfg.ConsoleLevel)
	}
	if log.cfg.StructuredLevel != LevelWarn {
		t.Errorf("StructuredLevel: want Warn, got %v", log.cfg.StructuredLevel)
	}
}

// TestWithLevel_OnlyConsole confirms that the existing WithLevel behaviour is
// unchanged and does NOT affect StructuredLevel — guards against an accidental
// regression from adding WithLevels.
func TestWithLevel_OnlyConsole(t *testing.T) {
	t.Parallel()

	// WithProduction sets StructuredLevel = LevelInfo; then WithLevel should only
	// move ConsoleLevel, leaving StructuredLevel untouched.
	log := New(WithProduction(), WithLevel(LevelDebug))

	if log.cfg.ConsoleLevel != LevelDebug {
		t.Errorf("ConsoleLevel: want Debug, got %v", log.cfg.ConsoleLevel)
	}
	if log.cfg.StructuredLevel != LevelInfo {
		t.Errorf("StructuredLevel: WithLevel should not change structured level; want Info, got %v", log.cfg.StructuredLevel)
	}
}

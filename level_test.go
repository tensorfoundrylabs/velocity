package velocity

import (
	"testing"
)

func TestMustParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"DeBuG", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"WARN", LevelWarn},
		{"warning", LevelWarn},
		{"WARNING", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"fatal", LevelFatal},
		{"FATAL", LevelFatal},
		{"off", LevelOff},
		{"OFF", LevelOff},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := MustParseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("MustParseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}

	// Test that invalid inputs cause panics
	panicTests := []string{
		"invalid",
		"",
		"unknown",
		"trace",
	}

	for _, input := range panicTests {
		t.Run("panic_"+input, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("MustParseLevel(%q) did not panic", input)
				}
			}()
			MustParseLevel(input)
		})
	}
}

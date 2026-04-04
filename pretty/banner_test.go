package pretty

import (
	"bytes"
	"strings"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity"
)

func TestBanner_SingleLine(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, velocity.ThemeNightOwl)

	p.Banner("Hello World")

	output := buf.String()
	// ANSI codes interfere with visual width calculations in tests
	cleanOutput := removeANSI(output)

	lines := strings.Split(strings.TrimSpace(cleanOutput), "\n")

	if len(lines) != 3 {
		t.Errorf("Expected 3 lines, got %d", len(lines))
	}

	// Rune count ensures correct width for multi-byte UTF-8 characters
	widths := make(map[int]bool)
	for _, line := range lines {
		widths[len([]rune(line))] = true
	}
	if len(widths) > 1 {
		t.Errorf("Lines have different widths (rune count): %v", lines)
		for i, line := range lines {
			t.Logf("Line %d: %d runes", i, len([]rune(line)))
		}
	}
}

func TestBanner_MultiLine(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, velocity.ThemeNightOwl)

	p.Banner("First line\nSecond line\nThird line")

	output := buf.String()
	cleanOutput := removeANSI(output)

	lines := strings.Split(strings.TrimSpace(cleanOutput), "\n")

	// Should have: top border + 3 content lines + bottom border = 5 lines
	if len(lines) != 5 {
		t.Errorf("Expected 5 lines, got %d\nOutput:\n%s", len(lines), cleanOutput)
	}
}

func removeANSI(s string) string {
	// State machine parsing preserves UTF-8 while stripping terminal control codes
	var result strings.Builder
	i := 0
	bs := []byte(s)

	for i < len(bs) {
		if i < len(bs)-1 && bs[i] == '\x1b' && bs[i+1] == '[' {
			// Skip until we find 'm'
			i += 2
			for i < len(bs) && bs[i] != 'm' {
				i++
			}
			i++ // skip the 'm'
		} else {
			_ = result.WriteByte(bs[i])
			i++
		}
	}
	return result.String()
}

func TestBanner_EmptyLine(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, velocity.ThemeNightOwl)

	p.Banner("")

	output := buf.String()
	cleanOutput := removeANSI(output)

	lines := strings.Split(strings.TrimSpace(cleanOutput), "\n")

	if len(lines) != 3 {
		t.Errorf("Expected 3 lines, got %d", len(lines))
	}
}

func TestBanner_VaryingLineLengths(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, velocity.ThemeNightOwl)

	p.Banner("Short\nThis is a much longer line\nMid")

	output := buf.String()
	cleanOutput := removeANSI(output)

	lines := strings.Split(strings.TrimSpace(cleanOutput), "\n")

	// Should have: top border + 3 content lines + bottom border = 5 lines
	if len(lines) != 5 {
		t.Errorf("Expected 5 lines, got %d", len(lines))
	}

	// All lines should have same width
	widths := make(map[int]bool)
	for _, line := range lines {
		widths[len([]rune(line))] = true
	}
	if len(widths) > 1 {
		t.Errorf("Lines have different widths")
		for i, line := range lines {
			t.Logf("Line %d: %d runes - %q", i, len([]rune(line)), line)
		}
	}
}

func TestBanner_TrailingWhitespace(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, velocity.ThemeNightOwl)

	// Test with trailing spaces (simulating ASCII art logo issue)
	// The trailing spaces should be stripped, making the box tight
	banner := "SERVICE                    \nTestApp v1.0.0 | Test Platform          "

	p.Banner(banner)

	output := buf.String()
	cleanOutput := removeANSI(output)

	lines := strings.Split(strings.TrimSpace(cleanOutput), "\n")

	// Should have: top border + 2 content lines + bottom border = 4 lines
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines, got %d", len(lines))
	}

	// All lines should have same width
	widths := make(map[int]bool)
	for _, line := range lines {
		widths[len([]rune(line))] = true
	}
	if len(widths) > 1 {
		t.Errorf("Lines have different widths")
		for i, line := range lines {
			t.Logf("Line %d: %d runes - %q", i, len([]rune(line)), line)
		}
	}

	// The box should be sized to "TestApp v1.0.0 | Test Platform" (30 chars) + 4 for borders = 34
	expectedWidth := 34
	actualWidth := len([]rune(lines[0]))
	if actualWidth != expectedWidth {
		t.Errorf("Expected box width of %d runes, got %d", expectedWidth, actualWidth)
		t.Logf("Top border: %q", lines[0])
	}
}

func TestBanner_Unicode(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, velocity.ThemeNightOwl)

	// Test with Unicode characters (like the ASCII art logo)
	banner := "███████╗ ██████╗\n██╔════╝██╔════╝"

	p.Banner(banner)

	output := buf.String()
	cleanOutput := removeANSI(output)

	lines := strings.Split(strings.TrimSpace(cleanOutput), "\n")

	// Should have: top border + 2 content lines + bottom border = 4 lines
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines, got %d", len(lines))
	}

	// All lines should have same width (in runes)
	widths := make(map[int]bool)
	for _, line := range lines {
		widths[len([]rune(line))] = true
	}
	if len(widths) > 1 {
		t.Errorf("Lines have different widths")
		for i, line := range lines {
			t.Logf("Line %d: %d runes, %d bytes - %q", i, len([]rune(line)), len(line), line)
		}
	}
}

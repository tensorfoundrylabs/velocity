package pretty_test

import (
	"bytes"
	"strings"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity"
)

// borderLen returns the visible character count of a line stripped of ANSI codes.
// Box borders use multi-byte UTF-8 box-drawing characters, so we count runes.
func borderLen(line string) int {
	return len([]rune(removeANSI(line)))
}

func TestBox_LongTitle(t *testing.T) {
	t.Helper()
	buf := &bytes.Buffer{}
	p := velocity.NewPretty(buf, velocity.ThemeNightOwl)

	// Title longer than 36 bytes with short content previously caused a panic
	// in strings.Repeat when the repeat count went negative.
	p.Box("This title is deliberately longer than forty characters total", "short")
}

func TestBox_BorderAlignment(t *testing.T) {
	buf := &bytes.Buffer{}
	p := velocity.NewPretty(buf, velocity.ThemeNightOwl)

	p.Box("Title", "Some content line")

	output := buf.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	// Expect at least top border, one content row, and bottom border.
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	topLen := borderLen(lines[0])
	bottomLen := borderLen(lines[len(lines)-1])

	if topLen != bottomLen {
		t.Errorf("top border length %d != bottom border length %d", topLen, bottomLen)
		t.Logf("top:    %q", removeANSI(lines[0]))
		t.Logf("bottom: %q", removeANSI(lines[len(lines)-1]))
	}
}

func TestBox_EmptyContent(t *testing.T) {
	t.Helper()
	buf := &bytes.Buffer{}
	p := velocity.NewPretty(buf, velocity.ThemeNightOwl)

	// Must not panic; empty content produces only borders.
	p.Box("Title", "")
}

// TestBox_EmptyLines verifies that blank lines inside content are rendered as empty padded rows,
// not silently dropped.
func TestBox_EmptyLines(t *testing.T) {
	buf := &bytes.Buffer{}
	p := velocity.NewPretty(buf, velocity.ThemeNightOwl)

	p.Box("Title", "line1\n\nline3")

	output := removeANSI(buf.String())
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	// Expect top border + 3 content rows (line1, empty, line3) + bottom border = 5 lines.
	if len(lines) != 5 {
		t.Errorf("expected 5 lines (top + 3 content + bottom), got %d:\n%s", len(lines), output)
	}
}

// TestBox_Unicode verifies that box borders align correctly when content contains multi-byte runes.
func TestBox_Unicode(t *testing.T) {
	buf := &bytes.Buffer{}
	p := velocity.NewPretty(buf, velocity.ThemeNightOwl)

	p.Box("", "日本語\nASCII\nCafe")

	output := buf.String()
	rawLines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	if len(rawLines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(rawLines))
	}

	topLen := borderLen(rawLines[0])
	bottomLen := borderLen(rawLines[len(rawLines)-1])

	if topLen != bottomLen {
		t.Errorf("top border length %d != bottom border length %d with Unicode content", topLen, bottomLen)
		t.Logf("top:    %q", removeANSI(rawLines[0]))
		t.Logf("bottom: %q", removeANSI(rawLines[len(rawLines)-1]))
	}

	// All content rows must have the same visible width as the borders.
	for i, line := range rawLines[1 : len(rawLines)-1] {
		rowLen := borderLen(line)
		if rowLen != topLen {
			t.Errorf("content row %d length %d != border length %d: %q", i+1, rowLen, topLen, removeANSI(line))
		}
	}
}

func TestBox_EmptyTitle(t *testing.T) {
	buf := &bytes.Buffer{}
	p := velocity.NewPretty(buf, velocity.ThemeNightOwl)

	// Must not panic; empty title uses plain border.
	p.Box("", "Some content")

	output := buf.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	topLen := borderLen(lines[0])
	bottomLen := borderLen(lines[len(lines)-1])

	if topLen != bottomLen {
		t.Errorf("top border length %d != bottom border length %d with empty title", topLen, bottomLen)
		t.Logf("top:    %q", removeANSI(lines[0]))
		t.Logf("bottom: %q", removeANSI(lines[len(lines)-1]))
	}
}

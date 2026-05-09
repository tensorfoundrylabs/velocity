package velocity

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// --- stripOSC8 ---

func TestStripOSC8_NoSequence(t *testing.T) {
	t.Parallel()

	input := "http://localhost:8080"
	got := stripOSC8(input)
	if got != input {
		t.Errorf("no-op string modified: got %q, want %q", got, input)
	}
}

func TestStripOSC8_SingleSequence(t *testing.T) {
	t.Parallel()

	// OSC 8: \x1b]8;;<uri>\x07<text>\x1b]8;;\x07
	input := "\x1b]8;;http://localhost:8080\x07click here\x1b]8;;\x07"
	got := stripOSC8(input)
	want := "click here"
	if got != want {
		t.Errorf("stripOSC8() = %q, want %q", got, want)
	}
}

func TestStripOSC8_TextAroundSequence(t *testing.T) {
	t.Parallel()

	input := "Visit " + "\x1b]8;;http://example.com\x07example.com\x1b]8;;\x07" + " for details"
	got := stripOSC8(input)
	want := "Visit example.com for details"
	if got != want {
		t.Errorf("stripOSC8() = %q, want %q", got, want)
	}
}

func TestStripOSC8_MultipleSequences(t *testing.T) {
	t.Parallel()

	a := "\x1b]8;;http://a.com\x07link-a\x1b]8;;\x07"
	b := "\x1b]8;;http://b.com\x07link-b\x1b]8;;\x07"
	input := a + " and " + b
	got := stripOSC8(input)
	want := "link-a and link-b"
	if got != want {
		t.Errorf("stripOSC8() = %q, want %q", got, want)
	}
}

func TestStripOSC8_Empty(t *testing.T) {
	t.Parallel()

	if got := stripOSC8(""); got != "" {
		t.Errorf("empty input gave %q", got)
	}
}

// --- ContinuationBlock.Render / String ---

func TestContinuationBlockNilReceiver(t *testing.T) {
	t.Parallel()

	var c *ContinuationBlock
	if s := c.String(); s != "" {
		t.Errorf("nil.String() = %q, want empty", s)
	}
	var buf bytes.Buffer
	if err := c.Render(&buf); err != nil {
		t.Errorf("nil.Render() error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("nil.Render() wrote bytes: %q", buf.String())
	}
}

func TestContinuationBlockNoLines(t *testing.T) {
	t.Parallel()

	c := NewContinuationBlock("HTTP server listening", nil, ThemeMono, false)
	out := c.String()

	// Only the header line should be present.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line for no-lines block, got %d: %q", len(lines), out)
	}
	if !strings.Contains(out, "HTTP server listening") {
		t.Errorf("expected message in output, got: %q", out)
	}
	// No glyph should appear.
	if strings.Contains(out, continuationGlyph) {
		t.Errorf("expected no glyph for empty lines, got: %q", out)
	}
}

func TestContinuationBlockMultipleLines(t *testing.T) {
	t.Parallel()

	lines := []string{
		"Available at http://localhost:8080",
		"Press Ctrl+C to stop",
	}
	c := NewContinuationBlock("HTTP server listening", lines, ThemeMono, false)
	out := c.String()

	// Header + 2 continuation lines.
	got := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(got) != 3 {
		t.Errorf("expected 3 lines, got %d: %q", len(got), out)
	}

	// Each continuation line must start with the glyph+space.
	for _, line := range lines {
		want := continuationGlyphSep + line
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %q", want, out)
		}
	}
}

func TestContinuationBlockEmptyLinePreserved(t *testing.T) {
	t.Parallel()

	c := NewContinuationBlock("msg", []string{"first", "", "last"}, ThemeMono, false)
	out := c.String()

	// Empty string still gets the glyph prefix (as an empty continuation).
	if !strings.Contains(out, continuationGlyphSep+"\n") {
		t.Errorf("expected empty continuation line with glyph, got: %q", out)
	}
}

func TestContinuationBlockTTYUsesGlyph(t *testing.T) {
	t.Parallel()

	c := NewContinuationBlock("msg", []string{"line one"}, ThemeMono, true)
	out := c.String()

	if !strings.Contains(out, continuationGlyph) {
		t.Errorf("TTY render missing glyph: %q", out)
	}
	if !strings.Contains(out, "line one") {
		t.Errorf("TTY render missing line text: %q", out)
	}
}

// --- TTY indent alignment ---

// TestLoggerContinueTTYIndent verifies that continuation lines land at the
// message column using tmpl.CachedMessageIndentStr(). The column is determined
// by: RFC3339 timestamp + space + level badge (6 chars "[INFO]") + space.
// We assert that each continuation line has at least that many leading spaces.
func TestLoggerContinueTTYIndent(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(&buf),
		WithColour(false),
	)

	log.Continue(LevelInfo, "HTTP server listening",
		"Available at http://localhost:8080",
		"Press Ctrl+C to stop",
	)

	out := buf.String()
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, continuationGlyph) {
			// The glyph line must have leading whitespace (the message-column indent).
			if !strings.HasPrefix(line, " ") {
				t.Errorf("continuation line missing indent: %q", line)
			}
		}
	}
}

// --- JSON output ---

func TestLoggerContinueJSON(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(&buf),
	)

	log.Continue(LevelInfo, "HTTP server listening",
		"Available at http://localhost:8080",
		"Press Ctrl+C to stop",
	)

	out := buf.String()
	// Single JSON line.
	jsonLines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(jsonLines) != 1 {
		t.Errorf("expected one JSON line, got %d: %q", len(jsonLines), out)
	}

	if !strings.Contains(out, `"continuation":[`) {
		t.Errorf("expected continuation array in JSON: %q", out)
	}
	if !strings.Contains(out, `"message":"HTTP server listening"`) {
		t.Errorf("expected message field in JSON: %q", out)
	}
	if !strings.Contains(out, `"Available at http://localhost:8080"`) {
		t.Errorf("expected first continuation line in JSON: %q", out)
	}
}

// --- OSC 8 sequences stripped from JSON ---

func TestLoggerContinueJSONStripsOSC8(t *testing.T) {
	t.Parallel()

	osc8Link := "\x1b]8;;http://localhost:8080\x07http://localhost:8080\x1b]8;;\x07"

	var buf safeBuffer
	log := New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(&buf),
	)

	log.Continue(LevelInfo, "Server ready", osc8Link)

	out := buf.String()
	// Raw ESC byte must not appear in JSON output.
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("OSC 8 escape leaked into JSON: %q", out)
	}
	// The plain text of the link must be preserved.
	if !strings.Contains(out, "http://localhost:8080") {
		t.Errorf("expected plain URL text in JSON: %q", out)
	}
}

// --- OSC 8 preserved in console output ---

func TestLoggerContinueConsolePreservesOSC8(t *testing.T) {
	t.Parallel()

	osc8Link := "\x1b]8;;http://localhost:8080\x07http://localhost:8080\x1b]8;;\x07"

	var buf safeBuffer
	log := New(
		WithConsoleOutput(&buf),
		WithColour(false),
	)

	log.Continue(LevelInfo, "Server ready", osc8Link)

	out := buf.String()
	// The OSC 8 sequence must be passed through unchanged for the terminal to render.
	if !strings.Contains(out, "\x1b]8;;") {
		t.Errorf("OSC 8 sequence missing from console output: %q", out)
	}
}

// --- Logger.Continue: nil logger ---

func TestLoggerContinueNil(t *testing.T) {
	t.Parallel()

	// Must not panic.
	var log *Logger
	log.Continue(LevelInfo, "should not panic", "line one")
}

// --- Logger.Continue: level filtering ---

func TestLoggerContinueLevelFilter(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithStructuredOutput(&buf),
		WithLevel(LevelError),
		WithStructuredLevel(LevelError),
	)

	log.Continue(LevelInfo, "this should be filtered", "line one")

	if out := buf.String(); out != "" {
		t.Errorf("expected no output when filtered, got: %q", out)
	}
}

// --- Logger.Continue: routes through console and JSON ---

func TestLoggerContinueBothWriters(t *testing.T) {
	t.Parallel()

	var consoleBuf, jsonBuf safeBuffer
	log := New(
		WithConsoleOutput(&consoleBuf),
		WithColour(false),
		WithStructuredOutput(&jsonBuf),
	)

	log.Continue(LevelInfo, "startup complete",
		"listening on :8080",
		"metrics on :9090",
	)

	// Console should have the header and both continuation lines.
	console := consoleBuf.String()
	if !strings.Contains(console, "startup complete") {
		t.Errorf("console missing message: %q", console)
	}
	if !strings.Contains(console, "listening on :8080") {
		t.Errorf("console missing first line: %q", console)
	}

	// JSON should have the continuation array.
	json := jsonBuf.String()
	if !strings.Contains(json, `"continuation":[`) {
		t.Errorf("JSON missing continuation array: %q", json)
	}
	if !strings.Contains(json, `"metrics on :9090"`) {
		t.Errorf("JSON missing second continuation line: %q", json)
	}
}

// --- continuationLinesField / continuationLinesFromField roundtrip ---

func TestContinuationLinesFieldRoundtrip(t *testing.T) {
	t.Parallel()

	lines := []string{"alpha", "beta", ""}

	f := continuationLinesField(lines)
	if f.Type != FieldTypeContinuationLines {
		t.Fatalf("expected FieldTypeContinuationLines, got %v", f.Type)
	}

	got := continuationLinesFromField(f)
	if len(got) != len(lines) {
		t.Fatalf("roundtrip length mismatch: got %d, want %d", len(got), len(lines))
	}
	for i, line := range lines {
		if got[i] != line {
			t.Errorf("lines[%d] = %q, want %q", i, got[i], line)
		}
	}
}

func TestContinuationLinesFromFieldWrongType(t *testing.T) {
	t.Parallel()

	f := String("key", "val")
	if got := continuationLinesFromField(f); got != nil {
		t.Errorf("expected nil for non-ContinuationLines field, got %v", got)
	}
}

// --- Render parity: TTY and non-TTY both have all lines ---

func TestContinuationBlockRenderParity(t *testing.T) {
	t.Parallel()

	lines := []string{"alpha", "beta", "gamma"}

	for _, isTTY := range []bool{true, false} {
		c := NewContinuationBlock("msg", lines, ThemeMono, isTTY)
		out := c.String()
		for _, line := range lines {
			if !strings.Contains(out, line) {
				t.Errorf("isTTY=%v: missing line %q in output: %q", isTTY, line, out)
			}
		}
	}
}

// --- JSON: empty continuation array ---

func TestLoggerContinueJSONEmpty(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(&buf),
	)

	log.Continue(LevelInfo, "no lines")

	out := buf.String()
	if !strings.Contains(out, `"continuation":[]`) {
		t.Errorf("expected empty continuation array in JSON: %q", out)
	}
}

package velocity

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

// testRenderable is a simple Renderable that writes a fixed string.
type testRenderable struct {
	content string
}

func (r *testRenderable) Render(w io.Writer) error {
	_, err := w.Write([]byte(r.content))
	return err
}

func TestLogger_Render_WritesIndentedOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := NewWithConfig(cfg)
	indent := log.consoleWriter.template.CachedMessageIndentStr()
	if indent == "" {
		t.Fatal("expected non-empty cached message indent string")
	}

	// First-line indent matters: table top borders, banners and other block
	// renderables write the very first line at the indent column.
	r := &testRenderable{content: "line1\nline2\n"}
	log.Render(r)

	out := buf.String()
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got: %q", out)
	}

	for i, want := range []string{"line1", "line2"} {
		expected := indent + want
		if lines[i] != expected {
			t.Errorf("line %d: expected %q, got %q", i, expected, lines[i])
		}
	}
}

func TestLogger_RenderRaw_WritesFlushLeft(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := NewWithConfig(cfg)

	r := &testRenderable{content: "rawline\n"}
	log.RenderRaw(r)

	if !strings.Contains(buf.String(), "rawline") {
		t.Errorf("expected 'rawline' in output, got: %s", buf.String())
	}
}

func TestLogger_Newline_WritesNewline(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := NewWithConfig(cfg)
	log.Newline()

	if buf.String() != "\n" {
		t.Errorf("expected single newline, got: %q", buf.String())
	}
}

func TestLogger_Render_NilSafety(t *testing.T) {
	t.Parallel()

	// nil logger must not panic
	var l *Logger
	l.Render(nil)
	l.RenderRaw(nil)
	l.Newline()

	// nil renderable must not panic on a real logger
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.StructuredOutput = nil
	log := NewWithConfig(cfg)
	log.Render(nil)
}

// TestLogger_Render_JSONWriterIgnores verifies that Render is a no-op when the logger
// has only a JSON writer and no console writer, so structured output is not polluted.
func TestLogger_Render_JSONWriterIgnores(t *testing.T) {
	t.Parallel()

	var jsonBuf bytes.Buffer

	cfg := DefaultConfig()
	cfg.ConsoleOutput = nil // no console writer
	cfg.StructuredOutput = &jsonBuf
	cfg.StructuredLevel = LevelDebug

	log := NewWithConfig(cfg)

	r := &testRenderable{content: "should-not-appear\n"}
	log.Render(r)
	log.RenderRaw(r)
	log.Newline()

	// JSON output must be empty — Render is terminal-only.
	if jsonBuf.Len() != 0 {
		t.Errorf("expected empty JSON output after Render, got: %q", jsonBuf.String())
	}
}

// TestLogger_Render_NoConsoleWriter verifies that Render is a no-op when
// consoleWriter is nil (e.g. logger constructed without a console output).
func TestLogger_Render_NoConsoleWriter(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = nil

	log := NewWithConfig(cfg)

	// Must not panic.
	log.Render(&testRenderable{content: "noop\n"})
	log.RenderRaw(&testRenderable{content: "noop\n"})
	log.Newline()
}

func TestLogger_Render_ConcurrentWithInfo(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := NewWithConfig(cfg)

	var wg sync.WaitGroup

	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info("concurrent log")
		}()
	}

	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Render(&testRenderable{content: "render\n"})
		}()
	}

	wg.Wait()
}

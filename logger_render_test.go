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

// TestLogger_Render_IndentMatchesCustomTimeFormat guards against a stale-cache bug:
// applying a custom TimeFormat after construction must recompute the cached message
// indent string, otherwise log lines render with one timestamp width while
// log.Render uses the indent computed from the construction-time format.
func TestLogger_Render_IndentMatchesCustomTimeFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil
	cfg.TimeFormat = "2006-01-02 15:04:05" // 19 chars, shorter than RFC3339

	log := newFromConfig(cfg)
	indent := log.consoleWriter.template.CachedMessageIndentStr()

	// Expected width: 19 (time) + 1 (space) + 6 (badge "[INFO]") + 1 (space) = 27.
	const expectedWidth = 27
	if len(indent) != expectedWidth {
		t.Fatalf("expected indent width %d for custom TimeFormat, got %d", expectedWidth, len(indent))
	}
}

func (r *testRenderable) Render(w io.Writer) error {
	_, err := w.Write([]byte(r.content))
	return err
}

func TestLogger_Render_WritesIndentedOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
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

	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)

	r := &testRenderable{content: "rawline\n"}
	log.RenderRaw(r)

	if !strings.Contains(buf.String(), "rawline") {
		t.Errorf("expected 'rawline' in output, got: %s", buf.String())
	}
}

func TestLogger_Newline_WritesNewline(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
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
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.StructuredOutput = nil
	log := newFromConfig(cfg)
	log.Render(nil)
}

// TestLogger_Render_JSONWriterIgnores verifies that Render is a no-op when the logger
// has only a JSON writer and no console writer, so structured output is not polluted.
func TestLogger_Render_JSONWriterIgnores(t *testing.T) {
	t.Parallel()

	var jsonBuf bytes.Buffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = nil // no console writer
	cfg.StructuredOutput = &jsonBuf
	cfg.StructuredLevel = LevelDebug

	log := newFromConfig(cfg)

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

	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)

	// Must not panic.
	log.Render(&testRenderable{content: "noop\n"})
	log.RenderRaw(&testRenderable{content: "noop\n"})
	log.Newline()
}

// TestLogger_Box verifies Box routes through Render and produces bordered output.
func TestLogger_Box(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
	log.Box("Title", "line one\nline two")

	out := buf.String()
	if !strings.Contains(out, "Title") {
		t.Errorf("expected title in box output, got: %s", out)
	}
	if !strings.Contains(out, "line one") {
		t.Errorf("expected content in box output, got: %s", out)
	}
}

// TestLogger_Table verifies Table routes through Render and produces column output.
func TestLogger_Table(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
	log.Table(
		[]string{"Name", "Status"},
		[][]string{{"auth", "ok"}, {"payments", "ok"}},
	)

	out := buf.String()
	if !strings.Contains(out, "Name") || !strings.Contains(out, "auth") {
		t.Errorf("expected table content in output, got: %s", out)
	}
}

// TestLogger_Tree verifies Tree routes through Render and produces hierarchy output.
func TestLogger_Tree(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
	log.Tree([]TreeItem{
		{Key: "root", Children: []TreeItem{{Key: "child", Value: "val"}}},
	})

	out := buf.String()
	if !strings.Contains(out, "root") || !strings.Contains(out, "child") {
		t.Errorf("expected tree nodes in output, got: %s", out)
	}
}

// TestLogger_KeyValues verifies KeyValues renders each pair to the console writer.
func TestLogger_KeyValues(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
	log.KeyValues([]KeyValuePair{
		{Key: "version", Value: "2.0.0"},
		{Key: "env", Value: "prod"},
	})

	out := buf.String()
	if !strings.Contains(out, "version") || !strings.Contains(out, "2.0.0") {
		t.Errorf("expected key-value content in output, got: %s", out)
	}
}

// TestLogger_SystemInfo verifies SystemInfo renders the titled metadata block.
func TestLogger_SystemInfo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
	log.SystemInfo(&SystemInfoData{
		Title:   "TensorFoundry",
		Version: "2.0.0",
		Fields:  []KeyValuePair{{Key: "env", Value: "production"}},
	})

	out := buf.String()
	if !strings.Contains(out, "TensorFoundry") {
		t.Errorf("expected title in system info output, got: %s", out)
	}
}

// TestLogger_Bullet verifies Bullet renders the indented bullet to the console writer.
func TestLogger_Bullet(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
	log.Bullet(0, "top-level item")
	log.Bullet(1, "nested item")

	out := buf.String()
	if !strings.Contains(out, "top-level item") || !strings.Contains(out, "nested item") {
		t.Errorf("expected bullet text in output, got: %s", out)
	}
	// Level 0 uses •, level 1 uses ◦.
	if !strings.Contains(out, "•") || !strings.Contains(out, "◦") {
		t.Errorf("expected bullet glyphs in output, got: %s", out)
	}
}

// TestLogger_Convenience_NilSafety verifies all convenience methods tolerate a nil receiver.
func TestLogger_Convenience_NilSafety(t *testing.T) {
	t.Parallel()

	var l *Logger
	l.Box("t", "b")
	l.Table([]string{"h"}, [][]string{{"v"}})
	l.Tree([]TreeItem{{Key: "k"}})
	l.KeyValues([]KeyValuePair{{Key: "k", Value: "v"}})
	l.SystemInfo(&SystemInfoData{Title: "T"})
	l.Bullet(0, "text")
}

// TestLogger_Convenience_ClosedLogger verifies methods are no-ops after Close.
func TestLogger_Convenience_ClosedLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)
	_ = log.Close()

	log.Box("title", "body")
	log.Table([]string{"h"}, [][]string{{"v"}})
	log.Bullet(0, "text")

	if buf.Len() != 0 {
		t.Errorf("expected no output after Close, got: %s", buf.String())
	}
}

// TestLogger_Convenience_JSONOnlyIgnores verifies methods are no-ops without a console writer.
func TestLogger_Convenience_JSONOnlyIgnores(t *testing.T) {
	t.Parallel()

	var jsonBuf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &jsonBuf
	cfg.StructuredLevel = LevelDebug

	log := newFromConfig(cfg)
	log.Box("title", "body")
	log.Table([]string{"h"}, [][]string{{"v"}})
	log.Tree([]TreeItem{{Key: "k"}})
	log.KeyValues([]KeyValuePair{{Key: "k", Value: "v"}})
	log.SystemInfo(&SystemInfoData{Title: "T"})
	log.Bullet(0, "text")

	if jsonBuf.Len() != 0 {
		t.Errorf("expected no JSON output from convenience methods, got: %s", jsonBuf.String())
	}
}

func TestLogger_Render_ConcurrentWithInfo(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = ThemeNightOwl
	cfg.StructuredOutput = nil

	log := newFromConfig(cfg)

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

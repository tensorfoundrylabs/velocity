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

	r := &testRenderable{content: "line1\nline2\n"}
	log.Render(r)

	out := buf.String()
	if !strings.Contains(out, "line1") {
		t.Errorf("expected 'line1' in output, got: %s", out)
	}
	// The second line should have some indent (from cachedPrefixWidth).
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got: %q", out)
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

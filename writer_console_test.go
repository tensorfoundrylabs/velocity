package velocity

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestConsoleWriter_InvalidLevel(_ *testing.T) {
	var out bytes.Buffer
	w := NewConsoleWriter(&out, nil)

	var bb bytes.Buffer
	buf := NewBytesBuffer(&bb)

	// Out-of-range levels must not panic on the levelColours array.
	w.formatLevel(buf, Level(100))
	w.formatLevel(buf, Level(6))
}

func TestConsoleWriter_AddCaller(t *testing.T) {
	var buf bytes.Buffer

	cfg := DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleLevel = LevelDebug
	cfg.StructuredOutput = nil
	cfg.ConsoleTheme = nil
	cfg.AddCaller = true

	log := NewWithConfig(cfg)
	log.Info("caller console test")

	if !strings.Contains(buf.String(), "_test.go:") {
		t.Fatalf("expected console output to contain caller reference, got: %s", buf.String())
	}
}

// TestTemplate_CachedPrefixWidth_BadgeStyle checks that the cached prefix width matches
// what calculatePrefixWidth produces for a real entry under the default badge style.
func TestTemplate_CachedPrefixWidth_BadgeStyle(t *testing.T) {
	t.Parallel()

	tmpl := TemplateDefault

	entry := GetEntry()
	entry.Time = time.Now()
	entry.Level = LevelInfo

	perCall := tmpl.calculatePrefixWidth(entry)
	cached := tmpl.CachedPrefixWidth()

	if cached != perCall {
		t.Errorf("cached prefix width %d != per-call %d", cached, perCall)
	}
}

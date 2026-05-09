package velocity

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestDetailed_ForcesTreeDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		logFunc func(l *Logger)
	}{
		{
			name: "Info on Detailed() child uses tree display even with inline config",
			logFunc: func(l *Logger) {
				l.Detailed().Info("Test message",
					String("key1", "value1"),
					Int("key2", 42),
					Bool("key3", true))
			},
		},
		{
			name: "Error on Detailed() child always uses tree display",
			logFunc: func(l *Logger) {
				l.Detailed().Error("Error occurred",
					String("error", "connection timeout"),
					Int("retry", 3))
			},
		},
		{
			name: "Warn on Detailed() child always uses tree display",
			logFunc: func(l *Logger) {
				l.Detailed().Warn("Warning message",
					String("warning", "high memory usage"),
					Float64("usage_percent", 89.5))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cfg := defaultConfig()
			cfg.FieldDisplayMode = FieldDisplayInline
			cfg.ConsoleOutput = &buf
			l := newFromConfig(cfg)

			tt.logFunc(l)

			// Tree display uses "├" or "└" as branch glyphs.
			out := buf.String()
			if !strings.ContainsAny(out, "├└") {
				t.Errorf("expected tree glyphs in output, got: %s", out)
			}
		})
	}
}

func TestDetailed_Debug_ForcesTreeDisplay(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.FieldDisplayMode = FieldDisplayInline
	cfg.ConsoleLevel = LevelDebug
	cfg.ConsoleOutput = &buf
	l := newFromConfig(cfg)

	l.Detailed().Debug("Debug info",
		String("module", "auth"),
		String("action", "token_refresh"))

	out := buf.String()
	if !strings.ContainsAny(out, "├└") {
		t.Errorf("expected tree glyphs in debug output, got: %s", out)
	}
}

func TestDetailed_RegularLoggerStillInline(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.FieldDisplayMode = FieldDisplayInline
	cfg.ConsoleOutput = &buf
	l := newFromConfig(cfg)

	l.Info("Regular message",
		String("key", "value"),
		Int("number", 123))

	out := buf.String()
	if strings.ContainsAny(out, "├└") {
		t.Errorf("regular logger should not produce tree glyphs in inline mode, got: %s", out)
	}
}

func TestDetailed_ThreadSafety(_ *testing.T) {
	cfg := defaultConfig()
	cfg.FieldDisplayMode = FieldDisplayInline
	logger := newFromConfig(cfg)
	detailed := logger.Detailed()

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := range 100 {
			detailed.Info("Detailed log", Int("iteration", i))
		}
	}()

	go func() {
		defer wg.Done()
		for i := range 100 {
			logger.Info("Normal log", Int("iteration", i))
		}
	}()

	go func() {
		defer wg.Done()
		for i := range 100 {
			detailed.Error("Detailed error", Int("iteration", i))
		}
	}()

	wg.Wait()
}

func TestDetailed_NilLogger(_ *testing.T) {
	var logger *Logger
	// Nil Detailed() returns nil — subsequent calls must not panic.
	d := logger.Detailed()
	if d != nil {
		d.Info("should not panic")
	}
}

func TestDetailed_RespectLogLevel(_ *testing.T) {
	cfg := defaultConfig()
	cfg.ConsoleLevel = LevelWarn
	logger := newFromConfig(cfg)
	detailed := logger.Detailed()

	// These are below threshold and should be filtered.
	detailed.Debug("Debug", String("key", "value"))
	detailed.Info("Info", String("key", "value"))

	// These should pass through without panic.
	detailed.Warn("Warn", String("key", "value"))
	detailed.Error("Error", String("key", "value"))
}

func TestDetailed_InheritsBaseFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	parent := newFromConfig(cfg)
	parent.baseFields = []Field{String("svc", "payments")}

	detailed := parent.Detailed()
	detailed.Info("checkout")

	if !strings.Contains(buf.String(), "payments") {
		t.Errorf("expected base field to propagate to Detailed child, got: %s", buf.String())
	}
}

func TestEntryPoolResetsForceTreeDisplay(t *testing.T) {
	t.Parallel()

	// Get an entry from the pool
	entry1 := GetEntry()

	if entry1.forceTreeDisplay {
		t.Error("New entry from pool should have forceTreeDisplay = false")
	}

	// Simulate what logDetailed used to do — flag on the entry directly.
	entry1.forceTreeDisplay = true
	entry1.Write()
	entry1.Release()

	// Get another entry (may be the same one returned to pool).
	entry2 := GetEntry()
	if entry2.forceTreeDisplay {
		t.Error("Entry from pool should have forceTreeDisplay reset to false after Reset()")
	}

	entry2.Write()
	entry2.Release()
}

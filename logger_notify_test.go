package velocity

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestLogger_Notify_WritesToConfiguredOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.StructuredOutput = nil
	cfg.NotifyOutput = &buf

	log := newFromConfig(cfg)
	log.Notify("hello %s", "operator")

	if !strings.Contains(buf.String(), "hello operator") {
		t.Errorf("expected notify message in output, got: %q", buf.String())
	}
}

func TestLogger_Notify_DefaultsToStderr(t *testing.T) {
	t.Parallel()

	// Verify that a logger with no NotifyOutput set does not panic and still
	// routes through the fallback path. We can't capture real stderr here, so
	// we confirm the notifyDest call returns a non-nil writer.
	cfg := defaultConfig()
	cfg.ConsoleOutput = &bytes.Buffer{}
	cfg.StructuredOutput = nil
	// NotifyOutput deliberately unset — should fall back to os.Stderr.
	log := newFromConfig(cfg)

	out, mu := log.notifyDest()
	if out == nil {
		t.Error("expected non-nil notify destination")
	}
	if mu == nil {
		t.Error("expected non-nil mutex from notifyDest")
	}
}

func TestLogger_Notify_BypassesLevelFilter(t *testing.T) {
	t.Parallel()

	var notifyBuf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &bytes.Buffer{}
	cfg.StructuredOutput = nil
	cfg.ConsoleLevel = LevelOff // nothing would normally reach the console writer
	cfg.NotifyOutput = &notifyBuf

	log := newFromConfig(cfg)
	log.Notify("this must appear even at LevelOff")

	if !strings.Contains(notifyBuf.String(), "this must appear") {
		t.Errorf("Notify must bypass level filter; got: %q", notifyBuf.String())
	}
}

func TestLogger_Notify_BypassesWriters(t *testing.T) {
	t.Parallel()

	// The JSON writer must not receive Notify output.
	var jsonBuf bytes.Buffer
	var notifyBuf bytes.Buffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &jsonBuf
	cfg.StructuredLevel = LevelDebug
	cfg.NotifyOutput = &notifyBuf

	log := newFromConfig(cfg)
	log.Notify("operator-only message")

	if jsonBuf.Len() != 0 {
		t.Errorf("Notify must not write to JSON writer; got: %q", jsonBuf.String())
	}
	if !strings.Contains(notifyBuf.String(), "operator-only message") {
		t.Errorf("Notify content missing from notify output; got: %q", notifyBuf.String())
	}
}

func TestLogger_Notify_ClosedLoggerDropsSilently(t *testing.T) {
	t.Parallel()

	var notifyBuf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &bytes.Buffer{}
	cfg.StructuredOutput = nil
	cfg.NotifyOutput = &notifyBuf

	log := newFromConfig(cfg)
	_ = log.Close()
	log.Notify("should be dropped")

	if notifyBuf.Len() != 0 {
		t.Errorf("expected no output from closed logger, got: %q", notifyBuf.String())
	}
}

func TestLogger_Notify_NilLogger(t *testing.T) {
	t.Parallel()

	var l *Logger
	// Must not panic.
	l.Notify("ignored")
	l.NotifyLines("ignored")
	l.NotifyBox(NewBox("t", "b", ThemeNightOwl))
}

func TestLogger_NotifyLines_JoinsWithNewlines(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &bytes.Buffer{}
	cfg.StructuredOutput = nil
	cfg.NotifyOutput = &buf

	log := newFromConfig(cfg)
	log.NotifyLines("line one", "line two", "line three")

	out := buf.String()
	for _, want := range []string{"line one\n", "line two\n", "line three\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in notify output; got: %q", want, out)
		}
	}
}

func TestLogger_NotifyLines_EmptyIsNoop(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &bytes.Buffer{}
	cfg.StructuredOutput = nil
	cfg.NotifyOutput = &buf

	log := newFromConfig(cfg)
	log.NotifyLines() // no lines — must not panic or write anything

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty NotifyLines; got: %q", buf.String())
	}
}

func TestLogger_NotifyBox_RendersToNotifyOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &bytes.Buffer{}
	cfg.StructuredOutput = nil
	cfg.NotifyOutput = &buf

	log := newFromConfig(cfg)
	log.NotifyBox(NewBox("Setup required", "Open the URL to continue.", ThemeNightOwl))

	out := buf.String()
	if !strings.Contains(out, "Setup required") {
		t.Errorf("expected box title in notify output; got: %q", out)
	}
	if !strings.Contains(out, "Open the URL") {
		t.Errorf("expected box content in notify output; got: %q", out)
	}
}

func TestLogger_NotifyBox_NilBoxIsNoop(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &bytes.Buffer{}
	cfg.StructuredOutput = nil
	cfg.NotifyOutput = &buf

	log := newFromConfig(cfg)
	log.NotifyBox(nil)

	if buf.Len() != 0 {
		t.Errorf("expected no output for nil box; got: %q", buf.String())
	}
}

func TestLogger_NotifyBox_BypassesJSONWriter(t *testing.T) {
	t.Parallel()

	var jsonBuf, notifyBuf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &jsonBuf
	cfg.StructuredLevel = LevelDebug
	cfg.NotifyOutput = &notifyBuf

	log := newFromConfig(cfg)
	log.NotifyBox(NewBox("Onboarding", "https://example.com/setup", ThemeNightOwl))

	if jsonBuf.Len() != 0 {
		t.Errorf("NotifyBox must not write to JSON writer; got: %q", jsonBuf.String())
	}
	if !strings.Contains(notifyBuf.String(), "Onboarding") {
		t.Errorf("expected box title in notify output; got: %q", notifyBuf.String())
	}
}

// TestLogger_Notify_WithNotifyOutput verifies the WithNotifyOutput option redirects correctly.
func TestLogger_Notify_WithNotifyOutput(t *testing.T) {
	t.Parallel()

	var notifyBuf bytes.Buffer
	log := New(WithDevelopment(), WithConsoleOutput(&bytes.Buffer{}), WithNotifyOutput(&notifyBuf))
	defer func() { _ = log.Close() }()

	log.Notify("redirected output")

	if !strings.Contains(notifyBuf.String(), "redirected output") {
		t.Errorf("WithNotifyOutput did not redirect; got: %q", notifyBuf.String())
	}
}

// TestLogger_Notify_SharesConsoleWriterMutex verifies that concurrent Info and Notify
// calls do not interleave — a heuristic race test that runs under -race.
func TestLogger_Notify_SharesConsoleWriterMutex(t *testing.T) {
	t.Parallel()

	var consoleBuf safeBuffer
	var notifyBuf safeBuffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = &consoleBuf
	cfg.StructuredOutput = nil
	cfg.NotifyOutput = &notifyBuf

	log := newFromConfig(cfg)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for range goroutines {
		go func() {
			defer wg.Done()
			log.Info("concurrent log call")
		}()
		go func() {
			defer wg.Done()
			log.Notify("concurrent notify\n")
		}()
	}

	wg.Wait()

	// The race detector enforces mutual exclusion; the count check confirms writes landed.
	notifyOut := notifyBuf.String()
	count := strings.Count(notifyOut, "concurrent notify")
	if count != goroutines {
		t.Errorf("expected %d notify writes, got %d; output:\n%s", goroutines, count, notifyOut)
	}
}

// TestLogger_Notify_FallbackMutexWithNoConsoleWriter verifies the package-level fallback
// mutex is used when no console writer is configured, preventing races on the notify dest.
func TestLogger_Notify_FallbackMutexWithNoConsoleWriter(t *testing.T) {
	t.Parallel()

	var notifyBuf safeBuffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = nil // no console writer — fallback mutex path
	cfg.StructuredOutput = nil
	cfg.NotifyOutput = &notifyBuf

	log := newFromConfig(cfg)

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			log.Notify("fallback\n")
		}()
	}

	wg.Wait()

	count := strings.Count(notifyBuf.String(), "fallback")
	if count != goroutines {
		t.Errorf("expected %d writes via fallback mutex, got %d", goroutines, count)
	}
}

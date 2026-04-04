package velocity

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestFatal_CustomHandler_CalledNotOsExit(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	called := false

	cfg := DefaultTestingConfig(&buf)
	cfg.FatalHandler = func() { called = true }
	l := NewWithConfig(cfg)

	l.Fatal("something went wrong", String("code", "E001"))

	if !called {
		t.Error("expected FatalHandler to be called")
	}
	out := buf.String()
	if !strings.Contains(out, "something went wrong") {
		t.Errorf("expected log message in output, got: %s", out)
	}
}

func TestFatal_WithFatalHandler_Builder(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	called := false

	l := NewWithBuilder(
		PresetTesting(&buf).WithFatalHandler(func() { called = true }),
	)

	l.Fatal("fatal via builder")

	if !called {
		t.Error("expected FatalHandler to be called via builder")
	}
}

// TestFatal_NilLogger_WritesStderrAndExits verifies that calling Fatal on a nil
// logger prints the message to stderr before exiting. Because Fatal calls
// os.Exit(1), the assertion runs in a subprocess spawned by re-executing the
// test binary with a sentinel environment variable.
func TestFatal_NilLogger_WritesStderrAndExits(t *testing.T) {
	t.Parallel()

	const sentinelEnv = "VELOCITY_TEST_NIL_FATAL"

	// Subprocess path: call Fatal on a nil logger and let it exit.
	if os.Getenv(sentinelEnv) == "1" {
		var l *Logger
		l.Fatal("nil logger fatal message")
	}

	// Parent path: run the test binary as a subprocess and capture stderr.
	// os.Args[0] is the compiled test binary — safe to use here.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestFatal_NilLogger_WritesStderrAndExits$", "-test.v") //nolint:gosec
	cmd.Env = append(os.Environ(), sentinelEnv+"=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	// The subprocess must have exited non-zero (os.Exit(1)).
	if err == nil {
		t.Fatal("expected subprocess to exit with non-zero status, but it succeeded")
	}

	out := stderr.String()
	if !strings.Contains(out, "nil logger fatal message") {
		t.Errorf("expected stderr to contain the fatal message, got: %q", out)
	}
	if !strings.Contains(out, "[FATL]") {
		t.Errorf("expected stderr to contain [FATL] prefix, got: %q", out)
	}
}

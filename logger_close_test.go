package velocity

import (
	"bytes"
	"testing"
)

func TestClose_Idempotent(t *testing.T) {
	t.Parallel()

	log := New(WithConsoleOutput(bytes.NewBuffer(nil)))

	if err := log.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}
	// Second call must not panic or return an error.
	if err := log.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func TestClose_PostCloseDropsSilently(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf))

	_ = log.Close()
	before := buf.Len()

	// Log calls after Close must not write anything.
	log.Info("should be dropped")
	log.Warn("also dropped")

	if buf.Len() != before {
		t.Errorf("expected no output after Close, got %d extra bytes", buf.Len()-before)
	}
}

func TestClose_NilLogger(t *testing.T) {
	t.Parallel()

	var l *Logger
	if err := l.Close(); err != nil {
		t.Errorf("nil logger Close() should return nil, got: %v", err)
	}
}

func TestClose_WithAdditionalWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf))
	log.AddWriter("test", WriterFunc(func(_ *Entry) error { return nil }))

	log.Info("before close")

	// Close must not block indefinitely; MultiWriter drains its channel.
	if err := log.Close(); err != nil {
		t.Fatalf("Close() with additional writer: %v", err)
	}
}

func TestStyle_ReturnsTheme(t *testing.T) {
	t.Parallel()

	log := New(
		WithConsoleOutput(bytes.NewBuffer(nil)),
		WithTheme(ThemeNightOwl),
	)

	theme := log.Style()
	if theme == nil {
		t.Fatal("Style() returned nil")
	}
	if theme == noColourTheme {
		t.Error("expected themed logger to return its theme, not noColourTheme")
	}
}

func TestStyle_NoConsoleWriter_ReturnsNoColour(t *testing.T) {
	t.Parallel()

	// WithNop produces a logger with no console writer.
	log := New(WithNop())
	theme := log.Style()
	if theme == nil {
		t.Fatal("Style() returned nil even for nop logger")
	}
	// The no-colour theme has an empty Name or all-zero ANSI codes.
	// We only verify it's non-nil and doesn't panic when its methods are called.
	_ = theme.CachedMessageFg()
}

func TestStyle_NilLogger(t *testing.T) {
	t.Parallel()

	var l *Logger
	theme := l.Style()
	if theme == nil {
		t.Fatal("nil logger Style() returned nil")
	}
}

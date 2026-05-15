package velocity

import (
	"bytes"
	"testing"
	"time"
)

// Compile-time interface satisfaction checks — these fail to build if a writer
// loses a capability it should provide.

var (
	_ ThemedWriter    = (*ConsoleWriter)(nil)
	_ ThemedWriter    = (*ConsoleWriterRB)(nil)
	_ FlushableWriter = (*JSONWriter)(nil)
)

// TestWriterTrusted_DefaultUntrusted verifies that AddWriter without options is untrusted.
func TestWriterTrusted_DefaultUntrusted(t *testing.T) {
	t.Parallel()

	log := New(WithConsoleOutput(&bytes.Buffer{}))
	log.AddWriter("sink", &NoOpWriter{})
	defer func() { _ = log.Close() }()

	log.writers.mu.RLock()
	trusted := log.writers.mw.IsTrusted("sink")
	log.writers.mu.RUnlock()

	if trusted {
		t.Error("writer added without WriterTrusted() should be untrusted by default")
	}
}

// TestWriterTrusted_ExplicitTrust verifies WriterTrusted() sets the flag.
func TestWriterTrusted_ExplicitTrust(t *testing.T) {
	t.Parallel()

	log := New(WithConsoleOutput(&bytes.Buffer{}))
	log.AddWriter("sink", &NoOpWriter{}, WriterTrusted())
	defer func() { _ = log.Close() }()

	log.writers.mu.RLock()
	trusted := log.writers.mw.IsTrusted("sink")
	log.writers.mu.RUnlock()

	if !trusted {
		t.Error("writer added with WriterTrusted() should be trusted")
	}
}

// TestWriterTrusted_MixedWriters verifies trust is tracked per-writer, not globally.
func TestWriterTrusted_MixedWriters(t *testing.T) {
	t.Parallel()

	log := New(WithConsoleOutput(&bytes.Buffer{}))
	log.AddWriter("trusted-sink", &NoOpWriter{}, WriterTrusted())
	log.AddWriter("untrusted-sink", &NoOpWriter{})
	defer func() { _ = log.Close() }()

	log.writers.mu.RLock()
	trustedYes := log.writers.mw.IsTrusted("trusted-sink")
	trustedNo := log.writers.mw.IsTrusted("untrusted-sink")
	log.writers.mu.RUnlock()

	if !trustedYes {
		t.Error("trusted-sink should be trusted")
	}
	if trustedNo {
		t.Error("untrusted-sink should not be trusted")
	}
}

// TestRemoveWriter_ReturnsWriter verifies the removed writer is returned to the caller.
func TestRemoveWriter_ReturnsWriter(t *testing.T) {
	t.Parallel()

	log := New(WithConsoleOutput(&bytes.Buffer{}))
	noop := &NoOpWriter{}
	log.AddWriter("sink", noop)

	removed := log.RemoveWriter("sink")
	if removed == nil {
		t.Fatal("RemoveWriter should return the removed writer, got nil")
	}

	// Allow the worker goroutine to drain before comparing.
	waitFor(t, func() bool { return true }, 100*time.Millisecond, 10*time.Millisecond, "drain")

	// The returned value should be the same writer we added.
	if removed != noop {
		t.Error("RemoveWriter returned a different writer than was registered")
	}
}

// TestRemoveWriter_UnknownName verifies nil is returned for an unregistered name.
func TestRemoveWriter_UnknownName(t *testing.T) {
	t.Parallel()

	log := New(WithConsoleOutput(&bytes.Buffer{}))
	defer func() { _ = log.Close() }()

	if got := log.RemoveWriter("nonexistent"); got != nil {
		t.Errorf("RemoveWriter for unknown name should return nil, got %T", got)
	}
}

// TestWriter_Accessor verifies Logger.Writer returns the registered writer.
func TestWriter_Accessor(t *testing.T) {
	t.Parallel()

	log := New(WithConsoleOutput(&bytes.Buffer{}))
	noop := &NoOpWriter{}
	log.AddWriter("sink", noop)
	defer func() { _ = log.Close() }()

	got := log.Writer("sink")
	if got == nil {
		t.Fatal("Logger.Writer should return the registered writer")
	}
	if got != noop {
		t.Error("Logger.Writer returned wrong writer")
	}
}

// TestWriter_AccessorUnknown verifies nil for unregistered name.
func TestWriter_AccessorUnknown(t *testing.T) {
	t.Parallel()

	log := New(WithConsoleOutput(&bytes.Buffer{}))
	defer func() { _ = log.Close() }()

	if got := log.Writer("nope"); got != nil {
		t.Errorf("Logger.Writer for unknown name should be nil, got %T", got)
	}
}

// TestJSONWriter_Flush verifies Flush compiles and runs without error on a plain buffer.
func TestJSONWriter_Flush(t *testing.T) {
	t.Parallel()

	jw := NewJSONWriter(&bytes.Buffer{})
	if err := jw.Flush(); err != nil {
		t.Errorf("Flush on plain buffer should be no-op: %v", err)
	}
}

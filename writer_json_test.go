package velocity

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestJSONWriter_NilErrorField(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	var errVal error
	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "test",
		Fields: []Field{{
			Key:   "err",
			Type:  FieldTypeError,
			value: unsafe.Pointer(&errVal),
		}},
	}

	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "null") {
		t.Errorf("expected null for nil error, got: %s", buf.String())
	}
}

func TestJSONWriter_NilStringerField(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "test",
		Fields: []Field{{
			Key:  "s",
			Type: FieldTypeStringer,
		}},
	}

	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "null") {
		t.Errorf("expected null for nil stringer, got: %s", buf.String())
	}
}

func TestJSONWriter_AddCaller(t *testing.T) {
	var buf bytes.Buffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true

	log := newFromConfig(cfg)
	log.Info("caller test")

	output := buf.String()
	if !strings.Contains(output, `"caller"`) {
		t.Fatalf("expected JSON output to contain caller field, got: %s", output)
	}
	// caller is now a separate string field; line is a separate numeric field
	if !strings.Contains(output, `"line"`) {
		t.Fatalf("expected JSON output to contain line field, got: %s", output)
	}
	if !strings.Contains(output, `_test.go"`) {
		t.Fatalf("expected caller to reference a test file, got: %s", output)
	}
}

// TestJSONWriter_ControlCharEscaping verifies that control characters in strings
// are encoded as \uXXXX sequences rather than raw bytes.
func TestJSONWriter_ControlCharEscaping(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	// Build message with raw control chars at runtime so the source stays clean.
	msg := "ctrl:" + string([]byte{0x01, 0x1f})

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: msg,
	}

	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	output := buf.String()
	// The JSON encoder must produce  and , not raw control bytes.
	if !strings.Contains(output, "\\u0001") {
		t.Errorf("expected \\u0001 escape in JSON output, got: %s", output)
	}
	if !strings.Contains(output, "\\u001f") {
		t.Errorf("expected \\u001f escape in JSON output, got: %s", output)
	}
}

func TestJSONWriter_CallerEscaping(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "escaping test",
		Caller:  `path\to\file.go`,
		Line:    42,
	}

	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	output := buf.String()

	// Must be valid JSON.
	// Manually verify required fields are present and properly escaped.
	if !strings.Contains(output, `"caller"`) {
		t.Errorf("expected caller field, got: %s", output)
	}
	if !strings.Contains(output, `"line"`) {
		t.Errorf("expected line field, got: %s", output)
	}
	// Backslashes must be escaped as \\ in JSON.
	if !strings.Contains(output, `path\\to\\file.go`) {
		t.Errorf("expected escaped backslashes in caller, got: %s", output)
	}
	// line must be a bare number, not quoted.
	if !strings.Contains(output, `"line":42`) {
		t.Errorf("expected numeric line value, got: %s", output)
	}
}

// TestConsoleOnlyLogger_LevelGate verifies that a console-only logger with a high
// console level does not process entries below that level. Previously, the default
// StructuredLevel (Info) was included in the effective-level min even when no
// structured output was configured, causing Debug entries to not be filtered when
// the console level was set to Warn.
func TestConsoleOnlyLogger_LevelGate(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleLevel = LevelWarn
	cfg.StructuredOutput = nil // no structured output
	cfg.StructuredLevel = LevelInfo
	log := newFromConfig(cfg)

	log.Info("should not appear")
	log.Debug("should not appear")

	if buf.Len() != 0 {
		t.Errorf("expected no output for sub-Warn entries on console-only logger, got: %q", buf.String())
	}

	log.Warn("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Errorf("expected Warn entry to appear, got: %q", buf.String())
	}
}

// TestStatus_CallerPoints_ToCallSite verifies that captureCaller is invoked
// with the right skip depth for Status, so the reported caller is the test
// function itself, not an internal dispatch helper.
func TestStatus_CallerPoints_ToCallSite(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true
	log := newFromConfig(cfg)

	log.Status(LevelInfo, StatusOK, "caller check") //nolint:testableexamples // line number pinned
	out := buf.String()
	if !strings.Contains(out, `writer_json_test.go`) {
		t.Errorf("Status caller should point to this test file, got: %s", out)
	}
}

// TestGroup_CallerPoints_ToCallSite verifies that captureCaller is invoked
// with the right skip depth for Group.
func TestGroup_CallerPoints_ToCallSite(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true
	log := newFromConfig(cfg)

	log.Group(LevelInfo, "caller check", GroupItem{Text: "item"})
	out := buf.String()
	if !strings.Contains(out, `writer_json_test.go`) {
		t.Errorf("Group caller should point to this test file, got: %s", out)
	}
}

// TestContinue_CallerPoints_ToCallSite verifies that captureCaller is invoked
// with the right skip depth for Continue.
func TestContinue_CallerPoints_ToCallSite(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true
	log := newFromConfig(cfg)

	log.Continue(LevelInfo, "caller check", "line one")
	out := buf.String()
	if !strings.Contains(out, `writer_json_test.go`) {
		t.Errorf("Continue caller should point to this test file, got: %s", out)
	}
}

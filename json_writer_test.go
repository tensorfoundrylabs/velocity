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

	cfg := DefaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true

	log := NewWithConfig(cfg)
	log.Info("caller test")

	output := buf.String()
	if !strings.Contains(output, `"caller"`) {
		t.Fatalf("expected JSON output to contain caller field, got: %s", output)
	}
	if !strings.Contains(output, `_test.go:`) {
		t.Fatalf("expected caller to reference a test file, got: %s", output)
	}
}

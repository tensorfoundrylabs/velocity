package velocity

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWith_FieldAppearsInOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	parent := newForTesting(&buf)
	child := parent.With(String("svc", "gateway"))

	child.Info("hello")

	out := buf.String()
	if !strings.Contains(out, "gateway") {
		t.Errorf("expected 'gateway' in output, got: %s", out)
	}
}

func TestWith_ChainedFieldsBothAppear(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	parent := newForTesting(&buf)
	child := parent.With(String("a", "alpha")).With(String("b", "beta"))

	child.Info("chained")

	out := buf.String()
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected 'alpha' in output, got: %s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("expected 'beta' in output, got: %s", out)
	}
}

func TestWith_ParentUnaffected(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	parent := newForTesting(&buf)
	_ = parent.With(String("child_field", "x"))

	buf.Reset()
	parent.Info("parent log")

	out := buf.String()
	if strings.Contains(out, "child_field") {
		t.Errorf("parent log should not contain child fields, got: %s", out)
	}
}

func TestWith_NilLogger_ReturnsNil(t *testing.T) {
	t.Parallel()

	var l *Logger
	child := l.With(String("k", "v"))
	if child != nil {
		t.Error("expected nil for nil.With()")
	}
}

func TestWith_EmptyFields_ReturnsSelf(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	parent := newForTesting(&buf)
	child := parent.With()
	if child != parent {
		t.Error("expected same logger when no fields passed to With()")
	}
}

func TestWithTemplate_PreservesParentState(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	parent := newForTesting(&buf)

	// Give the parent a sampler and a base field.
	sampler := NewCountSampler(10, 5)
	parent.sampler = sampler

	withField := parent.With(String("svc", "payments"))

	// Count entries received by the additional writer.
	var count atomic.Int32
	withField.AddWriter("counter", WriterFunc(func(_ *Entry) error {
		count.Add(1)
		return nil
	}))

	child := withField.WithTemplate(nil)

	// Sampler must be the same instance.
	if child.sampler != sampler {
		t.Error("expected child sampler to match parent sampler")
	}

	// baseFields must contain the "svc" field.
	found := false
	for _, f := range child.baseFields {
		if f.Key == "svc" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected child baseFields to contain 'svc' field")
	}

	child.Info("test message")

	// Close to flush the async MultiWriter before asserting.
	if err := child.Close(); err != nil {
		t.Fatalf("close error: %v", err)
	}

	if count.Load() == 0 {
		t.Error("expected additional writer to receive at least one entry")
	}
}

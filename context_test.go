package velocity

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNewContext_FromContext_RoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := NewForTesting(&buf)

	ctx := NewContext(context.Background(), l)
	got := FromContext(ctx)

	got.Info("round-trip")
	if !strings.Contains(buf.String(), "round-trip") {
		t.Errorf("expected 'round-trip' in output, got: %s", buf.String())
	}
}

func TestFromContext_EmptyContext_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	l := FromContext(context.Background())
	if l == nil {
		t.Fatal("FromContext on empty context returned nil")
	}
	// Should not panic
	l.Info("nop")
}

func TestContextWithFields_Accumulates(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	parent := NewForTesting(&buf)

	ctx := NewContext(context.Background(), parent)
	ctx = ContextWithFields(ctx, StringField("layer", "middleware"))
	ctx = ContextWithFields(ctx, StringField("req", "abc"))

	l := FromContext(ctx)
	l.Info("test")

	out := buf.String()
	if !strings.Contains(out, "middleware") {
		t.Errorf("expected 'middleware' in output, got: %s", out)
	}
	if !strings.Contains(out, "abc") {
		t.Errorf("expected 'abc' in output, got: %s", out)
	}
}

func TestContextWithFields_NoFields_ReturnsSameCtx(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	got := ContextWithFields(ctx)
	if got != ctx {
		t.Error("expected same context when no fields passed")
	}
}

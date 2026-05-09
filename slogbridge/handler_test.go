package slogbridge_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	velocity "github.com/tensorfoundrylabs/velocity"
	slogbridge "github.com/tensorfoundrylabs/velocity/slogbridge"
)

// newTestLogger creates a logger writing JSON to buf for easy assertion.
func newTestLogger(buf *bytes.Buffer) *velocity.Logger {
	return velocity.NewForTesting(buf)
}

func TestSlogHandler_NilLogger(t *testing.T) {
	t.Parallel()

	h := slogbridge.NewHandler(nil)

	ctx := context.Background()
	if h.Enabled(ctx, slog.LevelInfo) {
		t.Error("expected disabled for nil logger")
	}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	if err := h.Handle(ctx, r); err != nil {
		t.Fatalf("Handle on nil logger returned error: %v", err)
	}

	// WithAttrs and WithGroup must not panic.
	h2 := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	_ = h2.WithGroup("g")
}

func TestSlogHandler_BasicLogging(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l := newTestLogger(buf)
	sl := slogbridge.NewLogger(l)

	sl.Info("hello world")
	out := buf.String()
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected output to contain 'hello world', got: %s", out)
	}
}

func TestSlogHandler_WithAttrs(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l := newTestLogger(buf)
	sl := slogbridge.NewLogger(l)

	sl = sl.With("component", "auth", "version", 3)
	sl.Info("login")

	out := buf.String()
	if !strings.Contains(out, "component") {
		t.Errorf("expected 'component' in output, got: %s", out)
	}
	if !strings.Contains(out, "auth") {
		t.Errorf("expected 'auth' in output, got: %s", out)
	}
}

func TestSlogHandler_WithGroup(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l := newTestLogger(buf)
	sl := slogbridge.NewLogger(l)

	sl = sl.WithGroup("server").With("host", "localhost")
	sl.Info("starting")

	out := buf.String()
	if !strings.Contains(out, "server.host") {
		t.Errorf("expected 'server.host' in output, got: %s", out)
	}
	if !strings.Contains(out, "localhost") {
		t.Errorf("expected 'localhost' in output, got: %s", out)
	}
}

func TestSlogHandler_LevelFiltering(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l := newTestLogger(buf)
	l.SetLevel(velocity.LevelInfo)
	sl := slogbridge.NewLogger(l)

	sl.Debug("should be filtered")

	if buf.Len() != 0 {
		t.Errorf("expected no output for debug when logger at info, got: %s", buf.String())
	}

	sl.Info("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Errorf("expected info message in output, got: %s", buf.String())
	}
}

func TestSlogHandler_FieldTypes(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l := newTestLogger(buf)
	sl := slogbridge.NewLogger(l)

	now := time.Now()
	sl.Info("typed fields",
		slog.String("str", "hello"),
		slog.Int("count", 42),
		slog.Float64("ratio", 3.14),
		slog.Bool("ok", true),
		slog.Time("ts", now),
		slog.Duration("dur", 5*time.Second),
	)

	out := buf.String()
	for _, want := range []string{"str", "hello", "count", "ratio", "ok", "dur"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestSlogHandler_Enabled(t *testing.T) {
	t.Parallel()
	l := velocity.New(nil)
	l.SetLevel(velocity.LevelWarn)
	h := slogbridge.NewHandler(l)

	ctx := context.Background()
	if h.Enabled(ctx, slog.LevelDebug) {
		t.Error("expected debug to be disabled at warn level")
	}
	if h.Enabled(ctx, slog.LevelInfo) {
		t.Error("expected info to be disabled at warn level")
	}
	if !h.Enabled(ctx, slog.LevelWarn) {
		t.Error("expected warn to be enabled at warn level")
	}
	if !h.Enabled(ctx, slog.LevelError) {
		t.Error("expected error to be enabled at warn level")
	}
}

func TestSlogHandler_NestedGroups(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l := newTestLogger(buf)
	sl := slogbridge.NewLogger(l)

	sl = sl.WithGroup("a").WithGroup("b")
	sl.Info("nested", slog.String("key", "val"))

	out := buf.String()
	if !strings.Contains(out, "a.b.key") {
		t.Errorf("expected 'a.b.key' in output, got: %s", out)
	}
}

func TestSlogHandler_EmptyAttr(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	l := newTestLogger(buf)
	h := slogbridge.NewHandler(l)

	// WithAttrs with empty slice should return same handler.
	h2 := h.WithAttrs(nil)
	if h2 != h {
		t.Error("expected same handler for empty attrs")
	}

	// Empty slog.Attr should not panic or produce extra keys.
	// Use Handle directly to avoid loggercheck flagging slog.Attr{} as a bare key-value arg.
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	r.AddAttrs(slog.Attr{})
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "msg") {
		t.Errorf("expected 'msg' in output, got: %s", out)
	}
}

func TestSlogHandler_ConcurrentHandle(t *testing.T) {
	t.Parallel()

	buf := &bytes.Buffer{}
	l := newTestLogger(buf)
	sl := slogbridge.NewLogger(l)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sl.Info("concurrent", slog.String("k", "v"))
		}()
	}
	wg.Wait()
}

func BenchmarkSlogHandler_Info(b *testing.B) {
	l := velocity.New(nil) // nil discards console output
	sl := slogbridge.NewLogger(l)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sl.Info("benchmark message",
			slog.String("key1", "value1"),
			slog.String("key2", "value2"),
			slog.String("key3", "value3"),
		)
	}
}

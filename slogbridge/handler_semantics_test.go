package slogbridge_test

// slog handler-guide coverage: LogValuer resolution, empty/unnamed/nested
// group qualification, record time handling, Uint64 exactness without float
// round trips, and post-close dispatch behaviour.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	velocity "github.com/tensorfoundrylabs/velocity/v2"
	slogbridge "github.com/tensorfoundrylabs/velocity/v2/slogbridge"
)

// newJSONBridgeLogger builds a logger whose structured JSON output is
// captured in buf; the console side is discarded.
func newJSONBridgeLogger(buf io.Writer) *velocity.Logger {
	return velocity.New(
		velocity.WithConsoleOutput(io.Discard),
		velocity.WithStructuredOutput(buf),
		velocity.WithStructuredLevel(velocity.LevelDebug),
		velocity.WithLevel(velocity.LevelDebug),
	)
}

// decodeLastJSONLineUseNumber parses the last structured line with UseNumber
// so integers compare as exact literals.
func decodeLastJSONLineUseNumber(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	s := strings.TrimSpace(buf.String())
	if idx := strings.LastIndex(s, "\n"); idx >= 0 {
		s = s[idx+1:]
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("structured output is not valid JSON: %v\nline: %s", err, s)
	}
	return m
}

// TestSlogSemantics_LogValuerResolved: slog.LogValuer attributes must be
// resolved to their underlying value before conversion, in both WithAttrs
// (pre-converted) and record attrs (converted per Handle).
func TestSlogSemantics_LogValuerResolved(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newJSONBridgeLogger(&buf)
	h := slogbridge.NewHandler(l)

	tick := slog.NewRecord(time.Now(), slog.LevelInfo, "logvaluer", 0)
	tick.AddAttrs(slog.Any("trace", logValuerTrace{}))
	if err := h.Handle(context.Background(), tick); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	m := decodeLastJSONLineUseNumber(t, &buf)
	if got, ok := m["trace"].(string); !ok || got != "trace-abc123" {
		t.Errorf("LogValuer attr not resolved to its string value: %#v", m["trace"])
	}
}

type logValuerTrace struct{}

func (logValuerTrace) LogValue() slog.Value {
	return slog.StringValue("trace-abc123")
}

// TestSlogSemantics_GroupQualification: group attrs inline under a dotted
// prefix; an attr group with an EMPTY key flattens into the current prefix
// (slog's unnamed-group rule); WithGroup("") is a no-op returning the same
// handler.
func TestSlogSemantics_GroupQualification(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newJSONBridgeLogger(&buf)
	h := slogbridge.NewHandler(l)

	if got := h.WithGroup(""); got != h {
		t.Error("WithGroup(\"\") must return the same handler")
	}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "groups", 0)
	r.AddAttrs(
		slog.Attr{Key: "outer", Value: slog.GroupValue(
			slog.String("inner", "iv"),
			// Unnamed nested group: attrs flatten under "outer.".
			slog.Attr{Key: "", Value: slog.GroupValue(slog.String("flat", "fv"))},
		)},
	)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	m := decodeLastJSONLineUseNumber(t, &buf)
	if got, ok := m["outer.inner"].(string); !ok || got != "iv" {
		t.Errorf("group attr not qualified: %#v", m["outer.inner"])
	}
	if got, ok := m["outer.flat"].(string); !ok || got != "fv" {
		t.Errorf("unnamed group not flattened under parent prefix: %#v", m["outer.flat"])
	}
}

// TestSlogSemantics_RecordTimeHandling: an explicit record time is preserved;
// a zero record time is normalised to now rather than emitted as
// 0001-01-01T00:00:00Z.
func TestSlogSemantics_RecordTimeHandling(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newJSONBridgeLogger(&buf)
	h := slogbridge.NewHandler(l)

	fixed := time.Date(2024, 3, 1, 12, 30, 5, 0, time.UTC)
	explicit := slog.NewRecord(fixed, slog.LevelInfo, "explicit-time", 0)
	if err := h.Handle(context.Background(), explicit); err != nil {
		t.Fatalf("Handle explicit: %v", err)
	}
	if !strings.Contains(buf.String(), `"2024-03-01T12:30:05Z"`) {
		t.Errorf("explicit record time not preserved in JSON: %s", buf.String())
	}

	buf.Reset()
	before := time.Now().Add(-time.Second)
	zero := slog.NewRecord(time.Time{}, slog.LevelInfo, "zero-time", 0)
	if err := h.Handle(context.Background(), zero); err != nil {
		t.Fatalf("Handle zero-time: %v", err)
	}
	if strings.Contains(buf.String(), "0001-01-01T") {
		t.Errorf("zero record time emitted verbatim: %s", buf.String())
	}
	m := decodeLastJSONLineUseNumber(t, &buf)
	ts, ok := m["timestamp"].(string)
	if !ok {
		t.Fatalf("no timestamp field in JSON: %v", m)
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("timestamp %q not RFC3339: %v", ts, err)
	}
	if parsed.Before(before) {
		t.Errorf("normalised timestamp %v predates the call (%v)", parsed, before)
	}
}

// TestSlogSemantics_Uint64AttrExact: slog's KindUint64 must map to
// velocity.Uint64 and survive as an exact JSON integer literal — asserted via
// json.Number, never float64.
func TestSlogSemantics_Uint64AttrExact(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newJSONBridgeLogger(&buf)
	sl := slogbridge.NewLogger(l)

	sl.Info(
		"uint64 attr",
		slog.Uint64("max", math.MaxUint64),
		slog.Uint64("maxInt64Plus1", uint64(math.MaxInt64)+1),
	)

	m := decodeLastJSONLineUseNumber(t, &buf)
	if got := m["max"]; got == nil {
		t.Fatalf("max attr missing: %v", m)
	} else if n, ok := got.(json.Number); !ok || n.String() != "18446744073709551615" {
		t.Errorf("MaxUint64 lost exactness through slog bridge: %#v", got)
	}
	if got, ok := m["maxInt64Plus1"].(json.Number); !ok || got.String() != "9223372036854775808" {
		t.Errorf("MaxInt64+1 lost exactness through slog bridge: %#v", m["maxInt64Plus1"])
	}
}

// TestSlogSemantics_PostCloseNoWritesNoPanic: after the backing logger's
// writer family is closed, Handle must not write, must not panic and must
// still return nil — closure is the logger's concern, not the handler's.
func TestSlogSemantics_PostCloseNoWritesNoPanic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newJSONBridgeLogger(&buf)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h := slogbridge.NewHandler(l)
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "after close", 0)
	r.AddAttrs(slog.String("k", "v"))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle after close returned error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("post-close Handle produced %d bytes of output: %q", buf.Len(), buf.String())
	}

	// Enabled still answers, and handler derivation stays panic-free.
	_ = h.Enabled(context.Background(), slog.LevelInfo)
	_ = h.WithAttrs([]slog.Attr{slog.String("a", "b")}).WithGroup("g")
}

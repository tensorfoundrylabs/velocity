package velocity

// R09 dispatch regressions, promoted from the verification battery: lossless
// Uint64 and structured Any values in JSON output without float64 round
// trips, valid diagnostics for marshal failures, and balanced Entry
// refcounts on every rejected or dropped dispatch path.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// maxUint64Literal is asserted everywhere as an exact JSON integer literal;
// never compared through float64.
const maxUint64Literal = "18446744073709551615"

// parseJSONLineUseNumber decodes one JSON log line with UseNumber so numeric
// fields compare as exact integer literals rather than float64.
func parseJSONLineUseNumber(t *testing.T, line string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("line is not valid JSON: %v\nline: %s", err, line)
	}
	return m
}

func jsonNumString(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("key %q missing from JSON: %v", key, m)
	}
	n, ok := v.(json.Number)
	if !ok {
		t.Fatalf("key %q is not a JSON number: got %T (%v)", key, v, v)
	}
	return n.String()
}

// lastJSONLine trims trailing whitespace and returns the last complete line.
func lastJSONLine(buf *bytes.Buffer) string {
	s := strings.TrimSpace(buf.String())
	if idx := strings.LastIndex(s, "\n"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func newJSONTestLogger(buf io.Writer) *Logger {
	return New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(buf),
		WithStructuredLevel(LevelDebug),
		WithLevel(LevelDebug),
	)
}

// TestDispatch_Uint64Boundaries_ExactInJSON: MaxUint64 and MaxInt64+1 must
// survive the JSON writer as exact integer literals, never float64 round
// trips. Assertions go through json.Number, never float64.
func TestDispatch_Uint64Boundaries_ExactInJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newJSONTestLogger(&buf)

	l.Info("uint64-boundaries",
		Uint64("max", math.MaxUint64),
		Uint64("maxInt64Plus1", uint64(math.MaxInt64)+1),
		Uint64("zero", 0),
		Uint64("one", 1),
	)

	m := parseJSONLineUseNumber(t, strings.TrimSpace(buf.String()))
	if got := jsonNumString(t, m, "max"); got != maxUint64Literal {
		t.Errorf("MaxUint64 not preserved exactly: got %s", got)
	}
	if got := jsonNumString(t, m, "maxInt64Plus1"); got != "9223372036854775808" {
		t.Errorf("MaxInt64+1 not preserved exactly: got %s", got)
	}
	if got := jsonNumString(t, m, "zero"); got != "0" {
		t.Errorf("zero not preserved: got %s", got)
	}
	if got := jsonNumString(t, m, "one"); got != "1" {
		t.Errorf("one not preserved: got %s", got)
	}
}

// TestDispatch_Uint64ExactOnConsoleAndSnapshot: the same lossless rule on the
// console writer's field rendering and ring snapshot values (both go through
// FieldValueToString).
func TestDispatch_Uint64ExactOnConsoleAndSnapshot(t *testing.T) {
	t.Parallel()

	var console bytes.Buffer
	l := New(
		WithConsoleOutput(&console),
		WithLevel(LevelDebug),
		WithFieldDisplayMode(FieldDisplayTree),
	)
	l.Info("uint64 paths", Uint64("max", math.MaxUint64))
	if !strings.Contains(console.String(), maxUint64Literal) {
		t.Errorf("console output lost MaxUint64 exactness: %q", console.String())
	}

	r := NewRingBufferWriter(4)
	rl := New(WithConsoleOutput(io.Discard), WithLevel(LevelDebug))
	rl.AddWriter("ring", r)
	sub := r.Subscribe(context.Background(), 1)
	rl.Info("uint64 snapshot", Uint64("max", math.MaxUint64))

	// The named writer delivers asynchronously; the subscription channel is
	// the deterministic completion hook.
	select {
	case snap, ok := <-sub:
		if !ok {
			t.Fatal("subscription closed before delivery")
		}
		found := false
		for _, f := range snap.Fields {
			if f.Key == "max" {
				found = true
				if f.Value != maxUint64Literal {
					t.Errorf("snapshot lost MaxUint64 exactness: %q", f.Value)
				}
			}
		}
		if !found {
			t.Errorf("max field missing from snapshot: %+v", snap.Fields)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ring writer did not deliver the entry within 5s")
	}
	_ = rl.Close()
}

type badMarshalerStruct struct{}

func (badMarshalerStruct) MarshalJSON() ([]byte, error) {
	return nil, errors.New("marshal exploded")
}

// TestDispatch_AnyValues_ExactInJSON: Any-wrapped maps, slices and nils must
// be preserved structurally in the structured output, with uint64 values
// inside containers still exact via encoding/json's uint64 support.
func TestDispatch_AnyValues_ExactInJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newJSONTestLogger(&buf)

	obj := map[string]any{"big": uint64(math.MaxUint64), "neg": -2, "name": "any"}
	slice := []any{1, "two", nil}

	l.Info("any-values", Any("obj", obj), Any("slice", slice), Any("nilval", nil))

	m := parseJSONLineUseNumber(t, lastJSONLine(&buf))

	gotObj, ok := m["obj"].(map[string]any)
	if !ok {
		t.Fatalf("obj not a JSON object: %v", m["obj"])
	}
	if got, ok := gotObj["big"].(json.Number); !ok || got.String() != maxUint64Literal {
		t.Errorf("uint64 inside Any object lost precision: %#v", gotObj["big"])
	}
	if got, ok := gotObj["neg"].(json.Number); !ok || got.String() != "-2" {
		t.Errorf("neg inside Any object wrong: %#v", gotObj["neg"])
	}
	if gotObj["name"] != "any" {
		t.Errorf("string inside Any object wrong: %#v", gotObj["name"])
	}

	gotSlice, ok := m["slice"].([]any)
	if !ok || len(gotSlice) != 3 {
		t.Fatalf("slice inside Any not preserved: %#v", m["slice"])
	}
	if n, ok := gotSlice[0].(json.Number); !ok || n.String() != "1" {
		t.Errorf("slice[0] not exact 1: %#v", gotSlice[0])
	}
	if gotSlice[1] != "two" || gotSlice[2] != nil {
		t.Errorf("slice tail not preserved: %#v", gotSlice)
	}
	if m["nilval"] != nil {
		t.Errorf("nil Any not serialised as null: %#v", m["nilval"])
	}
}

// TestDispatch_AnyMarshalError_ValidJSONDiagnostic: a failing custom marshaler
// must produce a valid JSON line with an explicit diagnostic string, never
// broken output or a panic.
func TestDispatch_AnyMarshalError_ValidJSONDiagnostic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newJSONTestLogger(&buf)

	l.Info("bad-marshal", Any("bad", badMarshalerStruct{}))

	m := parseJSONLineUseNumber(t, lastJSONLine(&buf))
	v, ok := m["bad"].(string)
	if !ok {
		t.Fatalf("marshal-failure field is not a diagnostic string: %#v", m["bad"])
	}
	if !strings.Contains(v, "marshal exploded") {
		t.Errorf("diagnostic string lost the underlying error: %q", v)
	}
}

// TestDispatch_DisabledLevel_NoRefCountLeak: entries rejected by the level
// gate must not pick up a stray Retain — the caller's single reference is all
// that remains (LogEntry must balance on skipped paths).
func TestDispatch_DisabledLevel_NoRefCountLeak(t *testing.T) {
	t.Parallel()

	l := New(WithConsoleOutput(io.Discard), WithLevel(LevelError))

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("disabled-level-entry")

	l.LogEntry(e)

	if rc := e.refCount.Load(); rc != 1 {
		t.Errorf("disabled-level dispatch left refCount=%d, want 1 (leaked Retain)", rc)
	}
}

// TestDispatch_ClosedLogger_NoRefCountLeak: entries rejected because the
// writer family is closed must equally balance their reference counts.
func TestDispatch_ClosedLogger_NoRefCountLeak(t *testing.T) {
	t.Parallel()

	l := New(WithConsoleOutput(io.Discard), WithLevel(LevelDebug))
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	e := GetEntry()
	defer e.Release()
	e.SetLevel(LevelInfo)
	e.SetMessage("closed-logger-entry")

	l.LogEntry(e)

	if rc := e.refCount.Load(); rc != 1 {
		t.Errorf("closed-logger dispatch left refCount=%d, want 1 (leaked Retain)", rc)
	}
}

// TestDispatch_FullQueueDrop_RefCountBalanced: an entry dropped by a full
// MultiWriter channel must have its Retain/Release pair balanced. The gated
// writer parks the worker so the queue deterministically fills.
func TestDispatch_FullQueueDrop_RefCountBalanced(t *testing.T) {
	t.Parallel()

	gw := newGatedWriter()
	l := newForTesting(io.Discard)
	l.AddWriter("gated", gw)
	defer func() {
		close(gw.release)
		_ = l.Close()
	}()

	// 600 sends: 257 accepted (1 in-flight + 256 queued), the rest drop.
	for i := range 600 {
		l.Info("queuefill-" + strconv.Itoa(i))
	}

	e := GetEntry()
	e.SetLevel(LevelInfo)
	e.SetMessage("queuefull-probe")
	l.LogEntry(e)

	if rc := e.refCount.Load(); rc != 1 {
		t.Errorf("queue-full drop left refCount=%d, want 1 (Retain without matching Release)", rc)
	}
	e.Release()
}

// TestDispatch_DisabledLevel_ZeroAllocs: the disabled-level path must stay
// allocation-free — the level gate exists so sub-threshold calls cost one
// atomic load and nothing else. Asserted (not just benchmarked) because the
// zero-alloc claim is part of the library's contract.
// Not parallel: testing.AllocsPerRun panics inside parallel subtests.
func TestDispatch_DisabledLevel_ZeroAllocs(t *testing.T) {
	l := New(WithConsoleOutput(io.Discard), WithLevel(LevelError))
	fields := []Field{String("k", "v"), Int("n", 7)}

	allocs := testing.AllocsPerRun(200, func() {
		l.Info("disabled message", fields...)
	})
	if allocs != 0 {
		t.Errorf("disabled-level Info allocated %v times per call, want 0", allocs)
	}

	msg := "disabled message"
	allocs = testing.AllocsPerRun(200, func() {
		l.Info(msg, fields...)
	})
	if allocs != 0 {
		t.Errorf("disabled-level Info with prebuilt values allocated %v times per call, want 0", allocs)
	}
}

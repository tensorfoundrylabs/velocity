package velocity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEntryClearsRetainedFieldTail(t *testing.T) {
	e := GetEntry()
	e.Fields = append(e.Fields, String("large", strings.Repeat("x", 1024)))
	backing := e.Fields[:cap(e.Fields)]
	e.Fields = e.Fields[:0]
	e.Reset()
	if backing[0].value != nil {
		t.Fatal("Reset retained a shortened field tail")
	}
	live := GetEntry()
	live.Fields = append(live.Fields, String("large", "value"))
	live.Retain()
	live.Release()

	final := GetEntry()
	final.Fields = append(final.Fields, String("large", "value"))
	finalBacking := final.Fields[:cap(final.Fields)]
	final.Fields = final.Fields[:0]
	final.Release()
	if finalBacking[0].value != nil {
		t.Fatal("final Release retained a shortened field tail")
	}
	if live.Fields[0].value == nil {
		t.Fatal("non-final Release cleared a live entry")
	}
	live.Release()
}

func TestEntryReleaseDiscardsOversizedFields(t *testing.T) {
	e := GetEntry()
	e.Fields = make([]Field, 1, 65)
	e.Fields[0] = String("x", "value")
	e.Release()
	if e.Fields != nil {
		t.Fatal("oversized field storage was retained")
	}
}

func TestJSONWriterReusesBoundedBuffers(t *testing.T) {
	var out bytes.Buffer
	w := NewJSONWriter(&out)
	for _, n := range []int{512, 4096, 16384} {
		e := &Entry{Message: strings.Repeat("x", n)}
		if err := w.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	var value map[string]any
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'})
	if err := json.Unmarshal(lines[len(lines)-1], &value); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	spike := &Entry{Message: strings.Repeat("x", bufXLargeSize+1)}
	if err := w.Write(spike); err != nil {
		t.Fatal(err)
	}
	buf := w.getJSONBuffer()
	if buf.Cap() > bufXLargeSize {
		t.Fatalf("oversized JSON buffer retained: %d", buf.Cap())
	}
	w.putJSONBuffer(buf)
}

func TestFloatFormattingMatchesJSONSpecialValues(t *testing.T) {
	for _, value := range []float64{0, math.Copysign(0, -1), math.SmallestNonzeroFloat64, math.MaxFloat64, math.NaN(), math.Inf(1), math.Inf(-1)} {
		var console bytes.Buffer
		Float64("f", value).writeFormatted(&console)
		if got, want := console.String(), strconv.FormatFloat(value, 'g', -1, 64); got != want {
			t.Fatalf("console float changed: got %q, want %q", got, want)
		}
		var out bytes.Buffer
		w := NewJSONWriter(&out)
		if err := w.Write(&Entry{Message: "m", Fields: []Field{Float64("f", value), Redacted("secret")}}); err != nil {
			t.Fatal(err)
		}
		if !json.Valid(out.Bytes()) || !bytes.Contains(out.Bytes(), []byte(redactedMark)) {
			t.Fatalf("invalid or unredacted output for %v: %s", value, out.Bytes())
		}
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			var decoded struct {
				F float64 `json:"f"`
			}
			if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || math.Float64bits(decoded.F) != math.Float64bits(value) {
				t.Fatalf("finite float changed: %v, %v", value, decoded.F)
			}
		}
	}
}

func TestRingBufferDropsLargeIdleBatchCapacity(t *testing.T) {
	wrote := make(chan int, 2)
	r := NewRingBuffer(writerFunc(func(p []byte) (int, error) {
		select {
		case wrote <- cap(p):
		default:
		}
		return len(p), nil
	}), 2)
	if !r.Write(make([]byte, maxIdleBatchCapacity+1)) {
		t.Fatal("write dropped")
	}
	select {
	case <-wrote:
	case <-time.After(time.Second):
		t.Fatal("record was not drained")
	}
	if !r.Write([]byte("ok")) {
		t.Fatal("small write dropped")
	}
	select {
	case capacity := <-wrote:
		if capacity > maxIdleBatchCapacity {
			t.Fatalf("idle batch retained %d bytes", capacity)
		}
	case <-time.After(time.Second):
		t.Fatal("small record was not drained")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriberSkipsFullQueueAndCopiesAcceptedSnapshot(t *testing.T) {
	r := NewRingBufferWriter(2)
	t.Cleanup(func() { _ = r.Close() })
	full := r.Subscribe(context.Background(), 1)
	open := r.Subscribe(context.Background(), 2)
	e := &Entry{Message: "m", Fields: []Field{String("x", "one")}}
	_ = r.Write(e)
	drops := r.drops.Load()
	_ = r.Write(e)
	if r.drops.Load() <= drops {
		t.Fatal("full subscriber did not record a drop")
	}
	first := <-open
	if len(first.Fields) != 1 || first.Fields[0].Key != "x" {
		t.Fatal("accepted snapshot changed")
	}
	first.Fields[0].Key = "changed"
	if got := (<-open).Fields[0].Key; got != "x" {
		t.Fatalf("subscriber snapshots share fields: %q", got)
	}
	select {
	case <-full:
	default:
		t.Fatal("full subscriber was not initially filled")
	}
}

func TestLogEntryPrependsWithoutOverwritingAliasedFields(t *testing.T) {
	cfg := defaultConfig()
	cfg.ConsoleOutput = io.Discard
	l := newFromConfig(cfg).With(String("base1", "a"), String("base2", "b"))
	for _, capacity := range []int{2, 8} {
		fields := make([]Field, 2, capacity)
		fields[0], fields[1] = String("user1", "x"), String("user2", "y")
		e := &Entry{Level: LevelInfo, Fields: fields}
		l.LogEntry(e)
		if len(e.Fields) != 4 || e.Fields[0].Key != "base1" || e.Fields[1].Key != "base2" || e.Fields[2].Key != "user1" || e.Fields[3].Key != "user2" {
			t.Fatalf("prepend failed with cap %d: %#v", capacity, e.Fields)
		}
	}
}

func TestDetailedSharesImmutableBaseFields(t *testing.T) {
	cfg := defaultConfig()
	l := newFromConfig(cfg).With(String("base", "one"))
	detail := l.Detailed()
	sibling := detail.With(String("child", "two"))
	if len(l.baseFields) != 1 || len(detail.baseFields) != 1 || len(sibling.baseFields) != 2 {
		t.Fatal("child fields mutated an existing logger")
	}
	if l.baseFields[0].Key != "base" || detail.baseFields[0].Key != "base" || sibling.baseFields[1].Key != "child" {
		t.Fatal("shared base fields changed")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

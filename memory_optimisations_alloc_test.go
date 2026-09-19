//go:build !race

package velocity

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestSubscriberFullDropDoesNotClone(t *testing.T) {
	r := NewRingBufferWriter(2)
	t.Cleanup(func() { _ = r.Close() })
	_ = r.Subscribe(context.Background(), 1)
	e := &Entry{Message: "m", Fields: []Field{String("one", "value"), String("two", "value")}}
	_ = r.Write(e)
	_ = r.Write(e)
	allocs := testing.AllocsPerRun(1000, func() { _ = r.Write(e) })
	if allocs != 0 {
		t.Fatalf("full subscriber drop allocated %.2f times", allocs)
	}
}

func TestJSONWriterWarmReuseAllocations(t *testing.T) {
	w := NewJSONWriter(io.Discard)
	for _, size := range []int{512, 4096, 16384} {
		e := &Entry{Message: strings.Repeat("x", size)}
		_ = w.Write(e)
		allocs := testing.AllocsPerRun(1000, func() { _ = w.Write(e) })
		if allocs != 0 {
			t.Fatalf("%d-byte JSON write allocated %.2f times", size, allocs)
		}
	}
	entries := []*Entry{{Message: strings.Repeat("x", 512)}, {Message: strings.Repeat("x", 4096)}, {Message: strings.Repeat("x", 16384)}}
	for _, e := range entries {
		_ = w.Write(e)
	}
	index := 0
	if allocs := testing.AllocsPerRun(1000, func() { _ = w.Write(entries[index%len(entries)]); index++ }); allocs != 0 {
		t.Fatalf("alternating JSON writes allocated %.2f times", allocs)
	}
}

func TestPrebuiltScalarWritersAllocateNothing(t *testing.T) {
	fields := []Field{String("service", "api"), Int("port", 8080), Float64("latency", 1.25), Bool("ok", true), Duration("elapsed", time.Second)}
	e := &Entry{Time: time.Unix(0, 0), Level: LevelInfo, Message: "request", Fields: fields}
	console := NewConsoleWriter(io.Discard, ThemeNightOwl)
	jsonWriter := NewJSONWriter(io.Discard)
	_ = console.Write(e)
	_ = jsonWriter.Write(e)
	if allocs := testing.AllocsPerRun(1000, func() { _ = console.Write(e); _ = jsonWriter.Write(e) }); allocs != 0 {
		t.Fatalf("prebuilt scalar writers allocated %.2f times", allocs)
	}
}

func TestLogEntrySpareCapacityAllocatesNothing(t *testing.T) {
	cfg := defaultConfig()
	cfg.ConsoleOutput = io.Discard
	l := newFromConfig(cfg).With(String("base", "value"))
	e := &Entry{Level: LevelInfo, Fields: make([]Field, 1, 4)}
	e.Fields[0] = Int("port", 8080)
	l.LogEntry(e)
	if allocs := testing.AllocsPerRun(1000, func() { e.Fields = e.Fields[:1]; l.LogEntry(e) }); allocs != 0 {
		t.Fatalf("LogEntry with spare capacity allocated %.2f times", allocs)
	}
}

package velocity

import (
	"context"
	"io"
	"strings"
	"testing"
)

var memoryOptimisationsLoggerSink *Logger

func BenchmarkJSONWriter_Warm512(b *testing.B) { benchmarkJSONWriterWarm(b, 512) }
func BenchmarkJSONWriter_Warm4K(b *testing.B)  { benchmarkJSONWriterWarm(b, 4096) }
func BenchmarkJSONWriter_Warm16K(b *testing.B) { benchmarkJSONWriterWarm(b, 16384) }

func benchmarkJSONWriterWarm(b *testing.B, size int) {
	w := NewJSONWriter(io.Discard)
	e := &Entry{Message: strings.Repeat("x", size)}
	_ = w.Write(e)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = w.Write(e)
	}
}

func BenchmarkJSONWriter_WarmAlternating(b *testing.B) {
	w := NewJSONWriter(io.Discard)
	entries := []*Entry{{Message: strings.Repeat("x", 512)}, {Message: strings.Repeat("x", 4096)}, {Message: strings.Repeat("x", 16384)}}
	for _, e := range entries {
		_ = w.Write(e)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		_ = w.Write(entries[i%len(entries)])
	}
}

func BenchmarkRingSubscriber_FullDrop(b *testing.B) {
	r := NewRingBufferWriter(2)
	b.Cleanup(func() { _ = r.Close() })
	_ = r.Subscribe(context.Background(), 1)
	e := &Entry{Message: "m", Fields: []Field{String("one", "value"), String("two", "value")}}
	_ = r.Write(e)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = r.Write(e)
	}
}

func BenchmarkLogEntry_SpareCapacity(b *testing.B) {
	cfg := defaultConfig()
	cfg.ConsoleOutput = io.Discard
	l := newFromConfig(cfg).With(String("base", "value"))
	e := &Entry{Level: LevelInfo, Fields: make([]Field, 1, 4)}
	e.Fields[0] = Int("port", 8080)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e.Fields = e.Fields[:1]
		l.LogEntry(e)
	}
}

func BenchmarkDetailed_WithBaseField(b *testing.B) {
	cfg := defaultConfig()
	l := newFromConfig(cfg).With(String("base", "value"))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		memoryOptimisationsLoggerSink = l.Detailed()
	}
}

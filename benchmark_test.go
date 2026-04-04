package velocity

import (
	"io"
	"testing"
	"time"
)

// newDiscardLogger builds a real logger that formats output but discards it,
// so we measure formatting cost rather than I/O cost.
func newDiscardLogger() *Logger {
	cfg := DefaultConfig()
	cfg.ConsoleOutput = io.Discard
	cfg.StructuredOutput = io.Discard
	// Force both writers active so we measure full formatting overhead.
	cfg.ConsoleLevel = LevelDebug
	cfg.StructuredLevel = LevelDebug
	return NewWithConfig(cfg)
}

// fiveFields returns a representative slice of mixed-type fields.
func fiveFields() []Field {
	return []Field{
		StringField("service", "api-gateway"),
		Int("port", 8080),
		Float64("latency_ms", 1.23),
		Bool("success", true),
		Duration("elapsed", 42*time.Millisecond),
	}
}

func tenFields() []Field {
	return []Field{
		StringField("service", "api-gateway"),
		Int("port", 8080),
		Float64("latency_ms", 1.23),
		Bool("success", true),
		Duration("elapsed", 42*time.Millisecond),
		StringField("region", "ap-southeast-2"),
		Int64("request_id", 9876543210),
		Bool("cached", false),
		Float64("cpu", 0.72),
		StringField("user", "alice"),
	}
}

// ---- Core hot-path benchmarks -----------------------------------------------

func BenchmarkInfo_NoFields(b *testing.B) {
	l := newDiscardLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed")
	}
}

func BenchmarkInfo_OneStringField(b *testing.B) {
	l := newDiscardLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", StringField("service", "api-gateway"))
	}
}

func BenchmarkInfo_FiveFields(b *testing.B) {
	l := newDiscardLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", fields...)
	}
}

func BenchmarkInfo_TenFields(b *testing.B) {
	l := newDiscardLogger()
	fields := tenFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", fields...)
	}
}

// BenchmarkInfo_Disabled measures the cost of a level check that rejects the entry.
// This is the common case for Debug calls in a production logger set to Info.
func BenchmarkInfo_Disabled(b *testing.B) {
	l := newDiscardLogger()
	l.SetLevel(LevelInfo)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Debug("this is suppressed")
	}
}

func BenchmarkInfo_WithSampler(b *testing.B) {
	l := newDiscardLogger()
	l.sampler = NewCountSampler(1000, 100)
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("sampled message", fields...)
	}
}

// ---- Field construction benchmarks ------------------------------------------

func BenchmarkStringField(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var f Field
	for b.Loop() {
		f = StringField("key", "value")
	}
	_ = f
}

func BenchmarkIntField(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var f Field
	for b.Loop() {
		f = Int("port", 8080)
	}
	_ = f
}

func BenchmarkFloat64Field(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var f Field
	for b.Loop() {
		f = Float64("latency", 1.23)
	}
	_ = f
}

// BenchmarkF_String measures the type-switch overhead in the generic constructor.
func BenchmarkF_String(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var f Field
	for b.Loop() {
		f = F("key", "value")
	}
	_ = f
}

func BenchmarkF_Int(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var f Field
	for b.Loop() {
		f = F("port", 8080)
	}
	_ = f
}

// ---- Writer benchmarks ------------------------------------------------------

func BenchmarkJSONWriter_FiveFields(b *testing.B) {
	w := NewJSONWriter(io.Discard)
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e := GetEntry()
		e.SetLevel(LevelInfo)
		e.SetMessage("request completed")
		e.SetTime(time.Now())
		e.WithFields(fields...)
		_ = w.Write(e)
		e.Write()
		e.Release()
	}
}

func BenchmarkConsoleWriter_FiveFields(b *testing.B) {
	// Template path (default): exercises theme + template formatting.
	w := NewConsoleWriter(io.Discard, ThemeNightOwl)
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e := GetEntry()
		e.SetLevel(LevelInfo)
		e.SetMessage("request completed")
		e.SetTime(time.Now())
		e.WithFields(fields...)
		_ = w.Write(e)
		e.Write()
		e.Release()
	}
}

// BenchmarkConsoleWriter_NoTemplate exercises the formatEntry fallback path
// when no template is set (nil theme disables colour and template).
func BenchmarkConsoleWriter_NoTemplate(b *testing.B) {
	w := NewConsoleWriterWithOptions(io.Discard, nil, time.UTC, FieldDisplayInline)
	// Clear the template so ConsoleWriter falls back to formatEntry.
	w.SetTemplate(nil)
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e := GetEntry()
		e.SetLevel(LevelInfo)
		e.SetMessage("request completed")
		e.SetTime(time.Now())
		e.WithFields(fields...)
		_ = w.Write(e)
		e.Write()
		e.Release()
	}
}

// ---- Entry pool benchmarks --------------------------------------------------

func BenchmarkGetEntry_Release(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e := GetEntry()
		e.Write()
		e.Release()
	}
}

func BenchmarkEntry_WithFields(b *testing.B) {
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e := GetEntry()
		e.WithFields(fields...)
		e.Write()
		e.Release()
	}
}

// ---- Concurrency benchmarks -------------------------------------------------

func BenchmarkInfo_Parallel(b *testing.B) {
	l := newDiscardLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Info("parallel request", fields...)
		}
	})
}

func BenchmarkJSONWriter_Parallel(b *testing.B) {
	w := NewJSONWriter(io.Discard)
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			e := GetEntry()
			e.SetLevel(LevelInfo)
			e.SetMessage("parallel write")
			e.SetTime(time.Now())
			e.WithFields(fields...)
			_ = w.Write(e)
			e.Write()
			e.Release()
		}
	})
}

// ---- Buffer pool benchmarks -------------------------------------------------

func BenchmarkBufferPool_GetPut(b *testing.B) {
	pool := NewBufferPool()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf := pool.Get(HintStructuredLog)
		buf.WriteString("benchmark payload")
		pool.Put(buf)
	}
}

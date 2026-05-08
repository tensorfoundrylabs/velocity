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
		String("service", "api-gateway"),
		Int("port", 8080),
		Float64("latency_ms", 1.23),
		Bool("success", true),
		Duration("elapsed", 42*time.Millisecond),
	}
}

func tenFields() []Field {
	return []Field{
		String("service", "api-gateway"),
		Int("port", 8080),
		Float64("latency_ms", 1.23),
		Bool("success", true),
		Duration("elapsed", 42*time.Millisecond),
		String("region", "ap-southeast-2"),
		Int64("request_id", 9876543210),
		Bool("cached", false),
		Float64("cpu", 0.72),
		String("user", "alice"),
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

func BenchmarkInfo_OneString(b *testing.B) {
	l := newDiscardLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", String("service", "api-gateway"))
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

func BenchmarkString(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var f Field
	for b.Loop() {
		f = String("key", "value")
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

// ---- Tree-mode rendering benchmarks ----------------------------------------

// BenchmarkInfo_TreeMode measures the badge-style tree-mode path where the
// cachedIndentStr is used in place of strings.Repeat on every field.
func BenchmarkInfo_TreeMode(b *testing.B) {
	cfg := DefaultConfig()
	cfg.ConsoleOutput = io.Discard
	cfg.StructuredOutput = nil
	cfg.ConsoleLevel = LevelDebug
	cfg.FieldDisplayMode = FieldDisplayTree
	l := NewWithConfig(cfg)
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("tree mode entry", fields...)
	}
}

// BenchmarkInfo_TreeMode_Parallel measures concurrent tree-mode throughput.
func BenchmarkInfo_TreeMode_Parallel(b *testing.B) {
	cfg := DefaultConfig()
	cfg.ConsoleOutput = io.Discard
	cfg.StructuredOutput = nil
	cfg.ConsoleLevel = LevelDebug
	cfg.FieldDisplayMode = FieldDisplayTree
	l := NewWithConfig(cfg)
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Info("tree mode entry", fields...)
		}
	})
}

// BenchmarkInfoDetailed_TreeMode measures the InfoDetailed path which always forces
// tree display regardless of the configured FieldDisplayMode.
func BenchmarkInfoDetailed_TreeMode(b *testing.B) {
	l := newDiscardLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.InfoDetailed("detailed entry", fields...)
	}
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

// ---- Render API benchmarks --------------------------------------------------

// BenchmarkLogger_Render measures the cost of one Logger.Render call with a
// small pre-built TableResult (3 rows, 2 columns). Construction is excluded
// from the timer so we isolate the indentation + write path.
func BenchmarkLogger_Render(b *testing.B) {
	l := newDiscardLogger()
	// Import pretty types via the renderable interface — we keep the root
	// benchmark free of the pretty package import by using a minimal inline Renderable.
	payload := []byte("col1  col2\ncell1 cell2\ncell3 cell4\ncell5 cell6\n")
	r := &bytesRenderable{data: payload}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Render(r)
	}
}

// BenchmarkLogger_RenderRaw measures the flush-left write path — no indentation
// computation, so should be slightly cheaper than Render.
func BenchmarkLogger_RenderRaw(b *testing.B) {
	l := newDiscardLogger()
	payload := []byte("col1  col2\ncell1 cell2\ncell3 cell4\ncell5 cell6\n")
	r := &bytesRenderable{data: payload}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.RenderRaw(r)
	}
}

// BenchmarkLogger_Newline measures the trivial-call overhead of inserting a
// blank line under the console writer mutex.
func BenchmarkLogger_Newline(b *testing.B) {
	l := newDiscardLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Newline()
	}
}

// bytesRenderable is a minimal Renderable used in benchmarks to avoid importing
// the pretty package from the root benchmark file.
type bytesRenderable struct {
	data []byte
}

func (r *bytesRenderable) Render(w io.Writer) error {
	_, err := w.Write(r.data)
	return err
}

package velocity_test

// Matrix-gap benchmarks for the hardening-finish record: scenarios the WP0
// fixture set does not yet time — contextual children, pretty box/banner in
// ASCII and Unicode, Uint64 and Any JSON serialisation, close drain and
// fatal-with-returning-handler delivery. The WP0 fixtures are untouched; this
// file only adds coverage. sinkW/sinkB metrics let a reader confirm output
// actually flowed.

import (
	"io"
	"sync/atomic"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity/v2"
)

// matrixSink is a real io.Writer so console/JSON serialisation actually runs
// (same contract as the root package's benchSink).
type matrixSink struct {
	bytes  atomic.Int64
	writes atomic.Int64
}

func (s *matrixSink) Write(p []byte) (int, error) {
	s.bytes.Add(int64(len(p)))
	s.writes.Add(1)
	return len(p), nil
}

func (s *matrixSink) report(b *testing.B) {
	b.Helper()
	if b.N == 0 {
		return
	}
	n := float64(b.N)
	b.ReportMetric(float64(s.bytes.Load())/n, "sinkB/op")
	b.ReportMetric(float64(s.writes.Load())/n, "sinkW/op")
}

// matrixEntryWriter counts delivered entries for drain accounting.
type matrixEntryWriter struct {
	entries atomic.Int64
}

func (w *matrixEntryWriter) Write(_ *velocity.Entry) error {
	w.entries.Add(1)
	return nil
}

func (w *matrixEntryWriter) Close() error { return nil }

var (
	matrixUnicodeHeaders = []string{"サービス", "ステータス"}
	matrixUnicodeRows    = [][]string{
		{"ゲートウェイ", "稼働中"},
		{"ワーカー", "停止"},
		{"スケジューラ", "稼働中"},
	}
)

func benchmarkPrettyRender(b *testing.B, render func(p *velocity.Pretty)) {
	sink := &matrixSink{}
	p := velocity.NewPretty(sink, velocity.ThemeNightOwl)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		render(p)
	}
	b.StopTimer()
	sink.report(b)
}

func BenchmarkPretty_New_Box(b *testing.B) {
	benchmarkPrettyRender(b, func(p *velocity.Pretty) { p.Box("deploy", "service started") })
}

func BenchmarkPretty_New_Banner(b *testing.B) {
	benchmarkPrettyRender(b, func(p *velocity.Pretty) { p.Banner("velocity") })
}

func BenchmarkPretty_New_Table_Unicode(b *testing.B) {
	benchmarkPrettyRender(b, func(p *velocity.Pretty) { p.Table(matrixUnicodeHeaders, matrixUnicodeRows) })
}

func BenchmarkPretty_New_Box_Unicode(b *testing.B) {
	benchmarkPrettyRender(b, func(p *velocity.Pretty) { p.Box("デプロイ", "サービス開始") })
}

func BenchmarkPretty_New_Banner_Unicode(b *testing.B) {
	benchmarkPrettyRender(b, func(p *velocity.Pretty) { p.Banner("ベロシティ") })
}

// BenchmarkMatrix_ContextualChild: a prebuilt child logger (two base fields)
// logging three fields per call — the With() sharing path plus field
// concatenation, serialised to JSON.
func BenchmarkMatrix_ContextualChild(b *testing.B) {
	sink := &matrixSink{}
	l := velocity.New(
		velocity.WithConsoleOutput(io.Discard),
		velocity.WithStructuredOutput(sink),
		velocity.WithStructuredLevel(velocity.LevelDebug),
	)
	child := l.With(
		velocity.String("service", "bench"),
		velocity.String("env", "test"),
	)
	fields := []velocity.Field{
		velocity.String("path", "/api/run"),
		velocity.Int("status", 200),
		velocity.Int64("dur", 1500),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		child.Info("request completed", fields...)
	}
	b.StopTimer()
	sink.report(b)
}

// BenchmarkMatrix_JSONWriter_Uint64Fields: lossless uint64 serialisation —
// the path that must never round through float64.
func BenchmarkMatrix_JSONWriter_Uint64Fields(b *testing.B) {
	sink := &matrixSink{}
	l := velocity.New(
		velocity.WithConsoleOutput(io.Discard),
		velocity.WithStructuredOutput(sink),
		velocity.WithStructuredLevel(velocity.LevelDebug),
	)
	fields := []velocity.Field{
		velocity.Uint64("bytes", 18446744073709551615),
		velocity.Uint64("ops", 9223372036854775808),
		velocity.Uint64("seq", 42),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("uint64 fields", fields...)
	}
	b.StopTimer()
	sink.report(b)
}

// BenchmarkMatrix_JSONWriter_AnyField: the FieldTypeAny fallback, which uses
// encoding/json on the Any value only.
func BenchmarkMatrix_JSONWriter_AnyField(b *testing.B) {
	sink := &matrixSink{}
	l := velocity.New(
		velocity.WithConsoleOutput(io.Discard),
		velocity.WithStructuredOutput(sink),
		velocity.WithStructuredLevel(velocity.LevelDebug),
	)
	obj := map[string]any{"big": uint64(18446744073709551615), "name": "bench", "ok": true}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("any field", velocity.Any("obj", obj))
	}
	b.StopTimer()
	sink.report(b)
}

// BenchmarkMatrix_Uint64Field: the typed field constructor alone.
func BenchmarkMatrix_Uint64Field(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = velocity.Uint64("n", 18446744073709551615)
	}
}

// BenchmarkMatrix_CloseDrain: enqueue 256 entries to a named writer (filling
// its queue) then measure Close, which must drain every accepted entry.
// sinkW-style delivery is reported as entries/op via StopTimer accounting.
func BenchmarkMatrix_CloseDrain(b *testing.B) {
	b.ReportAllocs()
	// Classic loop: b.Loop forbids StopTimer inside the measured iteration,
	// and the per-iteration setup (filling the queue) must stay untimed.
	for range b.N {
		b.StopTimer()
		w := &matrixEntryWriter{}
		// io.Discard console means no console writer is constructed, but the
		// level gate stays open so the named writer receives every entry.
		l := velocity.New(
			velocity.WithConsoleOutput(io.Discard),
			velocity.WithLevel(velocity.LevelDebug),
		)
		l.AddWriter("drain", w)
		for range 256 {
			l.Info("drain entry")
		}
		b.StartTimer()
		_ = l.Close()
		b.StopTimer()
		if got := w.entries.Load(); got != 256 {
			b.Fatalf("drain delivered %d of 256 accepted entries", got)
		}
	}
}

// matrixCountingWriter wraps a Writer and counts delivered entries, so async
// benchmarks can assert exact delivery independently of io.Writer call counts
// (the root package's entryCountingWriter is internal and not reachable here).
type matrixCountingWriter struct {
	inner   velocity.Writer
	entries atomic.Int64
}

func (w *matrixCountingWriter) Write(e *velocity.Entry) error {
	w.entries.Add(1)
	return w.inner.Write(e)
}

func (w *matrixCountingWriter) Close() error { return w.inner.Close() }

// BenchmarkMatrix_Fatal_ReturningHandler: Fatal delivery with a FatalHandler
// that returns (logger stays reusable). Measures the full synchronous fatal:
// the direct structured write, the reliable enqueue to the named async writer,
// the per-item sentinel barrier — WriteReliable blocks until the worker has
// written and flushed this fatal behind everything accepted before it — and
// the returning handler. The named async sink is what engages the barrier:
// without it WriteReliable has no channel to wait on and the enqueue/barrier
// legs go unmeasured (the review's F5 coverage finding).
func BenchmarkMatrix_Fatal_ReturningHandler(b *testing.B) {
	sink := &matrixSink{}
	l := velocity.New(
		velocity.WithConsoleOutput(io.Discard),
		velocity.WithStructuredOutput(sink),
		velocity.WithLevel(velocity.LevelFatal),
		velocity.WithStructuredLevel(velocity.LevelFatal),
		velocity.WithFatalHandler(func() {}),
	)
	async := &matrixCountingWriter{inner: velocity.NewJSONWriter(sink)}
	l.AddWriter("fatal-async", async)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Fatal("fatal delivery", velocity.String("code", "X13"))
	}
	b.StopTimer()
	if err := l.Close(); err != nil {
		b.Fatalf("close: %v", err)
	}
	if got := async.entries.Load(); got != int64(b.N) {
		b.Fatalf("reliable fatal delivered %d of %d entries", got, b.N)
	}
	sink.report(b)
}

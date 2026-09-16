package velocity

import (
	"io"
	"testing"
	"time"
)

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

// ---- Core hot-path benchmarks ------------------------------------------------
//
// These measure real serialization: every entry is formatted by both the
// console and JSON writers and delivered to a benchSink. Validity of the
// output and the zero-write behaviour of the disabled path are asserted by
// TestPreflight_* in benchmark_preflight_test.go, outside the timed loops.

func BenchmarkInfo_NoFields(b *testing.B) {
	l, console, structured := newSinkLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed")
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

func BenchmarkInfo_OneString(b *testing.B) {
	l, console, structured := newSinkLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", String("service", "api-gateway"))
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

func BenchmarkInfo_FiveFields(b *testing.B) {
	l, console, structured := newSinkLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", fields...)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

// BenchmarkInfo_FiveFields_Inline constructs the fields at the call site so
// construction cost is included, matching the common inline-field usage.
func BenchmarkInfo_FiveFields_Inline(b *testing.B) {
	l, console, structured := newSinkLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info(
			"request completed",
			String("service", "api-gateway"),
			Int("port", 8080),
			Float64("latency_ms", 1.23),
			Bool("success", true),
			Duration("elapsed", 42*time.Millisecond),
		)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

func BenchmarkInfo_TenFields(b *testing.B) {
	l, console, structured := newSinkLogger()
	fields := tenFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", fields...)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

// BenchmarkInfo_Disabled measures the cost of a level check that rejects the
// entry. This is the common case for Debug calls in a production logger set
// to Info. TestPreflight_DisabledLevelWritesNothing proves the sinks receive
// zero bytes on this path.
func BenchmarkInfo_Disabled(b *testing.B) {
	l, console, structured := newSinkLogger()
	l.SetLevel(LevelInfo)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Debug("this is suppressed")
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

func BenchmarkInfo_WithSampler(b *testing.B) {
	l, console, structured := newSinkLogger()
	l.sampler = NewCountSampler(1000, 100)
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("sampled message", fields...)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

// ---- No-output benchmarks ----------------------------------------------------
//
// Separately labelled: these loggers are built on io.Discard, which
// newFromConfig maps to "no output" — no writer is constructed. They measure
// the entry pool round-trip plus field assembly only. Never compare these
// numbers against the serialization benchmarks above.

func BenchmarkNoOutput_Info_FiveFields(b *testing.B) {
	l := newNoOutputLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", fields...)
	}
}

func BenchmarkNoOutput_Info_TenFields(b *testing.B) {
	l := newNoOutputLogger()
	fields := tenFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", fields...)
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

// BenchmarkAny_String measures the Any() generic constructor overhead.
func BenchmarkAny_String(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var f Field
	for b.Loop() {
		f = Any("key", "value")
	}
	_ = f
}

func BenchmarkAny_Int(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var f Field
	for b.Loop() {
		f = Any("port", 8080)
	}
	_ = f
}

// ---- Writer benchmarks ------------------------------------------------------
//
// Direct writer benchmarks: the entry is fully formatted and Write is invoked
// on the sink per record.

func BenchmarkJSONWriter_FiveFields(b *testing.B) {
	sink := &benchSink{}
	w := NewJSONWriter(sink)
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
	b.StopTimer()
	reportSink(b, sink)
}

// unicodeFields mirrors fiveFields with realistic non-ASCII content so the JSON
// encoder's multibyte path stays measured alongside the ASCII control benchmark.
func unicodeFields() []Field {
	return []Field{
		String("service", "api-gateway"),
		String("payload", "ユーザー λογ audit ✓ 🎉 — done"),
		Int("port", 8080),
		Float64("latency_ms", 1.23),
		Bool("success", true),
	}
}

func BenchmarkJSONWriter_FiveFields_Unicode(b *testing.B) {
	sink := &benchSink{}
	w := NewJSONWriter(sink)
	fields := unicodeFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e := GetEntry()
		e.SetLevel(LevelInfo)
		e.SetMessage("request completed ✓ 日本語")
		e.SetTime(time.Now())
		e.WithFields(fields...)
		_ = w.Write(e)
		e.Write()
		e.Release()
	}
	b.StopTimer()
	reportSink(b, sink)
}

func BenchmarkConsoleWriter_FiveFields(b *testing.B) {
	// Template path (default): exercises theme + template formatting.
	sink := &benchSink{}
	w := NewConsoleWriter(sink, ThemeNightOwl)
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
	b.StopTimer()
	reportSink(b, sink)
}

// BenchmarkConsoleWriter_NoTemplate exercises the formatEntry fallback path
// when no template is set (nil theme disables colour and template).
func BenchmarkConsoleWriter_NoTemplate(b *testing.B) {
	sink := &benchSink{}
	w := NewConsoleWriterWithOptions(sink, nil, time.UTC, FieldDisplayInline)
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
	b.StopTimer()
	reportSink(b, sink)
}

// BenchmarkConsoleWriter_WriteStatus_WithCaller measures the direct status
// path (WriteStatus/WriteStatusSecure with caller information), which only
// runs on TTY consoles via buildStatusLine. The caller line number formats
// through the stack-buffer helper, so this path must stay allocation-free.
func BenchmarkConsoleWriter_WriteStatus_WithCaller(b *testing.B) {
	sink := &benchSink{}
	w := NewConsoleWriter(sink, ThemeNightOwl)
	// buildStatusLine runs only when the writer is a TTY; force it (the sink
	// is not a terminal) so the benchmark covers the status-shaped path.
	w.isTTY = true
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e := GetEntry()
		e.SetLevel(LevelInfo)
		e.SetMessage("deployed")
		e.SetTime(time.Now())
		e.statusKind = StatusOK
		e.Caller = "deploy/run.go"
		e.Line = 42
		_ = w.WriteStatus(e)
		e.Write()
		e.Release()
	}
	b.StopTimer()
	reportSink(b, sink)
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
	l, console, structured := newSinkLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Info("parallel request", fields...)
		}
	})
	reportSink(b, console, structured)
}

func BenchmarkJSONWriter_Parallel(b *testing.B) {
	sink := &benchSink{}
	w := NewJSONWriter(sink)
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
	reportSink(b, sink)
}

// ---- Tree-mode rendering benchmarks ----------------------------------------
//
// Console-only serialization (no structured writer), with the display-mode
// variants that exercise the tree layout path.

func newTreeSinkLogger() (*Logger, *benchSink) {
	console := &benchSink{}
	cfg := defaultConfig()
	cfg.ConsoleOutput = console
	cfg.StructuredOutput = nil
	cfg.ConsoleLevel = LevelDebug
	cfg.FieldDisplayMode = FieldDisplayTree
	return newFromConfig(cfg), console
}

// BenchmarkInfo_TreeMode measures the badge-style tree-mode path where the
// cachedIndentStr is used in place of strings.Repeat on every field.
func BenchmarkInfo_TreeMode(b *testing.B) {
	l, console := newTreeSinkLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("tree mode entry", fields...)
	}
	b.StopTimer()
	reportSink(b, console)
}

// BenchmarkInfo_TreeMode_Parallel measures concurrent tree-mode throughput.
func BenchmarkInfo_TreeMode_Parallel(b *testing.B) {
	l, console := newTreeSinkLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Info("tree mode entry", fields...)
		}
	})
	reportSink(b, console)
}

// BenchmarkDetailed_TreeMode measures the Detailed() child path which forces
// tree display regardless of the configured FieldDisplayMode.
func BenchmarkDetailed_TreeMode(b *testing.B) {
	l, console, structured := newSinkLogger()
	d := l.Detailed()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		d.Info("detailed entry", fields...)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

// ---- Async fan-out benchmarks ----------------------------------------------
//
// Async paths must be judged on delivered entries, drops and drain time —
// never on enqueue throughput alone. Both are reported as custom metrics;
// the drain happens with the timer stopped.

// BenchmarkAsync_MultiWriter_Deliver logs through a MultiWriter-attached JSON
// writer that drains concurrently with the producers. The timed loop is an
// ENQUEUE ATTEMPT, not a delivery: the non-blocking send drops whenever the
// worker channel is full, so ns/op is the cost of attempting a log call while
// a worker drains concurrently — the delivered/op and drops/op ratios beside
// it say how many attempts landed. e2e-ns/delivered extends the measurement
// past StopTimer through Close/drain: wall clock from loop start until every
// accepted record is written, divided by records actually delivered. That is
// the number to cite for end-to-end delivery cost.
func BenchmarkAsync_MultiWriter_Deliver(b *testing.B) {
	sink := &benchSink{}
	counter := &entryCountingWriter{inner: NewJSONWriter(sink)}
	l := newNoOutputLogger()
	l.SetLevel(LevelDebug)
	l.AddWriter("json", counter)
	mw := l.writers.mw
	b.ReportAllocs()
	b.ResetTimer()
	loopStart := time.Now()
	for b.Loop() {
		l.Info("async entry")
	}
	b.StopTimer()
	drainStart := time.Now()
	if err := l.Close(); err != nil {
		b.Fatalf("close: %v", err)
	}
	drainNs := time.Since(drainStart).Nanoseconds()
	delivered := counter.entries.Load()
	dropped := mw.DroppedCount()
	b.ReportMetric(float64(drainNs), "drain-ns")
	if b.N > 0 {
		b.ReportMetric(float64(delivered)/float64(b.N), "delivered/op")
		b.ReportMetric(float64(dropped)/float64(b.N), "drops/op")
		if delivered > 0 {
			b.ReportMetric(float64(time.Since(loopStart).Nanoseconds())/float64(delivered), "e2e-ns/delivered")
		}
	}
	// The MultiWriter is created for this benchmark alone, so dropped counts
	// only entries this loop enqueued and cannot exceed b.N.
	if int64(b.N) != delivered+int64(dropped) { //nolint:gosec // G115: dropped <= b.N < MaxInt64 on this fresh MultiWriter, conversion cannot overflow
		b.Fatalf("accounting mismatch: enqueued %d, delivered %d, dropped %d", b.N, delivered, dropped)
	}
}

// BenchmarkAsync_MultiWriter_DropPath gates the worker's writer so its channel
// fills and the non-blocking send drops. The timed loop measures the enqueue +
// drop path; the release, drain and accounting happen with the timer stopped.
func BenchmarkAsync_MultiWriter_DropPath(b *testing.B) {
	gated := newGatedWriter()
	l := newNoOutputLogger()
	l.SetLevel(LevelDebug)
	l.AddWriter("gated", gated)
	mw := l.writers.mw
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("async entry")
	}
	b.StopTimer()
	drainStart := time.Now()
	close(gated.release)
	if err := l.Close(); err != nil {
		b.Fatalf("close: %v", err)
	}
	drainNs := time.Since(drainStart).Nanoseconds()
	delivered := gated.writes.Load()
	dropped := mw.DroppedCount()
	b.ReportMetric(float64(drainNs), "drain-ns")
	if b.N > 0 {
		b.ReportMetric(float64(delivered)/float64(b.N), "delivered/op")
		b.ReportMetric(float64(dropped)/float64(b.N), "drops/op")
	}
	// The MultiWriter is created for this benchmark alone, so dropped counts
	// only entries this loop enqueued and cannot exceed b.N.
	if int64(b.N) != delivered+int64(dropped) { //nolint:gosec // G115: dropped <= b.N < MaxInt64 on this fresh MultiWriter, conversion cannot overflow
		b.Fatalf("accounting mismatch: enqueued %d, delivered %d, dropped %d", b.N, delivered, dropped)
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
// small pre-built payload (4 lines). Construction is excluded from the timer
// so we isolate the indentation + write path.
func BenchmarkLogger_Render(b *testing.B) {
	l, console, structured := newSinkLogger()
	payload := []byte("col1  col2\ncell1 cell2\ncell3 cell4\ncell5 cell6\n")
	r := &bytesRenderable{data: payload}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Render(r)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

// BenchmarkLogger_RenderRaw measures the flush-left write path — no indentation
// computation, so should be slightly cheaper than Render.
func BenchmarkLogger_RenderRaw(b *testing.B) {
	l, console, structured := newSinkLogger()
	payload := []byte("col1  col2\ncell1 cell2\ncell3 cell4\ncell5 cell6\n")
	r := &bytesRenderable{data: payload}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.RenderRaw(r)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

// BenchmarkLogger_Newline measures the trivial-call overhead of inserting a
// blank line under the console writer mutex.
func BenchmarkLogger_Newline(b *testing.B) {
	l, console, structured := newSinkLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Newline()
	}
	b.StopTimer()
	reportSink(b, console, structured)
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

// ---- v2 budget stubs --------------------------------------------------------
// These benchmarks establish the v1 baselines against which v2 targets are measured.
// See docs/bench-v1.1.3.txt for the captured numbers.

// BenchmarkWithComponent_Equivalent measures the cost of producing a child
// logger with a component field via With(). No log call is made, so sink
// delivery is not part of this measurement.
func BenchmarkWithComponent_Equivalent(b *testing.B) {
	l, _, _ := newSinkLogger()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		child := l.With(String("component", "auth-service"))
		_ = child
	}
}

// BenchmarkSecureScan_NoMatch measures an enabled log call whose message
// contains no '<', so the secure-tag scan short-circuits before field
// formatting while serialization still runs.
func BenchmarkSecureScan_NoMatch(b *testing.B) {
	l, console, structured := newSinkLogger()
	fields := fiveFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed successfully with no sensitive data in message", fields...)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

// BenchmarkSecureField_UntrustedWriter documents the cost of a Secure field
// hitting an untrusted JSON writer. Documents one redacted string alloc per
// affected (entry × untrusted writer) pair. The Secure() call itself is one
// alloc at construction; the alloc here is for string allocation on format path.
func BenchmarkSecureField_UntrustedWriter(b *testing.B) {
	l, console, structured := newSinkLogger()
	untrusted := &benchSink{}
	// Attach an untrusted JSON writer so scanSecure=true and redaction fires.
	l.AddWriter("json", NewJSONWriter(untrusted))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", Secure("session", "abc123def456"))
	}
	b.StopTimer()
	reportSink(b, console, structured, untrusted)
}

// ---- Inline-indicator benchmarks --------------------------------------------

// newIndicatorSinkLogger builds a logger with the full indicator set enabled
// and a console writer delivering to a real sink. Used to isolate indicator
// render cost.
func newIndicatorSinkLogger() (*Logger, *benchSink) {
	console := &benchSink{}
	cfg := defaultConfig()
	cfg.ConsoleOutput = console
	cfg.StructuredOutput = nil
	cfg.ConsoleLevel = LevelDebug
	cfg.Indicators = inlineIndicators{
		component:      true,
		componentField: "component",
		componentWidth: 8,
		countFields:    []string{"count"},
		timingFields:   []string{"startup_ms"},
		statePairs:     [][2]string{{"old_state", "new_state"}},
		removeFromTree: true,
		showGlyphs:     false,
		glyphsExplicit: true,
	}
	return newFromConfig(cfg), console
}

// indicatorFields returns a representative field set that matches every indicator
// key (component, count, timing, state pair) plus one ordinary field.
func indicatorFields() []Field {
	return []Field{
		String("component", "Scout"),
		Int("count", 4),
		Int("startup_ms", 2000),
		String("old_state", "idle"),
		String("new_state", "running"),
		String("env", "prod"),
	}
}

// noMatchFields returns a field set that contains no indicator keys, so the
// pre-scan short-circuits to the baseline path.
func noMatchFields() []Field {
	return []Field{
		String("service", "api-gateway"),
		Int("port", 8080),
		Bool("tls", true),
		String("region", "ap-southeast-2"),
	}
}

// BenchmarkIndicators_ON measures the console render path with indicators fully
// active: component prefix + count + timing + state arrow.
func BenchmarkIndicators_ON(b *testing.B) {
	l, console := newIndicatorSinkLogger()
	fields := indicatorFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("service started", fields...)
	}
	b.StopTimer()
	reportSink(b, console)
}

// BenchmarkIndicators_OFF_NoMatch measures the case where indicators are
// configured but NO entry fields match any indicator key. The pre-scan must
// short-circuit to the baseline code path with no measurable overhead vs a
// plain logger.
func BenchmarkIndicators_OFF_NoMatch(b *testing.B) {
	l, console := newIndicatorSinkLogger()
	fields := noMatchFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", fields...)
	}
	b.StopTimer()
	reportSink(b, console)
}

// BenchmarkIndicators_Disabled is the true baseline: indicators struct is
// entirely zero-valued, so isActive() returns false and the pre-scan is skipped.
func BenchmarkIndicators_Disabled(b *testing.B) {
	l, console, structured := newSinkLogger()
	fields := noMatchFields()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("request completed", fields...)
	}
	b.StopTimer()
	reportSink(b, console, structured)
}

// BenchmarkIndicators_Timing_IntMs isolates the timing-render path: an integer
// millisecond field promoted into the timing bracket and formatted by
// writeSmartDuration. Integer input now formats in ms units directly, so this
// path must stay allocation-free.
func BenchmarkIndicators_Timing_IntMs(b *testing.B) {
	console := &benchSink{}
	cfg := defaultConfig()
	cfg.ConsoleOutput = console
	cfg.StructuredOutput = nil
	cfg.ConsoleLevel = LevelDebug
	cfg.Indicators = inlineIndicators{
		timingFields:   []string{"startup_ms"},
		removeFromTree: true,
		showGlyphs:     false,
		glyphsExplicit: true,
	}
	l := newFromConfig(cfg)
	fields := []Field{Int("startup_ms", 2000)}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.Info("started", fields...)
	}
	b.StopTimer()
	reportSink(b, console)
}

package velocity

import (
	"io"
	"sync/atomic"
	"testing"
)

// benchSink is the serialization benchmark sink. io.Discard is not neutral in
// this package: newFromConfig treats it as "no output" and never constructs
// the console or JSON writers, so a benchmark wired to io.Discard measures
// field assembly and entry pooling but zero serialization. benchSink is a real
// io.Writer — writers are constructed, every entry is formatted, and Write is
// invoked per record. The two atomic adds per Write are the price of honest
// byte accounting and are identical in every run that shares these fixtures,
// so before/after comparisons remain valid.
type benchSink struct {
	bytes  atomic.Int64
	writes atomic.Int64
}

func (s *benchSink) Write(p []byte) (int, error) {
	s.bytes.Add(int64(len(p)))
	s.writes.Add(1)
	return len(p), nil
}

// reportSink publishes delivered sink volume next to ns/op so a benchmark
// reader can confirm output actually flowed (or, for disabled paths, did not).
func reportSink(b *testing.B, sinks ...*benchSink) {
	b.Helper()
	var sinkBytes, writes int64
	for _, s := range sinks {
		sinkBytes += s.bytes.Load()
		writes += s.writes.Load()
	}
	if b.N == 0 {
		return
	}
	n := float64(b.N)
	b.ReportMetric(float64(sinkBytes)/n, "sinkB/op")
	b.ReportMetric(float64(writes)/n, "sinkW/op")
}

// newSinkLogger builds a logger whose console and JSON writers deliver
// formatted records to real sinks. Both levels are Debug so every entry
// serializes to both formats — the configuration the enabled-path benchmarks
// claim to measure. The sinks are not terminals: colour resolves off, matching
// piped production output.
func newSinkLogger() (*Logger, *benchSink, *benchSink) {
	console := &benchSink{}
	structured := &benchSink{}
	cfg := defaultConfig()
	cfg.ConsoleOutput = console
	cfg.StructuredOutput = structured
	cfg.ConsoleLevel = LevelDebug
	cfg.StructuredLevel = LevelDebug
	return newFromConfig(cfg), console, structured
}

// newNoOutputLogger keeps the historical io.Discard configuration. Because
// newFromConfig maps io.Discard to "no output", no writer is constructed and
// log calls stop after field assembly, entry pooling and the writer nil
// checks. Benchmarks built on it are labelled NoOutput: they measure the
// no-output pipeline only and must never be presented as serialization
// numbers or compared against serialization benchmarks.
func newNoOutputLogger() *Logger {
	cfg := defaultConfig()
	cfg.ConsoleOutput = io.Discard
	cfg.StructuredOutput = io.Discard
	cfg.ConsoleLevel = LevelDebug
	cfg.StructuredLevel = LevelDebug
	return newFromConfig(cfg)
}

// gatedWriter blocks every Write until release is closed, then counts the
// entries it accepts. Used by drop-accounting benchmarks: while gated the
// worker makes no progress, its channel fills, and MultiWriter's non-blocking
// send produces observable drops instead of hidden back-pressure.
type gatedWriter struct {
	release chan struct{}
	writes  atomic.Int64
}

func newGatedWriter() *gatedWriter {
	return &gatedWriter{release: make(chan struct{})}
}

func (g *gatedWriter) Write(_ *Entry) error {
	<-g.release
	g.writes.Add(1)
	return nil
}

func (g *gatedWriter) Close() error { return nil }

// entryCountingWriter wraps a Writer and counts the entries delivered to it,
// independently of how many io.Writer calls the inner writer makes per entry.
// Used for exact delivered/dropped accounting in async benchmarks.
type entryCountingWriter struct {
	inner   Writer
	entries atomic.Int64
}

func (c *entryCountingWriter) Write(e *Entry) error {
	c.entries.Add(1)
	return c.inner.Write(e)
}

func (c *entryCountingWriter) Close() error { return c.inner.Close() }

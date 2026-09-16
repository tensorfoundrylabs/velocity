package velocity_test

import (
	"sync/atomic"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity/v2"
)

var (
	benchPrettyHeaders = []string{"Service", "Status"}
	benchPrettyRows    = [][]string{
		{"api-gateway", "running"},
		{"worker", "stopped"},
		{"scheduler", "running"},
	}
)

// prettyBenchSink is a real io.Writer so the full table render — layout,
// padding and the write itself — is measured. See bench_sink_test.go in the
// root package for why io.Discard is not a neutral sink here.
type prettyBenchSink struct {
	bytes  atomic.Int64
	writes atomic.Int64
}

func (s *prettyBenchSink) Write(p []byte) (int, error) {
	s.bytes.Add(int64(len(p)))
	s.writes.Add(1)
	return len(p), nil
}

// newPrettyBenchLogger builds a logger with a console writer that delivers to
// a real sink, so NewPrettyFromLogger's RenderRaw path performs actual writes.
func newPrettyBenchLogger(sink *prettyBenchSink) *velocity.Logger {
	return velocity.New(
		velocity.WithConsoleOutput(sink),
		velocity.WithLevel(velocity.LevelDebug),
	)
}

// BenchmarkPretty_NewFromLogger_Table measures the full render path via
// NewPrettyFromLogger: table layout plus the write through Logger.RenderRaw.
func BenchmarkPretty_NewFromLogger_Table(b *testing.B) {
	sink := &prettyBenchSink{}
	log := newPrettyBenchLogger(sink)
	p := velocity.NewPrettyFromLogger(log)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Table(benchPrettyHeaders, benchPrettyRows)
	}
	b.StopTimer()
	if b.N > 0 {
		n := float64(b.N)
		b.ReportMetric(float64(sink.bytes.Load())/n, "sinkB/op")
		b.ReportMetric(float64(sink.writes.Load())/n, "sinkW/op")
	}
}

// BenchmarkPretty_New_Table measures the same table render via the standalone
// NewPretty path.
func BenchmarkPretty_New_Table(b *testing.B) {
	sink := &prettyBenchSink{}
	p := velocity.NewPretty(sink, velocity.ThemeNightOwl)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Table(benchPrettyHeaders, benchPrettyRows)
	}
	b.StopTimer()
	if b.N > 0 {
		n := float64(b.N)
		b.ReportMetric(float64(sink.bytes.Load())/n, "sinkB/op")
		b.ReportMetric(float64(sink.writes.Load())/n, "sinkW/op")
	}
}

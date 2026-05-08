package pretty_test

import (
	"io"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity"
	"github.com/tensorfoundrylabs/velocity/pretty"
)

var (
	benchHeaders = []string{"Service", "Status"}
	benchRows    = [][]string{
		{"api-gateway", "running"},
		{"worker", "stopped"},
		{"scheduler", "running"},
	}
)

func newBenchLogger() *velocity.Logger {
	cfg := velocity.DefaultConfig()
	cfg.ConsoleOutput = io.Discard
	cfg.StructuredOutput = io.Discard
	cfg.ConsoleLevel = velocity.LevelDebug
	cfg.StructuredLevel = velocity.LevelDebug
	return velocity.NewWithConfig(cfg)
}

// BenchmarkPretty_NewFromLogger_Table measures the full path: construct a Pretty
// via NewFromLogger, then render a 3-row table through the logger writer.
// Construction is excluded from the timer; we want the per-render cost.
func BenchmarkPretty_NewFromLogger_Table(b *testing.B) {
	log := newBenchLogger()
	p := pretty.NewFromLogger(log)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Table(benchHeaders, benchRows)
	}
}

// BenchmarkPretty_New_Table measures the same table render via the standalone
// pretty.New path writing to io.Discard, for direct comparison with NewFromLogger.
func BenchmarkPretty_New_Table(b *testing.B) {
	p := pretty.New(io.Discard, velocity.ThemeNightOwl)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Table(benchHeaders, benchRows)
	}
}

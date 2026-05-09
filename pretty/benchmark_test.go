package pretty_test

import (
	"io"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity"
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

// BenchmarkPretty_NewFromLogger_Table measures the full render path via NewPrettyFromLogger.
func BenchmarkPretty_NewFromLogger_Table(b *testing.B) {
	log := newBenchLogger()
	p := velocity.NewPrettyFromLogger(log)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Table(benchHeaders, benchRows)
	}
}

// BenchmarkPretty_New_Table measures the same table render via the standalone NewPretty path.
func BenchmarkPretty_New_Table(b *testing.B) {
	p := velocity.NewPretty(io.Discard, velocity.ThemeNightOwl)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Table(benchHeaders, benchRows)
	}
}

package velocity_test

import (
	"io"
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

func newBenchLogger() *velocity.Logger {
	return velocity.New(
		velocity.WithConsoleOutput(io.Discard),
		velocity.WithStructuredOutput(io.Discard),
		velocity.WithLevel(velocity.LevelDebug),
		velocity.WithStructuredLevel(velocity.LevelDebug),
	)
}

// BenchmarkPretty_NewFromLogger_Table measures the full render path via NewPrettyFromLogger.
func BenchmarkPretty_NewFromLogger_Table(b *testing.B) {
	log := newBenchLogger()
	p := velocity.NewPrettyFromLogger(log)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Table(benchPrettyHeaders, benchPrettyRows)
	}
}

// BenchmarkPretty_New_Table measures the same table render via the standalone NewPretty path.
func BenchmarkPretty_New_Table(b *testing.B) {
	p := velocity.NewPretty(io.Discard, velocity.ThemeNightOwl)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.Table(benchPrettyHeaders, benchPrettyRows)
	}
}

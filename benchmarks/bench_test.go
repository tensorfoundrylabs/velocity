// Package benchmarks compares velocity against other Go logging libraries.
// Every library writes to a real counting sink: all libraries serialize and
// deliver their output, so the comparison measures comparable work. Output
// validity and disabled-path silence are asserted by TestPreflight_* in
// preflight_test.go, outside the timed loops.
package benchmarks

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	charmlog "github.com/charmbracelet/log"
	"github.com/pterm/pterm"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	velocity "github.com/tensorfoundrylabs/velocity/v2"
)

// benchSink is a real io.Writer that counts delivered bytes and write calls.
// Each benchmark constructs a fresh sink so per-op delivery volume can be
// reported next to ns/op. The atomics are identical for every library, so the
// comparison stays fair.
type benchSink struct {
	bytes  atomic.Int64
	writes atomic.Int64
}

func (s *benchSink) Write(p []byte) (int, error) {
	s.bytes.Add(int64(len(p)))
	s.writes.Add(1)
	return len(p), nil
}

func reportSink(b *testing.B, s *benchSink) {
	b.Helper()
	if b.N == 0 {
		return
	}
	n := float64(b.N)
	b.ReportMetric(float64(s.bytes.Load())/n, "sinkB/op")
	b.ReportMetric(float64(s.writes.Load())/n, "sinkW/op")
}

// Library describes the subset of a logging library's API needed for comparison.
// Setup receives the shared sink so every library writes to the same kind of
// destination. Format records the output encoding the configuration produces,
// used by the preflight assertions ("json" output must parse as JSON; "text"
// output must contain the message).
// Add new libraries by appending to the libraries slice below.
type Library struct {
	Name  string
	Setup func(w io.Writer) any

	Format string

	Info           func(logger any)
	InfoWithFields func(logger any)
	InfoDisabled   func(logger any)
	WithFields     func(logger any) any

	// AccumulatedCtx returns a logger pre-loaded with 10 contextual fields.
	AccumulatedCtx  func(w io.Writer) any
	InfoAccumulated func(logger any)

	// MixedTypes logs 8 different field types in a single call.
	MixedTypes func(logger any)

	// ErrorLog logs with a single error field.
	ErrorLog func(logger any)

	// TenFields logs with 10 inline fields (no With).
	TenFields func(logger any)

	// LargeMsg logs a 1 KB message string; tests buffer management.
	LargeMsg func(logger any)
}

var libraries = []Library{
	{
		Name: "velocity",
		Setup: func(w io.Writer) any {
			// JSON-only to the shared sink: console level set to Off so only
			// the JSON serialisation path runs. Makes the comparison fair
			// against pure structured loggers (zap, zerolog, slog).
			return velocity.New(
				velocity.WithLevel(velocity.LevelOff),
				velocity.WithStructuredOutput(w),
				velocity.WithStructuredLevel(velocity.LevelDebug),
			)
		},
		Format: "json",
		Info: func(l any) {
			l.(*velocity.Logger).Info("request completed")
		},
		InfoWithFields: func(l any) {
			l.(*velocity.Logger).Info(
				"request completed",
				velocity.String("method", "GET"),
				velocity.Int("status", 200),
				velocity.Float64("latency", 0.042),
			)
		},
		InfoDisabled: func(l any) {
			l.(*velocity.Logger).Debug("suppressed")
		},
		WithFields: func(l any) any {
			return l.(*velocity.Logger).With(
				velocity.String("service", "api"),
				velocity.String("region", "ap-southeast-2"),
			)
		},
		AccumulatedCtx: func(w io.Writer) any {
			l := velocity.New(
				velocity.WithLevel(velocity.LevelOff),
				velocity.WithStructuredOutput(w),
				velocity.WithStructuredLevel(velocity.LevelDebug),
			)
			return l.With(
				velocity.String("k1", "v1"),
				velocity.String("k2", "v2"),
				velocity.String("k3", "v3"),
				velocity.String("k4", "v4"),
				velocity.String("k5", "v5"),
				velocity.Int("k6", 6),
				velocity.Int("k7", 7),
				velocity.Bool("k8", true),
				velocity.Float64("k9", 9.9),
				velocity.String("k10", "v10"),
			)
		},
		InfoAccumulated: func(l any) {
			l.(*velocity.Logger).Info("accumulated context")
		},
		MixedTypes: func(l any) {
			l.(*velocity.Logger).Info(
				"mixed types",
				velocity.String("str", "hello"),
				velocity.Int("num", 42),
				velocity.Float64("flt", 3.14),
				velocity.Bool("flag", true),
				velocity.Time("ts", benchTime),
				velocity.Duration("dur", 250*time.Millisecond),
				velocity.Error("err", benchErr),
				velocity.Any("any", "interface-value"),
			)
		},
		ErrorLog: func(l any) {
			l.(*velocity.Logger).Error(
				"something failed",
				velocity.Error("err", benchErr),
			)
		},
		LargeMsg: func(l any) {
			l.(*velocity.Logger).Info(largeMsg)
		},
		TenFields: func(l any) {
			l.(*velocity.Logger).Info(
				"ten fields",
				velocity.String("f1", "v1"),
				velocity.String("f2", "v2"),
				velocity.String("f3", "v3"),
				velocity.String("f4", "v4"),
				velocity.String("f5", "v5"),
				velocity.Int("f6", 6),
				velocity.Int("f7", 7),
				velocity.Bool("f8", true),
				velocity.Float64("f9", 9.9),
				velocity.String("f10", "v10"),
			)
		},
	},
	{
		Name: "zap",
		Setup: func(w io.Writer) any {
			core := zapcore.NewCore(
				zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
				zapcore.AddSync(w),
				zap.DebugLevel,
			)
			return zap.New(core)
		},
		Format: "json",
		Info: func(l any) {
			l.(*zap.Logger).Info("request completed")
		},
		InfoWithFields: func(l any) {
			l.(*zap.Logger).Info(
				"request completed",
				zap.String("method", "GET"),
				zap.Int("status", 200),
				zap.Float64("latency", 0.042),
			)
		},
		InfoDisabled: func(l any) {
			l.(*zap.Logger).Debug("suppressed")
		},
		WithFields: func(l any) any {
			return l.(*zap.Logger).With(
				zap.String("service", "api"),
				zap.String("region", "ap-southeast-2"),
			)
		},
		AccumulatedCtx: func(w io.Writer) any {
			core := zapcore.NewCore(
				zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
				zapcore.AddSync(w),
				zap.DebugLevel,
			)
			return zap.New(core).With(
				zap.String("k1", "v1"),
				zap.String("k2", "v2"),
				zap.String("k3", "v3"),
				zap.String("k4", "v4"),
				zap.String("k5", "v5"),
				zap.Int("k6", 6),
				zap.Int("k7", 7),
				zap.Bool("k8", true),
				zap.Float64("k9", 9.9),
				zap.String("k10", "v10"),
			)
		},
		InfoAccumulated: func(l any) {
			l.(*zap.Logger).Info("accumulated context")
		},
		MixedTypes: func(l any) {
			l.(*zap.Logger).Info(
				"mixed types",
				zap.String("str", "hello"),
				zap.Int("num", 42),
				zap.Float64("flt", 3.14),
				zap.Bool("flag", true),
				zap.Time("ts", benchTime),
				zap.Duration("dur", 250*time.Millisecond),
				zap.Error(benchErr),
				zap.Any("any", "interface-value"),
			)
		},
		ErrorLog: func(l any) {
			l.(*zap.Logger).Error("something failed", zap.Error(benchErr))
		},
		LargeMsg: func(l any) {
			l.(*zap.Logger).Info(largeMsg)
		},
		TenFields: func(l any) {
			l.(*zap.Logger).Info(
				"ten fields",
				zap.String("f1", "v1"),
				zap.String("f2", "v2"),
				zap.String("f3", "v3"),
				zap.String("f4", "v4"),
				zap.String("f5", "v5"),
				zap.Int("f6", 6),
				zap.Int("f7", 7),
				zap.Bool("f8", true),
				zap.Float64("f9", 9.9),
				zap.String("f10", "v10"),
			)
		},
	},
	{
		Name: "zerolog",
		Setup: func(w io.Writer) any {
			return zerolog.New(w).Level(zerolog.DebugLevel)
		},
		Format: "json",
		Info: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Info().Msg("request completed")
		},
		InfoWithFields: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Info().
				Str("method", "GET").
				Int("status", 200).
				Float64("latency", 0.042).
				Msg("request completed")
		},
		InfoDisabled: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Debug().Msg("suppressed")
		},
		WithFields: func(l any) any {
			lg := l.(zerolog.Logger)
			return lg.With().
				Str("service", "api").
				Str("region", "ap-southeast-2").
				Logger()
		},
		AccumulatedCtx: func(w io.Writer) any {
			return zerolog.New(w).Level(zerolog.DebugLevel).With().
				Str("k1", "v1").
				Str("k2", "v2").
				Str("k3", "v3").
				Str("k4", "v4").
				Str("k5", "v5").
				Int("k6", 6).
				Int("k7", 7).
				Bool("k8", true).
				Float64("k9", 9.9).
				Str("k10", "v10").
				Logger()
		},
		InfoAccumulated: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Info().Msg("accumulated context")
		},
		MixedTypes: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Info().
				Str("str", "hello").
				Int("num", 42).
				Float64("flt", 3.14).
				Bool("flag", true).
				Time("ts", benchTime).
				Dur("dur", 250*time.Millisecond).
				Err(benchErr).
				Interface("any", "interface-value").
				Msg("mixed types")
		},
		ErrorLog: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Error().Err(benchErr).Msg("something failed")
		},
		LargeMsg: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Info().Msg(largeMsg)
		},
		TenFields: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Info().
				Str("f1", "v1").
				Str("f2", "v2").
				Str("f3", "v3").
				Str("f4", "v4").
				Str("f5", "v5").
				Int("f6", 6).
				Int("f7", 7).
				Bool("f8", true).
				Float64("f9", 9.9).
				Str("f10", "v10").
				Msg("ten fields")
		},
	},
	{
		Name: "slog",
		Setup: func(w io.Writer) any {
			return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			}))
		},
		Format: "json",
		Info: func(l any) {
			l.(*slog.Logger).Info("request completed")
		},
		InfoWithFields: func(l any) {
			l.(*slog.Logger).Info(
				"request completed",
				slog.String("method", "GET"),
				slog.Int("status", 200),
				slog.Float64("latency", 0.042),
			)
		},
		InfoDisabled: func(l any) {
			l.(*slog.Logger).Debug("suppressed")
		},
		WithFields: func(l any) any {
			return l.(*slog.Logger).With(
				slog.String("service", "api"),
				slog.String("region", "ap-southeast-2"),
			)
		},
		AccumulatedCtx: func(w io.Writer) any {
			return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})).With(
				slog.String("k1", "v1"),
				slog.String("k2", "v2"),
				slog.String("k3", "v3"),
				slog.String("k4", "v4"),
				slog.String("k5", "v5"),
				slog.Int("k6", 6),
				slog.Int("k7", 7),
				slog.Bool("k8", true),
				slog.Float64("k9", 9.9),
				slog.String("k10", "v10"),
			)
		},
		InfoAccumulated: func(l any) {
			l.(*slog.Logger).Info("accumulated context")
		},
		MixedTypes: func(l any) {
			l.(*slog.Logger).Info(
				"mixed types",
				slog.String("str", "hello"),
				slog.Int("num", 42),
				slog.Float64("flt", 3.14),
				slog.Bool("flag", true),
				slog.Time("ts", benchTime),
				slog.Duration("dur", 250*time.Millisecond),
				slog.Any("err", benchErr),
				slog.Any("any", "interface-value"),
			)
		},
		ErrorLog: func(l any) {
			l.(*slog.Logger).Error("something failed", slog.Any("err", benchErr))
		},
		LargeMsg: func(l any) {
			l.(*slog.Logger).Info(largeMsg)
		},
		TenFields: func(l any) {
			l.(*slog.Logger).Info(
				"ten fields",
				slog.String("f1", "v1"),
				slog.String("f2", "v2"),
				slog.String("f3", "v3"),
				slog.String("f4", "v4"),
				slog.String("f5", "v5"),
				slog.Int("f6", 6),
				slog.Int("f7", 7),
				slog.Bool("f8", true),
				slog.Float64("f9", 9.9),
				slog.String("f10", "v10"),
			)
		},
	},
	{
		Name: "charmbracelet",
		Setup: func(w io.Writer) any {
			lg := charmlog.New(w)
			lg.SetLevel(charmlog.DebugLevel)
			return lg
		},
		Format: "text",
		Info: func(l any) {
			l.(*charmlog.Logger).Info("request completed")
		},
		InfoWithFields: func(l any) {
			l.(*charmlog.Logger).Info(
				"request completed",
				"method", "GET",
				"status", 200,
				"latency", 0.042,
			)
		},
		InfoDisabled: func(l any) {
			l.(*charmlog.Logger).Debug("suppressed")
		},
		WithFields: func(l any) any {
			return l.(*charmlog.Logger).With(
				"service", "api",
				"region", "ap-southeast-2",
			)
		},
		AccumulatedCtx: func(w io.Writer) any {
			lg := charmlog.New(w)
			lg.SetLevel(charmlog.DebugLevel)
			return lg.With(
				"k1", "v1", "k2", "v2", "k3", "v3", "k4", "v4", "k5", "v5",
				"k6", 6, "k7", 7, "k8", true, "k9", 9.9, "k10", "v10",
			)
		},
		InfoAccumulated: func(l any) {
			l.(*charmlog.Logger).Info("accumulated context")
		},
		MixedTypes: func(l any) {
			l.(*charmlog.Logger).Info(
				"mixed types",
				"str", "hello",
				"num", 42,
				"flt", 3.14,
				"flag", true,
				"ts", benchTime,
				"dur", 250*time.Millisecond,
				"err", benchErr,
				"any", "interface-value",
			)
		},
		ErrorLog: func(l any) {
			l.(*charmlog.Logger).Error("something failed", "err", benchErr)
		},
		LargeMsg: func(l any) {
			l.(*charmlog.Logger).Info(largeMsg)
		},
		TenFields: func(l any) {
			l.(*charmlog.Logger).Info(
				"ten fields",
				"f1", "v1", "f2", "v2", "f3", "v3", "f4", "v4", "f5", "v5",
				"f6", 6, "f7", 7, "f8", true, "f9", 9.9, "f10", "v10",
			)
		},
	},
	{
		Name: "pterm",
		Setup: func(w io.Writer) any {
			lg := pterm.DefaultLogger.WithLevel(pterm.LogLevelTrace).WithWriter(w)
			return lg
		},
		Format: "text",
		Info: func(l any) {
			lg := l.(*pterm.Logger)
			lg.Info("request completed")
		},
		InfoWithFields: func(l any) {
			lg := l.(*pterm.Logger)
			lg.Info("request completed", lg.Args(
				"method", "GET",
				"status", 200,
				"latency", 0.042,
			))
		},
		InfoDisabled: func(l any) {
			lg := l.(*pterm.Logger)
			lg.Debug("suppressed")
		},
		// pterm has no With() child-logger API; return the same logger.
		WithFields: func(l any) any { return l },
		MixedTypes: func(l any) {
			lg := l.(*pterm.Logger)
			lg.Info("mixed types", lg.Args(
				"str", "hello",
				"num", 42,
				"flt", 3.14,
				"flag", true,
				"dur", 250*time.Millisecond,
				"err", benchErr,
			))
		},
		ErrorLog: func(l any) {
			lg := l.(*pterm.Logger)
			lg.Error("something failed", lg.Args("err", benchErr))
		},
		LargeMsg: func(l any) {
			lg := l.(*pterm.Logger)
			lg.Info(largeMsg)
		},
		TenFields: func(l any) {
			lg := l.(*pterm.Logger)
			lg.Info("ten fields", lg.Args(
				"f1", "v1", "f2", "v2", "f3", "v3", "f4", "v4", "f5", "v5",
				"f6", 6, "f7", 7, "f8", true, "f9", 9.9, "f10", "v10",
			))
		},
	},
}

// disabledLibraries are the same libraries but configured with level=Error
// so that Info/Debug calls are rejected at the level check.
var disabledLibraries = []Library{
	{
		Name: "velocity",
		Setup: func(w io.Writer) any {
			return velocity.New(
				velocity.WithLevel(velocity.LevelOff),
				velocity.WithStructuredOutput(w),
				velocity.WithStructuredLevel(velocity.LevelError),
			)
		},
		Format: "json",
		InfoDisabled: func(l any) {
			l.(*velocity.Logger).Debug("suppressed")
		},
	},
	{
		Name: "zap",
		Setup: func(w io.Writer) any {
			core := zapcore.NewCore(
				zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
				zapcore.AddSync(w),
				zap.ErrorLevel,
			)
			return zap.New(core)
		},
		Format: "json",
		InfoDisabled: func(l any) {
			l.(*zap.Logger).Debug("suppressed")
		},
	},
	{
		Name: "zerolog",
		Setup: func(w io.Writer) any {
			return zerolog.New(w).Level(zerolog.ErrorLevel)
		},
		Format: "json",
		InfoDisabled: func(l any) {
			lg := l.(zerolog.Logger)
			lg.Debug().Msg("suppressed")
		},
	},
	{
		Name: "slog",
		Setup: func(w io.Writer) any {
			return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))
		},
		Format: "json",
		InfoDisabled: func(l any) {
			l.(*slog.Logger).Debug("suppressed")
		},
	},
	{
		Name: "charmbracelet",
		Setup: func(w io.Writer) any {
			lg := charmlog.New(w)
			lg.SetLevel(charmlog.ErrorLevel)
			return lg
		},
		Format: "text",
		InfoDisabled: func(l any) {
			l.(*charmlog.Logger).Debug("suppressed")
		},
	},
	{
		Name: "pterm",
		Setup: func(w io.Writer) any {
			return pterm.DefaultLogger.WithLevel(pterm.LogLevelError).WithWriter(w)
		},
		Format: "text",
		InfoDisabled: func(l any) {
			l.(*pterm.Logger).Debug("suppressed")
		},
	},
}

// Package-level values created once so benchmarks don't include allocation cost.
var (
	benchErr  = errors.New("benchmark error")
	benchTime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	largeMsg  = strings.Repeat("x", 1024)
)

func BenchmarkLibraries(b *testing.B) {
	for _, lib := range libraries {
		lib := lib

		b.Run(lib.Name+"/Info_NoFields", func(b *testing.B) {
			sink := &benchSink{}
			logger := lib.Setup(sink)
			b.ReportAllocs()
			for b.Loop() {
				lib.Info(logger)
			}
			reportSink(b, sink)
		})

		b.Run(lib.Name+"/Info_ThreeFields", func(b *testing.B) {
			sink := &benchSink{}
			logger := lib.Setup(sink)
			b.ReportAllocs()
			for b.Loop() {
				lib.InfoWithFields(logger)
			}
			reportSink(b, sink)
		})

		b.Run(lib.Name+"/With_TwoFields", func(b *testing.B) {
			sink := &benchSink{}
			logger := lib.Setup(sink)
			b.ReportAllocs()
			for b.Loop() {
				child := lib.WithFields(logger)
				lib.Info(child)
			}
			reportSink(b, sink)
		})

		if lib.TenFields != nil {
			b.Run(lib.Name+"/Info_TenFields", func(b *testing.B) {
				sink := &benchSink{}
				logger := lib.Setup(sink)
				b.ReportAllocs()
				for b.Loop() {
					lib.TenFields(logger)
				}
				reportSink(b, sink)
			})
		}

		if lib.AccumulatedCtx != nil && lib.InfoAccumulated != nil {
			b.Run(lib.Name+"/Accumulated_10Fields", func(b *testing.B) {
				sink := &benchSink{}
				accLogger := lib.AccumulatedCtx(sink)
				b.ReportAllocs()
				for b.Loop() {
					lib.InfoAccumulated(accLogger)
				}
				reportSink(b, sink)
			})
		}

		if lib.MixedTypes != nil {
			b.Run(lib.Name+"/MixedFieldTypes", func(b *testing.B) {
				sink := &benchSink{}
				logger := lib.Setup(sink)
				b.ReportAllocs()
				for b.Loop() {
					lib.MixedTypes(logger)
				}
				reportSink(b, sink)
			})
		}

		if lib.ErrorLog != nil {
			b.Run(lib.Name+"/ErrorField", func(b *testing.B) {
				sink := &benchSink{}
				logger := lib.Setup(sink)
				b.ReportAllocs()
				for b.Loop() {
					lib.ErrorLog(logger)
				}
				reportSink(b, sink)
			})
		}

		// Large message: 1 KB body, tests buffer management.
		if lib.LargeMsg != nil {
			b.Run(lib.Name+"/LargeMessage", func(b *testing.B) {
				sink := &benchSink{}
				logger := lib.Setup(sink)
				b.ReportAllocs()
				for b.Loop() {
					lib.LargeMsg(logger)
				}
				reportSink(b, sink)
			})
		}

		b.Run(lib.Name+"/Parallel_4", func(b *testing.B) {
			sink := &benchSink{}
			logger := lib.Setup(sink)
			b.ReportAllocs()
			b.SetParallelism(4)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					lib.InfoWithFields(logger)
				}
			})
			reportSink(b, sink)
		})

		b.Run(lib.Name+"/Parallel_16", func(b *testing.B) {
			sink := &benchSink{}
			logger := lib.Setup(sink)
			b.ReportAllocs()
			b.SetParallelism(16)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					lib.InfoWithFields(logger)
				}
			})
			reportSink(b, sink)
		})
	}
}

// BenchmarkDisabledLevel measures the cost of a level check that rejects the
// entry. A well-designed logger should approach zero allocations here.
// TestPreflight_DisabledWritesNothing proves these configurations deliver
// zero output for the exact benchmark workload.
func BenchmarkDisabledLevel(b *testing.B) {
	for _, lib := range disabledLibraries {
		lib := lib
		b.Run(lib.Name, func(b *testing.B) {
			sink := &benchSink{}
			logger := lib.Setup(sink)
			b.ReportAllocs()
			for b.Loop() {
				lib.InfoDisabled(logger)
			}
			reportSink(b, sink)
		})
	}
}

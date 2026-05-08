<p align="center">
  <img src="assets/banner.png" alt="Velocity" width="600" /><br/>
  <a href="https://github.com/tensorfoundrylabs/velocity/actions/workflows/ci.yml"><img src="https://github.com/tensorfoundrylabs/velocity/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/tensorfoundrylabs/velocity"><img src="https://pkg.go.dev/badge/github.com/tensorfoundrylabs/velocity.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/tensorfoundrylabs/velocity"><img src="https://goreportcard.com/badge/github.com/tensorfoundrylabs/velocity" alt="Go Report Card"></a>
  <a href="https://github.com/tensorfoundrylabs/velocity/releases/latest"><img src="https://img.shields.io/github/v/release/tensorfoundrylabs/velocity?color=blue" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/tensorfoundrylabs/velocity" alt="License"></a>
  <a href="https://github.com/tensorfoundrylabs/velocity"><img src="https://img.shields.io/github/go-mod/go-version/tensorfoundrylabs/velocity" alt="Go Version"></a>
</p>

Fast, allocation-optimised structured logging for Go with rich terminal output. Battle-tested in TensorFoundry's [FoundryOS](https://tensorfoundry.io/products/foundryos) where it powers all CLI logging.

## Install

```bash
go get github.com/tensorfoundrylabs/velocity
```

## Quick Start

```go
log := velocity.New(os.Stdout)
log.Info("server started", velocity.String("addr", ":8080"), velocity.Int("workers", 4))
```

Or use a preset:

```go
log := velocity.NewDevelopment()                                // coloured console, debug level
log := velocity.NewWithBuilder(velocity.PresetProduction())     // structured JSON, info level
```

## Packages

```go
import (
    "github.com/tensorfoundrylabs/velocity"              // core logging, writers, config, themes
    "github.com/tensorfoundrylabs/velocity/pretty"       // boxes, panels, banners, tables, trees, progress
    velocityslog "github.com/tensorfoundrylabs/velocity/slog"  // log/slog bridge
)
```

| Package | Description |
|---------|-------------|
| `velocity` | Core logger with typed fields, console/JSON/multi/ring-buffer writers, themes, templates |
| `velocity/pretty` | Rich CLI display: `Box`, `Panel`, `Banner`, `Table`, `Tree`, `ProgressBar`, `Spinner` |
| `velocity/slog` | `Handler` implementing `log/slog.Handler` (package name: `velocityslog`) |

## Features

- **Zero-alloc on the hot path** — typed fields (`String`, `Int`, `Float64`, `Bool`, `Duration`, `Error`) use `unsafe.Pointer` storage with no `interface{}` boxing; 5 and 10 pre-built fields log at 34-39 ns with 0 allocs
- **Sub-100 ns logging** — 27 ns with no fields, 2.1 ns for disabled levels, 5.5 ns through a sampler
- **slog bridge** — `velocityslog.NewHandler` implements `log/slog.Handler` for incremental adoption
- **Rich terminal output** — boxes, panels, banners, tables, trees, progress bars and spinners in `velocity/pretty`
- **4 colour themes** — Night Owl (RGB), Solarized, Dracula, Nord; ANSI codes pre-cached at init
- **Log sampling** — `CountSampler` checked before pool acquisition; no allocs on the skip path
- **5 presets** — Development, Production, Container, Testing, HighPerformance
- **Nil-safe and testable** — every public method handles nil receivers; overridable `FatalHandler`; `NewForTesting()`
- **Dynamic writers** — add/remove writers at runtime; `Render`/`RenderRaw`/`Newline` serialised under the console writer mutex

## Performance

### Comparative benchmarks

Here's how Velocity stacks up against popular Go logging libraries (AMD Ryzen 9 5950X, Go 1.24, writing to `io.Discard`):

| Library | Info (no fields) | Info (3 fields) | With + Info | Disabled level |
|---------|-----------------|-----------------|-------------|---------------|
| **velocity** | **31 ns** / 0 alloc | **67 ns** / 1 alloc | **186 ns** / 4 alloc | 41 ns / 0 alloc |
| [zerolog](https://github.com/rs/zerolog) | 89 ns / 0 alloc | 204 ns / 0 alloc | 422 ns / 2 alloc | 10 ns / 0 alloc |
| [zap](https://github.com/uber-go/zap) | 240 ns / 0 alloc | 525 ns / 1 alloc | 1319 ns / 6 alloc | 9 ns / 0 alloc |
| [slog](https://pkg.go.dev/log/slog) | 663 ns / 0 alloc | 1666 ns / 4 alloc | 1684 ns / 11 alloc | 10 ns / 0 alloc |
| [charmbracelet/log](https://github.com/charmbracelet/log) | 4 ns / 0 alloc | 6 ns / 0 alloc | 2618 ns / 5 alloc | 4 ns / 0 alloc |
| [pterm](https://github.com/pterm/pterm) | 12926 ns / 65 alloc | 25334 ns / 144 alloc | 13125 ns / 65 alloc | 19 ns / 0 alloc |

Velocity is ~3x faster than zerolog and ~8x faster than zap on the hot logging path. charmbracelet/log's near-zero numbers are from short-circuiting format work when writing to non-TTY output; its `With` cost (2618 ns) shows the real overhead. pterm is a display library first, and its allocation profile reflects that.

### Realistic workload benchmarks

| Scenario | velocity | [zerolog](https://github.com/rs/zerolog) | [zap](https://github.com/uber-go/zap) | [slog](https://pkg.go.dev/log/slog) |
|----------|----------|---------|-----|------|
| Accumulated context (10 fields) | **45 ns** / 0 alloc | 99 ns / 0 alloc | 344 ns / 0 alloc | 672 ns / 0 alloc |
| Mixed field types (8 types) | **153 ns** / 4 alloc | 799 ns / 2 alloc | 1307 ns / 1 alloc | 2481 ns / 8 alloc |
| Error field | **96 ns** / 1 alloc | 136 ns / 0 alloc | 510 ns / 1 alloc | 912 ns / 1 alloc |
| Large message (1 KB) | **43 ns** / 0 alloc | 419 ns / 0 alloc | 1509 ns / 0 alloc | 2255 ns / 1 alloc |
| 10 inline fields | **117 ns** / 3 alloc | 383 ns / 0 alloc | 1159 ns / 1 alloc | 3170 ns / 10 alloc |
| Parallel (16 goroutines) | 53 ns / 1 alloc | **22 ns** / 0 alloc | 150 ns / 1 alloc | 279 ns / 0 alloc |

zerolog wins the parallel benchmark thanks to its lock-free event chaining design. Velocity wins everything else.

### Internal benchmarks (v1.1, AMD Ryzen 9 5950X, Go 1.24)

| Operation | ns/op | B/op | allocs/op |
|-----------|------:|-----:|----------:|
| Info, no fields | 27 | 0 | 0 |
| Info, 5 pre-built fields | 34 | 0 | 0 |
| Info, 10 pre-built fields | 39 | 0 | 0 |
| Info, tree mode (v1.1) | 36 | 0 | 0 |
| Level check (disabled) | 2.1 | 0 | 0 |
| Sampler check | 5.5 | 0 | 0 |
| Entry pool round-trip | 14 | 0 | 0 |
| Int field construction | 1.3 | 0 | 0 |
| ConsoleWriter, 5 fields | 431 | 32 | 3 |
| JSONWriter, 5 fields | 582 | 0 | 0 |
| JSONWriter, parallel | 170 | 0 | 0 |
| Render / RenderRaw (v1.1) | 1.8 | 0 | 0 |
| slog handler, 3 attrs | 445 | 192 | 6 |

v1.1 highlights: JSON writer dropped from 949 ns/1 alloc to 582 ns/0 alloc (inline hex escape); tree-mode field rendering is now zero-alloc (cached indent string); `Render`/`RenderRaw`/`Newline` are essentially free at ~2 ns.

Run internal benchmarks: `go test -bench=. -benchmem -count=3 ./...`

The comparative benchmark suite lives in `benchmarks/` as a separate Go module.

## Presets

| Preset | Output | Level | Use Case |
|--------|--------|-------|----------|
| `PresetDevelopment` | Coloured console | Debug | Local dev |
| `PresetProduction` | JSON | Info | Structured log aggregation |
| `PresetContainer` | JSON to stdout | Info | Docker/K8s |
| `PresetTesting` | Provided writer | Debug | Test harnesses |
| `PresetHighPerformance` | JSON to stderr | Info | High-volume with sampling |

## Integration

### log/slog bridge

```go
import velocityslog "github.com/tensorfoundrylabs/velocity/slog"

logger := velocity.NewDevelopment()
slog.SetDefault(velocityslog.NewLogger(logger))

slog.Info("request handled", "method", "GET", "status", 200, "duration", 42*time.Millisecond)
```

Groups produce dotted keys: `slog.WithGroup("server").With("host", "localhost")` renders as `server.host`.

### Pretty printing

```go
import "github.com/tensorfoundrylabs/velocity/pretty"

p := pretty.New(os.Stdout, velocity.ThemeNightOwl)
p.Box("Deploy Complete", "All services running")
p.Banner("v2.1.0 - Production release")
```

When a logger exists, prefer `NewFromLogger` — output routes through the logger's console writer and aligns with the message column:

```go
log := velocity.NewDevelopment()
p := pretty.NewFromLogger(log)

log.Info("deploying services")
log.Newline()
log.Render(p.NewTable([]string{"Service", "Status"}, [][]string{
    {"api", "running"},
    {"worker", "running"},
}))
```

### Log rotation with lumberjack

```go
rotator := &lumberjack.Logger{Filename: "/var/log/app.log", MaxSize: 500, Compress: true}
cfg := velocity.DefaultProductionConfig()
cfg.StructuredOutput = rotator
log := velocity.NewWithConfig(cfg)
```

## Dependencies

One: [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term) for TTY detection. No other external dependencies.

## Similar Libraries

- [pTerm](https://github.com/pterm/pterm) — visually rich terminal output library that Velocity's styles are modelled on; Velocity trades some visual features for speed and lower allocations
- [logrus](https://github.com/sirupsen/logrus) — popular structured logger; Velocity targets significantly lower latency for high-volume CLI workloads

## Licence

[MIT](LICENSE)

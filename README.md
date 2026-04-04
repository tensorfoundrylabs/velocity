<p align="center">
  <img src="assets/banner.png" alt="Velocity" />
</p>

<p align="center">
  <a href="https://github.com/tensorfoundrylabs/velocity/actions/workflows/ci.yml"><img src="https://github.com/tensorfoundrylabs/velocity/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/tensorfoundrylabs/velocity"><img src="https://pkg.go.dev/badge/github.com/tensorfoundrylabs/velocity.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/tensorfoundrylabs/velocity"><img src="https://goreportcard.com/badge/github.com/tensorfoundrylabs/velocity" alt="Go Report Card"></a>
  <a href="https://github.com/tensorfoundrylabs/velocity/releases/latest"><img src="https://img.shields.io/github/v/release/tensorfoundrylabs/velocity?color=blue" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/tensorfoundrylabs/velocity" alt="License"></a>
  <a href="https://github.com/tensorfoundrylabs/velocity"><img src="https://img.shields.io/github/go-mod/go-version/tensorfoundrylabs/velocity" alt="Go Version"></a>
</p>

Fast, allocation optimised structured logging for Go with rich terminal output for heavy log presentation and logging. Battle tested and hardened through years of heavy log use. Extracted from TensorFoundry's [FoundryOS](https://tensorfoundry.io/products/foundryos) where it powers all CLI logging.

We used this instead of [pTerm](https://github.com/pterm/pterm) for speed and efficiency, which was used previously in tools like [Olla](http://github.com/thushan/olla), but we hit the limits with FoundryOS.

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

Velocity is split into three packages:

```go
import (
    "github.com/tensorfoundrylabs/velocity"              // core logging, writers, config, themes
    "github.com/tensorfoundrylabs/velocity/pretty"       // CLI display: boxes, panels, banners, tables, trees, progress
    velocityslog "github.com/tensorfoundrylabs/velocity/slog"  // log/slog bridge
)
```

| Package | Description |
|---------|-------------|
| `velocity` | Core logger, fields (`String`, `Int`, `Error`, `Float64`, `Bool`, `Duration`, `Time`, `Stringer`, `Bytes`), writers (console, JSON, multi, ring buffer), config, themes, templates, buffers, pools |
| `velocity/pretty` | `Pretty`, `Box`, `Panel`, `Banner`, `Table`, `Tree`, `Bullet`, `KeyValue`, `SystemInfo`, `TreeItem`, `ProgressBar`, `Spinner`, `MultiProgress` |
| `velocity/slog` | `Handler`, `NewHandler`, `NewLogger` (package name: `velocityslog`) |

## Features

- **Zero-alloc fields** on hot paths. Typed constructors (`String`, `Int`, `Float64`, `Bool`, `Duration`, `Error`) use `unsafe.Pointer` + `int64` storage, no `interface{}` boxing
- **Entry pooling** via `sync.Pool` with atomic ref counting and CAS-based return. Tiered buffer pools (512B to 32KB)
- **Atomic level control**. Single atomic load per log call; entries below threshold never allocate
- **Log sampling**. `CountSampler` logs first N, then every Mth. Checked before pool acquisition
- **Caller capture**. `WithCaller(true)` renders file:line in JSON, console, and ring buffer writers
- **4 colour themes**. Night Owl (RGB), Solarized, Dracula, Nord (256-colour). ANSI codes pre-cached at theme init
- **4 templates**. Default (badge), Simple (text), Minimal (message only), JSON (RFC3339Nano)
- **5 presets**. Development, Production, Container, Testing, HighPerformance
- **Pretty printing** (in `velocity/pretty`). `Section`, `Box`, `Panel`, `Banner`, `Bullet`, `KeyValue`, `SystemInfo`. Unicode-safe alignment
- **Tree display** (in `velocity/pretty`). Hierarchical data with box-drawing characters
- **Tables** (in `velocity/pretty`). Auto-width columns with box-drawing borders
- **Progress** (in `velocity/pretty`). `ProgressBar`, `Spinner` (5 animation styles), `MultiProgress`. Thread-safe with CAS-guarded stop
- **Child loggers**. `With()` for scoped fields, `WithTemplate()` for output format. Both inherit writers, sampler, base fields
- **Context integration**. `NewContext()`, `FromContext()`, `ContextWithFields()`
- **Dynamic writers**. `AddWriter()`/`RemoveWriter()` at runtime, thread-safe. Workers close their own writer on shutdown
- **Ring buffer writer**. Lock-free CAS ring buffer with batched flushing, bounded spins, min size enforcement
- **JSON writer**. Hand-rolled serialisation, handles NaN/Infinity, base64 bytes, proper escaping. No `encoding/json`
- **Typed nil safety**. `Error` and `Stringer` constructors catch typed nils via `reflect` to prevent panics
- **Nil-safe**. Every public method handles nil receivers gracefully
- **slog bridge** (in `velocity/slog`). `NewHandler` implements `log/slog.Handler` for incremental adoption. `WithAttrs`/`WithGroup` with dotted key prefixes. Pre-converted fields, cached group prefix
- **Testable**. Overridable `FatalHandler`, `NewForTesting()` constructor

## Performance

### Comparative benchmarks

Velocity was built because we needed something faster and lighter than pterm for high-volume logging in FoundryOS. Here's how it stacks up against the popular Go logging libraries (AMD Ryzen 9 5950X, Go 1.24, writing to `io.Discard`):

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

These cover the scenarios that actually matter in production: accumulated context, mixed field types, large messages, and parallel contention.

| Scenario | velocity | [zerolog](https://github.com/rs/zerolog) | [zap](https://github.com/uber-go/zap) | [slog](https://pkg.go.dev/log/slog) |
|----------|----------|---------|-----|------|
| Accumulated context (10 fields) | **45 ns** / 0 alloc | 99 ns / 0 alloc | 344 ns / 0 alloc | 672 ns / 0 alloc |
| Mixed field types (8 types) | **153 ns** / 4 alloc | 799 ns / 2 alloc | 1307 ns / 1 alloc | 2481 ns / 8 alloc |
| Error field | **96 ns** / 1 alloc | 136 ns / 0 alloc | 510 ns / 1 alloc | 912 ns / 1 alloc |
| Large message (1 KB) | **43 ns** / 0 alloc | 419 ns / 0 alloc | 1509 ns / 0 alloc | 2255 ns / 1 alloc |
| 10 inline fields | **117 ns** / 3 alloc | 383 ns / 0 alloc | 1159 ns / 1 alloc | 3170 ns / 10 alloc |
| Parallel (16 goroutines) | 53 ns / 1 alloc | **22 ns** / 0 alloc | 150 ns / 1 alloc | 279 ns / 0 alloc |

zerolog wins the parallel benchmark thanks to its lock-free event chaining design. Velocity wins everything else.

### Run the benchmarks yourself

```bash
# Comparative benchmarks against other libraries
make bench-compare

# Quick single-pass comparison
make bench-compare-short

# Velocity internal benchmarks only
go test -bench=. -benchmem -count=3 ./...
```

The benchmark suite lives in `benchmarks/` as a separate Go module. Add new libraries by appending to the `libraries` slice in `benchmarks/bench_test.go`.

### Internal benchmarks

| Operation | ns/op | allocs/op |
|-----------|-------|-----------|
| Info, no fields | 31 | 0 |
| Info, 5 pre-built fields | 47 | 0 |
| Info, 10 fields | 56 | 0 |
| Level check (disabled) | 2.7 | 0 |
| Sampler check | 9.0 | 0 |
| Entry pool round-trip | 21 | 0 |
| Int field construction | 1.8 | 0 |
| ConsoleWriter, 5 fields | 694 | 3 |
| JSONWriter, 5 fields | 949 | 1 |
| JSONWriter, parallel | 153 | 1 |
| slog handler, 3 string attrs | 490 | 6 |
| Parallel Info, 3 fields | 46 | 0 |

Run internal benchmarks: `go test -bench=. -benchmem -count=3 ./...`

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

Use velocity as the backend for Go's standard structured logging:

```go
import velocityslog "github.com/tensorfoundrylabs/velocity/slog"

logger := velocity.NewDevelopment()
slog.SetDefault(velocityslog.NewLogger(logger))

slog.Info("request handled", "method", "GET", "status", 200, "duration", 42*time.Millisecond)
```

Groups produce dotted keys: `slog.WithGroup("server").With("host", "localhost")` renders as `server.host`.

### Pretty printing

Use `velocity/pretty` for rich CLI output:

```go
import "github.com/tensorfoundrylabs/velocity/pretty"

p := pretty.New(os.Stdout, velocity.ThemeNightOwl)
p.Box("Deploy Complete", "All services running")
p.Banner("v2.1.0 - Production release")
```

### Log rotation with lumberjack

Velocity's `Config.ConsoleOutput` and `Config.StructuredOutput` accept any `io.Writer`:

```go
rotator := &lumberjack.Logger{Filename: "/var/log/app.log", MaxSize: 500, Compress: true}
cfg := velocity.DefaultProductionConfig()
cfg.StructuredOutput = rotator
log := velocity.NewWithConfig(cfg)
```

## Dependencies

One: [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term) for TTY detection. No other external dependencies. Zero third-party test dependencies.

## Similar Libraries

* [pTerm](https://github.com/pterm/pterm) - Our favourite visually pleasing library for terminal output we modeled the styles on, but Velocity offers speed and efficiency for high-volume logging.
* [logrus](https://github.com/sirupsen/logrus) - Another popular structured logging library, but Velocity is designed for speed and efficiency in CLI applications.

## Licence

[MIT](LICENSE)

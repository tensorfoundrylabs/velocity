<p align="center">
  <img src="assets/banner.png" alt="Velocity" width="600" /><br/>
  <a href="https://github.com/tensorfoundrylabs/velocity/actions/workflows/ci.yml"><img src="https://github.com/tensorfoundrylabs/velocity/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/tensorfoundrylabs/velocity/v2"><img src="https://pkg.go.dev/badge/github.com/tensorfoundrylabs/velocity/v2.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/tensorfoundrylabs/velocity"><img src="https://goreportcard.com/badge/github.com/tensorfoundrylabs/velocity" alt="Go Report Card"></a>
  <a href="https://github.com/tensorfoundrylabs/velocity/releases/latest"><img src="https://img.shields.io/github/v/release/tensorfoundrylabs/velocity?color=blue" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/tensorfoundrylabs/velocity" alt="License"></a>
  <a href="https://github.com/tensorfoundrylabs/velocity"><img src="https://img.shields.io/github/go-mod/go-version/tensorfoundrylabs/velocity" alt="Go Version"></a>
</p>

Fast, allocation-optimised structured logging for Go with rich terminal output. Battle-tested in TensorFoundry's [FoundryOS](https://tensorfoundry.io/products/foundryos) where it powers all CLI logging.

## Install

```bash
go get github.com/tensorfoundrylabs/velocity/v2
```

## Quick Start

```go
log := velocity.New(velocity.WithDevelopment())
log.Info("server started", velocity.String("addr", ":8080"), velocity.Int("workers", 4))
```

## Packages

```go
import (
    "github.com/tensorfoundrylabs/velocity/v2"           // core logging, writers, renderables, themes
    "github.com/tensorfoundrylabs/velocity/v2/live"      // spinners and progress bars
    "github.com/tensorfoundrylabs/velocity/v2/slogbridge" // log/slog bridge
)
```

| Package | Description |
|---------|-------------|
| `velocity` | Core logger, typed fields, console/JSON/multi/ring-buffer writers, themes, renderables (Box, Table, Tree, Banner, …), secure-field redaction, Hyperlink helper |
| `velocity/live` | Stateful animated types: `ProgressBar`, `Spinner`, `MultiProgress` |
| `velocity/slogbridge` | `Handler` implementing `log/slog.Handler` (package name: `slogbridge`) |

## Features

- **Zero-alloc on the hot path** — typed fields (`String`, `Int`, `Float64`, `Bool`, `Duration`, `Error`) use `unsafe.Pointer` storage; 5 pre-built fields log at ~34 ns with 0 allocs
- **Sub-100 ns logging** — ~27 ns with no fields, ~2 ns for disabled levels, ~5 ns through a sampler
- **Options-only construction** — single `New(opts ...Option)` with preset options: `WithDevelopment()`, `WithProduction()`, `WithContainer()`, `WithTesting(t)`, `WithNop()`
- **Immutable themes** — `NewTheme` with `ThemeOption`, semantic `StyleSlot` enum, `Theme.Format(slot, s)` for coloured output without raw ANSI, five built-in themes
- **Renderables in root** — `Box`, `Table`, `Tree`, `Banner`, `KeyValue`, `SystemInfo` all live in the root package; `log.Table(...)`, `log.Box(...)` etc. are convenience methods
- **Field-level redaction** — `Secure`, `SecureURL`, `Redacted`, `Truncated` constructors; `<secure>...</secure>` tag scanning; per-writer trust model via `WriterTrusted()`
- **StatusItem / Group / ContinuationBlock** — structured visual primitives for check-lists, count-headed route lists, and multi-line server startup output
- **OSC 8 hyperlinks** — `Hyperlink(uri, text)` with TTY detection, three fallback modes, composes with `Theme.Format`
- **Notify channel** — `Logger.Notify/NotifyLines/NotifyBox` for ephemeral operator output that bypasses the structured pipeline
- **Ring buffer writer** — `RingBufferWriter` with `Snapshot(n)` and `Subscribe(ctx, bufSize)` for in-process log capture
- **slog bridge** — `slogbridge.NewHandler` implements `log/slog.Handler` for incremental adoption
- **Log sampling** — `CountSampler` checked before pool acquisition; no allocs on the skip path
- **Nil-safe and testable** — every public method handles nil receivers; overridable `FatalHandler`; `WithTesting(t)` preset

## Performance

### Comparative benchmarks

AMD Ryzen 9 5950X, Go 1.24, all libraries writing structured output to `io.Discard`.
Velocity runs JSON-only (console writer disabled via `WithLevel(LevelOff)`), same as every other library here.

| Library | Info (no fields) | Info (3 fields) | With + Info (per call) | Disabled level |
|---------|-----------------|-----------------|------------------------|---------------|
| **velocity** | **30 ns** / 0 alloc | **63 ns** / 1 alloc | **155 ns** / 4 alloc | **3.3 ns** / 0 alloc |
| [zerolog](https://github.com/rs/zerolog) | 66 ns / 0 alloc | 162 ns / 0 alloc | 459 ns / 2 alloc | 7.3 ns / 0 alloc |
| [zap](https://github.com/uber-go/zap) | 233 ns / 0 alloc | 475 ns / 1 alloc | 3045 ns / 6 alloc | 6.5 ns / 0 alloc |
| [slog](https://pkg.go.dev/log/slog) | 417 ns / 0 alloc | 992 ns / 4 alloc | 1177 ns / 11 alloc | 6.8 ns / 0 alloc |
| [charmbracelet/log](https://github.com/charmbracelet/log) | 3.2 ns / 0 alloc | 3.9 ns / 0 alloc | 2815 ns / 5 alloc | 3.2 ns / 0 alloc |
| [pterm](https://github.com/pterm/pterm) | 8376 ns / 65 alloc | 16637 ns / 144 alloc | 8213 ns / 65 alloc | 17 ns / 0 alloc |

Velocity leads zerolog by ~2x on Info throughput and zap by ~8x. The disabled-level check (~3 ns) is the fastest among the structured loggers. charmbracelet/log's sub-5 ns per-call numbers come from skipping format work when the output is not a TTY; its `With` cost (2815 ns) reflects the real allocation overhead. pterm is a display library, not a structured logger — its numbers are expected.

### Internal benchmarks (v2.0.0, AMD Ryzen 9 5950X, Go 1.24)

| Operation | v1.1.3 ns/op | v2.0.0 ns/op | delta | B/op | allocs/op |
|-----------|-------------:|-------------:|------:|-----:|----------:|
| Info, no fields | 26 | 28 | +8% | 0 | 0 |
| Info, 5 pre-built fields | 38 | 33 | -13% | 0 | 0 |
| Info, 10 pre-built fields | 39 | 35 | -10% | 0 | 0 |
| Info, tree mode | 36 | 33 | -8% | 0 | 0 |
| Level check (disabled) | 2.1 | 2.2 | +5% | 0 | 0 |
| Sampler check | 5.3 | 5.8 | +9% | 0 | 0 |
| Entry pool round-trip | 14 | 14 | 0% | 0 | 0 |
| Int field construction | 1.3 | 1.4 | +8% | 0 | 0 |
| ConsoleWriter, 5 fields | 433 | 483 | +12% | 32 | 3 |
| JSONWriter, 5 fields | 594 | 642 | +8% | 0 | 0 |
| JSONWriter, parallel | 170 | 192 | +13% | 0 | 0 |
| WithComponent child | 270 | 159 | -41% | 192 | 3 |
| Secure scan, no match | 67 | 35 | -48% | 0 | 0 |
| slog handler, 3 attrs | 468 | 99 | -79% | 144 | 3 |

**Notes on v2 changes:** `Info (no fields)` and the writer paths carry a small overhead from the added `scanSecure` flag check and immutable theme lookup (vs mutable cached fields). The multi-field paths are faster due to the unified `any`-field path elimination. `WithComponent` improved significantly from child-logger construction changes. `SecureScan_NoMatch` halved due to early-exit on the `IndexByte` fast path. The slog bridge numbers dropped from ~468 ns to ~99 ns because the v1 benchmark was writing to stdout rather than discarding — that was a measurement bug, not a real v1 advantage.

Run benchmarks: `go test -bench=. -benchmem -count=3 ./...`

## Usage

### Presets

```go
log := velocity.New(velocity.WithDevelopment())   // coloured console, debug level
log := velocity.New(velocity.WithProduction())    // JSON to stderr, info level
log := velocity.New(velocity.WithContainer())     // JSON to stdout, info level
log := velocity.New(velocity.WithTesting(t))      // writes via t.Log, cleaned up on test exit
log := velocity.New(velocity.WithNop())           // discards all output
```

### Typed fields

```go
log.Info("request handled",
    velocity.String("method", "GET"),
    velocity.Int("status", 200),
    velocity.Float64("duration_ms", 12.4),
    velocity.Bool("cached", true),
    velocity.Duration("elapsed", 42*time.Millisecond),
    velocity.Error("err", err),
)
```

### Child loggers

```go
reqLog := log.With(velocity.String("request_id", "req-abc123"))
reqLog.Info("handling request")

compLog := log.WithComponent("scheduler")
compLog.Debug("job queued", velocity.Int("job_id", 7))
```

### Secure fields and redaction

```go
// Plaintext on TTY console, [REDACTED] in JSON and non-TTY output.
log.Info("user authenticated", velocity.Secure("token", "tok_abc123"))

// <secure> tag scanning works in message strings too.
log.Info("connecting to <secure>redis://admin:hunter2@cache.internal</secure>")
```

### Themes

```go
// Built-in themes.
log := velocity.New(velocity.WithTheme(velocity.ThemeNightOwl))

// Custom theme with semantic slots.
theme := velocity.NewTheme("Custom",
    velocity.WithLevelColours(debug, info, warn, err, fatal),
    velocity.WithStyleSlot(velocity.SlotGood, velocity.RGB(0x00, 0xFF, 0xAA)),
)
styled := theme.Format(velocity.SlotGood, "all systems go")
```

**Colour model.** Colour is automatically enabled when stdout is a real terminal (via `term.IsTerminal`). Two environment variables override detection:

| Variable | Effect |
|---|---|
| `NO_COLOR=1` | Always disable ANSI, regardless of terminal type |
| `FORCE_COLOR=1` | Always enable ANSI, regardless of terminal type |

`NO_COLOR` takes precedence over `FORCE_COLOR`. `FORCE_COLOR=1` is useful on Windows where terminal emulators such as VS Code, Windows Terminal, and Git Bash proxy stdout through a named pipe, which causes `term.IsTerminal` to return false even in a fully colour-capable terminal.

### Renderables

```go
// Convenience methods route through the console writer mutex.
log.Table([]string{"Service", "Status"}, [][]string{{"api", "running"}})
log.Box("Deploy Complete", "3/4 nodes healthy")

// Standalone construction for embedding or capture.
t := velocity.NewTable(headers, rows, velocity.ThemeNightOwl)
fmt.Print(t.String())
```

### Visual primitives

```go
// StatusItem: themed badge with level-aware routing.
log.Status(velocity.LevelInfo, velocity.StatusOK, "postgres connected",
    velocity.Duration("latency", 4*time.Millisecond))

// Group: count-headed indented list.
log.Group(velocity.LevelInfo, "Registered routes",
    velocity.GroupItem{Text: "GET  /api/users"},
    velocity.GroupItem{Text: "POST /api/orders"},
)

// ContinuationBlock: multi-line output anchored to one structured entry.
log.Continue(velocity.LevelInfo, "Server listening",
    "API:     "+velocity.Hyperlink("http://localhost:8080", "http://localhost:8080"),
    "Metrics: "+velocity.Hyperlink("http://localhost:9090/metrics", "http://localhost:9090/metrics"),
)
```

### log/slog bridge

```go
import "github.com/tensorfoundrylabs/velocity/v2/slogbridge"

vlog := velocity.New(velocity.WithDevelopment())
slog.SetDefault(slogbridge.NewLogger(vlog))

slog.Info("request handled", "method", "GET", "status", 200)
```

## Integration

### Log rotation with lumberjack

```go
rotator := &lumberjack.Logger{Filename: "/var/log/app.log", MaxSize: 500, Compress: true}
log := velocity.New(
    velocity.WithConsoleOutput(os.Stdout),
    velocity.WithStructuredOutput(rotator),
)
```

## Dependencies

One: [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term) for TTY detection. No other external dependencies.

## Similar Libraries

- [pTerm](https://github.com/pterm/pterm) — visually rich terminal output library; Velocity trades some visual features for speed and lower allocations
- [logrus](https://github.com/sirupsen/logrus) — popular structured logger; Velocity targets significantly lower latency for high-volume CLI workloads

## Licence

[MIT](LICENSE)

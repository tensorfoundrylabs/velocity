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

- **Zero-alloc field encoding, where measured**: typed fields (`String`, `Int`, `Uint64`, `Float64`, `Bool`, `Duration`, `Error`) use `unsafe.Pointer`/`int64` storage. What the benchmarks establish: the `Int`, `Uint64` and `Float64` constructors are allocation-free (~1.3 ns each), and JSON serialisation of pre-built fields allocates nothing (0 B/op). The remaining constructors are not individually benchmarked. Not zero-allocation: `Any` fields (an `encoding/json` fallback on that value only), `String()` copies that must survive pooling, and ring-buffer snapshots — deliberate costs listed under Performance
- **Cheap disabled path**: a disabled level costs one atomic load: 2.1 ns/op with 0 B/op and 0 allocs/op, asserted by a test outside the benchmark suite
- **Options-only construction** — single `New(opts ...Option)` with preset options: `WithDevelopment()`, `WithProduction()`, `WithContainer()`, `WithTesting(t)`, `WithNop()`
- **Immutable themes** — `NewTheme` with `ThemeOption`, semantic `StyleSlot` enum, `Theme.Format(slot, s)` for coloured output without raw ANSI, five built-in themes
- **Renderables in root** — `Box`, `Table`, `Tree`, `Banner`, `KeyValue`, `SystemInfo` all live in the root package; `log.Table(...)`, `log.Box(...)` etc. are convenience methods
- **Field-level redaction** — `Secure`, `SecureURL`, `Redacted`, `Truncated` constructors; `<secure>...</secure>` tag scanning; per-writer trust model via `WriterTrusted()`
- **StatusItem / Group / ContinuationBlock** — structured visual primitives for check-lists, count-headed route lists, and multi-line server startup output
- **OSC 8 hyperlinks** — `Hyperlink(uri, text)` with TTY detection, three fallback modes, composes with `Theme.Format`
- **Notify channel** — `Logger.Notify/NotifyLines/NotifyBox` for ephemeral operator output that bypasses the structured pipeline
- **Ring buffer writer** — `RingBufferWriter` with `Snapshot(n)` and `Subscribe(ctx, bufSize)` for in-process log capture
- **slog bridge** — `slogbridge.NewHandler` implements `log/slog.Handler` for incremental adoption
- **Log sampling**: `CountSampler` is consulted before any entry or pool work, so sampled-out calls stop after the level check; `Logger.Fatal` is exempt and always delivered
- **Component-aware pretty output** — opt-in `WithComponentStyling()` folds service name, count, timing, and state-transition fields into compact inline indicators on the console; JSON output is unaffected and keeps every field expanded
- **Nil-safe and testable** — every public method handles nil receivers; overridable `FatalHandler`; `WithTesting(t)` preset

## Performance

The speedup tables that used to live here have been removed. They were measured
against `io.Discard` sinks, and Velocity maps an `io.Discard` destination to a
no-output fast path; those benchmarks exercised the level check and field
encoding but never serialised a record, so the comparison tables and the
sub-100 ns headlines built on them were invalid.

The numbers below are the corrected measurements: every enabled benchmark
writes to a real sink that receives and counts the bytes, and a preflight test
outside the timed loop asserts the output arrived and is valid for the claimed
format. They are medians of six interleaved rounds on one machine (AMD Ryzen
9 5950X, Go 1.26.5, GOMAXPROCS=32, colour env unset); a control benchmark held
18.8 ns/op on both sides of the run, so deltas under ~5% are noise. Raw output,
medians and the harness are kept in the development tree under
`docs/benchmarks/hardening-finish/` (the `docs/` directory is not published
with the repository).

### Logging baseline (console and JSON writers both active, both at Debug)

| Operation | ns/op | B/op | allocs/op |
|-----------|------:|-----:|----------:|
| Info, no fields | 425.7 | 0 | 0 |
| Info, 5 pre-built fields | 1078.0 | 32 | 3 |
| Info, 5 inline fields | 1118.0 | 48 | 4 |
| Info, 10 pre-built fields | 1572.0 | 60 | 5 |
| Info, parallel | 476.5 | 32 | 3 |
| JSON writer only, 5 fields | 679.9 | 0 | 0 |
| Console writer only, 5 fields | 399.5 | 32 | 3 |
| slog handler, info with 3 attrs | 3525.0 | 193 | 6 |
| Async MultiWriter enqueue attempt (draining consumer) | 195.7 | 0 | 0 |
| Async MultiWriter drop path | 101.1 | 0 | 0 |
| Disabled level | 2.1 | 0 | 0 |

Disabled logging excludes writer work. A pre-built `String` field and scalar
fields can remain allocation-free; constructing `String("key", "value")` in
the call costs 16 B and one allocation before the level check runs.

The async rows are enqueue attempts, not guaranteed delivery: the timed loop's
non-blocking send drops when the worker channel is full, and the drain runs
after the timer stops with every drop counted. Against this benchmark's single
concurrent JSON worker, 48-62% of attempts dropped per round (median 46%
delivered, 54% dropped); the ratio tracks producer/consumer speed balance, so
treat it as a workload property, not a library constant. End-to-end cost per
delivered record — timed loop plus Close drain divided by records actually
written — is ~440 ns.

Provenance: all rows except "Disabled level" were measured on 2026-09-16,
after two reliability reworks changed the serialisation paths — delivery
acknowledgement became per-item, and every console/JSON write now registers
as in-flight so Close drains admitted calls. That registration initially
cost one 16 B allocation per console write; the method-value allocation was
removed after re-measurement and admission is now allocation-free on the
console and JSON paths alike. The
disabled row never reaches a writer, and the pretty rows below render through
the standalone `NewPretty`, which bypasses console admission entirely — both
were re-checked flat on 2026-09-16 and keep their original campaign numbers.
The rows in the table above come from that post-fix campaign (three rounds,
same day, same set with the control benchmark flat at 18.8 ns; raw output in
`remeasure-f2admission/fix-methodvalue/`).

### Pretty rendering

| Renderable | ASCII ns/op | Unicode ns/op |
|------------|------------:|--------------:|
| Table | 738.0 | 3272.5 |
| Box | 308.8 | 1103.5 |
| Banner | 231.4 | 610.8 |

Tables, boxes, banners and component columns are measured in terminal cells
(grapheme clusters via [uniseg](https://github.com/rivo/uniseg)), so Unicode
output aligns correctly at the cost of the wider measurement. ASCII output
(the common case) takes an allocation-free fast path and never touches uniseg.

Snapshot copies are deliberate allocation costs, not regressions: `String()`
results that must survive pooling copy their bytes (48 B, 1 alloc), and each
ring-buffer subscriber receives a cloned field snapshot it owns (91 B, 3
allocs). A 64-entry `Snapshot(n)` deep copy costs 10240 B / 65 allocs because
the caller owns the result.

Run the same benchmarks yourself:

```bash
go test -run '^$' -bench . -benchtime=1s -count=1 . ./slogbridge/
cd benchmarks && go test -run '^$' -bench . -benchtime=1s -count=1
```

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

`FORCE_COLOR` changes presentation only. It does not make a pipe, file, or
buffer a trusted terminal: secure fields remain redacted there. `WithBufferSize`
and `WithFieldPoolSize` remain accepted for v2 compatibility but no longer tune
the shared pools.

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

## Lifecycle and shutdown

Parent and child loggers share one writer family and one lifetime. Once any
`Close` completes, the whole family is closed for good: subsequent log calls
are dropped, `AddWriter` cannot revive output, and concurrent `Close` calls
all wait for the same drain and return the same recorded result (worker close
errors are joined with `errors.Join`). Close also waits for calls that were
already admitted: every console and JSON write registers as in-flight for its
whole formatting cycle, so a paused call completes or is dropped rather than
writing after Close returned. `Logger.Close` never closes an
`io.Writer` you supplied; whoever constructed it still owns it.

`Logger.Fatal` is exempt from sampling and never takes the queue-full drop
path: it waits for preceding accepted entries, its own write, and a flush
before invoking the `FatalHandler` (or exiting). A custom handler that returns
leaves the logger reusable. Ordinary `io.Writer`s cannot be cancelled, so a
stalled destination can block `Fatal` and `Close` indefinitely: delivery is
not faked with a timeout. Records arriving through `log/slog` or
`Logger.LogEntry` at the fatal level are logged like any other entry and never
exit the process; only `Logger.Fatal` has process-control semantics.

### Sharing a terminal with live widgets

```go
out := live.NewOutput(os.Stdout)
log := velocity.New(velocity.WithConsoleOutput(out))
spin := live.NewSpinner(out, "working")

spin.Start()
log.Info("logged while the spinner is running") // clears, writes, redraws
spin.Stop()
```

Pass the same `*live.Output` to `WithConsoleOutput` and every widget
constructor. Log records then clear the active live rows, write the whole
record, and redraw the live area in one serialised operation, so lines never
glue themselves onto spinner frames. Ordinary `io.Writer` constructors keep
working standalone: they just don't coordinate, and raw writes that bypass
the shared object are outside the guarantee.

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

Two: [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term) for TTY detection and [`github.com/rivo/uniseg`](https://github.com/rivo/uniseg) for terminal cell widths in the pretty renderers. No other external dependencies.

## Similar Libraries

- [pTerm](https://github.com/pterm/pterm) — visually rich terminal output library; Velocity trades some visual features for speed and lower allocations
- [logrus](https://github.com/sirupsen/logrus) — popular structured logger; Velocity targets lower per-call cost for high-volume CLI workloads

## Licence

[MIT](LICENSE)

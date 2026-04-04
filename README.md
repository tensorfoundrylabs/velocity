# Velocity

> Give your Go CLI apps terminal Velocity!

Fast, allocation optimised structured logging for Go with rich terminal output for heavy log presentation and logging. Battle tested and hardened through years of heavy log use.

Extracted from TensorFoundry's [FoundryOS](https://tensorfoundry.io/products/foundryos) where it powers all CLI logging.

## Install

```bash
go get github.com/tensorfoundrylabs/velocity
```

## Quick Start

```go
log := velocity.New(os.Stdout)
log.Info("server started", velocity.StringField("addr", ":8080"), velocity.Int("workers", 4))
```

Or use a preset:

```go
log := velocity.NewDevelopment()                                // coloured console, debug level
log := velocity.NewWithBuilder(velocity.PresetProduction())     // structured JSON, info level
```

## Features

- **Zero-alloc fields** on hot paths. Typed constructors (`StringField`, `Int`, `Float64`, `Bool`, `Duration`, `Error`) use `unsafe.Pointer` + `int64` storage, no `interface{}` boxing
- **Entry pooling** via `sync.Pool` with atomic ref counting and CAS-based return. Tiered buffer pools (512B to 32KB)
- **Atomic level control**. Single atomic load per log call; entries below threshold never allocate
- **Log sampling**. `CountSampler` logs first N, then every Mth. Checked before pool acquisition
- **Caller capture**. `WithCaller(true)` renders file:line in JSON, console, and ring buffer writers
- **4 colour themes**. Night Owl (RGB), Solarized, Dracula, Nord (256-colour). ANSI codes pre-cached at theme init
- **4 templates**. Default (badge), Simple (text), Minimal (message only), JSON (RFC3339Nano)
- **5 presets**. Development, Production, Container, Testing, HighPerformance
- **Pretty printing**. `Section`, `Box`, `Panel`, `Banner`, `Bullet`, `KeyValue`, `SystemInfo`. Unicode-safe alignment
- **Tree display**. Hierarchical data with box-drawing characters, inline or below-message
- **Tables**. Auto-width columns with box-drawing borders
- **Progress**. `ProgressBar`, `Spinner` (5 animation styles), `MultiProgress`. Thread-safe with CAS-guarded stop
- **Child loggers**. `With()` for scoped fields, `WithTemplate()` for output format. Both inherit writers, sampler, base fields
- **Context integration**. `NewContext()`, `FromContext()`, `ContextWithFields()`
- **Dynamic writers**. `AddWriter()`/`RemoveWriter()` at runtime, thread-safe. Workers close their own writer on shutdown
- **Ring buffer writer**. Lock-free CAS ring buffer with batched flushing, bounded spins, min size enforcement
- **JSON writer**. Hand-rolled serialisation, handles NaN/Infinity, base64 bytes, proper escaping. No `encoding/json`
- **Typed nil safety**. `ErrorField` and `Stringer` constructors catch typed nils via `reflect` to prevent panics
- **Nil-safe**. Every public method handles nil receivers gracefully
- **slog bridge**. `NewSlogHandler` implements `log/slog.Handler` for incremental adoption. `WithAttrs`/`WithGroup` with dotted key prefixes. Pre-converted fields, cached group prefix
- **Testable**. Overridable `FatalHandler`, `NewForTesting()` constructor

## Performance

Hot path benchmarks (AMD Ryzen 7 5700G):

| Operation | ns/op | allocs/op |
|-----------|-------|-----------|
| Info, no fields | 29 | 0 |
| Info, 5 mixed fields | 38 | 0 |
| Info, 10 fields | 40 | 0 |
| Level check (disabled) | 2.3 | 0 |
| Sampler check | 6.0 | 0 |
| Entry pool round-trip | 15 | 0 |
| Int field construction | 1.4 | 0 |
| ConsoleWriter, 5 fields | 462 | 3 |
| JSONWriter, 5 fields | 702 | 1 |
| JSONWriter, parallel | 150 | 1 |
| slog handler, 3 string attrs | 490 | 6 |
| Parallel Info, 3 fields | 36 | 0 |

Run benchmarks: `go test -bench=. -benchmem -count=3 ./...`

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
logger := velocity.NewDevelopment()
slog.SetDefault(velocity.NewSlogLogger(logger))

slog.Info("request handled", "method", "GET", "status", 200, "duration", 42*time.Millisecond)
```

Groups produce dotted keys: `slog.WithGroup("server").With("host", "localhost")` renders as `server.host`.

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

## Licence

[MIT](LICENSE)

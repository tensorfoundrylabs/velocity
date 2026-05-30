# Velocity

Standalone Go logging library. Zero-allocation hot path, rich terminal output, hand-rolled JSON. Module path: `github.com/tensorfoundrylabs/velocity/v2`.

## Commands

```bash
make ready              # Pre-commit gate: tidy, fmt, align, lint, vet, test-race
make perf-gate          # Alloc-regression gate vs docs/bench-baseline.txt (slow; pre-tag)
make test               # Run all tests
make test-race          # Tests with race detector
make test-cover         # Tests with coverage report
make lint               # golangci-lint (strict, all linters)
make fmt                # goimports + gofumpt
make bench              # Quick bench (count=3) with allocs
make bench-baseline     # Capture count=10 run to docs/bench-baseline.txt
make install-tools      # golangci-lint, betteralign, goimports, gofumpt, benchstat
make help               # All targets
```

## Packages

Three: root `velocity`, `velocity/live`, `velocity/slogbridge`. `live` has no root imports; `slogbridge` imports root.

### Root

| File | Purpose |
|------|---------|
| `logger.go` | `Logger`, log methods, child loggers, `writerSet` sharing |
| `entry.go` | Pooled `Entry`, atomic ref counting |
| `field.go` / `field_convert.go` | Zero-alloc typed fields via `unsafe.Pointer`; typed nil guards via `reflect` |
| `config.go` | `Config`, TTY detection, `resolveColourForWriter` (NO_COLOR/FORCE_COLOR) |
| `options.go` | `New(opts...)` functional options; `WithDevelopment`, `WithProduction`, `WithContainer`, `WithNop`, `WithHighThroughput`, `WithTheme`, `WithLevel`, `WithStructuredLevel`, etc. |
| `level.go` | Log levels, `ParseLevel`, `MustParseLevel`, `AtomicLevel` |
| `writer.go` | `Writer`, `WriterFunc`, `NoOpWriter`, `FilteredWriter`, capability interfaces, `WriterTrusted()` |
| `writer_console.go` | Themed ANSI console output |
| `writer_console_rb.go` | Lock-free ring-buffer console writer |
| `writer_json.go` | Hand-rolled JSON (no `encoding/json`) |
| `writer_multi.go` | Async fan-out to named writers; workers close own writer on shutdown |
| `writer_ring.go` | `RingBufferWriter`, `EntrySnapshot`, `Snapshot`, `Subscribe`, `Stats` |
| `ringbuffer.go` | CAS-based ring buffer, bounded spins, batched flush |
| `template.go` | Log line templates with level styles and caller |
| `theme.go` | Immutable themes via `NewTheme` + `ThemeOption`; `StyleSlot` enum; `Theme.Format`/`Wrap`/`Stylish` |
| `sampler.go` | `CountSampler` for high-volume reduction |
| `context.go` | `context.Context` integration |
| `buffer.go` / `pool.go` | Tiered buffer pool, `UnsafeString`, entry/field pools |
| `errors.go` | Sentinel errors |
| `renderable.go` | `Renderable` and `TTYRenderable` interfaces; `Box`, `Table`, `Banner`, `Tree`, `KeyValue`, `SystemInfo` |
| `status.go` | `StatusItem`, `StatusKind` (`StatusOK/Fail/Warn/Info/Pending/Skipped`), `Logger.Status` (inline render) |
| `group.go` | `Group`, `GroupItem`, `Logger.Group` |
| `continuation.go` | `ContinuationBlock`, `Logger.Continue` |
| `pretty.go` | `Pretty` facade, `NewPretty`, `NewPrettyFromLogger`, `CreateBanner` |
| `secure.go` | `Secure`, `SecureURL`, `Redacted`, `Truncated` field constructors; `<secure>` tag scanner |
| `hyperlink.go` | OSC 8 `Hyperlink`, `HyperlinksSupported`, `HyperlinkFallback`, `WithHyperlinkFallback` |

### `velocity/live`

`progress.go` — `ProgressBar`, `Spinner`, `MultiProgress`, `SpinnerStyle` with CAS-guarded stop and TTY detection (NO_COLOR/FORCE_COLOR aware).

### `velocity/slogbridge`

`handler.go` — `Handler` implementing `log/slog.Handler`. `NewHandler`, `NewLogger`. `WithAttrs` pre-converts to velocity `Field`s; `WithGroup` caches dotted prefix.

## Design

- **Zero-alloc hot path**: `unsafe.Pointer` + `int64` field storage. Integer fields via `formatInt` stack buffer. Entry pooling with CAS-based return. ANSI codes pre-cached on `Theme`. Timestamps via `time.AppendFormat`. Writers format outside the mutex; lock only for I/O.
- **Nil-safe**: every public method handles nil receivers. Typed nils caught via `reflect` in `Error`/`Stringer` constructors.
- **Thread-safe**: atomic level checks, mutex-protected writers, lock-free ring buffer.
- **Trust model**: writers default-untrusted. `WriterTrusted()` opt-in. `Secure` field plaintext only shown to trusted writers; `<secure>...</secure>` tags in messages auto-scanned and redacted for untrusted writers.
- **Colour resolution**: `NO_COLOR` env disables. `FORCE_COLOR` env forces on. Otherwise `term.IsTerminal` on the writer's fd. All decisions go through `resolveColourForWriter`.
- **`Logger.Status`** renders inline (indented under parent log line, no own timestamp) on the console; JSON writers still receive structured records with `status` field.
- **Shared `writerSet`**: parent and child loggers (`With`, `Detailed`, `WithComponent`, `Request`) share writer topology and `scanSecure` atomic, so `AddWriter` after child creation is visible everywhere.
- **No `encoding/json`** in hot paths.
- **Inline indicators** (opt-in, pretty-only, JSON unaffected): `WithComponentStyling()` enables compact header indicators — a hashed-colour component name + muted `│` bar, `(N)` count suffix, `⏱ …` timing suffix, and `from → to` state-transition arrows. Promoted fields are removed from the tree by default (`removeFromTree=true`). Configured via `WithComponentField`, `WithComponentColumnWidth`, `WithCountFields`, `WithTimingFields`, `WithStateTransitionPairs`, `WithInlineGlyphs`. The component palette is set via `WithComponentPalette` / `WithComponentColour` `ThemeOption`s. JSON writers are never affected.

## Concurrency

- `AtomicLevel`: single atomic load per log call; sub-threshold entries never allocate.
- `MultiWriter`: per-writer buffered channels (256 cap), non-blocking send, `Retain`/`Release` lifecycle. Workers close their own writer via defer. Shutdown drain via `for range ch`.
- `RingBuffer`: CAS-based, power-of-2 sized, atomic commit flags, batched flush. Bounded spins (1000 iterations).
- `Entry`: `atomic.Int32` ref count; CAS to pool prevents double-release.
- `Logger.Render`/`RenderRaw`/`Newline`: render into a pooled buffer outside the lock, acquire `consoleWriter.mu` only for the final write — same mutex as log calls, so rich output cannot interleave.

## Linting

`default: all` in `.golangci.yml`. `unsafe.Pointer` usage excluded from gosec G103 in field/buffer files. Run `make lint` to verify.

## Code quality

**Always**: run `make ready` before commit · Australian English · comment **why**, not what.

**Never**: add dependencies without discussion · use `encoding/json` in hot paths · use `interface{}` where typed fields exist · create `_v2`/`_new`/`.bak` files · use `fmt.Sprintf` / `strconv.Itoa` on hot paths.

## Review

Run `/review-velocity` for an Opus-powered review covering concurrency, memory, correctness, performance, API, and benchmark validation.

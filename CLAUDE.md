# Velocity

Standalone Go logging library. Zero-allocation hot path, rich terminal output, hand-rolled JSON. Module path: `github.com/tensorfoundrylabs/velocity/v2`.

## Commands

```bash
make ready              # Pre-commit gate (read-only): pinned tools, tidy, fmt, align, lint, vet, test-race
make perf-gate          # Alloc-regression gate vs docs/bench-baseline.txt (slow; pre-tag)
make test               # Run all tests
make test-race          # Tests with race detector
make test-cover         # Tests with coverage report
make lint               # golangci-lint (strict, all linters, read-only; lint-fix mutates)
make fmt                # goimports + gofumpt -extra (rewrites files; fmt-check is the gate)
make bench              # Quick bench (count=3) with allocs
make bench-baseline     # Capture count=10 run to docs/bench-baseline.txt
make install-tools      # golangci-lint, betteralign, goimports, gofumpt, benchstat
make help               # All targets
```

## Packages

Three: root `velocity`, `velocity/live`, `velocity/slogbridge`. `live` has no root imports; `slogbridge` imports root. Direct dependencies: `golang.org/x/term` (TTY detection) and `github.com/rivo/uniseg` v0.4.7 (terminal cell widths, pretty renderers only).

### Root

| File | Purpose |
|------|---------|
| `logger.go` | `Logger`, log methods, child loggers, `writerSet` sharing |
| `entry.go` | Pooled `Entry`, atomic ref counting |
| `field.go` / `field_convert.go` | Zero-alloc typed fields via `unsafe.Pointer`; typed nil guards via `reflect` |
| `config.go` | `Config`, TTY detection, `resolveColourForWriter` (NO_COLOR/FORCE_COLOR) |
| `options.go` | `New(opts...)` functional options; `WithDevelopment`, `WithProduction`, `WithContainer`, `WithNop`, `WithHighThroughput`, `WithTheme`, `WithLevel`, `WithStructuredLevel`, etc. |
| `level.go` | Log levels, `ParseLevel`, `MustParseLevel`; level is stored as a bare `atomic.Int32` on `Logger` |
| `writer.go` | `Writer`, `WriterFunc`, `NoOpWriter`, `FilteredWriter`, capability interfaces, `WriterTrusted()` |
| `writer_console.go` | Themed ANSI console output; colour permission fixed at construction (`colourAllowed`/`colourExplicitlyDisabled`) |
| `writer_console_rb.go` | Deprecated batching console writer over the bounded byte queue; use `ConsoleWriter` |
| `writer_json.go` | Hand-rolled JSON (no `encoding/json`); `encoding/json` only on the `Any` fallback |
| `writer_multi.go` | Async fan-out to named writers; `WriteReliable` barrier for Fatal/Close; workers close own writer, errors joined |
| `writer_ring.go` | `RingBufferWriter`, `EntrySnapshot`, `Snapshot`, `Subscribe`, `Stats` |
| `ringbuffer.go` | Bounded power-of-2 byte queue: short mutex, owned byte storage, single drainer goroutine, drop-on-full counted |
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
| `secure.go` | `Secure`, `SecureURL`, `Redacted`, `Truncated` field constructors; `<secure>` tag scanner (`applySecureTags` extends it to group/continuation payloads) |
| `hyperlink.go` | OSC 8 `Hyperlink`, `HyperlinksSupported`, `HyperlinkFallback`, `WithHyperlinkFallback` |

### `velocity/live`

`output.go`: `Output` and `NewOutput(w)`, an opt-in shared terminal coordinator. Pass the SAME `*Output` to `WithConsoleOutput` and the widget constructors; it serialises clearing live rows, whole log records and redraws. Terminal/cursor capability comes from the real destination only.
`progress.go`: `ProgressBar`, `Spinner`, `MultiProgress`, `SpinnerStyle`. Exactly-once finalisation (CAS finaliser + waiter join); non-terminal output emits one summary line on Complete and no cursor escapes.

### `velocity/slogbridge`

`handler.go`: `Handler` implementing `log/slog.Handler`. `NewHandler`, `NewLogger`. `WithAttrs` pre-converts to velocity `Field`s; `WithGroup` caches dotted prefix. A record mapped to `LevelFatal` is logged but never exits the process; only `Logger.Fatal` has process-control semantics.

## Design

- **Zero-alloc hot path**: `unsafe.Pointer` + `int64` field storage. Integer fields via `formatInt` stack buffer. Entry pooling with CAS-based return. ANSI codes pre-cached on `Theme`. Timestamps via `time.AppendFormat`. Writers format outside the mutex; lock only for I/O.
- **Nil-safe**: every public method handles nil receivers. Typed nils caught via `reflect` in `Error`/`Stringer` constructors.
- **Thread-safe**: atomic level checks and mutex-protected writers.
- **Trust model**: writers default-untrusted. `WriterTrusted()` opt-in. `Secure` field plaintext only shown to trusted writers. `<secure>...</secure>` scanning is content-driven: the maybe-secure flag is derived from message content whenever scanning is on, independent of the current writer mix, so an `AddWriter` between scan and dispatch can never leak plaintext. Each writer then applies its own trust (redact for untrusted, strip markers for trusted). `WithSecureTags(false)` opts out of tag scanning, never of `Secure`-field redaction.
- **Colour resolution**: permission is fixed at writer construction: `WithColour(false)` survives every theme swap, and a mono-to-coloured swap restores colour only where permission allows. `NO_COLOR` disables ANSI, `FORCE_COLOR` enables styling on non-terminals but never grants trust or cursor control; trust remains based on the actual writer fd.
- **Family close**: parent and children share `writerSet` close state. After any member's `Close` completes the family is closed for good (no `AddWriter` revival); concurrent Closes all wait on the same drain and return the same recorded result. Built-in wrappers never close caller-owned `io.Writer`s; worker close errors are joined.
- **Fatal semantics**: `Logger.Fatal` is exempt from the sampler and delivered via a reliable ordered path (awaits preceding accepted entries, its own write, Flush) before `FatalHandler`/exit; a returning handler leaves the logger reusable. `LogEntry`/slog at `LevelFatal` never exits. A stalled generic `io.Writer` can block Fatal/Close indefinitely; there is no fake timeout.
- **Cell widths**: one three-tier `visibleLen` measures headers, cells, boxes, banners and component columns in terminal cells: allocation-free printable-ASCII fast path, then `uniseg.StringWidth`, then pooled escape-strip whole-measure for strings interleaving ESC with text. ANSI/OSC sequences are ignored; truncation never splits a grapheme cluster.
- **`Logger.Status`** renders inline (indented under parent log line, no own timestamp) on the console; JSON writers still receive structured records with `status` field.
- **Shared `writerSet`**: parent and child loggers (`With`, `Detailed`, `WithComponent`, `Request`) share writer topology and `scanSecure` atomic, so `AddWriter` after child creation is visible everywhere.
- **No `encoding/json`** in hot paths.
- **Inline indicators** (opt-in, pretty-only, JSON unaffected): `WithComponentStyling()` enables compact header indicators — a hashed-colour component name + muted `│` bar, `(N)` count suffix, `⏱ …` timing suffix, and `⟳ from → to` state-transition arrows. Promoted fields are removed from the tree by default (`removeFromTree=true`). Configured via `WithComponentField`, `WithComponentColumnWidth`, `WithCountFields`, `WithTimingFields`, `WithStateTransitionPairs`, `WithInlineGlyphs`. The component palette is set via `WithComponentPalette` / `WithComponentColour` `ThemeOption`s. JSON writers are never affected.

## Concurrency

- **Level gate**: `Logger.level` is a bare `atomic.Int32`; a single atomic load per log call means sub-threshold entries never allocate. `writerSet.closing` is the write-admission gate for the whole family.
- `MultiWriter`: per-writer buffered channels (256 cap), non-blocking send, `Retain`/`Release` lifecycle. `WriteReliable` (Fatal/Close) blocks until workers process everything enqueued before it, then flushes in-line. Workers close their own writer via defer; close errors are joined into `Close`'s result. Lock order: `writers.mu` then `mw.mu`; external callbacks (e.g. `SetTheme`) run outside both.
- `RingBuffer`: mutex-guarded bounded power-of-2 byte queue, owned byte storage, single drainer goroutine with wake-token-or-pending plus a 10ms backstop; drop-on-full is counted, accepted order preserved, Close drains then is idempotent.
- `Entry`: `atomic.Int32` ref count; CAS to pool prevents double-release. Final release clears pointer-bearing storage and drops oversized field slices.
- `themeState`: one mutex-guarded theme shared by a logger and all its children; renderers snapshot it before rendering and it is never held across writer or renderable calls.
- `Logger.Render`/`RenderRaw`/`Newline`: render into a pooled buffer outside the lock, acquire `consoleWriter.mu` only for the final write: same mutex as log calls, so rich output cannot interleave.

## Linting

`default: all` in `.golangci.yml`. `unsafe.Pointer` usage excluded from gosec G103 in field/buffer files. Run `make lint` to verify.

## Code quality

**Always**: run `make ready` before commit · Australian English · comment **why**, not what.

**Never**: add dependencies without discussion · use `encoding/json` in hot paths · use `interface{}` where typed fields exist · create `_v2`/`_new`/`.bak` files · use `fmt.Sprintf` / `strconv.Itoa` on hot paths.

## Review

Run `/review-velocity` for an Opus-powered review covering concurrency, memory, correctness, performance, API, and benchmark validation.

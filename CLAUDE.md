# Velocity

Standalone Go logging library with zero-allocation fields and rich terminal output. Extracted from FoundryOS.

## Commands

```bash
make ready              # Pre-commit gate: tidy, fmt, align, lint, vet, test-race, perf gate
make test               # Run all tests
make test-race          # Tests with race detector
make test-cover         # Tests with coverage report
make lint               # golangci-lint (strict, all linters)
make fmt                # goimports + gofumpt
make install-tools      # Install golangci-lint, betteralign, goimports, gofumpt, benchstat
make help               # Show all targets
make bench              # Quick bench run (count=3) with allocs
make bench-baseline     # Capture count=10 run to docs/bench-baseline.txt
make bench-perf-gate    # Compare current vs baseline; fail on >5% regression
```

Benchmarks: `go test -bench=. -benchmem -count=3 ./...`

## Package Structure

Three packages: root `velocity`, `velocity/pretty`, and `velocity/slog`.

### Root (`package velocity`)

| File | Purpose |
|------|---------|
| `logger.go` | Core `Logger` type, log methods, `logInternal` |
| `entry.go` | Pooled `Entry` with atomic ref counting |
| `field.go` | Zero-alloc typed fields via `unsafe.Pointer`, typed nil guards via `reflect` |
| `field_convert.go` | Field value extraction and string conversion |
| `config.go` | `Config` struct, `Builder`, presets, default config values, TTY detection |
| `options.go` | Functional options (`WithLevel`, `WithTheme`, etc.) |
| `level.go` | Log levels, `AtomicLevel`, `MustParseLevel` |
| `writer.go` | `Writer` interface, `WriterFunc`, `NoOpWriter` |
| `writer_console.go` | Themed ANSI console output with caller rendering |
| `writer_console_rb.go` | Lock-free ring buffer console writer with timezone support |
| `writer_json.go` | Hand-rolled JSON output (no `encoding/json`) with caller rendering |
| `writer_multi.go` | Async fan-out to named writers, workers close own writer on shutdown |
| `ringbuffer.go` | CAS-based ring buffer, bounded spins, min size 2, batched flushing |
| `template.go` | Log line templates with level styles and caller output |
| `theme.go` | Colour themes with pre-cached ANSI codes via `Theme.Cache()` |
| `sampler.go` | `CountSampler` for high-volume log reduction |
| `context.go` | `context.Context` integration |
| `buffer.go` | Tiered `BufferPool`, zero-copy `BytesBuffer`, `AppendTime`, `UnsafeString` |
| `pool.go` | `sync.Pool` instances for entries, fields, buffers |
| `errors.go` | Sentinel errors |
| `renderable.go` | `Renderable` interface; all `*Result` types (`BoxResult`, `TableResult`, `BannerResult`, `TreeResult`, `KeyValueResult`, `SystemInfoResult`) |
| `doc.go` | Package documentation |

### `velocity/pretty` (`package pretty`)

| File | Purpose |
|------|---------|
| `pretty.go` | `Pretty` type, `Box`, `Panel`, `Banner`, `Table`, `Tree`, `Bullet`, `KeyValue`, `SystemInfo`, `TreeItem` |
| `renderable.go` | Result types implementing `velocity.Renderable`; pooled render paths for all pretty primitives |
| `progress.go` | `ProgressBar`, `Spinner`, `MultiProgress`, `SpinnerStyle` with CAS-guarded stop |
| `doc.go` | Package documentation |

### `velocity/slog` (`package velocityslog`)

| File | Purpose |
|------|---------|
| `handler.go` | `Handler` implementing `log/slog.Handler`, `NewHandler`, `NewLogger` |
| `doc.go` | Package documentation |

## Test Files

### Root

| File | Coverage |
|------|----------|
| `field_test.go` | `itoa` edge cases, typed nil error/stringer |
| `writer_json_test.go` | Nil fields, caller output |
| `writer_console_test.go` | Invalid level bounds, caller output |
| `writer_console_rb_test.go` | Timezone in fallback path |
| `writer_multi_test.go` | Multi-writer fan-out, shutdown drain |
| `ringbuffer_test.go` | Concurrent writes, overflow, bounded spin, zero-length, min size |
| `progress_test.go` | Concurrent Complete/Stop, nil SetStyle (in `pretty/`) |
| `benchmark_test.go` | Benchmarks covering hot paths, fields, writers, pooling, tree-mode, Render API |
| `entry_test.go` | Entry pool, ref counting, concurrent access |
| `with_test.go` | `With()`, `WithTemplate`, nil/empty |
| `fatal_test.go` | Fatal handler, nil logger subprocess test |
| `testutil_test.go` | Shared helpers: `waitFor`, `safeBuffer` |
| `buffer_test.go` | Buffer pool, `UnsafeString` |
| `context_test.go` | Context integration |
| `level_test.go` | Level parsing, atomic level |
| `logger_addwriter_test.go` | Dynamic writer add/remove |
| `logger_detailed_test.go` | Detailed logger behaviour |
| `logger_render_test.go` | `Logger.Render`, `RenderRaw`, `Newline`; JSON writer ignore; no-console no-op |
| `logger_settheme_test.go` | `Logger.Theme()`, `SetTheme` propagation, `With()` clone inheritance |
| `integration_test.go` | End-to-end integration |

### `velocity/pretty`

| File | Coverage |
|------|----------|
| `box_test.go` | Long title, border alignment, empty content, Unicode |
| `banner_test.go` | Banner rendering |
| `progress_test.go` | Concurrent Complete/Stop, nil SetStyle |
| `renderable_test.go` | Compile-time Renderable compliance; render parity for all result types |
| `pretty_logger_test.go` | `NewFromLogger` routing; nil logger returns nil |

### `velocity/slog`

| File | Coverage |
|------|----------|
| `handler_test.go` | slog bridge: basic, attrs, groups, levels, types, nil, concurrency |

## Dependencies

- `golang.org/x/term` for TTY detection only
- Zero third-party test dependencies

## Design Principles

- **Zero-alloc hot path**: Fields use `unsafe.Pointer` + `int64` storage. Integer fields write directly via `formatInt` stack buffer. Entry pooling via `sync.Pool` with CAS-based return. ANSI codes pre-cached on `Theme`. Timestamps via `time.AppendFormat`. Floats via `strconv.FormatFloat`. Writers format outside the mutex, locking only for I/O.
- **Three-package split**: Core logging stays in the root package. Pretty-printing (boxes, panels, banners, tables, trees, progress) lives in `velocity/pretty` to keep the root package focused on the hot path. The slog bridge lives in `velocity/slog` (`package velocityslog`) to avoid pulling `log/slog` into callers that don't need it.
- **Field constructors**: `String` (formerly `StringField`), `Error` (formerly `ErrorField`), `Int`, `Float64`, `Bool`, `Duration`, `Time`, `Stringer`, `Bytes`. Typed nils caught via `reflect` in `Error`/`Stringer` constructors.
- **Nil-safe**: Every public method handles nil receivers. Typed nils caught via `reflect` in `Error`/`Stringer` constructors.
- **Thread-safe**: Atomic level checks, mutex-protected writers, lock-free ring buffer. Progress/spinner stop uses `CompareAndSwap` to prevent double-close panics.
- **No `encoding/json`**: JSON writer is hand-rolled for performance.
- **Caller capture**: `AddCaller` populates file:line, rendered by all four writer paths (JSON, template, console fallback, ring buffer fallback).
- **slog bridge**: `velocityslog.Handler` implements `log/slog.Handler`. WithAttrs pre-converts to velocity Fields. WithGroup caches dotted prefix. Level mapping via `mapSlogLevel`. Entry pool used for Handle.

## Concurrency

- `AtomicLevel`: single atomic load per log call, entries below threshold never allocate
- `MultiWriter`: per-writer buffered channels (256 cap), non-blocking send, `Retain()`/`Release()` lifecycle. Workers close their own writer via defer. Shutdown drain uses `for range ch` to guarantee all entries are processed.
- `RingBuffer`: CAS-based circular buffer, power-of-2 sizing, atomic commit flags, batched flush. Bounded spins (1000 iterations) in both writer and flusher to prevent hangs. Minimum size of 2 enforced. Flusher uses a single reusable batch buffer to avoid per-entry allocations. Shutdown flushes batchBuf before draining the ring.
- `Entry` ref counting: `atomic.Int32`, CAS return to pool prevents double-release
- `Logger.Render`/`RenderRaw`/`Newline`: render into a pooled buffer outside the lock, then acquire `consoleWriter.mu` only for the final write — same mutex as log calls, so rich output cannot interleave with log lines

## Dependency Graph

```
velocity/pretty  --> velocity (imports root for Logger, Field types)
velocity/slog    --> velocity (imports root for Logger, Entry, Field, Level)
```

## Linting

Uses `default: all` in `.golangci.yml`. `unsafe.Pointer` usage in field/buffer files is excluded from gosec G103. Run `make lint` to verify.

## Code Quality

### Always
- Run `make ready` before commit
- Australian English in comments/docs
- Comment **why**, not what

### Never
- Add dependencies without discussion
- Use `encoding/json` in hot paths
- Use `interface{}` where typed fields exist
- Create `_v2`, `_new`, `.bak` files
- Use `fmt.Sprintf` or `strconv.Itoa` on hot paths; use buffer writes or `formatInt`

## Review

Run `/review-velocity` for a comprehensive Opus-powered code review covering concurrency, memory, correctness, performance, API, user expectations, and benchmark validation.

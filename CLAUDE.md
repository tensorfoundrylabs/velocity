# Velocity

Standalone Go logging library with zero-allocation fields and rich terminal output. Extracted from FoundryOS.

## Commands

```bash
make ready          # Pre-commit gate: tidy, fmt, align, lint, vet, test-race
make test           # Run all tests
make test-race      # Tests with race detector
make test-cover     # Tests with coverage report
make lint           # golangci-lint (strict, all linters)
make fmt            # goimports + gofumpt
make install-tools  # Install golangci-lint, betteralign, goimports, gofumpt
make help           # Show all targets
```

Benchmarks: `go test -bench=. -benchmem -count=3 ./...`

## Package Structure

Single flat package, all files at repo root under `package velocity`.

| File | Purpose |
|------|---------|
| `logger.go` | Core `Logger` type, log methods, `logInternal`, tree/table output |
| `entry.go` | Pooled `Entry` with atomic ref counting |
| `field.go` | Zero-alloc typed fields via `unsafe.Pointer`, typed nil guards via `reflect` |
| `field_conversion.go` | Field value extraction and string conversion |
| `config.go` | `Config` struct, `Builder`, presets |
| `options.go` | Functional options (`WithLevel`, `WithTheme`, etc.) |
| `defaults.go` | Default config values, TTY detection |
| `level.go` | Log levels, `AtomicLevel`, `MustParseLevel` |
| `console_writer.go` | Themed ANSI console output with caller rendering |
| `console_writer_ringbuffer.go` | Lock-free ring buffer console writer with timezone support |
| `json_writer.go` | Hand-rolled JSON output (no `encoding/json`) with caller rendering |
| `multi_writer.go` | Async fan-out to named writers, workers close own writer on shutdown |
| `writer.go` | `Writer` interface, `WriterFunc`, `NoOpWriter` |
| `ring_buffer.go` | CAS-based ring buffer, bounded spins, min size 2, batched flushing |
| `pretty.go` | Styled output: boxes (Unicode-safe), panels, banners, bullets, tables, trees |
| `progress.go` | `ProgressBar`, `Spinner`, `MultiProgress` with CAS-guarded stop |
| `template.go` | Log line templates with level styles and caller output |
| `theme.go` | Colour themes with pre-cached ANSI codes via `Theme.Cache()` |
| `sampler.go` | `CountSampler` for high-volume log reduction |
| `context.go` | `context.Context` integration |
| `buffer.go` | Tiered `BufferPool`, zero-copy `BytesBuffer`, `AppendTime` |
| `pools.go` | `sync.Pool` instances for entries, fields, buffers |
| `slog_handler.go` | `log/slog.Handler` bridge with cached group prefix, pre-converted attrs |
| `errors.go` | Sentinel errors |
| `doc.go` | Package documentation |

## Test Files

| File | Coverage |
|------|----------|
| `field_test.go` | `itoa` edge cases, typed nil error/stringer |
| `json_writer_test.go` | Nil fields, caller output |
| `console_writer_test.go` | Invalid level bounds, caller output |
| `console_writer_ringbuffer_test.go` | Timezone in fallback path |
| `ring_buffer_test.go` | Concurrent writes, overflow, bounded spin, zero-length, min size |
| `pretty_box_test.go` | Long title, border alignment, empty content, Unicode |
| `progress_test.go` | Concurrent Complete/Stop, nil SetStyle |
| `benchmark_test.go` | 19 benchmarks covering hot paths, fields, writers, pooling |
| `entry_test.go` | Entry pool, ref counting, concurrent access |
| `with_test.go` | `With()`, `WithTemplate`, nil/empty |
| `slog_handler_test.go` | slog bridge: basic, attrs, groups, levels, types, nil, concurrency |
| `fatal_test.go` | Fatal handler, nil logger subprocess test |
| `testutil_test.go` | Shared helpers: `waitFor`, `safeBuffer` |

## Dependencies

- `golang.org/x/term` for TTY detection only
- Zero third-party test dependencies

## Design Principles

- **Zero-alloc hot path**: Fields use `unsafe.Pointer` + `int64` storage. Integer fields write directly via `formatInt` stack buffer. Entry pooling via `sync.Pool` with CAS-based return. ANSI codes pre-cached on `Theme`. Timestamps via `time.AppendFormat`. Floats via `strconv.FormatFloat`. Writers format outside the mutex, locking only for I/O.
- **Nil-safe**: Every public method handles nil receivers. Typed nils caught via `reflect` in `ErrorField`/`Stringer` constructors.
- **Thread-safe**: Atomic level checks, mutex-protected writers, lock-free ring buffer. Progress/spinner stop uses `CompareAndSwap` to prevent double-close panics.
- **No `encoding/json`**: JSON writer is hand-rolled for performance.
- **Caller capture**: `AddCaller` populates file:line, rendered by all four writer paths (JSON, template, console fallback, ring buffer fallback).
- **slog bridge**: `SlogHandler` implements `log/slog.Handler`. WithAttrs pre-converts to velocity Fields. WithGroup caches dotted prefix. Level mapping via `mapSlogLevel`. Entry pool used for Handle.

## Concurrency

- `AtomicLevel`: single atomic load per log call, entries below threshold never allocate
- `MultiWriter`: per-writer buffered channels (256 cap), non-blocking send, `Retain()`/`Release()` lifecycle. Workers close their own writer via defer. Shutdown drain uses `for range ch` to guarantee all entries are processed.
- `RingBuffer`: CAS-based circular buffer, power-of-2 sizing, atomic commit flags, batched flush. Bounded spins (1000 iterations) in both writer and flusher to prevent hangs. Minimum size of 2 enforced. Flusher uses a single reusable batch buffer to avoid per-entry allocations. Shutdown flushes batchBuf before draining the ring.
- `Entry` ref counting: `atomic.Int32`, CAS return to pool prevents double-release

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

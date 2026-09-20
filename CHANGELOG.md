# Changelog

## Unreleased

### Added

- `WithAsyncOutput(AsyncConfig{Queue, OnFull})` makes the primary structured
  (JSON) output non-blocking. Callers format each record exactly as before,
  then enqueue the finished bytes onto a bounded queue drained by a single
  goroutine that performs the write, so no syscall (and no mutex held across
  one) runs on the logging goroutine. `OnFull` selects `AsyncBlock`
  (lossless back-pressure over Queue records of headroom, then the caller
  back-pressures to the sink's write rate; the default) or `AsyncDrop`
  (never block; losses counted and reported via
  `Logger.StructuredDroppedCount` and `JSONWriter.DroppedCount`; the choice
  for availability-critical request paths). Default queue depth is `DefaultAsyncQueue`
  (8192), about 140ms of burst headroom at 60k lines/s; formatting buffers
  live in `sync.Pool`, so the queue retains nothing itself and the collector
  reclaims idle buffers (the first Queue records warm the pool once, 16 MiB
  at the default depth). Fatal delivery stays reliable and ordered behind a barrier
  before the FatalHandler runs; Flush and Close drain everything accepted,
  with no timeout (a stalled sink blocks Close as it would synchronously),
  and worst-case Close performs Queue sink writes, so a deadline-bounded
  caller must bound Close itself.
  Console output is unaffected. Without the option, behaviour is unchanged
  and the synchronous path gains no allocations.
- `NewAsyncJSONWriter(out, AsyncConfig)` constructs the async JSON writer
  directly.
- `WithWriterQueueDepth(n)` (`WriterOption`) configures the per-writer
  channel depth `MultiWriter` allocates in `AddWriter`, previously hardcoded
  at 256 (still the default; non-positive values fall back to it).

### Fixed

- An `Any` field holding an error now renders its `Error()` message as a
  JSON string, and a `fmt.Stringer` whose marshaled form is an empty object
  renders `String()`. Since v2.2.0 `Any` renders through `json.Marshal`,
  which only sees exported fields, so `errors.New`, `fmt.Errorf`, every
  `runtime.Error` and any opaque struct logged as `{}` and a recovered
  panic lost its message. This is a behaviour change from v2.2.0's `{}`
  output: those values now appear as their text. Precedence: a
  `json.Marshaler`'s explicit form always wins (a structured error carrying
  `MarshalJSON` keeps its shape), then `Error()` for errors, then
  `String()` for stringers that marshal to `{}`. Typed nils render null
  instead of panicking on a nil receiver.

## v2.2.1 (2026-09-19)

Fixes and allocation work from the post-v2.2.0 review round. No public API
changes.

### Bug fixes

- Malformed UTF-8 in JSON strings is now replaced with `U+FFFD` escape
  sequences instead of producing invalid JSON; valid Unicode is passed through
  untouched.
- Integer millisecond timings in inline indicators no longer overflow into
  nanoseconds.
- The count, timing and state-transition indicator options now consistently
  remove promoted fields from the pretty tree.
- Pooled entries clear hidden field references through the slice's full
  capacity on reset and release, so a later reuse cannot observe stale
  pointers.

### Performance

- Floats are formatted with appends into concrete buffers, avoiding temporary
  allocations on the formatting path.
- JSON buffers are reused up to 32 KiB; larger ones are dropped instead of
  retaining peak capacity in the pool.
- Oversized ring batches are released rather than kept at peak capacity.
- Snapshot copies are skipped when a subscriber queue is already full and the
  entry would be dropped anyway.
- `slogbridge` field prepending shifts fields without a temporary slice.
- `Detailed` children share the parent's immutable base fields instead of
  copying them.
- Status caller line numbers are formatted through the stack buffer.

### Tooling

- Gate tools are pinned to exact versions and `make ready` is read-only;
  formatter failures now surface instead of passing silently. Development
  tooling needs a newer toolchain than the library's Go 1.24 minimum; the
  library itself still supports Go 1.24.

## 2.2.0 (2026-09-16)

Behaviour and API changes from the logging-hardening fix-and-finish run
(see `docs/specs/logging-hardening-finish-report.md` in the development
tree for the full mapping to tests and measurements).

### Behaviour changes

- Logger family lifetime is now shared. Once any `Close` completes, the whole
  parent/child family is closed for good: later log calls are dropped,
  `AddWriter` can no longer revive output, and concurrent `Close` calls all
  wait for the same drain and return the same recorded result. Close also
  drains calls that were already admitted: console and JSON writes register as
  in-flight for the whole formatting cycle, and the render helpers
  (`Render`, `RenderRaw`, `Newline`, `BannerLines`) register before running,
  so a call that passed admission completes (or is dropped) rather than
  writing after Close returned. `Notify`/`NotifyLines`/`NotifyBox` remain a
  documented bypass with their own caller-owned destination.
- `MultiWriter.Close` (and `Logger.Close` through it) now returns worker close
  errors joined with `errors.Join`; previously all but one error were dropped.
- `Logger.Fatal` is exempt from the sampler in every dispatch path and takes a
  reliable ordered delivery: it waits for preceding accepted entries, its own
  write and a flush before invoking `FatalHandler`/exiting. Delivery is
  acknowledged per queue item (a FIFO barrier sentinel closed by the worker
  that dequeues it), so the wait cannot be satisfied by aggregate counters
  racing an ordinary sender. A custom handler
  that returns leaves the logger reusable. `LogEntry` and slog records at
  `LevelFatal` are logged but never exit the process.
- `<secure>` tag handling is now content-driven: the maybe-secure flag is
  derived from message content whenever scanning is enabled, independent of
  the current writer mix, so adding a writer after a scan can no longer leak
  plaintext. On topologies where every writer is trusted, the markers are
  stripped (plaintext shown); untrusted writers still see redaction. Group
  item text and continuation lines now follow the same policy as headers on
  console and JSON output.
- Colour permission is fixed at writer construction: `WithColour(false)`
  survives every theme swap, and a mono-to-coloured swap restores colour only
  where permission allows. `FORCE_COLOR` can style non-terminals but never
  grants trust or cursor control.
- A second `ConsoleWriterRB.Close` now waits for the same drain and returns
  `nil` instead of an error.
- `ConsoleWriterRB`'s ring-full direct-write fallback has been removed: when
  the byte queue is full the record is dropped, counted in `DroppedCount`
  (and the metrics error count), and `Write` returns `nil`. The deprecated
  writer is scheduled for removal in v3; use `ConsoleWriter`.
- Live widgets finalise exactly once: concurrent `Stop`/`Complete` calls all
  wait for the one finalisation, and no frame or summary is written after they
  return. Cursor-control capability now depends on the real destination, not
  on `FORCE_COLOR`.
- Table, box, banner, component-column and truncation widths are measured in
  terminal cells (uniseg grapheme widths) with an allocation-free printable
  ASCII fast path. Absent table cells are padded to the declared geometry and
  negative `Bullet` nesting is clamped.
- `Logger.Status` now respects the logger's resolved colour permission on
  terminals: `WithColour(false)` and `NO_COLOR` suppress status styling that
  previously leaked through the raw terminal flag. Styling and trust are
  propagated separately, so a trusted terminal with styling disabled still
  shows `Secure` field plaintext, without ANSI.
- Multi-row live displays no longer walk down the terminal on each repaint:
  clearing now returns the cursor to the first live row, so repeated
  repaints, grow/shrink, widget removal and interleaved log lines hold a
  stable vertical position.

### New APIs

- `live.NewOutput(io.Writer) *live.Output`: opt-in shared terminal
  coordinator. Pass the same `*Output` to `WithConsoleOutput` and the widget
  constructors so log records and live displays serialise on one destination.
- `StyledRenderable`: optional extension to `Renderable` for types that need
  resolved styling and trust propagated separately at render time
  (`RenderStyled(w, styled, trusted)`). `Logger.Render` and `RenderRaw`
  dispatch to it first; `StatusItem` implements it. Legacy `Renderable` and
  `TTYRenderable` implementations are unaffected.
- `velocity.Uint64(key string, val uint64) Field`: lossless unsigned integer
  field on every output path, stored as bits without a float64 round trip.

### Internal

- `ringbuffer.go` rewritten as a mutex-guarded bounded byte queue with owned
  byte storage and a single drainer goroutine; the speculative CAS/skip
  reclamation protocol is gone.
- The unused per-Logger buffer pool (`Logger.bufPool`) was removed.
  `WithBufferSize` and `WithFieldPoolSize` remain deprecated compatibility
  options that do not tune the shared pools.
- `PutFieldSlice` now clears the pooled slice through its capacity, so a
  later, smaller use cannot observe stale field pointers.
- New direct dependency: `github.com/rivo/uniseg` v0.4.7 for terminal cell
  widths. `golang.org/x/term` remains.

## v2.1.0

Tag when ready (after CI is green): `git tag v2.1.0`

### New features

- `MultiWriter.DroppedCount() uint64` exposes a running count of entries silently discarded when a worker's buffered channel is full. Mirrors the same metric on `RingBuffer` so callers can observe back-pressure from slow writers without polling `Stats()`.
- `WithLevels(level Level) Option` sets both `ConsoleLevel` and `StructuredLevel` in a single call. Reach for it when all outputs should share the same threshold; use `WithLevel` or `WithStructuredLevel` when the thresholds need to differ.
- `Logger.CallerEnabled()` reports whether the logger is configured to capture caller information. Adapters that carry their own program counter (such as `slogbridge`, which receives `record.PC` from slog) use it to decide whether to resolve that PC into `Caller`/`Line`/`Function` fields.
- `FORCE_COLOR=<non-empty>` environment variable forces ANSI colour output regardless of whether stdout is a real terminal. Useful on Windows where terminal emulators proxy stdout through a named pipe, causing `term.IsTerminal` to return false even in a colour-capable terminal.
- `NO_COLOR=<non-empty>` environment variable unconditionally disables ANSI colour output, following the https://no-color.org convention. Takes precedence over `FORCE_COLOR`.
- `TTYRenderable` interface — optional extension to `Renderable` for types that need the terminal state at render time. `Logger.Render` and `Logger.RenderRaw` detect this interface and pass the console writer's resolved TTY state so colour decisions are correct even when rendering through an intermediate buffer.

### Bug fixes

- Fixed field corruption in `Logger.LogEntry` (used by slogbridge) when base fields were prepended in-place into a shared backing array, silently overwriting the first `len(baseFields)` user fields.
- Fixed caller off-by-one for `Logger.Status`, `Logger.Group`, and `Logger.Continue`: these 3-frame call paths were skipping 4 frames and reported the wrong source location.
- `slogbridge.Handler` now resolves `record.PC` into `Caller`/`Line`/`Function` fields when the velocity logger has caller capture enabled; previously PC was silently dropped.
- `ConsoleWriter.SetTheme` now builds a new `Template` rather than mutating the existing one in-place, eliminating a data race with concurrent `WriteSecure` calls that snapshot the template pointer under the lock.
- Guarded the `l.writers.mw != nil` nil-check in `logStatusStructuredWithFields` under an RLock, preventing a race with concurrent `AddWriter`/`Close` calls.
- Ring-buffer flusher no longer busy-polls when idle: replaced the spinning `default:` branch with a signal channel (`writeCh`) that `Write` kicks after each commit, parking the flusher until work arrives.
- `MultiWriter.Write` now takes `RLock` instead of a full write lock, allowing concurrent log fan-outs to proceed in parallel.
- `FieldValueToString` (`FieldTypeInt`, `FieldTypeGroupItems`, `FieldTypeContinuationLines`) no longer returns a string backed by a stack-local buffer; uses `strconv.FormatInt` instead.
- All four JSON write paths (`WriteSecure`, `WriteStatusSecure`, `WriteGroupSecure`, `WriteContinueSecure`) now append the newline inside the buffer before a single `Write` call, halving syscalls per entry.
- Effective log level now only accounts for outputs that actually exist; a console-only logger with a high console level no longer paid for sub-threshold structured work due to the default `StructuredLevel` dragging the gate down.
- `ConsoleWriterRB` direct-write fallback no longer races with the ring-buffer flusher: both paths now serialise via a shared mutex. `ConsoleWriterRB` is also marked deprecated.
- `ConsoleWriterRB` constructor and `SetTheme` now derive `useColours` from `resolveColourForWriter` instead of hardcoding `true`, so ANSI sequences are not emitted into pipes or files.
- `FromContext` returns a package-level singleton nop logger on cache miss instead of allocating a new `Logger` per call.
- `PutFieldSlice` no longer heap-allocates a new `*[]Field` wrapper on every call; the wrapper is now recycled from a secondary pool.
- `visibleLen` now correctly skips OSC 8 hyperlink escape sequences (`ESC ] 8 ; ... ST`) so column-width arithmetic in `Table`/`KeyValue` cells is correct when cells contain hyperlinks.
- `isTerminal` now calls `term.IsTerminal` for any `*os.File`, not only the three standard streams, matching `IsTerminalWriter` behaviour.
- `CLAUDE.md` corrected: `AtomicLevel` type reference removed (level is a bare `atomic.Int32`).
- Banner renderer now uses a consistent single-line box-drawing set (`┌─┐│└┘`) instead of mixing double corners (`╔╗╚╝`) with single-line fills.
- Console writer now correctly emits colour when no theme is explicitly configured; previously, the default-theme path silently disabled colour.
- `StatusItem`, `Group`, and `ContinuationBlock` now implement `TTYRenderable` and expose a `RenderTTY(w, isTTY)` method. Previously, when rendered via `Logger.Render`, `IsTerminalWriter` on the intermediate buffer always returned false, producing plain (uncoloured) badge/item output even on real terminals.
- `template.useColours` is now gated on actual TTY state at writer construction, not just on whether the theme has colours. Previously, ANSI sequences were always emitted when the theme was non-mono, including when stdout was a pipe or file.
- `ConsoleWriter.SetTheme` now updates `template.useColours` to reflect the new theme and current TTY state; previously it left `useColours=false` from initial construction when the writer was built on a non-TTY.
- `ConsoleWriterRB` now uses TTY detection (`resolveColourForWriter`) to set its trust state, matching `ConsoleWriter`'s model. Previously, the template path always rendered Secure fields as plaintext regardless of whether the output was a terminal or a file/pipe.
- `StatusItem.writeStatusFields` now applies the same TTY-as-trust model as `ConsoleWriter`: Secure fields show plaintext on terminal output and are redacted in plain (non-TTY) renders. Previously, Secure fields were always redacted in Status badge output even on trusted terminals.
- `SetTheme(nil)` now documents and enforces "nil = reset to NightOwl" semantics; `Style()` and `cfg.ConsoleTheme` both reflect the reset. Previously, the nil behaviour was not regression-tested.

## v2.0.2 — 2026-05-30

### Bug fixes

- Pin the display timezone in the inline-indicator tests so the golden-output test is deterministic across host timezones. It asserted a fixed local timestamp and failed CI on UTC runners. Library code is identical to v2.0.1; this release only fixes the test so CI and the release build pass.

## v2.0.1 — 2026-05-30

### New features

- **Inline indicators** (`WithComponentStyling` and friends) — opt-in, pretty-console-only feature that promotes a configurable set of well-known fields to compact header tokens: a hashed-colour component name with a muted `│` bar, a `(N)` count suffix, a `⏱ …` timing suffix, and `⟳ from → to` state-transition arrows. Promoted fields are removed from the field tree by default so they are not shown twice. JSON writers are completely unaffected — every field still appears fully expanded.
  - `WithComponentStyling()` — convenience option: component field `"component"`, count field `"count"`, state pairs `old_state`/`new_state` and `prev_state`/`next_state`, glyph auto-detection. Timing fields are left for the caller (names are application-specific).
  - `WithComponentField(name string)` — enable component prefix, set field name
  - `WithComponentColumnWidth(n int)` — fixed column width for the name (default 8)
  - `WithCountFields(names ...string)` — promote integer count fields
  - `WithTimingFields(names ...string)` — promote timing fields (int ms or `time.Duration`)
  - `WithStateTransitionPairs(pairs ...[2]string)` — register from/to field pairs
  - `WithInlineGlyphs(enabled bool)` — override `VELOCITY_GLYPHS` env detection
  - `WithComponentPalette(colours ...Colour)` — `ThemeOption` to set the hash palette
  - `WithComponentColour(name string, c Colour)` — `ThemeOption` to pin one component name
- New example `examples/component-logging` demonstrating the full indicator set with a Fleet/Scout/Relay service simulation and JSON-parity proof.

## v2.0.0 — 2026-05-15

Tag when ready: `git tag v2.0.0 feature/v2`

### Breaking

- Module path changed to `github.com/tensorfoundrylabs/velocity/v2` — update all imports accordingly
- `NewWithBuilder`, `NewWithOptions`, `NewWithConfig`, `NewDevelopment`, `NewForTesting` removed — use `New(opts ...Option)` with preset options
- `NopLogger()` retained for compatibility — equivalent to `New(WithNop())`
- `Builder` type removed — configure via options only
- `Config` struct unexported — no direct field access
- `Default*Config` family removed
- `Fields` struct, `NewFields`, `F`, `Milliseconds` removed — use typed field constructors directly
- `*Detailed` methods removed (`DebugDetailed`, `InfoDetailed`, etc.) — use `Logger.Detailed()` for a child logger with tree display
- `Logger.Raw` removed
- `Logger.Banner` renamed to `Logger.BannerLines` (avoids collision with `Banner` renderable type)
- `Logger.SetTemplate` / `Logger.WithTemplate` removed
- `Logger.Status() *StatusFormatter` removed — use `Logger.Style() *Theme`
- `StatusFormatter` type removed
- `AtomicLevel` exported type removed — level is now an internal `atomic.Int32`
- `velocity/pretty` package removed — all renderables moved to root package
- `velocity/slog` package removed — replaced by `velocity/slogbridge` (`package slogbridge`)
- `BoxResult`, `TableResult`, `TreeResult`, `BannerResult`, `KeyValueResult`, `SystemInfoResult` removed — types renamed to `Box`, `Table`, `Tree`, `Banner`, `KeyValue`, `SystemInfo`
- `BulletResult` removed — `Bullet` was only ever a `Logger` method, not a standalone type
- `NewFromLogger` constructor pattern removed from pretty — use `Logger.Box(...)`, `Logger.Table(...)`, etc. directly
- Theme `Cache()` and `EnsureCached()` removed — themes are immutable post-construction
- Colour options consolidated to `WithColour(bool)`
- `WithDisplayTimezone` now takes `*time.Location` directly; helper `MustLocation(name string)` added
- `Logger.AddWriter` now accepts `...WriterOption` for trust and capability configuration

### New features

- `New(opts ...Option)` and `TryNew(opts ...Option)` — single constructor entry point
- Preset options: `WithDevelopment()`, `WithProduction()`, `WithContainer()`, `WithTesting(t)`, `WithNop()`, `WithHighThroughput()`
- `Logger.WithComponent(name string) *Logger` — named child logger
- `Logger.WithRequest(id string) *Logger` — request-scoped child logger
- `Logger.Detailed() *Logger` — child logger with forced tree display
- `Logger.Style() *Theme` — theme accessor
- `Logger.Close()` — idempotent, flushes all owned writers; after-close calls are silent no-ops
- `ParseLevel(string) (Level, error)` — non-panicking sibling to `MustParseLevel`
- `NewTheme(name, ...ThemeOption) *Theme` — immutable theme builder
- `StyleSlot` enum with 16 semantic slots: `SlotGood`, `SlotBad`, `SlotWarn`, `SlotMuted`, `SlotStrong`, `SlotHeading`, `SlotEndpoint`, `SlotHyperlink`, `SlotContinuation`, `SlotCount`, `SlotSecure`, `SlotStatusOK`, `SlotStatusFail`, `SlotStatusWarn`, `SlotStatusInfo`, `SlotTableHeader`
- `Theme.Format(slot, s)`, `Theme.Wrap(slot)`, `Theme.Stylish(w)` — theme styling API
- `ThemeMono` — new colour-free built-in theme
- Writer capability interfaces: `ThemedWriter`, `LeveledWriter`, `FlushableWriter`, `TrustedWriter`
- `WriterTrusted()` writer option — marks a writer as trusted for receiving un-redacted secure fields
- `FilteredWriter` — wraps any writer with level filtering
- `Logger.Writer(name) Writer` — accessor for named writers
- `Logger.RemoveWriter(name) Writer` — returns removed writer for caller cleanup
- `RingBufferWriter` — in-process log capture with `Snapshot`, `Subscribe`, `Stats`
- `EntrySnapshot` — deep-copy value type; redacted unless writer is trusted
- `RingStats` — capacity, fill, drop counts
- `Logger.Notify(format, args...)`, `Logger.NotifyLines(lines...)`, `Logger.NotifyBox(*Box)` — ephemeral operator output that bypasses structured pipeline
- `WithNotifyOutput(io.Writer)` — override notify target (default `os.Stderr`)
- Secure field constructors: `Secure(k, v)`, `SecureURL(k, u)`, `Redacted(k)`, `Truncated(k, v, maxLen)`
- `<secure>...</secure>` tag scanning in message strings — redacted in JSON and non-TTY console output
- `WithSecureTags(bool)` option — explicit opt-out of tag scanning
- `scanSecure` per-instance atomic flag — recomputed on writer add/remove; zero cost when all writers are trusted
- `StatusItem` renderable and `Logger.Status(level, kind, msg, fields...)` log-call form
  - Console path: renders an inline indented badge (no timestamp, no level label) via `Logger.Render`
  - JSON path: emits a full structured record with `"status"` field for log queries
  - `NewStatusItem` no longer takes `isTTY bool` — TTY is resolved at `Render(w)` time
- `StatusKind` enum: `StatusOK`, `StatusFail`, `StatusWarn`, `StatusInfo`, `StatusPending`, `StatusSkipped`
- `Group` renderable and `Logger.Group(level, msg, items...)` — count-headed indented block
  - `NewGroup` no longer takes `isTTY bool` — TTY resolved at `Render(w)` time
- `GroupItem{Marker, Text}` — individual group entry
- `ContinuationBlock` renderable and `Logger.Continue(level, msg, lines...)` — `│`-glyph continuation lines
  - `NewContinuationBlock` no longer takes `isTTY bool` — TTY resolved at `Render(w)` time
- `Hyperlink(uri, text, ...opts) string` — OSC 8 hyperlink with TTY detection and three fallback modes
- `HyperlinkFallbackNone`, `HyperlinkFallbackParens`, `HyperlinkFallbackBrackets` — fallback modes
- `HyperlinksSupported()` — cached TTY detection
- `WithHyperlinkFallback(mode)` — per-call fallback override
- `Pretty` facade moved to root; `NewPretty(w, theme)` and `NewPrettyFromLogger(logger)` constructors
- Logger convenience methods: `Logger.Box`, `Logger.Table`, `Logger.Tree`, `Logger.BannerLines`, `Logger.KeyValues`, `Logger.SystemInfo` — render directly through console writer mutex
- `velocity/live` package — stateful animated types (`Spinner`, `ProgressBar`, `MultiProgress`) extracted from old `velocity/pretty`; all types suppress control sequences (\r, ANSI erase) when writer is not a terminal
- `velocity/slogbridge` — slog bridge package renamed, with corrected benchmark (was writing to stdout in v1)
- 18 examples covering all major features (up from 11 in v1)

### Performance

- `Info, 5 fields`: 38 ns → 33 ns (-13%); zero allocs preserved
- `Info, 10 fields`: 39 ns → 35 ns (-10%); zero allocs preserved
- `Info, tree mode`: 36 ns → 33 ns (-8%); zero allocs preserved
- `WithComponent child`: 270 ns → 159 ns (-41%); 3 allocs preserved
- `SecureScan_NoMatch`: 67 ns → 35 ns (-48%); zero allocs preserved; `IndexByte` fast-exit before any field inspection
- `slog handler, 3 attrs`: benchmark corrected (v1 was writing to stdout); real v2 cost is ~99 ns / 3 allocs / 144 B
- `Info, no fields`: 26 ns → 28 ns (+8%); scanSecure flag check adds ~2 ns on the no-field path
- `ConsoleWriter, 5 fields`: 433 ns → 483 ns (+12%); immutable theme lookup and writer capability checks added
- `JSONWriter, 5 fields`: 594 ns → 642 ns (+8%); `<secure>` tag scan path added; zero allocs preserved
- `BufferPool_GetPut`: 25 ns → 17 ns (-32%); tiered pool restructure
- Allocation profile unchanged on all zero-alloc paths

### Internal

- Package `velocity/pretty` eliminated; import cycle resolved by moving all renderables to root
- Package `velocity/slog` renamed `velocity/slogbridge` (`package slogbridge`)
- Package `velocity/live` created for stateful animated types
- `AtomicLevel` is now an internal `atomic.Int32`; API surface uses `Level` type throughout
- Theme construction is eager — no `sync.Once`, no `Cache()` call needed by callers
- `scanSecure atomic.Bool` recomputed on writer topology changes, not per log call
- All built-in themes ported to `NewTheme` immutable form
- `slogbridge` benchmark fixed to use `WithNop()` instead of writing to stdout

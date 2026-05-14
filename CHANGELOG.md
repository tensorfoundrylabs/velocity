# Changelog

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

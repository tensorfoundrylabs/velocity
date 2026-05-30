# Changelog

## Unreleased

### New features

- `FORCE_COLOR=<non-empty>` environment variable forces ANSI colour output regardless of whether stdout is a real terminal. Useful on Windows where terminal emulators proxy stdout through a named pipe, causing `term.IsTerminal` to return false even in a colour-capable terminal.
- `NO_COLOR=<non-empty>` environment variable unconditionally disables ANSI colour output, following the https://no-color.org convention. Takes precedence over `FORCE_COLOR`.
- `TTYRenderable` interface — optional extension to `Renderable` for types that need the terminal state at render time. `Logger.Render` and `Logger.RenderRaw` detect this interface and pass the console writer's resolved TTY state so colour decisions are correct even when rendering through an intermediate buffer.

### Bug fixes

- Console writer now correctly emits colour when no theme is explicitly configured; previously, the default-theme path silently disabled colour.
- `StatusItem`, `Group`, and `ContinuationBlock` now implement `TTYRenderable` and expose a `RenderTTY(w, isTTY)` method. Previously, when rendered via `Logger.Render`, `IsTerminalWriter` on the intermediate buffer always returned false, producing plain (uncoloured) badge/item output even on real terminals.
- `template.useColours` is now gated on actual TTY state at writer construction, not just on whether the theme has colours. Previously, ANSI sequences were always emitted when the theme was non-mono, including when stdout was a pipe or file.
- `ConsoleWriter.SetTheme` now updates `template.useColours` to reflect the new theme and current TTY state; previously it left `useColours=false` from initial construction when the writer was built on a non-TTY.
- `ConsoleWriterRB` now uses TTY detection (`resolveColourForWriter`) to set its trust state, matching `ConsoleWriter`'s model. Previously, the template path always rendered Secure fields as plaintext regardless of whether the output was a terminal or a file/pipe.
- `StatusItem.writeStatusFields` now applies the same TTY-as-trust model as `ConsoleWriter`: Secure fields show plaintext on terminal output and are redacted in plain (non-TTY) renders. Previously, Secure fields were always redacted in Status badge output even on trusted terminals.
- `SetTheme(nil)` now documents and enforces "nil = reset to NightOwl" semantics; `Style()` and `cfg.ConsoleTheme` both reflect the reset. Previously, the nil behaviour was not regression-tested.

## v2.0.1 — pending

### New features

- **Inline indicators** (`WithComponentStyling` and friends) — opt-in, pretty-console-only feature that promotes a configurable set of well-known fields to compact header tokens: a hashed-colour component name with a muted `│` bar, a `(N)` count suffix, a `[⏱ …]` timing suffix, and `from → to` state-transition arrows. Promoted fields are removed from the field tree by default so they are not shown twice. JSON writers are completely unaffected — every field still appears fully expanded.
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

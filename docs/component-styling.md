# Component-aware pretty output

When multiple services share a single console stream, the standard field tree
becomes hard to scan at speed. Every line has `component: Fleet` or `component: Scout`
as its first tree child, and recurring fields like `count`, `startup_ms`, or
`old_state`/`new_state` push the actual message further down.

The inline-indicator feature solves this by promoting a small set of well-known fields
to compact tokens on the header line itself. The change is **opt-in and pretty-only**.
JSON and all other structured writers are completely unaffected.

## Before / after

Without indicators (default tree mode):

```
2026-05-30 14:52:33 [INFO] service started
                     ├ component: Scout
                     ├ startup_ms: 2000
                     ├ name: discovery
                     └ phase: 0
2026-05-30 14:52:33 [INFO] stopping services
                     ├ component: Relay
                     └ count: 4
2026-05-30 14:52:33 [WARN] circuit breaker opened
                     ├ component: Scout
                     ├ old_state: closed
                     ├ new_state: open
                     └ endpoint: 192.168.0.181:8010
2026-05-30 14:52:33 [INFO] Source state transition
                     ├ component: Fleet
                     ├ prev_state: connected
                     ├ next_state: stale
                     └ source_id: relay-CODY-RYZEN
2026-05-30 14:52:33 [INFO] Fleet has shutdown
                     └ component: Fleet
```

With `WithComponentStyling()` + `WithTimingFields("startup_ms")`:

```
2026-05-30 14:52:33 [INFO] Scout │ service started ⏱ 2.00s
                            ├ name: discovery
                            └ phase: 0
2026-05-30 14:52:33 [INFO] Relay │ stopping services (4)
2026-05-30 14:52:33 [WARN] Scout │ circuit breaker opened closed → open
                            └ endpoint: 192.168.0.181:8010
2026-05-30 14:52:33 [INFO] Fleet │ Source state transition connected → stale
                            └ source_id: relay-CODY-RYZEN
2026-05-30 14:52:33 [INFO] Fleet │ Fleet has shutdown
```

The component column is **compact** by default: the name keeps its natural width and
the bars line up whenever names share a length (the common case for a fixed service
set like Fleet/Scout/Relay). For guaranteed alignment when names vary in length, set
a fixed column with `WithComponentColumnWidth(n)` (for example `WithComponentColumnWidth(5)`).

Reading order is now: **when** (timestamp), **how bad** (level), **who** (component), **what** (message).

## Quick start

```go
log := velocity.New(
    velocity.WithDevelopment(),
    velocity.WithComponentStyling(),
    velocity.WithTimingFields("startup_ms", "startup_time", "shutdown_ms"),
    velocity.WithFieldDisplayMode(velocity.FieldDisplayTree),
)

fleet := log.WithComponent("Fleet")
scout := log.WithComponent("Scout")

fleet.Info("source connected",
    velocity.String("old_state", "disconnected"),
    velocity.String("new_state", "connected"),
    velocity.String("source_id", "relay-CODY-RYZEN"),
)
scout.Warn("circuit breaker opened",
    velocity.String("old_state", "closed"),
    velocity.String("new_state", "open"),
    velocity.String("endpoint", "192.168.0.181:8010"),
)
```

See `examples/component-logging` for a complete Fleet/Scout/Relay simulation.

## Options

### `WithComponentStyling()`

Convenience preset. Enables:

- Component prefix with field name `"component"`, compact column (natural width)
- Count promotion on field `"count"`
- State-transition pairs `old_state`/`new_state` and `prev_state`/`next_state`
- Glyph auto-detection (reads `VELOCITY_GLYPHS` env var at first use)

Timing fields are **not** included, because their names are application-specific. Call
`WithTimingFields` separately.

### `WithComponentField(name string)`

Enables the component prefix and sets the field name to look up on each entry.
The column is compact (natural width) unless you set `WithComponentColumnWidth`.

### `WithComponentColumnWidth(n int)`

Sets a fixed column width for the component name so the bars align even when names
differ in length. Names shorter than `n` are right-padded with spaces; names longer
than `n` are truncated with an ellipsis. The default (unset, or `0`) is compact: the
name keeps its natural width. For a uniform set of short service names, a small fixed
width such as `WithComponentColumnWidth(5)` (Fleet, Scout, Relay) keeps a tidy column
with a single space before the bar.

### `WithCountFields(names ...string)`

Registers field names whose integer values are promoted to `(N)` after the message.
The first matching field on each entry wins. Pass multiple names when different
services use different field names.

### `WithTimingFields(names ...string)`

Registers field names promoted to a `⏱ …` timing suffix (bracketless with the
stopwatch glyph; `[…]` is the ASCII fallback when glyphs are off). All matching fields
appear inside one bracket, comma-separated. Accepts both integer millisecond fields
and `time.Duration` fields. The value is formatted with smart precision:

| Value | Rendered as |
|-------|-------------|
| 45 ms | `45ms` |
| 800 ms | `800ms` |
| 1500 ms | `1.50s` |
| 2000 ms | `2.00s` |

### `WithStateTransitionPairs(pairs ...[2]string)`

Registers pairs of field names that together represent a state transition. When both
fields of a pair are present on the same entry, they are collapsed into a
`from → to` suffix. If only one half is present, both stay in the tree (no partial
collapse). Pairs are checked in order; the first complete pair wins.

```go
velocity.WithStateTransitionPairs(
    [2]string{"old_state", "new_state"},    // standard pair
    [2]string{"prev_state", "next_state"},  // alternate naming
)
```

### `WithInlineGlyphs(enabled bool)`

Overrides automatic glyph detection. When `false`, Unicode glyphs (`⏱`, `→`) are
replaced with ASCII fallbacks (`[…]`, `->`). The default is to read the
`VELOCITY_GLYPHS` environment variable (set to `0` to disable).

## Theme customisation

The component name colour is derived from a deterministic FNV-1a hash of the name
into a curated per-theme palette. The same name maps to the same colour across
restarts and processes, which is useful when services run as separate OS processes.

Override the palette or pin a specific name via `ThemeOption`s:

```go
myTheme := velocity.NewTheme("custom",
    // Replace the default palette with your own.
    velocity.WithComponentPalette(
        velocity.RGB(0x82, 0xAA, 0xFF), // blue
        velocity.RGB(0xC3, 0xE8, 0x8D), // green
        velocity.RGB(0xFF, 0xCB, 0x6B), // amber
    ),
    // Pin one service to a specific colour regardless of the hash.
    velocity.WithComponentColour("Fleet", velocity.RGB(0xFF, 0x5F, 0x87)),
)

log := velocity.New(
    velocity.WithDevelopment(),
    velocity.WithTheme(myTheme),
    velocity.WithComponentStyling(),
)
```

## Colour and glyph degradation

| Condition | Effect |
|-----------|--------|
| `NO_COLOR=1` env | All colour disabled; component name renders in plain text |
| `FORCE_COLOR=1` env | Colour forced on (useful on Windows with piped stdout) |
| `VELOCITY_GLYPHS=0` env | Unicode glyphs replaced: `⏱ …` → `[…]`, `→` → `->` |
| `WithInlineGlyphs(false)` | Same as `VELOCITY_GLYPHS=0`, takes precedence over env |
| Non-TTY stdout (pipe/file) | Colour auto-disabled; glyphs unaffected |

## JSON parity guarantee

The indicator config lives exclusively on the console writer's template. The JSON
writer (`writer_json.go`) is not modified and never consulted during indicator
rendering. A single entry logged through both writers produces:

**Console:**
```
2026-05-30 14:52:33 [INFO] Scout │ service started (12) ⏱ 3.20s
```

**JSON:**
```json
{"timestamp":"2026-05-30T14:52:33Z","level":"INFO","message":"service started",
 "component":"Scout","count":12,"startup_ms":3200}
```

Every promoted field is still present in the JSON record, fully expanded.
Structured queries, log aggregators, and alert rules see the complete data.

## Out of scope (v2.1 candidates)

- `Logger.Status`, `Group`, and `Continue` rich renderables (separate code paths)
- Repeated-line deduplication
- Relative timestamp eliding on sequential lines from the same component

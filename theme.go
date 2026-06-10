package velocity

import (
	"hash/fnv"
	"io"
	"sync"
)

// Colour represents an ANSI terminal colour, either 256-colour or true-colour RGB.
type Colour struct {
	colour256 int
	r, g, b   uint8
	isRGB     bool
}

// RGB constructs a true-colour (24-bit) Colour value.
func RGB(r, g, b uint8) Colour {
	return Colour{r: r, g: g, b: b, isRGB: true}
}

// Colour256 constructs a 256-colour Colour value. Values outside [0,255] clamp to 0.
func Colour256(c int) Colour {
	if c < 0 || c > 255 {
		c = 0
	}
	return Colour{colour256: c}
}

// ANSI generates the foreground or background escape sequence for this colour.
func (c Colour) ANSI(foreground bool) string {
	if c.isRGB {
		if foreground {
			return "\033[38;2;" + itoa(int(c.r)) + ";" + itoa(int(c.g)) + ";" + itoa(int(c.b)) + "m"
		}
		return "\033[48;2;" + itoa(int(c.r)) + ";" + itoa(int(c.g)) + ";" + itoa(int(c.b)) + "m"
	}
	if foreground {
		return "\033[38;5;" + itoa(c.colour256) + "m"
	}
	return "\033[48;5;" + itoa(c.colour256) + "m"
}

// isZero reports whether the colour is the zero value (no colour set).
func (c Colour) isZero() bool {
	return !c.isRGB && c.colour256 == 0
}

// Reset is the ANSI sequence that clears all colour and style attributes.
const Reset = "\033[0m"

// StyleSlot is a semantic slot in a Theme. Callers use slots rather than raw colours
// so themes can be swapped without updating every call site.
type StyleSlot uint8

const (
	SlotGood         StyleSlot = iota // success / positive outcome
	SlotBad                           // error / failure
	SlotWarn                          // warning / degraded
	SlotInfo                          // informational
	SlotMuted                         // secondary / de-emphasised text
	SlotStrong                        // emphasis / bold-equivalent
	SlotHeading                       // section headings
	SlotEndpoint                      // service/URL labels
	SlotHyperlink                     // clickable URLs (OSC 8)
	SlotContinuation                  // │ glyph on continuation lines
	SlotCount                         // count/numeric badge
	SlotSecure                        // masked/secure field indicator
	SlotStatusOK                      // [ OK ] badge
	SlotStatusFail                    // [FAIL] badge
	SlotStatusWarn                    // [WARN] badge
	SlotStatusInfo                    // [INFO] badge
	SlotTableHeader                   // table column header text
	slotCount                         // sentinel — must stay last and unexported
)

// ThemeOption configures a Theme during construction.
type ThemeOption func(*themeBuilder)

// themeBuilder accumulates colours before the Theme is constructed.
type themeBuilder struct {
	// componentPins maps specific component names to pinned colours, overriding the hash.
	componentPins map[string]Colour

	// componentPalette is the ordered set of colours used for auto-hashing component names.
	componentPalette []Colour
	slotColours      [slotCount]Colour

	levelColours [6]Colour

	// Core log-line slots map to named fields for clarity.
	timestampColour Colour
	messageColour   Colour
	fieldKeyColour  Colour
	fieldValColour  Colour
	errorValColour  Colour

	// hasAnyColour is set by any option that sets at least one colour, so that
	// buildTheme can distinguish "no options passed" (Mono) from "options set colour
	// only on some fields". Without this flag, a theme with only level colours would
	// incorrectly be treated as noColour because the named colour fields are zero.
	hasAnyColour bool
}

// WithLevelColour sets the foreground colour for a specific log level.
func WithLevelColour(level Level, c Colour) ThemeOption {
	return func(b *themeBuilder) {
		if level >= 0 && int(level) < len(b.levelColours) {
			b.levelColours[level] = c
			b.hasAnyColour = true
		}
	}
}

// WithLevelColours sets foreground colours for all five log levels in one call.
func WithLevelColours(debug, info, warn, errr, fatal Colour) ThemeOption {
	return func(b *themeBuilder) {
		b.levelColours[LevelDebug] = debug
		b.levelColours[LevelInfo] = info
		b.levelColours[LevelWarn] = warn
		b.levelColours[LevelError] = errr
		b.levelColours[LevelFatal] = fatal
		// LevelOff inherits Info.
		b.levelColours[LevelOff] = info
		b.hasAnyColour = true
	}
}

// WithStyleSlot sets the foreground colour for a semantic style slot.
func WithStyleSlot(slot StyleSlot, c Colour) ThemeOption {
	return func(b *themeBuilder) {
		if slot < slotCount {
			b.slotColours[slot] = c
			b.hasAnyColour = true
		}
	}
}

// WithMessageColour sets the foreground colour for log message text.
func WithMessageColour(c Colour) ThemeOption {
	return func(b *themeBuilder) { b.messageColour = c; b.hasAnyColour = true }
}

// WithFieldColours sets the foreground colours for field keys, values, and error values.
func WithFieldColours(key, value, errorVal Colour) ThemeOption {
	return func(b *themeBuilder) {
		b.fieldKeyColour = key
		b.fieldValColour = value
		b.errorValColour = errorVal
		b.hasAnyColour = true
	}
}

// WithTimestampColour sets the foreground colour for the log timestamp.
func WithTimestampColour(c Colour) ThemeOption {
	return func(b *themeBuilder) { b.timestampColour = c; b.hasAnyColour = true }
}

// WithBracketColour sets the foreground colour used for structural chrome (borders, brackets).
// Equivalent to calling WithFieldColours with the same key colour; exists as a named shorthand
// for callers who only want to theme the chrome without touching value colours.
func WithBracketColour(c Colour) ThemeOption {
	return func(b *themeBuilder) { b.fieldKeyColour = c; b.hasAnyColour = true }
}

// WithComponentPalette sets the ordered palette used to colour component names via FNV-1a
// hashing. Each component name hashes to a stable index into this slice. Pass at least two
// colours for meaningful visual distinction. Replaces any previously set palette.
func WithComponentPalette(colours ...Colour) ThemeOption {
	return func(b *themeBuilder) {
		if len(colours) == 0 {
			return
		}
		b.componentPalette = make([]Colour, len(colours))
		copy(b.componentPalette, colours)
		b.hasAnyColour = true
	}
}

// WithComponentColour pins a specific component name to a fixed colour, bypassing the
// hash. Useful when a critical component (e.g. "Scout") must always appear in a
// specific colour regardless of palette ordering.
func WithComponentColour(name string, c Colour) ThemeOption {
	return func(b *themeBuilder) {
		if name == "" {
			return
		}
		if b.componentPins == nil {
			b.componentPins = make(map[string]Colour)
		}
		b.componentPins[name] = c
		b.hasAnyColour = true
	}
}

// Theme is an immutable colour palette constructed via NewTheme.
// All ANSI escape codes are pre-computed at construction — there is no lazy caching.
// After NewTheme returns, no field on Theme is ever mutated, with the exception of
// componentCache which is lazily populated under a sync.Map (safe for concurrent use).
type Theme struct {
	// componentPins maps exact component names to their pinned ANSI codes.
	// Built once at construction; immutable after that.
	componentPins map[string]string

	// Pre-computed per-slot foreground codes.
	cachedSlotFg [slotCount]string

	// Per-level foreground codes, indexed by Level constant.
	cachedLevelFg [6]string

	// componentCache memoises componentColourCode results so FNV hashing and map
	// lookups run at most once per unique component name. safe.Map handles concurrent
	// access; values are strings so there is no further synchronisation needed.
	componentCache sync.Map

	name string

	// Pre-computed ANSI sequences for the hot-path log line renderer.
	cachedTimestampFg string
	cachedMessageFg   string
	cachedFieldKeyFg  string
	cachedFieldValFg  string
	cachedErrorValFg  string

	// componentPalette holds the pre-computed ANSI foreground codes for component
	// colour hashing. Populated at construction; immutable after that.
	componentPalette []string

	// noColour is true when all slots are intentionally empty (ThemeMono).
	noColour bool
}

// NewTheme constructs an immutable Theme with all ANSI codes pre-computed.
// Panics if an invalid option is passed (no current option can produce this,
// but guards future additions). The name is informational only.
func NewTheme(name string, opts ...ThemeOption) *Theme {
	b := &themeBuilder{}
	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}
	return buildTheme(name, b)
}

func buildTheme(name string, b *themeBuilder) *Theme {
	t := &Theme{name: name}

	if !b.hasAnyColour {
		// No colour options were applied — this is a mono/passthrough theme.
		// All cached strings remain empty strings; Format returns the input unchanged.
		t.noColour = true
		return t
	}

	// Only generate ANSI strings for colours that were explicitly set.
	// Zero-value Colour (Colour256(0)) would produce a valid but unintended escape sequence.
	if !b.timestampColour.isZero() {
		t.cachedTimestampFg = b.timestampColour.ANSI(true)
	}
	if !b.messageColour.isZero() {
		t.cachedMessageFg = b.messageColour.ANSI(true)
	}
	if !b.fieldKeyColour.isZero() {
		t.cachedFieldKeyFg = b.fieldKeyColour.ANSI(true)
	}
	if !b.fieldValColour.isZero() {
		t.cachedFieldValFg = b.fieldValColour.ANSI(true)
	}
	if !b.errorValColour.isZero() {
		t.cachedErrorValFg = b.errorValColour.ANSI(true)
	}

	for i, c := range b.levelColours {
		if !c.isZero() {
			t.cachedLevelFg[i] = c.ANSI(true)
		}
	}

	for i, c := range b.slotColours {
		if !c.isZero() {
			t.cachedSlotFg[i] = c.ANSI(true)
		}
	}

	// Pre-compute component palette ANSI codes so componentColourCode is zero-alloc
	// after the first lookup per name.
	if len(b.componentPalette) > 0 {
		t.componentPalette = make([]string, len(b.componentPalette))
		for i, c := range b.componentPalette {
			if !c.isZero() {
				t.componentPalette[i] = c.ANSI(true)
			}
		}
	}

	if len(b.componentPins) > 0 {
		t.componentPins = make(map[string]string, len(b.componentPins))
		for name, c := range b.componentPins {
			if !c.isZero() {
				t.componentPins[name] = c.ANSI(true)
			}
		}
	}

	return t
}

// Name returns the theme's display name.
func (t *Theme) Name() string {
	if t == nil {
		return ""
	}
	return t.name
}

// Format wraps s with the ANSI foreground code for slot and the Reset sequence.
// When the theme has no colour configured for that slot (or the theme is Mono),
// s is returned unchanged with zero allocations.
func (t *Theme) Format(slot StyleSlot, s string) string {
	if t == nil || t.noColour || slot >= slotCount {
		return s
	}
	code := t.cachedSlotFg[slot]
	if code == "" {
		return s
	}
	return code + s + Reset
}

// Wrap returns the ANSI prefix and suffix strings for the given slot.
// Callers building strings around their own formatting can embed these directly.
// Both strings are empty when the theme has no colour for the slot.
func (t *Theme) Wrap(slot StyleSlot) (prefix, suffix string) {
	if t == nil || t.noColour || slot >= slotCount {
		return "", ""
	}
	code := t.cachedSlotFg[slot]
	if code == "" {
		return "", ""
	}
	return code, Reset
}

// Stylish reports whether the writer is ANSI-capable (i.e. a real terminal).
// Useful when callers want to decide whether to use Format before building a string.
// The theme is not consulted; only the writer matters.
func (*Theme) Stylish(w io.Writer) bool {
	return IsTerminalWriter(w)
}

// --- Internal accessors used by template.go, renderable.go, and pretty.go ---
// These return empty strings on mono/nil themes, so callers need not nil-check.

func (t *Theme) cachedTimestampFgStr() string {
	if t == nil {
		return ""
	}
	return t.cachedTimestampFg
}

func (t *Theme) cachedMessageFgStr() string {
	if t == nil {
		return ""
	}
	return t.cachedMessageFg
}

func (t *Theme) cachedFieldKeyFgStr() string {
	if t == nil {
		return ""
	}
	return t.cachedFieldKeyFg
}

func (t *Theme) cachedFieldValFgStr() string {
	if t == nil {
		return ""
	}
	return t.cachedFieldValFg
}

func (t *Theme) cachedErrorValFgStr() string {
	if t == nil {
		return ""
	}
	return t.cachedErrorValFg
}

func (t *Theme) cachedTableHeaderFgStr() string {
	if t == nil {
		return ""
	}
	return t.cachedSlotFg[SlotTableHeader]
}

func (t *Theme) cachedInfoColourFgStr() string {
	if t == nil {
		return ""
	}
	// SlotInfo maps to the info colour used in SystemInfo headers.
	return t.cachedSlotFg[SlotInfo]
}

// cachedLevelCode returns the pre-computed ANSI code for a log level.
func (t *Theme) cachedLevelCode(level Level) string {
	if t == nil || level < 0 || int(level) >= len(t.cachedLevelFg) {
		return ""
	}
	return t.cachedLevelFg[level]
}

// levelColourForStatus returns the ANSI code for a slot used by status colouring.
func (t *Theme) slotCode(slot StyleSlot) string {
	if t == nil || slot >= slotCount {
		return ""
	}
	return t.cachedSlotFg[slot]
}

// --- Public compatibility accessors used by Pretty.printStyled and external callers ---

// LevelColour returns the ANSI foreground escape for the given level.
// Empty string when no colour is configured or the theme is nil.
func (t *Theme) LevelColour(level Level) string {
	return t.cachedLevelCode(level)
}

// SlotColour returns the ANSI foreground escape for the given slot.
// Empty string when no colour is configured or the theme is nil.
func (t *Theme) SlotColour(slot StyleSlot) string {
	return t.slotCode(slot)
}

// --- Public accessor methods (replaces the old exported Cached* methods) ---

// CachedFieldKeyFg returns the pre-computed ANSI foreground for field keys.
// Retained for callers (renderable.go, template.go) that access it by method.
func (t *Theme) CachedFieldKeyFg() string { return t.cachedFieldKeyFgStr() }

// CachedFieldValFg returns the pre-computed ANSI foreground for field values.
func (t *Theme) CachedFieldValFg() string { return t.cachedFieldValFgStr() }

// CachedMessageFg returns the pre-computed ANSI foreground for message text.
func (t *Theme) CachedMessageFg() string { return t.cachedMessageFgStr() }

// CachedTableHeaderFg returns the pre-computed ANSI foreground for table headers.
func (t *Theme) CachedTableHeaderFg() string { return t.cachedTableHeaderFgStr() }

// CachedInfoColourFg returns the pre-computed ANSI foreground for the info colour.
func (t *Theme) CachedInfoColourFg() string { return t.cachedInfoColourFgStr() }

// ResetStr returns the ANSI reset sequence for colour themes, or an empty string
// for colour-free (mono) themes. Use this in renderers instead of the bare Reset
// constant so that NO_COLOR and ThemeMono suppress resets along with the colour codes.
func (t *Theme) ResetStr() string {
	if t == nil || t.noColour {
		return ""
	}
	return Reset
}

// componentColourCode returns the ANSI foreground code for the given component name.
// Pin overrides win; otherwise FNV-1a hash maps the name to a palette index.
// The result is memoised in componentCache so FNV runs at most once per unique name.
// Returns "" when the theme has no palette (noColour or palette not configured).
func (t *Theme) componentColourCode(name string) string {
	if t == nil || t.noColour || len(t.componentPalette) == 0 {
		return ""
	}

	// Fast path: already memoised.
	if v, ok := t.componentCache.Load(name); ok {
		return v.(string) //nolint:forcetypeassert // sync.Map only holds strings here
	}

	// Check pin overrides before hashing.
	var code string
	if len(t.componentPins) > 0 {
		if pinned, ok := t.componentPins[name]; ok {
			code = pinned
		}
	}

	if code == "" {
		// FNV-1a: deterministic, fast, good distribution for short strings.
		// Reduce modulo in uint32 space and index directly (a slice accepts any
		// integer type), avoiding an int(uint32) conversion that could wrap
		// negative on 32-bit platforms and panic on the slice index.
		h := fnv.New32a()
		_, _ = h.Write([]byte(name))                       //nolint:errcheck // hash.Hash32.Write never errors
		idx := h.Sum32() % uint32(len(t.componentPalette)) //nolint:gosec // palette length is small and positive; no overflow
		code = t.componentPalette[idx]
	}

	// Store and return. If two goroutines race here the value is identical, so
	// the later Store is a no-op in practice (same string).
	t.componentCache.Store(name, code)
	return code
}

// --- Built-in themes ---

// ThemeNightOwl is a dark, high-contrast palette inspired by the Night Owl VS Code theme.
var ThemeNightOwl = NewTheme(
	"Night Owl",
	WithLevelColours(
		RGB(0xC7, 0x92, 0xEA), // debug: purple
		RGB(0x82, 0xAA, 0xFF), // info: blue
		RGB(0xFF, 0xCB, 0x6B), // warn: amber
		RGB(0xFF, 0x55, 0x72), // error: red
		RGB(0xFF, 0x00, 0x00), // fatal: bright red
	),
	WithTimestampColour(RGB(0x7E, 0x8E, 0xA6)),
	WithMessageColour(RGB(0xE0, 0xE0, 0xE0)),
	WithFieldColours(
		RGB(0x7E, 0x8E, 0xA6), // key: muted steel
		RGB(0xD3, 0xD3, 0xD3), // value: light grey
		RGB(0xFF, 0x55, 0x72), // error value: red
	),
	WithStyleSlot(SlotGood, RGB(0x80, 0xD4, 0xAA)),
	WithStyleSlot(SlotBad, RGB(0xFF, 0x55, 0x72)),
	WithStyleSlot(SlotWarn, RGB(0xFF, 0xCB, 0x6B)),
	WithStyleSlot(SlotInfo, RGB(0x82, 0xAA, 0xFF)),
	WithStyleSlot(SlotMuted, RGB(0x7E, 0x8E, 0xA6)),
	WithStyleSlot(SlotStrong, RGB(0xE0, 0xE0, 0xE0)),
	WithStyleSlot(SlotHeading, RGB(0x7F, 0xD3, 0xFF)),
	WithStyleSlot(SlotEndpoint, RGB(0x82, 0xAA, 0xFF)),
	WithStyleSlot(SlotHyperlink, RGB(0x7F, 0xD3, 0xFF)),
	WithStyleSlot(SlotContinuation, RGB(0x7E, 0x8E, 0xA6)),
	WithStyleSlot(SlotCount, RGB(0xC7, 0x92, 0xEA)),
	WithStyleSlot(SlotSecure, RGB(0xFF, 0xCB, 0x6B)),
	WithStyleSlot(SlotStatusOK, RGB(0x80, 0xD4, 0xAA)),
	WithStyleSlot(SlotStatusFail, RGB(0xFF, 0x55, 0x72)),
	WithStyleSlot(SlotStatusWarn, RGB(0xFF, 0xCB, 0x6B)),
	WithStyleSlot(SlotStatusInfo, RGB(0x82, 0xAA, 0xFF)),
	WithStyleSlot(SlotTableHeader, RGB(0x7F, 0xD3, 0xFF)),
	// Component palette: 10 warm-to-cool hues legible on the dark NightOwl background.
	// Chosen to differ from level colours (purple/blue/amber/red) and muted steel.
	WithComponentPalette(
		RGB(0x80, 0xD4, 0xAA), // mint green
		RGB(0x7F, 0xD3, 0xFF), // sky cyan
		RGB(0xFF, 0xB3, 0x6B), // peach orange
		RGB(0xD6, 0xAC, 0xFF), // lavender
		RGB(0x6B, 0xD8, 0xD8), // teal
		RGB(0xFF, 0xD6, 0xA5), // warm sand
		RGB(0xA5, 0xF3, 0xC4), // seafoam
		RGB(0xF7, 0xA0, 0xC4), // rose pink
		RGB(0xB3, 0xDC, 0xFF), // ice blue
		RGB(0xD4, 0xFF, 0xA0), // chartreuse
	),
)

// ThemeSolarized is a classic Solarized 256-colour palette.
var ThemeSolarized = NewTheme(
	"Solarized",
	WithLevelColours(
		Colour256(61),  // debug
		Colour256(33),  // info
		Colour256(136), // warn
		Colour256(160), // error
		Colour256(124), // fatal
	),
	WithTimestampColour(Colour256(8)),
	WithMessageColour(Colour256(7)),
	WithFieldColours(Colour256(8), Colour256(7), Colour256(160)),
	WithStyleSlot(SlotGood, Colour256(64)),
	WithStyleSlot(SlotBad, Colour256(160)),
	WithStyleSlot(SlotWarn, Colour256(136)),
	WithStyleSlot(SlotInfo, Colour256(33)),
	WithStyleSlot(SlotMuted, Colour256(8)),
	WithStyleSlot(SlotStrong, Colour256(7)),
	WithStyleSlot(SlotHeading, Colour256(37)),
	WithStyleSlot(SlotEndpoint, Colour256(33)),
	WithStyleSlot(SlotHyperlink, Colour256(37)),
	WithStyleSlot(SlotContinuation, Colour256(8)),
	WithStyleSlot(SlotCount, Colour256(61)),
	WithStyleSlot(SlotSecure, Colour256(136)),
	WithStyleSlot(SlotStatusOK, Colour256(64)),
	WithStyleSlot(SlotStatusFail, Colour256(160)),
	WithStyleSlot(SlotStatusWarn, Colour256(136)),
	WithStyleSlot(SlotStatusInfo, Colour256(33)),
	WithStyleSlot(SlotTableHeader, Colour256(37)),
	// Component palette: mid-range 256-colour hues legible on the Solarized base03 background.
	// Avoids debug/61, info/33, warn/136, error/160, muted/8, heading/37.
	WithComponentPalette(
		Colour256(64),  // green (good)
		Colour256(37),  // cyan
		Colour256(167), // salmon
		Colour256(125), // magenta
		Colour256(68),  // slate blue
		Colour256(107), // olive
		Colour256(73),  // steel teal
		Colour256(172), // gold
		Colour256(110), // periwinkle
		Colour256(71),  // forest green
	),
)

// ThemeDracula is the Dracula 256-colour palette.
var ThemeDracula = NewTheme(
	"Dracula",
	WithLevelColours(
		Colour256(141), // debug: purple
		Colour256(81),  // info: cyan
		Colour256(228), // warn: yellow
		Colour256(212), // error: pink
		Colour256(196), // fatal: red
	),
	WithTimestampColour(Colour256(59)),
	WithMessageColour(Colour256(231)),
	WithFieldColours(Colour256(59), Colour256(188), Colour256(212)),
	WithStyleSlot(SlotGood, Colour256(84)),
	WithStyleSlot(SlotBad, Colour256(212)),
	WithStyleSlot(SlotWarn, Colour256(228)),
	WithStyleSlot(SlotInfo, Colour256(81)),
	WithStyleSlot(SlotMuted, Colour256(59)),
	WithStyleSlot(SlotStrong, Colour256(231)),
	WithStyleSlot(SlotHeading, Colour256(87)),
	WithStyleSlot(SlotEndpoint, Colour256(81)),
	WithStyleSlot(SlotHyperlink, Colour256(87)),
	WithStyleSlot(SlotContinuation, Colour256(59)),
	WithStyleSlot(SlotCount, Colour256(141)),
	WithStyleSlot(SlotSecure, Colour256(228)),
	WithStyleSlot(SlotStatusOK, Colour256(84)),
	WithStyleSlot(SlotStatusFail, Colour256(212)),
	WithStyleSlot(SlotStatusWarn, Colour256(228)),
	WithStyleSlot(SlotStatusInfo, Colour256(81)),
	WithStyleSlot(SlotTableHeader, Colour256(87)),
	// Component palette: vivid hues on Dracula's dark background.
	// Avoids debug/141, info/81, warn/228, error/212, muted/59, heading/87.
	WithComponentPalette(
		Colour256(84),  // bright green
		Colour256(215), // apricot
		Colour256(117), // powder blue
		Colour256(183), // lilac
		Colour256(121), // mint
		Colour256(222), // butter yellow
		Colour256(159), // pale cyan
		Colour256(210), // light coral
		Colour256(114), // sage
		Colour256(189), // pale mauve
	),
)

// ThemeNord is the Nord 256-colour palette, cool and arctic.
var ThemeNord = NewTheme(
	"Nord",
	WithLevelColours(
		Colour256(139), // debug
		Colour256(109), // info
		Colour256(180), // warn
		Colour256(191), // error
		Colour256(167), // fatal
	),
	WithTimestampColour(Colour256(59)),
	WithMessageColour(Colour256(216)),
	WithFieldColours(Colour256(59), Colour256(188), Colour256(191)),
	WithStyleSlot(SlotGood, Colour256(108)),
	WithStyleSlot(SlotBad, Colour256(167)),
	WithStyleSlot(SlotWarn, Colour256(180)),
	WithStyleSlot(SlotInfo, Colour256(109)),
	WithStyleSlot(SlotMuted, Colour256(59)),
	WithStyleSlot(SlotStrong, Colour256(216)),
	WithStyleSlot(SlotHeading, Colour256(110)),
	WithStyleSlot(SlotEndpoint, Colour256(109)),
	WithStyleSlot(SlotHyperlink, Colour256(110)),
	WithStyleSlot(SlotContinuation, Colour256(59)),
	WithStyleSlot(SlotCount, Colour256(139)),
	WithStyleSlot(SlotSecure, Colour256(180)),
	WithStyleSlot(SlotStatusOK, Colour256(108)),
	WithStyleSlot(SlotStatusFail, Colour256(167)),
	WithStyleSlot(SlotStatusWarn, Colour256(180)),
	WithStyleSlot(SlotStatusInfo, Colour256(109)),
	WithStyleSlot(SlotTableHeader, Colour256(110)),
	// Component palette: cool arctic tones legible on Nord's dark polar-night background.
	// Avoids debug/139, info/109, warn/180, error/191, muted/59, heading/110.
	WithComponentPalette(
		Colour256(152), // frost light blue
		Colour256(114), // aurora green
		Colour256(216), // aurora orange (same as message but distinct from level colours)
		Colour256(183), // soft violet
		Colour256(159), // pale aqua
		Colour256(222), // aurora yellow
		Colour256(147), // periwinkle
		Colour256(157), // light mint
		Colour256(210), // salmon rose
		Colour256(153), // sky blue
	),
)

// ThemeMono is a colour-free theme. Format always returns the input unchanged.
// Use it when piping output to files or other tools that don't interpret ANSI.
var ThemeMono = NewTheme("Mono")

// noColourTheme is the fallback for Logger.Style() when colour is disabled or there
// is no console writer. Equivalent to ThemeMono: Format returns the input unchanged.
var noColourTheme = NewTheme("none")

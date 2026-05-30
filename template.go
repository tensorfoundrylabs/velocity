package velocity

import (
	"bytes"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// referenceTime is a fixed timestamp used to measure formatted time width at construction.
// We use a non-UTC zone offset so RFC3339 output includes "+HH:MM" (the longest form),
// giving us the worst-case byte length for cache computation.
var referenceTime = time.Date(2006, 1, 2, 15, 4, 5, 0, time.FixedZone("TST", 10*60*60))

type TemplateType int

const (
	TemplateTypeDefault TemplateType = iota
	TemplateTypeSimple
	TemplateTypeMinimal
	TemplateTypeJSON
)

type Template struct {
	timeFormat   string
	fieldSep     string
	fieldPairSep string
	// cachedPrefixWidth and cachedIndentStr are computed at construction for tree mode.
	// Tree mode positions ├/└ glyphs slightly past the message column, so the cached
	// width is intentionally wider than the actual message column by one space — this
	// matches the existing visual where field labels nest under the message text.
	cachedIndentStr string
	// cachedMessageIndentStr aligns flush under the message column (timestamp + 1 +
	// level + 1). Used by Logger.Render so block output (tables, banners, boxes)
	// lands at the same column as log message text.
	cachedMessageIndentStr string
	// indicators carries the opt-in compact header config. Zero value means all
	// indicators are disabled and the render path is unchanged from baseline.
	indicators          inlineIndicators
	levelStyle          LevelStyle
	fieldDisplayMode    FieldDisplayMode
	cachedPrefixWidth   int
	cachedMessageColumn int
	showTime            bool
	showLevel           bool
	showMessage         bool
	showFields          bool
	useColours          bool
}

type LevelStyle int

const (
	LevelStyleText LevelStyle = iota
	LevelStyleIcon
	LevelStyleBadge
)

var TemplateDefault = initTemplate(&Template{
	showTime:         true,
	timeFormat:       time.RFC3339,
	showLevel:        true,
	levelStyle:       LevelStyleBadge,
	showMessage:      true,
	showFields:       true,
	fieldSep:         " ",
	fieldPairSep:     ": ",
	fieldDisplayMode: FieldDisplayInline,
	useColours:       true,
})

var TemplateSimple = initTemplate(&Template{
	showTime:         false,
	showLevel:        true,
	levelStyle:       LevelStyleText,
	showMessage:      true,
	showFields:       true,
	fieldSep:         " ",
	fieldPairSep:     ": ",
	fieldDisplayMode: FieldDisplayInline,
	useColours:       true,
})

var TemplateMinimal = initTemplate(&Template{
	showTime:         false,
	showLevel:        false,
	showMessage:      true,
	showFields:       false,
	fieldDisplayMode: FieldDisplayInline,
	useColours:       false,
})

var TemplateJSON = initTemplate(&Template{
	showTime:    true,
	timeFormat:  time.RFC3339Nano,
	showLevel:   true,
	levelStyle:  LevelStyleText,
	showMessage: true,
	showFields:  true,
	useColours:  false,
})

// initTemplate calls initCache and returns t, for use in package-level var initialisers.
func initTemplate(t *Template) *Template {
	t.initCache()
	return t
}

// indicatorScanResult holds the output of the single pre-scan pass over entry.Fields.
// All storage is on the caller's stack — no heap allocations.
// timingIdx and stateIdx are field indices into entry.Fields; -1 means absent.
// skip is a bitmask of field indices to omit from the tree/inline renderer.
type indicatorScanResult struct {
	// timingIdx holds indices of matching timingFields, in field order. Capped at 8.
	timingIdx [8]int
	timingN   int // number of valid entries in timingIdx

	stateFromIdx int // index of the "from" field of the winning state pair, or -1
	stateToIdx   int // index of the "to"   field of the winning state pair, or -1
	componentIdx int // index of the component field, or -1
	countIdx     int // index of the first matching count field, or -1

	// skip bitmask: bit i set means entry.Fields[i] must not appear in the tree.
	// Only used when len(entry.Fields) <= 64; >64 fields fall back to rendering all.
	skip uint64
}

// isActive reports whether any indicator is enabled for this template.
// When false, buildWithTimezoneSecure takes the baseline path unchanged.
func (t *Template) isActive() bool {
	ind := &t.indicators
	return ind.component || len(ind.countFields) > 0 || len(ind.timingFields) > 0 || len(ind.statePairs) > 0
}

// resolveGlyphs returns the effective glyph setting for this render call.
// When the caller set WithInlineGlyphs explicitly that wins; otherwise the
// runtime VELOCITY_GLYPHS detection applies.
func (t *Template) resolveGlyphs() bool {
	if t.indicators.glyphsExplicit {
		return t.indicators.showGlyphs
	}
	return GlyphsSupported()
}

// isSecureOnUntrusted reports whether field f would render as redacted on an
// untrusted writer. Such fields must not be promoted to the header.
func isSecureOnUntrusted(f Field, trusted bool) bool {
	if trusted {
		return false
	}
	switch f.Type {
	case FieldTypeSecure, FieldTypeSecureURL, FieldTypeRedacted:
		return true
	default:
		return false
	}
}

// pairSides tracks the field indices for a single state-transition pair.
type pairSides struct{ fromIdx, toIdx int }

// scanIndicators performs a single pass over entry.Fields to locate all
// promoted-field indices. It is allocation-free: all state lives on the stack.
// Returns the scan result and whether any indicator was found.
func (t *Template) scanIndicators(entry *Entry, trusted bool) (r indicatorScanResult, anyFound bool) {
	r.componentIdx = -1
	r.countIdx = -1
	r.stateFromIdx = -1
	r.stateToIdx = -1

	ind := &t.indicators
	activePairs := min(len(ind.statePairs), 8)

	// Track per-pair state on the stack; -1 means not yet seen.
	var pairState [8]pairSides
	for i := range activePairs {
		pairState[i] = pairSides{fromIdx: -1, toIdx: -1}
	}

	for i, f := range entry.Fields {
		matchComponentField(ind, f, i, trusted, &r, &anyFound)
		matchCountField(ind, f, i, &r, &anyFound)
		matchTimingField(ind, f, i, &r, &anyFound)
		matchStatePairs(ind, f, i, activePairs, pairState[:])
	}

	// Resolve the winning state pair: first complete pair wins.
	for p := range activePairs {
		if pairState[p].fromIdx >= 0 && pairState[p].toIdx >= 0 {
			r.stateFromIdx = pairState[p].fromIdx
			r.stateToIdx = pairState[p].toIdx
			anyFound = true
			break
		}
	}

	buildSkipMask(ind, entry, &r)
	return r, anyFound
}

func matchComponentField(ind *inlineIndicators, f Field, i int, trusted bool, r *indicatorScanResult, anyFound *bool) {
	if !ind.component || r.componentIdx >= 0 || f.Key != ind.componentField {
		return
	}
	if f.Type == FieldTypeString && !isSecureOnUntrusted(f, trusted) {
		r.componentIdx = i
		*anyFound = true
	}
}

func matchCountField(ind *inlineIndicators, f Field, i int, r *indicatorScanResult, anyFound *bool) {
	if r.countIdx >= 0 || len(ind.countFields) == 0 {
		return
	}
	for _, name := range ind.countFields {
		if f.Key == name && (f.Type == FieldTypeInt || f.Type == FieldTypeInt64) {
			r.countIdx = i
			*anyFound = true
			return
		}
	}
}

func matchTimingField(ind *inlineIndicators, f Field, i int, r *indicatorScanResult, anyFound *bool) {
	if r.timingN >= 8 || len(ind.timingFields) == 0 {
		return
	}
	for _, name := range ind.timingFields {
		if f.Key == name && (f.Type == FieldTypeInt || f.Type == FieldTypeInt64 || f.Type == FieldTypeDuration) {
			r.timingIdx[r.timingN] = i
			r.timingN++
			*anyFound = true
			return
		}
	}
}

func matchStatePairs(ind *inlineIndicators, f Field, i, activePairs int, pairState []pairSides) {
	for p := range activePairs {
		pair := ind.statePairs[p]
		switch f.Key {
		case pair[0]:
			pairState[p].fromIdx = i
		case pair[1]:
			pairState[p].toIdx = i
		}
	}
}

// buildSkipMask populates r.skip from the found indices.
// Only built when removeFromTree is enabled and field count fits in uint64.
func buildSkipMask(ind *inlineIndicators, entry *Entry, r *indicatorScanResult) {
	if !ind.removeFromTree || len(entry.Fields) > 64 {
		return
	}
	setSkip := func(idx int) {
		if idx >= 0 {
			r.skip |= 1 << uint(idx) //nolint:gosec // G115: idx is [0,63]; uint conversion is safe
		}
	}
	setSkip(r.componentIdx)
	setSkip(r.countIdx)
	for j := range r.timingN {
		setSkip(r.timingIdx[j])
	}
	setSkip(r.stateFromIdx)
	setSkip(r.stateToIdx)
}

// buildWithTimezoneSecure is the trust-aware template rendering path.
func (t *Template) buildWithTimezoneSecure(buf *bytes.Buffer, entry *Entry, theme *Theme, displayTimezone *time.Location, trusted bool, redactionMark string) {
	// Fast path: no indicators configured at all — take the pre-existing code
	// path completely unchanged. This is the zero-regression guarantee: no extra
	// work, no pre-scan, output is bit-for-bit identical to baseline.
	if !t.isActive() {
		t.buildBaselineSecure(buf, entry, theme, displayTimezone, trusted, redactionMark)
		return
	}

	// Pre-scan once. If nothing matched, fall back to baseline so a mismatched
	// entry (no component/count/timing keys) produces identical output.
	scan, anyFound := t.scanIndicators(entry, trusted)
	if !anyFound {
		t.buildBaselineSecure(buf, entry, theme, displayTimezone, trusted, redactionMark)
		return
	}

	// --- Indicators are active and at least one field was found. ---
	glyphs := t.resolveGlyphs()

	if t.showTime && !entry.Time.IsZero() {
		t.writeTimestampWithTimezone(buf, entry, theme, displayTimezone)
	}

	if t.showLevel {
		t.writeLevel(buf, entry, theme)
	}

	// Phase 2: component prefix " <name padded> │ ".
	if scan.componentIdx >= 0 {
		t.writeComponentPrefix(buf, entry.Fields[scan.componentIdx], theme)
	}

	if t.showMessage && entry.Message != "" {
		if buf.Len() > 0 {
			_ = buf.WriteByte(' ')
		}
		t.writeMessageSecure(buf, entry, theme, trusted, redactionMark)
	}

	// Phase 3: count suffix " (N)".
	if scan.countIdx >= 0 {
		t.writeCountSuffix(buf, entry.Fields[scan.countIdx], theme)
	}

	// Phase 5: state-transition arrow " from → to".
	if scan.stateFromIdx >= 0 {
		t.writeStateArrow(buf, entry.Fields[scan.stateFromIdx], entry.Fields[scan.stateToIdx], theme, trusted, redactionMark, glyphs)
	}

	// Phase 4: timing suffix " [⏱ ...]".
	if scan.timingN > 0 {
		t.writeTimingSuffix(buf, entry, scan, theme, glyphs)
	}

	if entry.Caller != "" {
		_ = buf.WriteByte(' ')
		if t.useColours && theme != nil {
			buf.WriteString(theme.cachedTimestampFgStr())
		}
		buf.WriteString(entry.Caller)
		_ = buf.WriteByte(':')
		var lineBuf [10]byte
		buf.Write(strconv.AppendInt(lineBuf[:0], int64(entry.Line), 10))
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}
	}

	// Phase 6: fields with skip mask.
	remainingFields := countRemainingFields(entry.Fields, scan.skip)
	if t.showFields && remainingFields > 0 {
		if buf.Len() > 0 {
			_ = buf.WriteByte(' ')
		}
		t.writeFieldsSecureWithSkip(buf, entry, theme, trusted, redactionMark, scan.skip)
	}

	if buf.Len() == 0 || buf.Bytes()[buf.Len()-1] != '\n' {
		_ = buf.WriteByte('\n')
	}
}

// buildBaselineSecure is the original build path extracted verbatim, so the
// indicator-enabled path can call it when nothing matches without any divergence.
func (t *Template) buildBaselineSecure(buf *bytes.Buffer, entry *Entry, theme *Theme, displayTimezone *time.Location, trusted bool, redactionMark string) {
	if t.showTime && !entry.Time.IsZero() {
		t.writeTimestampWithTimezone(buf, entry, theme, displayTimezone)
	}

	if t.showLevel {
		t.writeLevel(buf, entry, theme)
	}

	if t.showMessage && entry.Message != "" {
		if buf.Len() > 0 {
			_ = buf.WriteByte(' ')
		}
		t.writeMessageSecure(buf, entry, theme, trusted, redactionMark)
	}

	if entry.Caller != "" {
		_ = buf.WriteByte(' ')
		if t.useColours && theme != nil {
			buf.WriteString(theme.cachedTimestampFgStr())
		}
		buf.WriteString(entry.Caller)
		_ = buf.WriteByte(':')
		var lineBuf [10]byte
		buf.Write(strconv.AppendInt(lineBuf[:0], int64(entry.Line), 10))
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}
	}

	if t.showFields && len(entry.Fields) > 0 {
		if buf.Len() > 0 {
			_ = buf.WriteByte(' ')
		}
		t.writeFieldsSecure(buf, entry, theme, trusted, redactionMark)
	}

	if buf.Len() == 0 || buf.Bytes()[buf.Len()-1] != '\n' {
		_ = buf.WriteByte('\n')
	}
}

// writeComponentPrefix renders " <name> │ " after the level badge.
// Name is left-aligned and padded/truncated to componentWidth runes.
// Name uses the theme's component colour; │ uses SlotMuted.
func (t *Template) writeComponentPrefix(buf *bytes.Buffer, f Field, theme *Theme) {
	_ = buf.WriteByte(' ')

	name := *(*string)(f.value)
	// width <= 0 is compact: the name keeps its natural length (bars still align
	// when names share a length). A positive width pads/truncates to a fixed column.
	width := t.indicators.componentWidth

	// Apply component colour.
	var colCode string
	if t.useColours && theme != nil {
		colCode = theme.componentColourCode(name)
		if colCode != "" {
			buf.WriteString(colCode)
		}
	}

	// Write name padded/truncated to width runes.
	writeRunePadded(buf, name, width)

	if colCode != "" {
		buf.WriteString(Reset)
	}

	// Separator │ in SlotMuted.
	_ = buf.WriteByte(' ')
	if t.useColours && theme != nil {
		muted := theme.cachedSlotFg[SlotMuted]
		if muted != "" {
			buf.WriteString(muted)
		}
	}
	buf.WriteString("│")
	if t.useColours && theme != nil && theme.cachedSlotFg[SlotMuted] != "" {
		buf.WriteString(Reset)
	}
}

// writeRunePadded writes s to buf, left-aligned, exactly width runes wide.
// Truncates with '…' if s is longer; pads with spaces if shorter.
func writeRunePadded(buf *bytes.Buffer, s string, width int) {
	if width <= 0 {
		// Compact: natural width, no column padding or truncation.
		buf.WriteString(s)
		return
	}
	n := utf8.RuneCountInString(s)
	if n > width {
		// Truncate: write width-1 runes then '…'.
		count := 0
		for _, r := range s {
			if count == width-1 {
				break
			}
			writeRune(buf, r)
			count++
		}
		buf.WriteString("…")
		return
	}
	buf.WriteString(s)
	for i := n; i < width; i++ {
		_ = buf.WriteByte(' ')
	}
}

// writeRune writes a single rune into a bytes.Buffer.
func writeRune(buf *bytes.Buffer, r rune) {
	var tmp [utf8.UTFMax]byte
	n := utf8.EncodeRune(tmp[:], r)
	buf.Write(tmp[:n])
}

// writeCountSuffix appends " (N)" after the message using SlotCount colour.
func (t *Template) writeCountSuffix(buf *bytes.Buffer, f Field, theme *Theme) {
	_ = buf.WriteByte(' ')
	if t.useColours && theme != nil {
		code := theme.cachedSlotFg[SlotCount]
		if code != "" {
			buf.WriteString(code)
		}
	}
	_ = buf.WriteByte('(')
	var tmp [20]byte
	n := formatInt(tmp[:], f.num)
	buf.Write(tmp[:n])
	_ = buf.WriteByte(')')
	if t.useColours && theme != nil && theme.cachedSlotFg[SlotCount] != "" {
		buf.WriteString(Reset)
	}
}

// writeStateArrow renders " from → to" (or " from -> to" when glyphs off).
// Both field values are read as strings; other types are left in the tree.
func (t *Template) writeStateArrow(buf *bytes.Buffer, fromField, toField Field, theme *Theme, trusted bool, redactionMark string, glyphs bool) {
	_ = buf.WriteByte(' ')

	var muted string
	if t.useColours && theme != nil {
		muted = theme.cachedSlotFg[SlotMuted]
	}
	if muted != "" {
		buf.WriteString(muted)
	}

	writeFieldValueString(buf, fromField, trusted, redactionMark)
	if glyphs {
		buf.WriteString(" → ")
	} else {
		buf.WriteString(" -> ")
	}
	writeFieldValueString(buf, toField, trusted, redactionMark)

	if muted != "" {
		buf.WriteString(Reset)
	}
}

// writeFieldValueString writes the string representation of a field's value.
// Used for state-arrow rendering where we want the bare value (no key, no quotes).
func writeFieldValueString(buf *bytes.Buffer, f Field, trusted bool, redactionMark string) {
	switch f.Type {
	case FieldTypeString:
		buf.WriteString(*(*string)(f.value))
	case FieldTypeSecure, FieldTypeSecureURL:
		switch {
		case trusted && f.value != nil:
			buf.WriteString((*secureValue)(f.value).plain)
		case f.value != nil:
			buf.WriteString((*secureValue)(f.value).redacted)
		default:
			buf.WriteString(redactionMark)
		}
	case FieldTypeRedacted:
		buf.WriteString(redactionMark)
	case FieldTypeInt, FieldTypeInt64:
		var tmp [20]byte
		n := formatInt(tmp[:], f.num)
		buf.Write(tmp[:n])
	default:
		// For non-string types fall back to safe formatted output.
		f.writeFormattedWithMark(buf, redactionMark)
	}
}

// writeTimingSuffix renders " ⏱ t1, t2" with the stopwatch glyph, or the bracketed
// ASCII fallback " [t1, t2]" when glyphs are off. With the glyph present, the glyph
// and the muted colour set the timing apart so the brackets are redundant; without it
// the brackets keep the value from reading as part of the message.
// Integer fields are treated as milliseconds; Duration fields use smart formatting.
func (t *Template) writeTimingSuffix(buf *bytes.Buffer, entry *Entry, scan indicatorScanResult, theme *Theme, glyphs bool) {
	_ = buf.WriteByte(' ')

	var muted string
	if t.useColours && theme != nil {
		muted = theme.cachedSlotFg[SlotMuted]
	}
	if muted != "" {
		buf.WriteString(muted)
	}

	if glyphs {
		buf.WriteString("⏱ ")
	} else {
		_ = buf.WriteByte('[')
	}

	for i := range scan.timingN {
		if i > 0 {
			buf.WriteString(", ")
		}
		f := entry.Fields[scan.timingIdx[i]]
		writeSmartDuration(buf, f)
	}

	if !glyphs {
		_ = buf.WriteByte(']')
	}

	if muted != "" {
		buf.WriteString(Reset)
	}
}

// writeSmartDuration renders a timing field value in a compact human form.
// Integer/Int64 fields are treated as milliseconds (renders as "294ms", "2.78s").
// Duration fields (field.num as time.Duration) use the same compact logic.
// This avoids time.Duration.String() and fmt.Sprintf entirely.
func writeSmartDuration(buf *bytes.Buffer, f Field) {
	var ns int64
	switch f.Type {
	case FieldTypeDuration:
		ns = f.num // already nanoseconds
	case FieldTypeInt, FieldTypeInt64:
		// Treat as milliseconds.
		ns = f.num * int64(time.Millisecond)
	default:
		// Fallback: write the raw int.
		var tmp [20]byte
		n := formatInt(tmp[:], f.num)
		buf.Write(tmp[:n])
		return
	}

	if ns < 0 {
		_ = buf.WriteByte('-')
		ns = -ns
	}

	ms := ns / int64(time.Millisecond)
	us := ns / int64(time.Microsecond)
	sec := ns / int64(time.Second)

	switch {
	case ns < int64(time.Millisecond):
		// Sub-millisecond: render in microseconds.
		var tmp [20]byte
		n := formatInt(tmp[:], us)
		buf.Write(tmp[:n])
		buf.WriteString("µs")

	case ns < int64(time.Second):
		// Millisecond range: render as "NNNms".
		var tmp [20]byte
		n := formatInt(tmp[:], ms)
		buf.Write(tmp[:n])
		buf.WriteString("ms")

	default:
		// Second range: render as "X.XXs" (2 decimal places).
		remainMs := (ns - sec*int64(time.Second)) / int64(time.Millisecond)
		var tmp [20]byte
		n := formatInt(tmp[:], sec)
		buf.Write(tmp[:n])
		_ = buf.WriteByte('.')
		// Two decimal digits from millisecond remainder (0-999 → 00-99 after /10).
		centis := remainMs / 10
		if centis < 10 {
			_ = buf.WriteByte('0')
		}
		m := formatInt(tmp[:], centis)
		buf.Write(tmp[:m])
		_ = buf.WriteByte('s')
	}
}

// countRemainingFields returns the number of fields not masked by skip.
// When there are more than 64 fields the skip mask is not used and all fields count.
func countRemainingFields(fields []Field, skip uint64) int {
	if len(fields) > 64 {
		return len(fields)
	}
	n := 0
	for i := range fields {
		if skip&(1<<uint(i)) == 0 {
			n++
		}
	}
	return n
}

func (t *Template) writeTimestampWithTimezone(buf *bytes.Buffer, entry *Entry, theme *Theme, displayTimezone *time.Location) {
	if t.useColours && theme != nil {
		buf.WriteString(theme.cachedTimestampFgStr())
	}

	displayTime := entry.Time.In(displayTimezone)
	buf.Write(displayTime.AppendFormat(buf.AvailableBuffer(), t.timeFormat))

	if t.useColours && theme != nil {
		buf.WriteString(Reset)
	}
}

func (t *Template) writeLevel(buf *bytes.Buffer, entry *Entry, theme *Theme) {
	if buf.Len() > 0 {
		_ = buf.WriteByte(' ')
	}

	var levelCode string
	if t.useColours && theme != nil {
		levelCode = theme.cachedLevelCode(entry.Level)
	}

	switch t.levelStyle {
	case LevelStyleIcon:
		if levelCode != "" {
			buf.WriteString(levelCode)
		}
		buf.WriteString(entry.Level.Icon())
		if levelCode != "" {
			buf.WriteString(Reset)
		}

	case LevelStyleBadge:
		if levelCode != "" {
			buf.WriteString(levelCode)
		}
		_ = buf.WriteByte('[')
		buf.WriteString(entry.Level.ConciseLabel())
		_ = buf.WriteByte(']')
		if levelCode != "" {
			buf.WriteString(Reset)
		}

	case LevelStyleText:
		if levelCode != "" {
			buf.WriteString(levelCode)
		}
		buf.WriteString(entry.Level.String())
		if levelCode != "" {
			buf.WriteString(Reset)
		}
	}
}

func (t *Template) writeMessageSecure(buf *bytes.Buffer, entry *Entry, theme *Theme, trusted bool, redactionMark string) {
	if t.useColours && theme != nil {
		buf.WriteString(theme.cachedMessageFgStr())
	}

	msg := entry.Message
	if entry.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}
	buf.WriteString(msg)

	if t.useColours && theme != nil {
		buf.WriteString(Reset)
	}
}

func (t *Template) writeFieldsSecure(buf *bytes.Buffer, entry *Entry, theme *Theme, trusted bool, redactionMark string) {
	// Check if tree display is forced on the entry, otherwise use template's mode
	if entry.forceTreeDisplay || t.fieldDisplayMode == FieldDisplayTree {
		t.writeFieldsTreeSecure(buf, entry, theme, trusted, redactionMark)
	} else {
		t.writeFieldsInlineSecure(buf, entry, theme, trusted, redactionMark)
	}
}

// writeFieldsSecureWithSkip renders fields, skipping those whose index bits are
// set in skip. Called from the indicators path only.
func (t *Template) writeFieldsSecureWithSkip(buf *bytes.Buffer, entry *Entry, theme *Theme, trusted bool, redactionMark string, skip uint64) {
	if entry.forceTreeDisplay || t.fieldDisplayMode == FieldDisplayTree {
		t.writeFieldsTreeSecureWithSkip(buf, entry, theme, trusted, redactionMark, skip)
	} else {
		t.writeFieldsInlineSecureWithSkip(buf, entry, theme, trusted, redactionMark, skip)
	}
}

func (t *Template) writeFieldsInlineSecure(buf *bytes.Buffer, entry *Entry, theme *Theme, trusted bool, redactionMark string) {
	for i, field := range entry.Fields {
		if i > 0 {
			buf.WriteString(t.fieldSep)
		}

		if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldKeyFgStr())
		}
		buf.WriteString(field.Key)
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}

		buf.WriteString(t.fieldPairSep)

		if t.useColours && field.Type == FieldTypeError && theme != nil {
			buf.WriteString(theme.cachedErrorValFgStr())
		} else if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldValFgStr())
		}

		if trusted {
			field.writeFormattedTrusted(buf)
		} else {
			field.writeFormattedWithMark(buf, redactionMark)
		}

		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}
	}
}

func (t *Template) writeFieldsInlineSecureWithSkip(buf *bytes.Buffer, entry *Entry, theme *Theme, trusted bool, redactionMark string, skip uint64) {
	first := true
	for i, field := range entry.Fields {
		if len(entry.Fields) <= 64 && skip&(1<<uint(i)) != 0 {
			continue
		}
		if !first {
			buf.WriteString(t.fieldSep)
		}
		first = false

		if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldKeyFgStr())
		}
		buf.WriteString(field.Key)
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}

		buf.WriteString(t.fieldPairSep)

		if t.useColours && field.Type == FieldTypeError && theme != nil {
			buf.WriteString(theme.cachedErrorValFgStr())
		} else if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldValFgStr())
		}

		if trusted {
			field.writeFormattedTrusted(buf)
		} else {
			field.writeFormattedWithMark(buf, redactionMark)
		}

		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}
	}
}

func (t *Template) writeFieldsTreeSecure(buf *bytes.Buffer, entry *Entry, theme *Theme, trusted bool, redactionMark string) {
	// Badge style has a fixed prefix width regardless of level, so we use the
	// pre-built indent string. Other styles vary by level and must compute per call.
	var indentStr string
	if t.levelStyle == LevelStyleBadge {
		indentStr = t.cachedIndentStr
	} else {
		indentStr = strings.Repeat(" ", t.calculatePrefixWidth(entry))
	}

	for i, field := range entry.Fields {
		_ = buf.WriteByte('\n')
		buf.WriteString(indentStr)

		var treeChar string
		if i == len(entry.Fields)-1 {
			treeChar = "└ "
		} else {
			treeChar = "├ "
		}

		buf.WriteString(treeChar)

		if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldKeyFgStr())
		}
		buf.WriteString(field.Key)
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}

		buf.WriteString(t.fieldPairSep)

		if t.useColours && field.Type == FieldTypeError && theme != nil {
			buf.WriteString(theme.cachedErrorValFgStr())
		} else if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldValFgStr())
		}

		if trusted {
			field.writeFormattedTrusted(buf)
		} else {
			field.writeFormattedWithMark(buf, redactionMark)
		}

		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}
	}
}

func (t *Template) writeFieldsTreeSecureWithSkip(buf *bytes.Buffer, entry *Entry, theme *Theme, trusted bool, redactionMark string, skip uint64) {
	var indentStr string
	if t.levelStyle == LevelStyleBadge {
		indentStr = t.cachedIndentStr
	} else {
		indentStr = strings.Repeat(" ", t.calculatePrefixWidth(entry))
	}

	// Determine the last non-skipped index for the └ glyph.
	lastIdx := -1
	for i := range entry.Fields {
		if len(entry.Fields) <= 64 && skip&(1<<uint(i)) != 0 {
			continue
		}
		lastIdx = i
	}

	for i, field := range entry.Fields {
		if len(entry.Fields) <= 64 && skip&(1<<uint(i)) != 0 {
			continue
		}

		_ = buf.WriteByte('\n')
		buf.WriteString(indentStr)

		var treeChar string
		if i == lastIdx {
			treeChar = "└ "
		} else {
			treeChar = "├ "
		}

		buf.WriteString(treeChar)

		if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldKeyFgStr())
		}
		buf.WriteString(field.Key)
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}

		buf.WriteString(t.fieldPairSep)

		if t.useColours && field.Type == FieldTypeError && theme != nil {
			buf.WriteString(theme.cachedErrorValFgStr())
		} else if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldValFgStr())
		}

		if trusted {
			field.writeFormattedTrusted(buf)
		} else {
			field.writeFormattedWithMark(buf, redactionMark)
		}

		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}
	}
}

// CalculatePrefixWidth determines indentation needed for tree alignment.
// Exported version for use by the Logger.
func (t *Template) CalculatePrefixWidth(entry *Entry) int {
	return t.calculatePrefixWidth(entry)
}

// CachedPrefixWidth returns the pre-computed worst-case prefix width for tree indentation.
// Use this in preference to CalculatePrefixWidth on hot paths.
func (t *Template) CachedPrefixWidth() int { return t.cachedPrefixWidth }

// CachedIndentStr returns the pre-built indent string for tree alignment.
// Avoids strings.Repeat on callers that need the string form directly.
func (t *Template) CachedIndentStr() string { return t.cachedIndentStr }

// calculatePrefixWidth determines indentation needed for tree alignment.
// For badge style the width is constant; for text/icon styles it uses the
// actual entry level so per-call calculation is unavoidable for variable widths.
//
// We use referenceTime (a non-UTC fixed zone) to measure timestamp width rather
// than entry.Time, keeping this consistent with computePrefixWidth. Without this,
// UTC-zoned machines produce a 5-byte shorter RFC3339 string ("Z" vs "+HH:MM"),
// causing the cached and per-call widths to diverge.
func (t *Template) calculatePrefixWidth(entry *Entry) int {
	width := 0

	if t.showTime && !entry.Time.IsZero() {
		width += len(referenceTime.Format(t.timeFormat))
		width++
	}

	if t.showLevel {
		switch t.levelStyle {
		case LevelStyleIcon:
			width += len(entry.Level.Icon())
			width++
		case LevelStyleBadge:
			width += 7
			width++
		case LevelStyleText:
			width += len(entry.Level.String())
			width++
		}
	}

	return width
}

// computePrefixWidth calculates the worst-case prefix width using the reference time
// and the widest level label so we can cache a stable value at construction.
func (t *Template) computePrefixWidth() int {
	width := 0

	if t.showTime {
		width += len(referenceTime.Format(t.timeFormat))
		width++ // separator space
	}

	if t.showLevel {
		switch t.levelStyle {
		case LevelStyleIcon:
			// Find the widest icon by byte length (matches calculatePrefixWidth behaviour).
			maxIcon := 0
			for _, lvl := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal} {
				if n := len(lvl.Icon()); n > maxIcon {
					maxIcon = n
				}
			}
			width += maxIcon
			width++
		case LevelStyleBadge:
			// Badge is always "[XXXX]" = 6 chars, but calculatePrefixWidth uses 7.
			// Keep parity with the existing per-call value.
			width += 7
			width++
		case LevelStyleText:
			// "DEBUG"/"ERROR"/"FATAL" are widest at 5 chars.
			width += 5
			width++
		}
	}

	return width
}

// initCache pre-computes cached values that are constant for the lifetime of this Template.
// Call after all fields are set.
func (t *Template) initCache() {
	t.cachedPrefixWidth = t.computePrefixWidth()
	t.cachedIndentStr = strings.Repeat(" ", t.cachedPrefixWidth)
	t.cachedMessageColumn = t.computeMessageColumn()
	t.cachedMessageIndentStr = strings.Repeat(" ", t.cachedMessageColumn)
}

// computeMessageColumn returns the visible column where the message text begins:
// timestamp + space + level + space. Unlike computePrefixWidth this uses the actual
// rendered widths (badge is 6 chars, not 7), so block output via Logger.Render lands
// flush under the message text rather than at the tree-glyph offset.
func (t *Template) computeMessageColumn() int {
	width := 0

	if t.showTime {
		width += len(referenceTime.Format(t.timeFormat))
		width++
	}

	if t.showLevel {
		switch t.levelStyle {
		case LevelStyleIcon:
			maxIcon := 0
			for _, lvl := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal} {
				if n := len(lvl.Icon()); n > maxIcon {
					maxIcon = n
				}
			}
			width += maxIcon
			width++
		case LevelStyleBadge:
			width += 6 // [XXXX] is exactly 6 chars
			width++
		case LevelStyleText:
			width += 5 // widest level label
			width++
		}
	}

	return width
}

// CachedMessageIndentStr returns the indent string that aligns flush under the
// message column. Used by Logger.Render for block output like tables and banners.
func (t *Template) CachedMessageIndentStr() string { return t.cachedMessageIndentStr }

type TemplateBuilder struct {
	template *Template
}

func NewTemplateBuilder() *TemplateBuilder {
	return &TemplateBuilder{
		template: &Template{
			showTime:         true,
			timeFormat:       time.RFC3339,
			showLevel:        true,
			levelStyle:       LevelStyleBadge,
			showMessage:      true,
			showFields:       true,
			fieldSep:         " ",
			fieldPairSep:     ": ",
			fieldDisplayMode: FieldDisplayInline,
			useColours:       true,
		},
	}
}

func (b *TemplateBuilder) WithTime(enabled bool) *TemplateBuilder {
	b.template.showTime = enabled
	return b
}

func (b *TemplateBuilder) WithTimeFormat(format string) *TemplateBuilder {
	b.template.timeFormat = format
	return b
}

func (b *TemplateBuilder) WithLevel(enabled bool) *TemplateBuilder {
	b.template.showLevel = enabled
	return b
}

func (b *TemplateBuilder) WithLevelStyle(style LevelStyle) *TemplateBuilder {
	b.template.levelStyle = style
	return b
}

func (b *TemplateBuilder) WithMessage(enabled bool) *TemplateBuilder {
	b.template.showMessage = enabled
	return b
}

func (b *TemplateBuilder) WithFields(enabled bool) *TemplateBuilder {
	b.template.showFields = enabled
	return b
}

func (b *TemplateBuilder) WithFieldDisplayMode(mode FieldDisplayMode) *TemplateBuilder {
	b.template.fieldDisplayMode = mode
	return b
}

func (b *TemplateBuilder) WithColours(enabled bool) *TemplateBuilder {
	b.template.useColours = enabled
	return b
}

func (b *TemplateBuilder) Build() *Template {
	b.template.initCache()
	return b.template
}

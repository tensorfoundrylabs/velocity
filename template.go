package velocity

import (
	"bytes"
	"strconv"
	"strings"
	"time"
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
	levelStyle             LevelStyle
	fieldDisplayMode       FieldDisplayMode
	cachedPrefixWidth      int
	cachedMessageColumn    int
	showTime               bool
	showLevel              bool
	showMessage            bool
	showFields             bool
	useColours             bool
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

// buildWithTimezone converts UTC timestamps to the display timezone before rendering.
// Delegates to buildWithTimezoneSecure with TTY-trusted defaults (called from
// ConsoleWriter.WriteSecure which handles trust itself via formatEntrySecure for
// the non-template path; the template path always calls this form from ConsoleWriter).
func (t *Template) buildWithTimezone(buf *bytes.Buffer, entry *Entry, theme *Theme, displayTimezone *time.Location) {
	// Template path: trust is handled by the caller (ConsoleWriter.WriteSecure
	// which passes trusted=isTTY). The template itself doesn't know trust state;
	// it renders plaintext for Secure fields and strips <secure> tags always.
	// This preserves backward compatibility for callers that build templates directly.
	t.buildWithTimezoneSecure(buf, entry, theme, displayTimezone, true, "[REDACTED]")
}

// buildWithTimezoneSecure is the trust-aware template rendering path.
func (t *Template) buildWithTimezoneSecure(buf *bytes.Buffer, entry *Entry, theme *Theme, displayTimezone *time.Location, trusted bool, redactionMark string) {
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

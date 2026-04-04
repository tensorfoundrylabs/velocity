package velocity

import (
	"bytes"
	"strconv"
	"strings"
	"time"
)

type TemplateType int

const (
	TemplateTypeDefault TemplateType = iota
	TemplateTypeSimple
	TemplateTypeMinimal
	TemplateTypeJSON
)

type Template struct {
	timeFormat       string
	fieldSep         string
	fieldPairSep     string
	levelStyle       LevelStyle
	fieldDisplayMode FieldDisplayMode
	showTime         bool
	showLevel        bool
	showMessage      bool
	showFields       bool
	useColours       bool
}

type LevelStyle int

const (
	LevelStyleText LevelStyle = iota
	LevelStyleIcon
	LevelStyleBadge
)

var TemplateDefault = &Template{
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
}

var TemplateSimple = &Template{
	showTime:         false,
	showLevel:        true,
	levelStyle:       LevelStyleText,
	showMessage:      true,
	showFields:       true,
	fieldSep:         " ",
	fieldPairSep:     ": ",
	fieldDisplayMode: FieldDisplayInline,
	useColours:       true,
}

var TemplateMinimal = &Template{
	showTime:         false,
	showLevel:        false,
	showMessage:      true,
	showFields:       false,
	fieldDisplayMode: FieldDisplayInline,
	useColours:       false,
}

var TemplateJSON = &Template{
	showTime:    true,
	timeFormat:  time.RFC3339Nano,
	showLevel:   true,
	levelStyle:  LevelStyleText,
	showMessage: true,
	showFields:  true,
	useColours:  false,
}

// buildWithTimezone converts UTC timestamps to the display timezone before rendering.
func (t *Template) buildWithTimezone(buf *bytes.Buffer, entry *Entry, theme *Theme, displayTimezone *time.Location) {
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
		t.writeMessage(buf, entry, theme)
	}

	if entry.Caller != "" {
		_ = buf.WriteByte(' ')
		if t.useColours && theme != nil {
			buf.WriteString(theme.cachedTimestampFg)
		}
		buf.WriteString(entry.Caller)
		_ = buf.WriteByte(':')
		buf.WriteString(strconv.Itoa(entry.Line))
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}
	}

	if t.showFields && len(entry.Fields) > 0 {
		if buf.Len() > 0 {
			_ = buf.WriteByte(' ')
		}
		t.writeFields(buf, entry, theme)
	}

	if buf.Len() == 0 || buf.Bytes()[buf.Len()-1] != '\n' {
		_ = buf.WriteByte('\n')
	}
}

func (t *Template) writeTimestampWithTimezone(buf *bytes.Buffer, entry *Entry, theme *Theme, displayTimezone *time.Location) {
	if t.useColours && theme != nil {
		buf.WriteString(theme.cachedTimestampFg)
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
		lvl := entry.Level
		if lvl >= 0 && int(lvl) < len(theme.cachedLevelFg) {
			levelCode = theme.cachedLevelFg[lvl]
		}
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

func (t *Template) writeMessage(buf *bytes.Buffer, entry *Entry, theme *Theme) {
	if t.useColours && theme != nil {
		buf.WriteString(theme.cachedMessageFg)
	}

	buf.WriteString(entry.Message)

	if t.useColours && theme != nil {
		buf.WriteString(Reset)
	}
}

func (t *Template) writeFields(buf *bytes.Buffer, entry *Entry, theme *Theme) {
	// Check if tree display is forced on the entry, otherwise use template's mode
	if entry.forceTreeDisplay || t.fieldDisplayMode == FieldDisplayTree {
		t.writeFieldsTree(buf, entry, theme)
	} else {
		t.writeFieldsInline(buf, entry, theme)
	}
}

func (t *Template) writeFieldsInline(buf *bytes.Buffer, entry *Entry, theme *Theme) {
	for i, field := range entry.Fields {
		if i > 0 {
			buf.WriteString(t.fieldSep)
		}

		if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldKeyFg)
		}
		buf.WriteString(field.Key)
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}

		buf.WriteString(t.fieldPairSep)

		if t.useColours && field.Type == FieldTypeError && theme != nil {
			buf.WriteString(theme.cachedErrorValFg)
		} else if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldValFg)
		}

		field.writeFormatted(buf)

		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}
	}
}

// writeFieldsTree renders fields in a tree structure aligned with the message.
func (t *Template) writeFieldsTree(buf *bytes.Buffer, entry *Entry, theme *Theme) {
	indent := t.calculatePrefixWidth(entry)
	indentStr := strings.Repeat(" ", indent)

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
			buf.WriteString(theme.cachedFieldKeyFg)
		}
		buf.WriteString(field.Key)
		if t.useColours && theme != nil {
			buf.WriteString(Reset)
		}

		buf.WriteString(t.fieldPairSep)

		if t.useColours && field.Type == FieldTypeError && theme != nil {
			buf.WriteString(theme.cachedErrorValFg)
		} else if t.useColours && theme != nil {
			buf.WriteString(theme.cachedFieldValFg)
		}

		field.writeFormatted(buf)

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

// calculatePrefixWidth determines indentation needed for tree alignment.
func (t *Template) calculatePrefixWidth(entry *Entry) int {
	width := 0

	if t.showTime && !entry.Time.IsZero() {
		timeStr := entry.Time.Format(t.timeFormat)
		width += len(timeStr)
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

func padRight(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return s + strings.Repeat(" ", length-len(s))
}

// padRightRunes pads a string to the specified length using rune count instead of byte count.
// This is important for Unicode text where characters may use multiple bytes.
func padRightRunes(s string, length int) string {
	runeLen := len([]rune(s))
	if runeLen >= length {
		return s
	}
	return s + strings.Repeat(" ", length-runeLen)
}

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
	return b.template
}

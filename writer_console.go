package velocity

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"sync"
	"time"

	"golang.org/x/term"
)

type ConsoleWriter struct {
	out      io.Writer
	theme    *Theme
	template *Template
	timeFunc func() time.Time
	bufPool  *BufferPool

	// Logs are always stored in UTC, but this controls how they're displayed
	displayTimezone *time.Location

	// Cached ANSI colour codes for fast access without repeated theme lookups
	levelColours [6]string
	mu           sync.Mutex
	isTTY        bool
	closed       bool
}

func NewConsoleWriter(out io.Writer, theme *Theme) *ConsoleWriter {
	return NewConsoleWriterWithTimezone(out, theme, time.Local)
}

func NewConsoleWriterWithTimezone(out io.Writer, theme *Theme, displayTimezone *time.Location) *ConsoleWriter {
	return NewConsoleWriterWithOptions(out, theme, displayTimezone, FieldDisplayInline)
}

func NewConsoleWriterWithOptions(out io.Writer, theme *Theme, displayTimezone *time.Location, fieldDisplayMode FieldDisplayMode) *ConsoleWriter {
	// Track if theme was explicitly nil for disabling colors
	useColours := true
	if theme == nil {
		theme = ThemeNightOwl
		useColours = false // Explicitly disable colors when theme is nil
	}

	if displayTimezone == nil {
		displayTimezone = time.Local
	}

	templateCopy := *TemplateDefault
	templateCopy.fieldDisplayMode = fieldDisplayMode
	templateCopy.useColours = useColours

	w := &ConsoleWriter{
		out:             out,
		theme:           theme,
		template:        &templateCopy,
		timeFunc:        time.Now,
		bufPool:         NewBufferPool(),
		displayTimezone: displayTimezone,
	}

	if f, ok := out.(interface{ Fd() uintptr }); ok {
		w.isTTY = term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: uintptr fd fits in int on all supported platforms
	}

	if w.isTTY && useColours {
		w.cacheLevelColours()
	}

	return w
}

// cacheLevelColours pre-computes ANSI codes to avoid allocation during log writes
func (w *ConsoleWriter) cacheLevelColours() {
	if w.theme == nil {
		return
	}

	levels := []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal}
	for _, lvl := range levels {
		c := w.theme.GetColourForLevel(lvl)
		w.levelColours[lvl] = c.ANSI(true)
	}
}

func (w *ConsoleWriter) SetTemplate(t *Template) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t == nil {
		w.template = TemplateDefault
	} else {
		w.template = t
	}
}

func (w *ConsoleWriter) Write(e *Entry) error {
	// Snapshot mutable state under a brief lock so formatting runs unlocked.
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrWriterClosed
	}
	tmpl := w.template
	theme := w.theme
	tz := w.displayTimezone
	lvlColours := w.levelColours
	isTTY := w.isTTY
	w.mu.Unlock()

	if tmpl != nil {
		tempBuf := GetTemplateBuffer()
		tmpl.buildWithTimezone(tempBuf, e, theme, tz)

		w.mu.Lock()
		_, err := w.out.Write(tempBuf.Bytes())
		w.mu.Unlock()

		PutTemplateBuffer(tempBuf)
		return err
	}

	rawBuf := w.bufPool.Get(HintConsoleLog)
	buf := NewBytesBuffer(rawBuf)
	w.formatEntryWithSnap(buf, e, theme, tz, lvlColours, isTTY)

	w.mu.Lock()
	_, err := w.out.Write(buf.Bytes())
	if err == nil {
		_, err = w.out.Write(newlineByte)
	}
	w.mu.Unlock()

	w.bufPool.Put(rawBuf)
	if err != nil {
		return fmt.Errorf("console write failed: %w", err)
	}
	return nil
}

// formatEntryWithSnap formats using snapshotted state, safe to call without the mutex.
func (w *ConsoleWriter) formatEntryWithSnap(buf *BytesBuffer, e *Entry, theme *Theme, tz *time.Location, lvlColours [6]string, isTTY bool) {
	buf.WriteString("[")
	displayTime := e.Time.In(tz)
	buf.AppendTime(displayTime, time.RFC3339)
	buf.WriteString("] ")

	_ = buf.WriteByte('[')
	if isTTY && theme != nil && e.Level >= 0 && int(e.Level) < len(lvlColours) {
		buf.WriteString(lvlColours[e.Level])
	}
	buf.WriteString(e.Level.ConciseLabel())
	if isTTY && theme != nil && e.Level >= 0 && int(e.Level) < len(lvlColours) {
		buf.WriteString(Reset)
	}
	_ = buf.WriteByte(']')

	buf.WriteString(" ")
	buf.WriteString(e.Message)

	if e.Caller != "" {
		buf.WriteString(" (")
		buf.WriteString(e.Caller)
		_ = buf.WriteByte(':')
		buf.WriteInt(int64(e.Line))
		_ = buf.WriteByte(')')
	}

	if len(e.Fields) > 0 {
		w.formatFields(buf, e.Fields)
	}
}

func (w *ConsoleWriter) formatLevel(buf *BytesBuffer, level Level) {
	_ = buf.WriteByte('[')

	if w.isTTY && w.theme != nil && level >= 0 && int(level) < len(w.levelColours) {
		buf.WriteString(w.levelColours[level])
	}

	buf.WriteString(level.ConciseLabel())

	if w.isTTY && w.theme != nil && level >= 0 && int(level) < len(w.levelColours) {
		buf.WriteString(Reset)
	}

	_ = buf.WriteByte(']')
}

func (w *ConsoleWriter) formatFields(buf *BytesBuffer, fields []Field) {
	for _, f := range fields {
		_ = buf.WriteByte(' ')
		buf.WriteString(f.Key)
		buf.WriteString(": ")
		w.formatValue(buf, f)
	}
}

func (*ConsoleWriter) formatValue(buf *BytesBuffer, f Field) {
	switch f.Type {
	case FieldTypeString:
		v := *(*string)(f.value)
		_ = buf.WriteByte('"')
		buf.WriteString(v)
		_ = buf.WriteByte('"')

	case FieldTypeInt:
		buf.WriteInt(f.num)

	case FieldTypeInt64:
		buf.WriteInt(f.num)

	case FieldTypeFloat64:
		floatValue := math.Float64frombits(uint64(f.num)) //nolint:gosec // G115: bit-pattern reinterpretation, not value conversion
		buf.WriteString(strconv.FormatFloat(floatValue, 'g', -1, 64))

	case FieldTypeBool:
		if f.num != 0 {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}

	case FieldTypeTime:
		t := *(*time.Time)(f.value)
		buf.WriteString(t.Format(time.RFC3339))

	case FieldTypeDuration:
		d := time.Duration(f.num)
		buf.WriteString(d.String())

	case FieldTypeError:
		if f.value == nil {
			buf.WriteString("<nil>")
			break
		}
		err := *(*error)(f.value)
		if err == nil {
			buf.WriteString("<nil>")
		} else {
			_ = buf.WriteByte('"')
			buf.WriteString(err.Error())
			_ = buf.WriteByte('"')
		}

	case FieldTypeStringer:
		if f.value == nil {
			buf.WriteString("<nil>")
			break
		}
		s := *(*fmt.Stringer)(f.value)
		if s == nil {
			buf.WriteString("<nil>")
		} else {
			_ = buf.WriteByte('"')
			buf.WriteString(s.String())
			_ = buf.WriteByte('"')
		}

	case FieldTypeBytes:
		const hexDigits = "0123456789abcdef"
		b := *(*[]byte)(f.value)
		_ = buf.WriteByte('[')
		for i, v := range b {
			if i > 0 {
				_ = buf.WriteByte(' ')
			}
			_ = buf.WriteByte(hexDigits[v>>4])
			_ = buf.WriteByte(hexDigits[v&0x0f])
		}
		_ = buf.WriteByte(']')

	case FieldTypeAny:
		v := *(*any)(f.value)
		buf.WriteString(fmt.Sprintf("%v", v))

	case FieldTypeUnknown:
		// Unknown field type - write nothing
	}
}

func (w *ConsoleWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true
	if s, ok := w.out.(interface{ Sync() error }); ok {
		return s.Sync()
	}
	return nil
}

func (w *ConsoleWriter) SetTheme(theme *Theme) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.theme = theme
	w.cacheLevelColours()
}

func (w *ConsoleWriter) IsTTY() bool {
	return w.isTTY
}

package velocity

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
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
	// Track if theme was explicitly nil for disabling colours.
	useColours := true
	if theme == nil {
		theme = ThemeNightOwl
		useColours = false
	}
	// Themes are immutable from NewTheme — no caching step needed here.

	if displayTimezone == nil {
		displayTimezone = time.Local
	}

	templateCopy := *TemplateDefault
	templateCopy.fieldDisplayMode = fieldDisplayMode
	templateCopy.useColours = useColours
	// Recompute cached widths after mutation. fieldDisplayMode and useColours do not affect
	// prefix widths today, but initCache is cheap and prevents stale caches if future
	// mutations here are width-affecting.
	templateCopy.initCache()

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

// cacheLevelColours pre-computes ANSI codes to avoid allocation during log writes.
// The theme carries pre-cached strings from construction, so this is a straight copy.
func (w *ConsoleWriter) cacheLevelColours() {
	if w.theme == nil {
		return
	}

	levels := []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal}
	for _, lvl := range levels {
		w.levelColours[lvl] = w.theme.cachedLevelCode(lvl)
	}
}

// WriteStatus renders a status-badged log line for entries produced by Logger.Status.
// The badge replaces the normal level label on TTY; on non-TTY it falls back to the
// standard formatEntrySecure path so the output remains readable without ANSI.
func (w *ConsoleWriter) WriteStatus(e *Entry) error {
	return w.WriteStatusSecure(e, w.isTTY, "[REDACTED]")
}

// WriteStatusSecure is the trust-aware status write path, mirroring WriteSecure.
func (w *ConsoleWriter) WriteStatusSecure(e *Entry, trusted bool, redactionMark string) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrWriterClosed
	}
	tmpl := w.template
	theme := w.theme
	tz := w.displayTimezone
	isTTY := w.isTTY
	w.mu.Unlock()

	tempBuf := GetTemplateBuffer()
	defer PutTemplateBuffer(tempBuf)

	switch {
	case isTTY && tmpl != nil:
		buildStatusLine(tempBuf, e, theme, tz, trusted, redactionMark)
	case tmpl != nil:
		// Non-TTY: use the standard path so the output is undecorated but complete.
		tmpl.buildWithTimezoneSecure(tempBuf, e, theme, tz, trusted, redactionMark)
	default:
		// Fallback: no template, produce a minimal status line.
		fmt.Fprintf(tempBuf, "[%s] %s\n", e.statusKind.String(), e.Message)
	}

	w.mu.Lock()
	_, err := w.out.Write(tempBuf.Bytes())
	w.mu.Unlock()
	return err
}

// buildStatusLine builds the TTY status line into buf:
// timestamp + " " + badge + "   " + message + fields + "\n"
// The badge format is '[' + coloured-padded-token + ']' at fixed width.
func buildStatusLine(buf *bytes.Buffer, e *Entry, theme *Theme, tz *time.Location, trusted bool, redactionMark string) {
	// Timestamp (reuses AppendFormat to avoid intermediate string alloc).
	if !e.Time.IsZero() {
		if theme != nil {
			buf.WriteString(theme.cachedTimestampFgStr())
		}
		displayTime := e.Time.In(tz)
		buf.Write(displayTime.AppendFormat(buf.AvailableBuffer(), time.RFC3339))
		if theme != nil {
			buf.WriteString(Reset)
		}
		_ = buf.WriteByte(' ')
	}

	// Status badge: '[' + coloured token (padded to statusBadgeWidth) + ']'.
	token := e.statusKind.String()
	slot := e.statusKind.Slot()
	_ = buf.WriteByte('[')
	if theme != nil {
		prefix, suffix := theme.Wrap(slot)
		buf.WriteString(prefix)
		buf.WriteString(token)
		if pad := statusBadgeWidth - len(token); pad > 0 {
			buf.WriteString(strings.Repeat(" ", pad))
		}
		buf.WriteString(suffix)
	} else {
		buf.WriteString(token)
		if pad := statusBadgeWidth - len(token); pad > 0 {
			buf.WriteString(strings.Repeat(" ", pad))
		}
	}
	_ = buf.WriteByte(']')
	buf.WriteString(statusBadgeSep)

	// Message (with secure-tag handling).
	msg := e.Message
	if e.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}
	if theme != nil {
		buf.WriteString(theme.cachedMessageFgStr())
	}
	buf.WriteString(msg)
	if theme != nil {
		buf.WriteString(Reset)
	}

	// Caller (if present).
	if e.Caller != "" {
		_ = buf.WriteByte(' ')
		_ = buf.WriteByte('(')
		buf.WriteString(e.Caller)
		_ = buf.WriteByte(':')
		buf.Write(strconv.AppendInt(nil, int64(e.Line), 10))
		_ = buf.WriteByte(')')
	}

	// Fields rendered key=value inline.
	for _, f := range e.Fields {
		_ = buf.WriteByte(' ')
		keyCode := ""
		valCode := ""
		if theme != nil {
			keyCode = theme.CachedFieldKeyFg()
			if f.Type == FieldTypeError {
				valCode = theme.cachedErrorValFgStr()
			} else {
				valCode = theme.CachedFieldValFg()
			}
		}
		if keyCode != "" {
			buf.WriteString(keyCode)
		}
		buf.WriteString(f.Key)
		if keyCode != "" {
			buf.WriteString(Reset)
		}
		_ = buf.WriteByte('=')
		if valCode != "" {
			buf.WriteString(valCode)
		}
		// Quote string-like types to match console writer convention.
		switch f.Type {
		case FieldTypeString, FieldTypeError, FieldTypeStringer, FieldTypeTruncated:
			_ = buf.WriteByte('"')
			if trusted {
				f.writeFormattedTrusted(buf)
			} else {
				f.writeFormattedWithMark(buf, redactionMark)
			}
			_ = buf.WriteByte('"')
		default:
			if trusted {
				f.writeFormattedTrusted(buf)
			} else {
				f.writeFormattedWithMark(buf, redactionMark)
			}
		}
		if valCode != "" {
			buf.WriteString(Reset)
		}
	}

	_ = buf.WriteByte('\n')
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
	// TTY console writers are trusted by context — they render to a human-facing
	// terminal session, not a file or pipeline. Non-TTY consoles are untrusted.
	return w.WriteSecure(e, w.isTTY, "[REDACTED]")
}

// WriteSecure implements SecureWriter. When trusted is true, Secure field
// plaintext is shown and <secure> markers are stripped. When false, both are
// replaced with redactionMark.
func (w *ConsoleWriter) WriteSecure(e *Entry, trusted bool, redactionMark string) error {
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
	w.mu.Unlock()

	if tmpl != nil {
		tempBuf := GetTemplateBuffer()
		tmpl.buildWithTimezoneSecure(tempBuf, e, theme, tz, trusted, redactionMark)

		w.mu.Lock()
		_, err := w.out.Write(tempBuf.Bytes())
		w.mu.Unlock()

		PutTemplateBuffer(tempBuf)
		return err
	}

	rawBuf := w.bufPool.Get(HintConsoleLog)
	buf := NewBytesBuffer(rawBuf)
	w.formatEntrySecure(buf, e, theme, tz, lvlColours, trusted, redactionMark) //nolint:staticcheck // intentional: lvlColours unused when not TTY

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

// formatEntrySecure formats an entry using snapshotted state, applying redaction
// when trusted is false. Safe to call without the mutex.
func (w *ConsoleWriter) formatEntrySecure(buf *BytesBuffer, e *Entry, theme *Theme, tz *time.Location, lvlColours [6]string, trusted bool, redactionMark string) {
	buf.WriteString("[")
	displayTime := e.Time.In(tz)
	buf.AppendTime(displayTime, time.RFC3339)
	buf.WriteString("] ")

	_ = buf.WriteByte('[')
	if trusted && theme != nil && e.Level >= 0 && int(e.Level) < len(lvlColours) {
		buf.WriteString(lvlColours[e.Level])
	}
	buf.WriteString(e.Level.ConciseLabel())
	if trusted && theme != nil && e.Level >= 0 && int(e.Level) < len(lvlColours) {
		buf.WriteString(Reset)
	}
	_ = buf.WriteByte(']')

	buf.WriteString(" ")

	msg := e.Message
	if e.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}
	buf.WriteString(msg)

	if e.Caller != "" {
		buf.WriteString(" (")
		buf.WriteString(e.Caller)
		_ = buf.WriteByte(':')
		buf.WriteInt(int64(e.Line))
		_ = buf.WriteByte(')')
	}

	if len(e.Fields) > 0 {
		w.formatFieldsSecure(buf, e.Fields, trusted, redactionMark)
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

func (w *ConsoleWriter) formatFieldsSecure(buf *BytesBuffer, fields []Field, trusted bool, redactionMark string) {
	for _, f := range fields {
		_ = buf.WriteByte(' ')
		buf.WriteString(f.Key)
		buf.WriteString(": ")
		w.formatValueSecure(buf, f, trusted, redactionMark)
	}
}

func (*ConsoleWriter) formatValueSecure(buf *BytesBuffer, f Field, trusted bool, redactionMark string) {
	switch f.Type {
	case FieldTypeSecure, FieldTypeSecureURL:
		if trusted && f.value != nil {
			_ = buf.WriteByte('"')
			buf.WriteString((*secureValue)(f.value).plain)
			_ = buf.WriteByte('"')
		} else {
			// Emit field-level redacted form (e.g. URL with password replaced)
			// rather than the generic writer mark, so structured context is preserved.
			if f.value != nil {
				_ = buf.WriteByte('"')
				buf.WriteString((*secureValue)(f.value).redacted)
				_ = buf.WriteByte('"')
			} else {
				buf.WriteString(redactionMark)
			}
		}
		return
	case FieldTypeRedacted:
		buf.WriteString(redactionMark)
		return
	case FieldTypeTruncated:
		if f.value != nil {
			_ = buf.WriteByte('"')
			buf.WriteString(*(*string)(f.value))
			_ = buf.WriteByte('"')
		}
		return
	default:
		consoleFormatValueCore(buf, f)
	}
}

func consoleFormatValueCore(buf *BytesBuffer, f Field) {
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
		// Fprintf writes directly into the buffer, avoiding the intermediate string alloc
		// that fmt.Sprintf("%v", v) would produce.
		_, _ = fmt.Fprintf(buf, "%v", v)

	case FieldTypeSecure, FieldTypeSecureURL, FieldTypeRedacted, FieldTypeTruncated:
		// Handled upstream by formatValueSecure before consoleFormatValueCore is called.

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

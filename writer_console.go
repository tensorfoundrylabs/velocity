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
	// nil theme means "use the default" — not "disable colour".
	// Colour is disabled by passing noColourTheme explicitly (see newFromConfig).
	if theme == nil {
		theme = ThemeNightOwl
	}
	themeHasColour := !theme.noColour

	if displayTimezone == nil {
		displayTimezone = time.Local
	}

	// Resolve whether this writer should emit ANSI sequences. This checks
	// NO_COLOR / FORCE_COLOR first, then falls back to fd-level detection.
	// On Windows, terminal emulators often proxy stdout as a named pipe;
	// FORCE_COLOR=1 is the escape hatch for those environments.
	isTTY := resolveColourForWriter(out)

	// useColours is true only when both the writer can render colour AND the
	// theme actually carries colour slots. A no-colour theme (noColourTheme,
	// ThemeMono) always produces plain output regardless of TTY state.
	useColours := isTTY && themeHasColour

	templateCopy := *TemplateDefault
	templateCopy.fieldDisplayMode = fieldDisplayMode
	templateCopy.useColours = useColours
	// Recompute cached widths after mutation. fieldDisplayMode and useColours do not affect
	// prefix widths today, but initCache is cheap and prevents stale caches if future
	// mutations here are width-affecting.
	templateCopy.initCache()
	// indicators is intentionally left as zero value here; callers that have a full
	// config (i.e. newFromConfig) apply it separately after construction.

	w := &ConsoleWriter{
		out:             out,
		theme:           theme,
		template:        &templateCopy,
		timeFunc:        time.Now,
		bufPool:         NewBufferPool(),
		displayTimezone: displayTimezone,
		isTTY:           isTTY,
	}

	if useColours {
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

	// Status badge: '[' + coloured token + ']' — variable width, no padding.
	token := e.statusKind.String()
	slot := e.statusKind.Slot()
	_ = buf.WriteByte('[')
	if theme != nil {
		prefix, suffix := theme.Wrap(slot)
		buf.WriteString(prefix)
		buf.WriteString(token)
		buf.WriteString(suffix)
	} else {
		buf.WriteString(token)
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
		// Dispatch on the hottest log path here rather than inside
		// buildWithTimezoneSecure: isActive inlines but the render functions do not,
		// so routing the no-indicators case straight to buildBaselineSecure keeps the
		// disabled path at one render call, the same cost as before the feature.
		if tmpl.isActive() {
			tmpl.buildWithTimezoneSecure(tempBuf, e, theme, tz, trusted, redactionMark)
		} else {
			tmpl.buildBaselineSecure(tempBuf, e, theme, tz, trusted, redactionMark)
		}

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

	case FieldTypeGroupItems:
		// Group items are rendered by WriteGroup; in generic paths emit a hint.
		if f.value != nil {
			items := *(*[]GroupItem)(f.value)
			var tmp [20]byte
			n := formatInt(tmp[:], int64(len(items)))
			_ = buf.WriteByte('[')
			_, _ = buf.Write(tmp[:n])
			buf.WriteString(" items]")
		}

	case FieldTypeContinuationLines:
		// Continuation lines are rendered by WriteContinue; in generic paths emit a hint.
		if f.value != nil {
			lines := *(*[]string)(f.value)
			var tmp [20]byte
			n := formatInt(tmp[:], int64(len(lines)))
			_ = buf.WriteByte('[')
			_, _ = buf.Write(tmp[:n])
			buf.WriteString(" lines]")
		}

	case FieldTypeUnknown:
		// Unknown field type - write nothing
	}
}

// WriteGroup renders a Group log entry. On TTY it emits the coloured count header
// followed by indented item lines; on non-TTY it falls back to the standard
// template path for the header and appends plain item lines.
func (w *ConsoleWriter) WriteGroup(e *Entry, items []GroupItem) error {
	return w.WriteGroupSecure(e, items, w.isTTY, "[REDACTED]")
}

// WriteGroupSecure is the trust-aware group write path.
func (w *ConsoleWriter) WriteGroupSecure(e *Entry, items []GroupItem, trusted bool, redactionMark string) error {
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
		// On TTY: coloured level + count-coloured message header, then indented item lines.
		buildGroupLineTTY(tempBuf, e, theme, tz, trusted, redactionMark, items, tmpl)
	case tmpl != nil:
		// Non-TTY: standard template for the header (level + plain message with count),
		// then plain item lines appended directly.
		tmpl.buildWithTimezoneSecure(tempBuf, e, theme, tz, trusted, redactionMark)
		// The template appends a trailing '\n'; item lines follow without extra spacing.
		writeGroupConsoleItems(tempBuf, items)
	default:
		fmt.Fprintf(tempBuf, "%s\n", e.Message)
		writeGroupConsoleItems(tempBuf, items)
	}

	w.mu.Lock()
	_, err := w.out.Write(tempBuf.Bytes())
	w.mu.Unlock()
	return err
}

// buildGroupLineTTY builds the full TTY group block: timestamp + level + coloured
// message+count header, then indented+coloured item lines.
func buildGroupLineTTY(buf *bytes.Buffer, e *Entry, theme *Theme, tz *time.Location, trusted bool, redactionMark string, items []GroupItem, tmpl *Template) {
	// Timestamp.
	if !e.Time.IsZero() {
		if theme != nil {
			buf.WriteString(theme.cachedTimestampFgStr())
		}
		displayTime := e.Time.In(tz)
		buf.Write(displayTime.AppendFormat(buf.AvailableBuffer(), time.RFC3339))
		if theme != nil {
			buf.WriteString(Reset)
		}
		buf.WriteByte(' ')
	}

	// Level badge "[INFO]".
	if theme != nil {
		buf.WriteString(theme.cachedLevelCode(e.Level))
	}
	buf.WriteByte('[')
	buf.WriteString(e.Level.ConciseLabel())
	buf.WriteByte(']')
	if theme != nil {
		buf.WriteString(Reset)
	}
	buf.WriteByte(' ')

	// Message (secure-aware, with count rendered in SlotCount colour).
	msg := e.Message
	if e.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}

	// msg here is already "text (N)" — we need to split out the " (N)" suffix and
	// re-render it with the SlotCount colour. Find the last " (" which we inserted.
	// This is safe because groupMsgWithCount always appends " (N)".
	if idx := strings.LastIndex(msg, " ("); idx >= 0 {
		head := msg[:idx]
		tail := msg[idx+2 : len(msg)-1] // extract the count digits only
		if theme != nil {
			buf.WriteString(theme.CachedMessageFg())
		}
		buf.WriteString(head)
		if theme != nil {
			buf.WriteString(Reset)
		}
		buf.WriteString(" (")
		countPrefix, countSuffix := theme.Wrap(SlotCount)
		buf.WriteString(countPrefix)
		buf.WriteString(tail)
		buf.WriteString(countSuffix)
		buf.WriteByte(')')
	} else {
		if theme != nil {
			buf.WriteString(theme.CachedMessageFg())
		}
		buf.WriteString(msg)
		if theme != nil {
			buf.WriteString(Reset)
		}
	}
	buf.WriteByte('\n')

	// Item lines indented to the message column so they sit flush under the header.
	indent := tmpl.CachedMessageIndentStr()
	for i, item := range items {
		marker := resolvedMarker(item.Marker, i, len(items))
		buf.WriteString(indent)
		buf.WriteString(groupItemIndent)
		if theme != nil {
			buf.WriteString(theme.CachedFieldKeyFg())
		}
		buf.WriteString(marker)
		buf.WriteByte(' ')
		if theme != nil {
			buf.WriteString(Reset)
			buf.WriteString(theme.CachedMessageFg())
		}
		buf.WriteString(item.Text)
		if theme != nil {
			buf.WriteString(Reset)
		}
		buf.WriteByte('\n')
	}
}

// WriteContinue renders a ContinuationBlock log entry. The header line is the
// standard log line; continuation lines follow with the │ glyph prefix indented
// to the message column so they land flush under the header text.
func (w *ConsoleWriter) WriteContinue(e *Entry, lines []string) error {
	return w.WriteContinueSecure(e, lines, w.isTTY, "[REDACTED]")
}

// WriteContinueSecure is the trust-aware continuation write path.
func (w *ConsoleWriter) WriteContinueSecure(e *Entry, lines []string, trusted bool, redactionMark string) error {
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
		buildContinueLineTTY(tempBuf, e, theme, tz, trusted, redactionMark, lines, tmpl)
	case tmpl != nil:
		// Non-TTY: standard template for the header, then plain continuation lines.
		tmpl.buildWithTimezoneSecure(tempBuf, e, theme, tz, trusted, redactionMark)
		writeContinuationLines(tempBuf, lines, tmpl.CachedMessageIndentStr(), false, nil)
	default:
		fmt.Fprintf(tempBuf, "%s\n", e.Message)
		writeContinuationLines(tempBuf, lines, "", false, nil)
	}

	w.mu.Lock()
	_, err := w.out.Write(tempBuf.Bytes())
	w.mu.Unlock()
	return err
}

// buildContinueLineTTY builds the full TTY continuation block: the standard log
// header (timestamp + level badge + message) then each continuation line indented
// to the message column with a SlotContinuation-coloured │ glyph.
func buildContinueLineTTY(buf *bytes.Buffer, e *Entry, theme *Theme, tz *time.Location, trusted bool, redactionMark string, lines []string, tmpl *Template) {
	// Header: identical to the standard TTY log line.
	tmpl.buildWithTimezoneSecure(buf, e, theme, tz, trusted, redactionMark)
	// buildWithTimezoneSecure appends '\n'; continuation lines follow directly.
	writeContinuationLines(buf, lines, tmpl.CachedMessageIndentStr(), true, theme)
}

// writeContinuationLines appends each line prefixed with the message-column indent
// and the │ glyph. When styled is true and theme is non-nil, the glyph is wrapped
// with SlotContinuation ANSI codes.
func writeContinuationLines(buf *bytes.Buffer, lines []string, indent string, styled bool, theme *Theme) {
	var glyphPrefix, glyphSuffix string
	if styled && theme != nil {
		glyphPrefix, glyphSuffix = theme.Wrap(SlotContinuation)
	}

	for _, line := range lines {
		buf.WriteString(indent)
		if glyphPrefix != "" {
			buf.WriteString(glyphPrefix)
		}
		buf.WriteString(continuationGlyphSep)
		if glyphSuffix != "" {
			buf.WriteString(glyphSuffix)
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
}

func (w *ConsoleWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true
	// Sync is best-effort: pipes and redirected streams reject it on Windows.
	if s, ok := w.out.(interface{ Sync() error }); ok {
		_ = s.Sync()
	}
	return nil
}

// SetTheme replaces the active theme. When colour was disabled at construction
// (e.g. no-colour theme or non-TTY writer) it stays disabled — switching to a
// coloured theme on a non-TTY writer does not re-enable ANSI output.
// When the writer is a TTY and the new theme has colour, colour is re-enabled.
func (w *ConsoleWriter) SetTheme(theme *Theme) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.theme = theme
	// Re-derive useColours from current isTTY and new theme state.
	themeHasColour := theme != nil && !theme.noColour
	w.template.useColours = w.isTTY && themeHasColour
	w.cacheLevelColours()
}

func (w *ConsoleWriter) IsTTY() bool {
	return w.isTTY
}

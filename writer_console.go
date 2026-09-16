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

	// colourAllowed is the stable presentation permission, resolved once at
	// construction from NO_COLOR / FORCE_COLOR / terminal detection. Theme
	// swaps re-derive useColours from this bit; they never re-resolve the
	// environment and never infer permission from the previous theme's
	// useColours value (a mono theme must not revoke FORCE_COLOR permission).
	colourAllowed bool

	// colourExplicitlyDisabled records WithColour(false) from the owning
	// logger. It outranks colourAllowed forever: no theme swap can restore
	// ANSI after an explicit disable.
	colourExplicitlyDisabled bool

	// inFlight tracks admitted write cycles: a caller that passed the closed
	// check and registered here may still be formatting (a Stringer or
	// Renderable callback can block indefinitely) before its final write.
	// Close drains on it, so no admitted write lands after Close returned and
	// no admitted call is cut off mid-flight (the F2 finding: the closed check
	// alone only stopped calls begun after close, not paused admitted calls).
	inFlight sync.WaitGroup

	// Family close lifecycle for direct Close callers, mirroring MultiWriter:
	// the drain runs exactly once, every concurrent Close waits on closeDone
	// and returns the same recorded closeErr rather than racing a flag and
	// reporting success while the first Close is still draining (WP2 contract).
	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

// consoleAdmission carries the state snapshot taken atomically with the
// in-flight registration. ok is false exactly when the writer is closed —
// otherwise the caller MUST balance with w.inFlight.Done() exactly once after
// its final write. There is deliberately no done func() field: storing the
// w.inFlight.Done method value boxes the receiver and allocates on every
// admitted write (measured +16 B/+1 alloc on the console hot path), so the
// call sites defer w.inFlight.Done() directly.
type consoleAdmission struct {
	tmpl    *Template
	theme   *Theme
	tz      *time.Location
	isTTY   bool
	colours [6]string
}

// admit is the single admission point for every console write path: the
// closed check and the in-flight registration happen under one mutex
// acquisition, so Close (which sets closed then waits on inFlight under the
// same mutex ordering) can never slip between them. The state snapshot rides
// the same critical section the callers previously took anyway.
func (w *ConsoleWriter) admit() (consoleAdmission, bool) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return consoleAdmission{}, false
	}
	w.inFlight.Add(1)
	adm := consoleAdmission{
		tmpl:    w.template,
		theme:   w.theme,
		tz:      w.displayTimezone,
		isTTY:   w.isTTY,
		colours: w.levelColours,
	}
	w.mu.Unlock()
	return adm, true
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
	// Trust is a property of the destination, while colour is a presentation
	// choice. FORCE_COLOR affects only the latter.
	isTTY := IsTerminalWriter(out)

	// useColours is true only when both the writer is permitted to render
	// colour AND the theme actually carries colour slots. A no-colour theme
	// (noColourTheme, ThemeMono) always produces plain output regardless of
	// TTY state; FORCE_COLOR permits styling on pipes and files.
	colourAllowed := resolveColourForWriter(out)
	useColours := colourAllowed && themeHasColour

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
		colourAllowed:   colourAllowed,
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
	adm, ok := w.admit()
	if !ok {
		return ErrWriterClosed
	}
	defer w.inFlight.Done()
	tmpl, theme, tz, isTTY := adm.tmpl, adm.theme, adm.tz, adm.isTTY

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
	// Admission + state snapshot in one critical section: formatting runs
	// unlocked below (and may block in a Stringer), Close drains this cycle
	// before it returns, and the final write happens under mu as before.
	adm, ok := w.admit()
	if !ok {
		return ErrWriterClosed
	}
	defer w.inFlight.Done()
	tmpl, theme, tz, lvlColours := adm.tmpl, adm.theme, adm.tz, adm.colours

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
	case FieldTypeUint64:
		// Stack-buffer form: strconv.FormatUint allocates for values >= 100.
		var tmp [20]byte
		n := formatUint(tmp[:], uint64(f.num)) //nolint:gosec // G115: field storage is bit-pattern int64, reinterpretation is the contract
		_, _ = buf.Write(tmp[:n])

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
	adm, ok := w.admit()
	if !ok {
		return ErrWriterClosed
	}
	defer w.inFlight.Done()
	tmpl, theme, tz, isTTY := adm.tmpl, adm.theme, adm.tz, adm.isTTY

	tempBuf := GetTemplateBuffer()
	defer PutTemplateBuffer(tempBuf)

	switch {
	case isTTY && tmpl != nil:
		// On TTY: coloured level + count-coloured message header, then indented item lines.
		buildGroupLineTTY(tempBuf, e, theme, tz, trusted, redactionMark, items, tmpl)
	case tmpl != nil:
		// Non-TTY: standard template for the header (level + plain message with count),
		// then plain item lines appended directly. Item text gets the same
		// secure-tag treatment as the header — non-header payloads must not leak.
		tmpl.buildWithTimezoneSecure(tempBuf, e, theme, tz, trusted, redactionMark)
		// The template appends a trailing '\n'; item lines follow without extra spacing.
		writeGroupConsoleItems(tempBuf, items, e.maybeSecure, trusted, redactionMark)
	default:
		fmt.Fprintf(tempBuf, "%s\n", e.Message)
		writeGroupConsoleItems(tempBuf, items, e.maybeSecure, trusted, redactionMark)
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
		buf.WriteString(applySecureTags(item.Text, e.maybeSecure, trusted, redactionMark))
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
	adm, ok := w.admit()
	if !ok {
		return ErrWriterClosed
	}
	defer w.inFlight.Done()
	tmpl, theme, tz, isTTY := adm.tmpl, adm.theme, adm.tz, adm.isTTY

	tempBuf := GetTemplateBuffer()
	defer PutTemplateBuffer(tempBuf)

	switch {
	case isTTY && tmpl != nil:
		buildContinueLineTTY(tempBuf, e, theme, tz, trusted, redactionMark, lines, tmpl)
	case tmpl != nil:
		// Non-TTY: standard template for the header, then plain continuation
		// lines. Line text gets the same secure-tag treatment as the header.
		tmpl.buildWithTimezoneSecure(tempBuf, e, theme, tz, trusted, redactionMark)
		writeContinuationLines(tempBuf, lines, tmpl.CachedMessageIndentStr(), false, nil, e.maybeSecure, trusted, redactionMark)
	default:
		fmt.Fprintf(tempBuf, "%s\n", e.Message)
		writeContinuationLines(tempBuf, lines, "", false, nil, e.maybeSecure, trusted, redactionMark)
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
	writeContinuationLines(buf, lines, tmpl.CachedMessageIndentStr(), true, theme, e.maybeSecure, trusted, redactionMark)
}

// writeContinuationLines appends each line prefixed with the message-column indent
// and the │ glyph. When styled is true and theme is non-nil, the glyph is wrapped
// with SlotContinuation ANSI codes. secureActive applies the entry's secure-tag
// policy to each line so non-header payloads follow the same redaction rules as
// the header message.
func writeContinuationLines(buf *bytes.Buffer, lines []string, indent string, styled bool, theme *Theme, secureActive, trusted bool, redactionMark string) {
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
		buf.WriteString(applySecureTags(line, secureActive, trusted, redactionMark))
		buf.WriteByte('\n')
	}
}

// Flush drains the underlying buffered writer without closing this writer.
// Only has effect when the output implements Flush. Fatal uses this before its
// handler/exit decision so buffered bytes reach the destination while the
// logger stays reusable for a returning custom handler.
func (w *ConsoleWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrWriterClosed
	}
	if f, ok := w.out.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

func (w *ConsoleWriter) Close() error {
	w.closeOnce.Do(func() {
		// Created here (not at construction) so zero-value writers are safe;
		// Once's completion guarantee makes the field visible to every caller
		// before the receive below.
		w.closeDone = make(chan struct{})
		defer close(w.closeDone)

		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()

		// Drain admitted callers outside the mutex — they need it for their
		// final writes, so waiting while holding it would deadlock them. When
		// Wait returns no in-flight write cycle remains: the flush below is
		// the last underlying write and none can follow it after Close
		// returns.
		w.inFlight.Wait()

		var flushErr error
		if f, ok := w.out.(interface{ Flush() error }); ok {
			flushErr = f.Flush()
		}
		// Sync is best-effort: pipes and redirected streams reject it on Windows.
		if s, ok := w.out.(interface{ Sync() error }); ok {
			_ = s.Sync()
		}
		w.closeErr = flushErr
	})
	// A second concurrent Close waits for the same completed drain and
	// returns the same recorded result — never an early nil.
	<-w.closeDone
	return w.closeErr
}

// snapshotState returns a stable template pointer for lock-free rendering.
// The template is replaced wholesale under mu (never mutated in place after
// the writer is published), so holding the returned pointer is safe once
// acquired. Style/KeyValues/Bullet read through this so a concurrent SetTheme
// cannot race them. Write paths that can block in user callbacks use admit()
// instead, which pairs the same snapshot with in-flight registration.
func (w *ConsoleWriter) snapshotState() *Template {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.template
}

// SetTheme replaces the active theme. useColours is re-derived from the
// construction-time colour permission (NO_COLOR / FORCE_COLOR / terminal
// detection) and the new theme's own colour content — never from the previous
// theme's useColours value, so a mono-to-coloured swap restores colour when
// permission allows (including FORCE_COLOR on a non-terminal). An explicit
// WithColour(false) on the owning logger survives every swap. Theme changes
// never affect trust.
func (w *ConsoleWriter) SetTheme(theme *Theme) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.theme = theme
	// Build a NEW Template rather than mutating the existing one in-place;
	// WriteSecure and snapshotState read the template pointer under the lock
	// and then read its fields outside the lock, so mutating the pointed-at
	// struct would be a data race on those concurrent read paths.
	themeHasColour := theme != nil && !theme.noColour
	var newTmpl Template
	if w.template != nil {
		newTmpl = *w.template // copy all fields
	}
	newTmpl.useColours = !w.colourExplicitlyDisabled && w.colourAllowed && themeHasColour
	w.template = &newTmpl
	w.cacheLevelColours()
}

func (w *ConsoleWriter) IsTTY() bool {
	return w.isTTY
}

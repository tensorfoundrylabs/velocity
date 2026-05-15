package velocity

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unsafe"
)

// continuationGlyph is the Unicode box-drawing pipe used on continuation lines.
// It renders cleanly in every modern terminal and editor — far more common than
// some full-block alternatives, and visually distinct from the ASCII pipe character.
const continuationGlyph = "│"

// continuationGlyphSep is the glyph followed by a single space, written before each line.
const continuationGlyphSep = continuationGlyph + " "

// continuationKey is the JSON field name for the continuation lines array.
const continuationKey = "continuation"

// osc8Open and osc8Close are the byte sequences that delimit an OSC 8 hyperlink.
// Format: \x1b]8;;<uri>\x07<text>\x1b]8;;\x07
// We strip these from JSON because log aggregators and structured pipelines have
// no use for terminal control sequences and must not receive raw ESC bytes.
const (
	osc8Open  = "\x1b]8;;"
	osc8Close = "\x1b]8;;\x07"
)

// stripOSC8 removes OSC 8 hyperlink escape sequences from s, leaving only the
// visible display text.  A compliant OSC 8 sequence looks like:
//
//	\x1b]8;;<uri>\x07<text>\x1b]8;;\x07
//
// When no sequences are present the original string is returned without allocation.
func stripOSC8(s string) string {
	if !strings.Contains(s, osc8Open) {
		return s
	}

	var b strings.Builder
	for len(s) > 0 {
		idx := strings.Index(s, osc8Open)
		if idx < 0 {
			b.WriteString(s)
			break
		}
		// Write everything before the opening escape.
		b.WriteString(s[:idx])
		s = s[idx+len(osc8Open):]

		// Skip to the \x07 that terminates the URI part, then the display text begins.
		bell := strings.IndexByte(s, '\x07')
		if bell < 0 {
			// Malformed sequence — emit the remainder as-is.
			b.WriteString(s)
			break
		}
		s = s[bell+1:] // advance past <uri>\x07

		// Collect display text up to the closing osc8Close sequence.
		end := strings.Index(s, osc8Close)
		if end < 0 {
			// No closing tag — treat the rest as visible text.
			b.WriteString(s)
			break
		}
		b.WriteString(s[:end])
		s = s[end+len(osc8Close):]
	}
	return b.String()
}

// ContinuationBlock is a Renderable that displays a primary log line followed by
// continuation lines prefixed with a │ glyph and indented to the message column.
// Designed for structured "server started at <URL>"-style output that needs both
// the log discipline of a proper entry and the readability of multi-line display.
//
// On TTY the glyph is coloured with SlotContinuation; on non-TTY the same Unicode
// glyph is used without colour — keeping visual parity across pipe and terminal
// while only the decoration differs. TTY detection happens at Render time via
// IsTerminalWriter, so the same block may be rendered to both a terminal and a file.
//
// In JSON the continuation lines are emitted as a "continuation" array. Any OSC 8
// hyperlink sequences in the lines are stripped from the JSON form because log
// aggregators cannot render terminal control sequences.
type ContinuationBlock struct {
	theme *Theme
	msg   string
	lines []string
}

// NewContinuationBlock constructs a ContinuationBlock. theme may be nil (falls
// back to ThemeNightOwl). TTY detection is deferred to Render time — callers do
// not need to pass isTTY.
func NewContinuationBlock(msg string, lines []string, theme *Theme) *ContinuationBlock {
	if theme == nil {
		theme = ThemeNightOwl
	}
	ls := make([]string, len(lines))
	copy(ls, lines)
	return &ContinuationBlock{
		msg:   msg,
		lines: ls,
		theme: theme,
	}
}

// Render writes the continuation block to w. TTY is detected from w at call time:
// when w is a real terminal the SlotContinuation glyph is coloured; otherwise plain.
// The first line is the message; subsequent lines follow with the │ prefix and indent.
//
// Note: when called via Logger.Render the writer is an intermediate buffer.
// Logger.Render detects TTYRenderable and calls RenderTTY with the correct TTY state.
func (c *ContinuationBlock) Render(w io.Writer) error {
	if c == nil {
		return nil
	}
	return c.RenderTTY(w, IsTerminalWriter(w))
}

// RenderTTY writes the continuation block to w with explicit TTY state. Callers
// that already know the terminal state (e.g. Logger.Render) should use this to
// avoid false-negative TTY detection on intermediate buffers.
func (c *ContinuationBlock) RenderTTY(w io.Writer, isTTY bool) error {
	if c == nil {
		return nil
	}
	var buf bytes.Buffer
	if isTTY {
		renderContinuationTTY(&buf, c.msg, c.lines, c.theme)
	} else {
		renderContinuationPlain(&buf, c.msg, c.lines)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// String renders the block to a string. Useful in tests and for capture.
func (c *ContinuationBlock) String() string {
	if c == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = c.Render(&buf)
	return buf.String()
}

// renderContinuationTTY builds the ANSI form: coloured message, then each
// continuation line prefixed with a SlotContinuation-coloured │ glyph.
func renderContinuationTTY(buf *bytes.Buffer, msg string, lines []string, theme *Theme) {
	// Header message, coloured.
	msgCode := theme.CachedMessageFg()
	if msgCode != "" {
		buf.WriteString(msgCode)
	}
	buf.WriteString(msg)
	if msgCode != "" {
		buf.WriteString(theme.ResetStr())
	}
	buf.WriteByte('\n')

	// Continuation lines: │ glyph (SlotContinuation), space, line text.
	glyphPrefix, glyphSuffix := theme.Wrap(SlotContinuation)
	for _, line := range lines {
		buf.WriteString(glyphPrefix)
		buf.WriteString(continuationGlyphSep)
		buf.WriteString(glyphSuffix)
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
}

// renderContinuationPlain builds the non-ANSI form. The same Unicode │ glyph is
// kept — it is a printable character that renders in any modern terminal, editor,
// or log file viewer. Only the colour wrapper is omitted. This maintains visual
// parity with the TTY form without emitting ANSI control bytes.
func renderContinuationPlain(buf *bytes.Buffer, msg string, lines []string) {
	buf.WriteString(msg)
	buf.WriteByte('\n')
	for _, line := range lines {
		buf.WriteString(continuationGlyphSep)
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
}

// continuationLinesField constructs a Field carrying a []string slice.
// One heap alloc per Logger.Continue call; entries that never call Continue pay nothing.
func continuationLinesField(lines []string) Field {
	cp := make([]string, len(lines))
	copy(cp, lines)
	return Field{
		Key:   continuationKey,
		Type:  FieldTypeContinuationLines,
		value: unsafe.Pointer(&cp), //nolint:gosec // G103: same unsafe.Pointer pattern used throughout field.go
	}
}

// continuationLinesFromField recovers the []string stored in a FieldTypeContinuationLines field.
// Returns nil if f is not of that type.
func continuationLinesFromField(f Field) []string {
	if f.Type != FieldTypeContinuationLines || f.value == nil {
		return nil
	}
	return *(*[]string)(f.value)
}

// Continue logs a primary message at the given level, then emits each of lines
// as a continuation line prefixed with a │ glyph indented to the message column.
//
// On a TTY console the output looks like:
//
//	2006-01-02T15:04:05+10:00 [INFO] HTTP server listening
//	                                  │ Available at http://localhost:8080
//	                                  │ Press Ctrl+C to stop
//
// The JSON writer emits a single entry with a "continuation" array. OSC 8
// hyperlink escape sequences in the lines are stripped from JSON output —
// log aggregators cannot render terminal control sequences.
//
// All standard log-call semantics apply: level filtering, sampling, base fields.
func (l *Logger) Continue(level Level, msg string, lines ...string) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", level.ConciseLabel(), msg)
		for _, line := range lines {
			fmt.Fprintf(os.Stderr, "  %s %s\n", continuationGlyph, line)
		}
		return
	}
	if l.closed.Load() || !l.isEnabled(level) {
		return
	}
	l.logContinue(level, msg, lines)
}

// logContinue is the internal implementation of Continue.
func (l *Logger) logContinue(level Level, msg string, lines []string) {
	if l == nil {
		return
	}

	if l.sampler != nil && !l.sampler.Sample(level, msg) {
		return
	}

	entry := GetEntry()
	defer entry.Release()

	entry.SetLevel(level)
	entry.SetMessage(msg)
	entry.SetTime(time.Now())
	entry.forceTreeDisplay = l.forceTreeDisplay

	if l.writers.scanSecure.Load() && strings.IndexByte(msg, '<') >= 0 {
		entry.maybeSecure = true
	}

	if len(l.baseFields) > 0 {
		entry.WithFields(l.baseFields...)
	}

	l.captureCaller(entry, 0)

	if l.cfg != nil {
		if level >= l.cfg.ConsoleLevel && l.consoleWriter != nil {
			if err := l.consoleWriter.WriteContinue(entry, lines); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
			}
		}

		if level >= l.cfg.StructuredLevel && l.jsonWriter != nil {
			if err := l.jsonWriter.WriteContinue(entry, lines); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
			}
		}

		entry.Write()

		l.writers.mu.RLock()
		if l.writers.mw != nil {
			// Additional writers receive a typed field so they can render the lines
			// if they choose. Writers that don't understand FieldTypeContinuationLines
			// emit "[N lines]" as a fallback hint (see writeFormatted).
			entry.WithFields(continuationLinesField(lines))
			_ = l.writers.mw.Write(entry)
		}
		l.writers.mu.RUnlock()
		return
	}

	entry.Write()
}

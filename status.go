package velocity

import (
	"bytes"
	"io"
	"strings"
)

// StatusKind identifies the semantic outcome of an operation for StatusItem rendering.
type StatusKind uint8

const (
	StatusOK      StatusKind = iota // positive outcome
	StatusFail                      // failure / error
	StatusWarn                      // degraded / warning
	StatusInfo                      // informational
	StatusPending                   // in-progress / not yet resolved
	StatusSkipped                   // intentionally bypassed
)

// String returns the canonical label for a StatusKind.
// Used as the JSON field value ("ok", "fail", etc.) when lowercased, and as
// the badge text in console output.
func (k StatusKind) String() string {
	switch k {
	case StatusOK:
		return "OK"
	case StatusFail:
		return "FAIL"
	case StatusWarn:
		return "WARN"
	case StatusInfo:
		return "INFO"
	case StatusPending:
		return "PENDING"
	case StatusSkipped:
		return "SKIP"
	default:
		return "INFO"
	}
}

// statusJSONValue returns the lowercase JSON field value for the status.
func (k StatusKind) statusJSONValue() string {
	switch k {
	case StatusOK:
		return statusJSONOK
	case StatusFail:
		return statusJSONFail
	case StatusWarn:
		return statusJSONWarn
	case StatusInfo:
		return statusJSONInfo
	case StatusPending:
		return statusJSONPending
	case StatusSkipped:
		return statusJSONSkip
	default:
		return statusJSONInfo
	}
}

// Slot maps a StatusKind to the theme StyleSlot used to colour the badge text.
// StatusPending reuses SlotStatusInfo (no dedicated slot — close semantic fit).
// StatusSkipped reuses SlotMuted (de-emphasised, intentionally bypassed).
func (k StatusKind) Slot() StyleSlot {
	switch k {
	case StatusOK:
		return SlotStatusOK
	case StatusFail:
		return SlotStatusFail
	case StatusWarn:
		return SlotStatusWarn
	case StatusInfo:
		return SlotStatusInfo
	case StatusPending:
		return SlotStatusInfo
	case StatusSkipped:
		return SlotMuted
	default:
		return SlotStatusInfo
	}
}

// statusJSONOK etc. are kept as constants to satisfy the goconst linter — these
// values appear across String(), statusJSONValue(), and Slot() switch arms.
const (
	statusJSONOK      = "ok"
	statusJSONFail    = "fail"
	statusJSONWarn    = "warn"
	statusJSONInfo    = "info"
	statusJSONPending = "pending"
	statusJSONSkip    = "skip"
)

// statusKindNone is the sentinel stored in Entry.statusKind when no Status call was made.
// Using 0xFF rather than a zero value lets StatusOK (0) remain a valid status.
// The sentinel is package-private — callers never see it.
const statusKindNone StatusKind = 0xFF

// statusBadgeWidth is the fixed visible width of the status token inside the
// brackets. Sized to PENDING (7 chars), the longest token, so all badges align.
// Badge format: '[' + 7-char padded token + ']' = 9 visible chars.
const statusBadgeWidth = 7

// statusBadgeSep is the separator between the badge and the message text.
const statusBadgeSep = "   "

// StatusItem is a Renderable that displays an outcome badge followed by a message
// and optional structured fields. On TTY it renders a coloured [ OK ] style badge;
// on non-TTY and in JSON output the badge becomes a structured "status" field.
type StatusItem struct {
	theme  *Theme
	msg    string
	fields []Field
	kind   StatusKind
	isTTY  bool
}

// NewStatusItem constructs a StatusItem. theme may be nil (falls back to ThemeNightOwl).
// isTTY controls whether the coloured badge or the plain text form is used when
// Render is called directly; Logger.Status determines this from its console writer.
func NewStatusItem(kind StatusKind, msg string, theme *Theme, isTTY bool, fields ...Field) *StatusItem {
	if theme == nil {
		theme = ThemeNightOwl
	}
	s := &StatusItem{
		kind:   kind,
		msg:    msg,
		theme:  theme,
		isTTY:  isTTY,
		fields: make([]Field, len(fields)),
	}
	copy(s.fields, fields)
	return s
}

// Render writes the status item to w. On TTY it emits the coloured badge form;
// otherwise it emits a plain-text form with no ANSI escapes.
// The trailing newline is always written so consecutive StatusItems align without
// the caller having to manage spacing.
func (s *StatusItem) Render(w io.Writer) error {
	if s == nil {
		return nil
	}

	var buf bytes.Buffer
	if s.isTTY {
		renderStatusItemTTY(&buf, s.kind, s.msg, s.theme, s.fields)
	} else {
		renderStatusItemPlain(&buf, s.kind, s.msg, s.fields)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// String renders the status item to a string. Useful in tests and for capture.
func (s *StatusItem) String() string {
	if s == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = s.Render(&buf)
	return buf.String()
}

// renderStatusItemTTY builds the ANSI badge line into buf.
// Format: '[' + <coloured-padded-token> + ']' + "   " + message + fields
func renderStatusItemTTY(buf *bytes.Buffer, kind StatusKind, msg string, theme *Theme, fields []Field) {
	token := kind.String()
	slot := kind.Slot()

	// Unstyled left bracket.
	buf.WriteByte('[')

	// Coloured token padded to statusBadgeWidth, left-justified.
	prefix, suffix := theme.Wrap(slot)
	buf.WriteString(prefix)
	buf.WriteString(token)
	// Pad with spaces so all tokens occupy the same width.
	if pad := statusBadgeWidth - len(token); pad > 0 {
		buf.WriteString(strings.Repeat(" ", pad))
	}
	buf.WriteString(suffix)

	// Unstyled right bracket + separator.
	buf.WriteByte(']')
	buf.WriteString(statusBadgeSep)

	// Message.
	msgCode := theme.CachedMessageFg()
	if msgCode != "" {
		buf.WriteString(msgCode)
	}
	buf.WriteString(msg)
	if msgCode != "" {
		buf.WriteString(Reset)
	}

	// Fields rendered inline with key/value colours from the theme.
	writeStatusFields(buf, fields, theme, true)

	buf.WriteByte('\n')
}

// renderStatusItemPlain builds the non-ANSI form. The badge becomes plain text
// so the output is grep-friendly in pipes and log files.
func renderStatusItemPlain(buf *bytes.Buffer, kind StatusKind, msg string, fields []Field) {
	token := kind.String()
	buf.WriteByte('[')
	buf.WriteString(token)
	if pad := statusBadgeWidth - len(token); pad > 0 {
		buf.WriteString(strings.Repeat(" ", pad))
	}
	buf.WriteByte(']')
	buf.WriteString(statusBadgeSep)
	buf.WriteString(msg)
	writeStatusFields(buf, fields, nil, false)
	buf.WriteByte('\n')
}

// writeStatusFields appends inline key=value pairs to buf.
// Strings and errors are quoted for readability; numerics are written raw.
// When themed and useColours is true, field keys and values are coloured.
func writeStatusFields(buf *bytes.Buffer, fields []Field, theme *Theme, useColours bool) {
	for _, f := range fields {
		buf.WriteByte(' ')

		keyCode := ""
		valCode := ""
		if useColours && theme != nil {
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

		buf.WriteByte('=')

		if valCode != "" {
			buf.WriteString(valCode)
		}

		// Quote string-like types to match the console writer convention.
		switch f.Type {
		case FieldTypeString, FieldTypeError, FieldTypeStringer, FieldTypeTruncated:
			buf.WriteByte('"')
			f.writeFormatted(buf)
			buf.WriteByte('"')
		default:
			f.writeFormatted(buf)
		}

		if valCode != "" {
			buf.WriteString(Reset)
		}
	}
}

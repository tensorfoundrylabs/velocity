package velocity

import (
	"bytes"
	"io"
	"strings"
	"unsafe"
)

// GroupItem is one entry in a Group block. Marker is an optional prefix glyph
// (e.g. "├─", "✓", "•"); if empty, the renderer supplies a default tree glyph
// and promotes the last item to "└─" automatically.
type GroupItem struct {
	Marker string
	Text   string
}

// Group is a Renderable that emits a count-headed block:
//
//	INFO  Registering routes (3)
//	        ├─ GET  /api/v1/users
//	        ├─ POST /api/v1/users
//	        └─ GET  /api/v1/users/:id
//
// On TTY the count token is coloured with SlotCount; items are indented past the
// message column. On non-TTY the count token is plain text. JSON output emits a
// single entry with "count" and "items" fields — markers are visual-only and
// are stripped from JSON.
type Group struct {
	theme *Theme
	msg   string
	items []GroupItem
	isTTY bool
}

// NewGroup constructs a Group. theme may be nil (falls back to ThemeNightOwl).
// isTTY controls whether ANSI codes are emitted; Logger.Group sets this from its
// console writer.
func NewGroup(msg string, items []GroupItem, theme *Theme, isTTY bool) *Group {
	if theme == nil {
		theme = ThemeNightOwl
	}
	// Copy so the caller's slice is safe to mutate after the call.
	its := make([]GroupItem, len(items))
	copy(its, items)
	return &Group{
		msg:   msg,
		items: its,
		theme: theme,
		isTTY: isTTY,
	}
}

// Render writes the group block to w. The header line carries the message and
// count; each item follows on its own indented line.
func (g *Group) Render(w io.Writer) error {
	if g == nil {
		return nil
	}
	var buf bytes.Buffer
	if g.isTTY {
		renderGroupTTY(&buf, g.msg, g.items, g.theme)
	} else {
		renderGroupPlain(&buf, g.msg, g.items)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// String renders the group to a string. Useful in tests and for capture.
func (g *Group) String() string {
	if g == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = g.Render(&buf)
	return buf.String()
}

// renderGroupTTY writes the ANSI form: coloured count token, indented item lines.
func renderGroupTTY(buf *bytes.Buffer, msg string, items []GroupItem, theme *Theme) {
	// Header: message + " (" + coloured count + ")"
	msgCode := theme.CachedMessageFg()
	if msgCode != "" {
		buf.WriteString(msgCode)
	}
	buf.WriteString(msg)
	if msgCode != "" {
		buf.WriteString(Reset)
	}
	buf.WriteString(" (")
	countPrefix, countSuffix := theme.Wrap(SlotCount)
	buf.WriteString(countPrefix)
	var tmp [20]byte
	n := formatInt(tmp[:], int64(len(items)))
	buf.Write(tmp[:n])
	buf.WriteString(countSuffix)
	buf.WriteByte(')')
	buf.WriteByte('\n')

	// Item lines.
	writeGroupConsoleTTYItems(buf, items, theme)
}

// renderGroupPlain writes a non-ANSI form suitable for pipes and log files.
func renderGroupPlain(buf *bytes.Buffer, msg string, items []GroupItem) {
	buf.WriteString(msg)
	buf.WriteString(" (")
	var tmp [20]byte
	n := formatInt(tmp[:], int64(len(items)))
	buf.Write(tmp[:n])
	buf.WriteByte(')')
	buf.WriteByte('\n')

	writeGroupConsoleItems(buf, items)
}

// resolvedMarker returns the item's explicit marker, or a default tree glyph.
// When the marker is empty and there are multiple items, the last gets "└─".
func resolvedMarker(marker string, idx, total int) string {
	if marker != "" {
		return marker
	}
	if total > 1 && idx == total-1 {
		return groupLastGlyph
	}
	return groupDefaultGlyph
}

// groupItemIndent is the per-item prefix before the marker. When rendered via
// Logger.Group the full message-column indent is prepended separately via
// indentLines, so item content lands flush under the log message text.
const groupItemIndent = "  "

const (
	groupDefaultGlyph = "├─"
	groupLastGlyph    = "└─"
)

// groupCountKey and groupItemsKey are the JSON field names emitted by Logger.Group.
const (
	groupCountKey = "count"
	groupItemsKey = "items"
)

// writeGroupConsoleTTYItems writes ANSI-coloured item lines into buf.
func writeGroupConsoleTTYItems(buf *bytes.Buffer, items []GroupItem, theme *Theme) {
	keyCode := theme.CachedFieldKeyFg()
	msgCode := theme.CachedMessageFg()
	for i, item := range items {
		marker := resolvedMarker(item.Marker, i, len(items))
		buf.WriteString(groupItemIndent)
		if keyCode != "" {
			buf.WriteString(keyCode)
		}
		buf.WriteString(marker)
		buf.WriteByte(' ')
		if keyCode != "" {
			buf.WriteString(Reset)
		}
		if msgCode != "" {
			buf.WriteString(msgCode)
		}
		buf.WriteString(item.Text)
		if msgCode != "" {
			buf.WriteString(Reset)
		}
		buf.WriteByte('\n')
	}
}

// writeGroupConsoleItems writes plain (non-ANSI) item lines into buf.
func writeGroupConsoleItems(buf *bytes.Buffer, items []GroupItem) {
	for i, item := range items {
		marker := resolvedMarker(item.Marker, i, len(items))
		buf.WriteString(groupItemIndent)
		buf.WriteString(marker)
		buf.WriteByte(' ')
		buf.WriteString(item.Text)
		buf.WriteByte('\n')
	}
}

// groupMsgWithCount builds the composite header string "msg (N)" used as the
// Entry.Message when routing through the standard log pipeline. The count is
// styled at render time; this is the plain-text form for the structured channel.
func groupMsgWithCount(msg string, count int) string {
	var sb strings.Builder
	sb.WriteString(msg)
	sb.WriteString(" (")
	var tmp [20]byte
	n := formatInt(tmp[:], int64(count))
	sb.Write(tmp[:n])
	sb.WriteByte(')')
	return sb.String()
}

// groupItemsField constructs a Field that carries a []GroupItem slice.
// One heap alloc per Logger.Group call; entries that never call Group pay nothing.
// The key is set to groupItemsKey so JSON writers can use it directly.
func groupItemsField(items []GroupItem) Field {
	// Copy so the caller's variadic slice is safe to mutate after return.
	cp := make([]GroupItem, len(items))
	copy(cp, items)
	return Field{
		Key:   groupItemsKey,
		Type:  FieldTypeGroupItems,
		value: unsafe.Pointer(&cp), //nolint:gosec // G103: same unsafe.Pointer pattern used throughout field.go
	}
}

// groupItemsFromField recovers the []GroupItem stored in a FieldTypeGroupItems field.
// Returns nil if f is not of that type.
func groupItemsFromField(f Field) []GroupItem {
	if f.Type != FieldTypeGroupItems || f.value == nil {
		return nil
	}
	return *(*[]GroupItem)(f.value)
}

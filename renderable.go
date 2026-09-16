package velocity

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/rivo/uniseg"
)

// Renderable is implemented by any value that can write a formatted representation
// of itself to an io.Writer.
//
// The primary use is Logger.Render and Logger.RenderRaw, which route Renderable
// values through the console writer with appropriate indentation.
// JSON writers silently ignore Render calls since they write structured data.
type Renderable interface {
	Render(w io.Writer) error
}

// TTYRenderable is an optional extension to Renderable for types that need to
// know whether the destination is a TTY before choosing between ANSI and plain
// output. Logger.Render checks for this interface and passes the console writer's
// resolved TTY state (which accounts for FORCE_COLOR / NO_COLOR env vars and
// fd-level detection), so rendering decisions are consistent with how the rest
// of the log line was formatted.
//
// Types that implement this interface should NOT call IsTerminalWriter on the
// supplied io.Writer — they should use the isTTY argument instead, because the
// writer is an intermediate buffer, not the final output sink.
type TTYRenderable interface {
	Renderable
	RenderTTY(w io.Writer, isTTY bool) error
}

// StyledRenderable is an optional extension to Renderable for types that need
// STYLING (resolved colour permission) and TRUST (actual terminal destination)
// as separate inputs. Logger.Render checks for this interface first and passes
// both bits from the console writer's runtime state:
//
//   - styled comes from the template's useColours — the resolved
//     !colourExplicitlyDisabled && colourAllowed && themeHasColour — so
//     WithColour(false) and NO_COLOR suppress ANSI and FORCE_COLOR permits it
//     even on non-terminals, exactly matching ordinary log lines;
//   - trusted is the actual terminal classification and is never influenced
//     by colour or theme choices: secure plaintext stays visible on a trusted
//     terminal even when styling is off.
//
// Implementations that only care about terminal-ness can keep TTYRenderable;
// types where colour and content visibility must diverge (StatusItem) need
// this one. The F4 finding in the finish review: StatusItem previously
// selected its coloured path from the TTY bool alone, so explicit no-colour
// was ignored on terminals.
type StyledRenderable interface {
	Renderable
	RenderStyled(w io.Writer, styled, trusted bool) error
}

// Box-drawing constants shared by tree, box, and table renderers.
const (
	treeBranch = "├─ "
	treeCorner = "└─ "
	treePipe   = "│   "
	treeBlank  = "    "
)

// TreeItem represents a node in a hierarchical display tree.
type TreeItem struct {
	Key      string
	Value    any
	Children []TreeItem
}

// KeyValuePair is a labelled string value used in SystemInfo display blocks.
type KeyValuePair struct {
	Key   string
	Value string
}

// SystemInfoData is startup/configuration metadata for display via SystemInfo.
// Renamed from SystemInfo to free that name for the Renderable type.
type SystemInfoData struct {
	Title   string
	Version string
	Fields  []KeyValuePair
}

// Box holds the configuration for a bordered box render.
type Box struct {
	theme   *Theme
	title   string
	content string
}

// NewBox returns a Box ready to render.
func NewBox(title, content string, theme *Theme) *Box {
	if theme == nil {
		theme = ThemeNightOwl
	}
	return &Box{title: title, content: content, theme: theme}
}

// Render writes the bordered box (title + content) to w.
func (b *Box) Render(w io.Writer) error {
	buf := GetBuffer(512)
	defer PutBuffer(buf)
	renderBox(buf, b.theme, b.title, b.content)
	_, err := buf.WriteTo(w)
	return err
}

// String renders the box to a string — useful for tests and capture.
func (b *Box) String() string {
	var buf bytes.Buffer
	_ = b.Render(&buf)
	return buf.String()
}

func renderBox(buf *bytes.Buffer, theme *Theme, title, content string) {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	maxLineCells := 0
	for _, line := range lines {
		if n := visibleLen(line); n > maxLineCells {
			maxLineCells = n
		}
	}

	width := max(maxLineCells+4, 42)
	if titleWidth := visibleLen(title) + 6; titleWidth > width {
		width = titleWidth
	}

	topFill := width - 2 - 1
	if title != "" {
		topFill -= visibleLen(title) + 1
	}

	buf.WriteString(theme.CachedFieldKeyFg())
	buf.WriteString("┌─")
	if title != "" {
		buf.WriteString(title)
		buf.WriteString("─")
	}
	buf.WriteString(strings.Repeat("─", topFill))
	buf.WriteString("┐")
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")

	for _, line := range lines {
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString("│ ")
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedMessageFg())
		writePaddedVisible(buf, line, width-3)
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString("│")
		buf.WriteString(theme.ResetStr())
		buf.WriteString("\n")
	}

	buf.WriteString(theme.CachedFieldKeyFg())
	buf.WriteString("└")
	buf.WriteString(strings.Repeat("─", width-2))
	buf.WriteString("┘")
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")
}

// Table holds the configuration for a table render.
type Table struct {
	theme   *Theme
	headers []string
	rows    [][]string
}

// NewTable returns a Table ready to render.
func NewTable(headers []string, rows [][]string, theme *Theme) *Table {
	if theme == nil {
		theme = ThemeNightOwl
	}
	return &Table{headers: headers, rows: rows, theme: theme}
}

// Render writes the aligned table with auto-sized columns to w.
// Returns nil without writing if headers or rows are empty.
func (t *Table) Render(w io.Writer) error {
	if len(t.headers) == 0 || len(t.rows) == 0 {
		return nil
	}
	buf := GetBuffer(1024)
	defer PutBuffer(buf)
	renderTable(buf, t.theme, t.headers, t.rows)
	_, err := buf.WriteTo(w)
	return err
}

// String renders the table to a string — useful for tests and capture.
func (t *Table) String() string {
	var buf bytes.Buffer
	_ = t.Render(&buf)
	return buf.String()
}

func renderTable(buf *bytes.Buffer, theme *Theme, headers []string, rows [][]string) {
	colWidths := calcColumnWidths(headers, rows)
	writeTableTopBorder(buf, theme, colWidths)
	writeTableHeaders(buf, theme, headers, colWidths)
	writeTableHeaderSeparator(buf, theme, colWidths)
	for _, row := range rows {
		writeTableRow(buf, theme, row, colWidths)
	}
	writeTableBottomBorder(buf, theme, colWidths)
}

func calcColumnWidths(headers []string, rows [][]string) []int {
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = visibleLen(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(colWidths) {
				if vl := visibleLen(cell); vl > colWidths[i] {
					colWidths[i] = vl
				}
			}
		}
	}
	return colWidths
}

func writeTableTopBorder(buf *bytes.Buffer, theme *Theme, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, w := range colWidths {
		buf.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┬")
		}
	}
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")
}

func writeTableHeaders(buf *bytes.Buffer, theme *Theme, headers []string, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, header := range headers {
		if i > 0 {
			buf.WriteString("│")
		}
		buf.WriteString(" ")
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedTableHeaderFg())
		writePaddedVisible(buf, header, colWidths[i])
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString(" ")
	}
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")
}

func writeTableHeaderSeparator(buf *bytes.Buffer, theme *Theme, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, w := range colWidths {
		buf.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┼")
		}
	}
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")
}

func writeTableRow(buf *bytes.Buffer, theme *Theme, row []string, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i := range colWidths {
		// Absent cells pad to the declared column geometry so short rows keep
		// the borders and column edges intact; extra cells beyond the header
		// count are dropped, matching the previous behaviour.
		var cell string
		if i < len(row) {
			cell = row[i]
		}
		buf.WriteString(" ")
		buf.WriteString(theme.CachedMessageFg())
		writePaddedVisible(buf, cell, colWidths[i])
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString(" ")
		if i < len(colWidths)-1 {
			buf.WriteString("│")
		}
	}
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")
}

func writeTableBottomBorder(buf *bytes.Buffer, theme *Theme, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, w := range colWidths {
		buf.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┴")
		}
	}
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")
}

// Banner holds the configuration for a double-border banner box render.
type Banner struct {
	theme *Theme
	text  string
}

// NewBanner returns a Banner ready to render.
func NewBanner(text string, theme *Theme) *Banner {
	if theme == nil {
		theme = ThemeNightOwl
	}
	return &Banner{text: text, theme: theme}
}

// Render writes the double-border banner box to w.
func (b *Banner) Render(w io.Writer) error {
	buf := GetBuffer(512)
	defer PutBuffer(buf)
	renderBanner(buf, b.theme, b.text)
	_, err := buf.WriteTo(w)
	return err
}

// String renders the banner to a string — useful for tests and capture.
func (b *Banner) String() string {
	var buf bytes.Buffer
	_ = b.Render(&buf)
	return buf.String()
}

func renderBanner(buf *bytes.Buffer, theme *Theme, text string) {
	lines := strings.Split(text, "\n")

	maxLen := 0
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
		if n := visibleLen(lines[i]); n > maxLen {
			maxLen = n
		}
	}

	contentWidth := maxLen
	boxWidth := contentWidth + 2

	// Use a consistent single-line box-drawing set throughout (┌─┐│└┘).
	// The previous code mixed double corners (╔╗╚╝) with single-line fills (─│),
	// which looks broken in terminals and fonts that render them at different weights.
	buf.WriteString(theme.CachedFieldKeyFg())
	buf.WriteString("┌")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("┐")
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")

	for _, line := range lines {
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString("│ ")
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedMessageFg())
		writePaddedVisible(buf, line, contentWidth)
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString(" │")
		buf.WriteString(theme.ResetStr())
		buf.WriteString("\n")
	}

	buf.WriteString(theme.CachedFieldKeyFg())
	buf.WriteString("└")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("┘")
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")
}

// Tree holds tree nodes for rendering.
type Tree struct {
	theme *Theme
	nodes []TreeItem
}

// NewTree returns a Tree ready to render.
func NewTree(nodes []TreeItem, theme *Theme) *Tree {
	if theme == nil {
		theme = ThemeNightOwl
	}
	return &Tree{nodes: nodes, theme: theme}
}

// Render writes the tree hierarchy with box-drawing connectors to w.
func (t *Tree) Render(w io.Writer) error {
	buf := GetBuffer(512)
	defer PutBuffer(buf)
	for i, node := range t.nodes {
		writeTreeItemInto(buf, t.theme, node, "", i == len(t.nodes)-1)
	}
	_, err := buf.WriteTo(w)
	return err
}

// String renders the tree to a string — useful for tests and capture.
func (t *Tree) String() string {
	var buf bytes.Buffer
	_ = t.Render(&buf)
	return buf.String()
}

func writeTreeItemInto(buf *bytes.Buffer, theme *Theme, node TreeItem, prefix string, isLast bool) {
	connector := treeBranch
	if isLast {
		connector = treeCorner
	}

	buf.WriteString(prefix)
	buf.WriteString(connector)
	buf.WriteString(theme.CachedMessageFg())
	if node.Value != nil {
		_, _ = fmt.Fprintf(buf, "%s: %v", node.Key, node.Value)
	} else {
		buf.WriteString(node.Key)
	}
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")

	childPrefix := prefix
	if isLast {
		childPrefix += treeBlank
	} else {
		childPrefix += treePipe
	}

	for i, child := range node.Children {
		writeTreeItemInto(buf, theme, child, childPrefix, i == len(node.Children)-1)
	}
}

// KeyValue holds a key-value pair for rendering.
type KeyValue struct {
	theme *Theme
	key   string
	value string
}

// NewKeyValue returns a KeyValue ready to render.
func NewKeyValue(key, value string, theme *Theme) *KeyValue {
	if theme == nil {
		theme = ThemeNightOwl
	}
	return &KeyValue{key: key, value: value, theme: theme}
}

// Render writes "key: value\n" with theme colouring to w.
func (kv *KeyValue) Render(w io.Writer) error {
	buf := GetBuffer(128)
	defer PutBuffer(buf)
	buf.WriteString(kv.theme.CachedFieldKeyFg())
	buf.WriteString(kv.key)
	buf.WriteString(kv.theme.ResetStr())
	buf.WriteString(": ")
	buf.WriteString(kv.theme.CachedFieldValFg())
	buf.WriteString(kv.value)
	buf.WriteString(kv.theme.ResetStr())
	buf.WriteString("\n")
	_, err := buf.WriteTo(w)
	return err
}

// String renders the key-value pair to a string — useful for tests and capture.
func (kv *KeyValue) String() string {
	var buf bytes.Buffer
	_ = kv.Render(&buf)
	return buf.String()
}

// SystemInfo holds system info metadata for rendering.
type SystemInfo struct {
	theme *Theme
	info  *SystemInfoData
}

// NewSystemInfo returns a SystemInfo ready to render.
func NewSystemInfo(info *SystemInfoData, theme *Theme) *SystemInfo {
	if theme == nil {
		theme = ThemeNightOwl
	}
	return &SystemInfo{info: info, theme: theme}
}

// Render writes the titled block of key-value system info pairs to w.
// Returns nil without writing if info is nil.
func (s *SystemInfo) Render(w io.Writer) error {
	if s.info == nil {
		return nil
	}
	buf := GetBuffer(512)
	defer PutBuffer(buf)

	if s.info.Title != "" {
		buf.WriteString(s.theme.CachedInfoColourFg())
		buf.WriteString("▓ ")
		buf.WriteString(s.info.Title)
		if s.info.Version != "" {
			buf.WriteString(" v")
			buf.WriteString(s.info.Version)
		}
		buf.WriteString(" ▓")
		buf.WriteString(s.theme.ResetStr())
		buf.WriteString("\n")
	}

	for _, pair := range s.info.Fields {
		buf.WriteString(s.theme.CachedFieldKeyFg())
		writePaddedVisible(buf, pair.Key+":", 20)
		buf.WriteString(s.theme.ResetStr())
		buf.WriteString(" ")
		buf.WriteString(s.theme.CachedMessageFg())
		buf.WriteString(pair.Value)
		buf.WriteString(s.theme.ResetStr())
		buf.WriteString("\n")
	}

	_, err := buf.WriteTo(w)
	return err
}

// String renders the system info block to a string — useful for tests and capture.
func (s *SystemInfo) String() string {
	var buf bytes.Buffer
	_ = s.Render(&buf)
	return buf.String()
}

// isPrintableASCII reports whether s is entirely printable ASCII (0x20–0x7E).
// Such strings occupy exactly one terminal cell per byte — the allocation-free
// common case for headers, cells, box lines and component names, so uniseg is
// only reached when non-ASCII bytes or escapes are actually present.
func isPrintableASCII(s string) bool {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// visibleLen returns the terminal cell width of s, ignoring ANSI escape
// sequences and OSC 8 hyperlink sequences.
//
// SGR escapes: ESC [ ... m  (ends on 'm')
// OSC sequences: ESC ] ... BEL  or  ESC ] ... ESC \
// Both forms are transparent to column-width arithmetic — only the visible link
// text (between the OSC 8 open/close sequences) contributes to the count.
//
// Three tiers, cheapest first: printable ASCII is one cell per byte; strings
// without ESC go straight to uniseg; strings interleaving ESC with text are
// stripped into a pooled scratch buffer and measured whole, so a grapheme
// cluster split by an embedded control sequence (a colour change between a
// base character and its combining mark, say) still counts as the single
// cluster a terminal renders. Stepping cluster-by-cluster across a skipped
// escape loses the cluster continuation and over-counts.
func visibleLen(s string) int {
	if isPrintableASCII(s) {
		return len(s)
	}
	if !strings.ContainsRune(s, '\033') {
		return uniseg.StringWidth(s)
	}
	buf := GetBuffer(len(s))
	defer PutBuffer(buf)
	writeEscapeStripped(buf, s)
	stripped := UnsafeString(buf.Bytes())
	if isPrintableASCII(stripped) {
		return buf.Len()
	}
	return uniseg.StringWidth(stripped)
}

// writeEscapeStripped copies s to buf with ANSI CSI/OSC escape sequences
// removed — the bytes a terminal actually renders.
func writeEscapeStripped(buf *bytes.Buffer, s string) {
	i := 0
	for i < len(s) {
		if s[i] != '\033' {
			_ = buf.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			// Trailing lone ESC — nothing visible follows.
			break
		}
		switch s[i+1] {
		case '[':
			// SGR / CSI sequence: skip to the final byte (0x40–0x7E).
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			if i < len(s) {
				i++
			}
		case ']':
			// OSC sequence: skip until BEL (\a) or ESC \ (ST).
			i += 2
			for i < len(s) {
				if s[i] == '\a' {
					i++
					break
				}
				if s[i] == '\033' && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default:
			// Unknown escape — skip just the ESC.
			i++
		}
	}
}

// padSpaces avoids a strings.Repeat allocation per padded cell.
const padSpaces = "                                "

// writeSpaces writes exactly n spaces to buf without allocating.
func writeSpaces(buf *bytes.Buffer, n int) {
	for n > len(padSpaces) {
		buf.WriteString(padSpaces)
		n -= len(padSpaces)
	}
	if n > 0 {
		buf.WriteString(padSpaces[:n])
	}
}

// writePaddedVisible writes s, then pads with spaces to width terminal cells.
// Text wider than width is written verbatim — callers size columns to fit.
func writePaddedVisible(buf *bytes.Buffer, s string, width int) {
	buf.WriteString(s)
	if n := width - visibleLen(s); n > 0 {
		writeSpaces(buf, n)
	}
}

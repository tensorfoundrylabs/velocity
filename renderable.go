package velocity

import (
	"bytes"
	"fmt"
	"io"
	"strings"
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

	maxLineRunes := 0
	for _, line := range lines {
		if n := len([]rune(line)); n > maxLineRunes {
			maxLineRunes = n
		}
	}

	width := max(maxLineRunes+4, 42)
	if titleWidth := len([]rune(title)) + 6; titleWidth > width {
		width = titleWidth
	}

	topFill := width - 2 - 1
	if title != "" {
		topFill -= len([]rune(title)) + 1
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
		buf.WriteString(padRightRunes(line, width-3))
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
		colWidths[i] = len(h)
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
		buf.WriteString(padRight(header, colWidths[i]))
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
	for i, cell := range row {
		if i >= len(colWidths) {
			break
		}
		buf.WriteString(" ")
		buf.WriteString(theme.CachedMessageFg())
		buf.WriteString(padRightVisible(cell, colWidths[i]))
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
		if n := len([]rune(lines[i])); n > maxLen {
			maxLen = n
		}
	}

	contentWidth := maxLen
	boxWidth := contentWidth + 2

	buf.WriteString(theme.CachedFieldKeyFg())
	buf.WriteString("╔")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("╗")
	buf.WriteString(theme.ResetStr())
	buf.WriteString("\n")

	for _, line := range lines {
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString("│ ")
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedMessageFg())
		buf.WriteString(padRightRunes(line, contentWidth))
		buf.WriteString(theme.ResetStr())
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString(" │")
		buf.WriteString(theme.ResetStr())
		buf.WriteString("\n")
	}

	buf.WriteString(theme.CachedFieldKeyFg())
	buf.WriteString("╚")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("╝")
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
		buf.WriteString(padRight(pair.Key+":", 20))
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

// visibleLen returns the number of visible runes in s, ignoring ANSI escape sequences.
func visibleLen(s string) int {
	n := 0
	inEscape := false
	for _, r := range s {
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		if r == '\033' {
			inEscape = true
			continue
		}
		n++
	}
	return n
}

// padRightVisible pads s to width based on visible rune count, accounting for ANSI codes.
func padRightVisible(s string, width int) string {
	visible := visibleLen(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

func padRight(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return s + strings.Repeat(" ", length-len(s))
}

func padRightRunes(s string, length int) string {
	runeLen := len([]rune(s))
	if runeLen >= length {
		return s
	}
	return s + strings.Repeat(" ", length-runeLen)
}

package pretty

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	velocity "github.com/tensorfoundrylabs/velocity"
)

// Box-drawing constants for tree rendering.
const (
	treeBranch = "├─ "
	treeCorner = "└─ "
	treePipe   = "│   "
	treeBlank  = "    "
)

// Pretty provides formatted output utilities for styled terminal printing.
type Pretty struct {
	writer io.Writer
	theme  *velocity.Theme
}

func New(w io.Writer, theme *velocity.Theme) *Pretty {
	if theme == nil {
		theme = velocity.ThemeNightOwl
	}
	return &Pretty{
		writer: w,
		theme:  theme,
	}
}

// Info prints an info-styled message with nil-safe fallback to stdout.
func (p *Pretty) Info(message string) {
	if p == nil {
		fmt.Println("ℹ️ " + message)
		return
	}
	p.printStyled("ℹ️", message, p.theme.InfoColour)
}

// Success prints a success-styled message with nil-safe fallback to stdout.
func (p *Pretty) Success(message string) {
	if p == nil {
		fmt.Println("✅ " + message)
		return
	}
	p.printStyled("✅", message, p.theme.InfoColour)
}

// Warn prints a warning-styled message with nil-safe fallback to stdout.
func (p *Pretty) Warn(message string) {
	if p == nil {
		fmt.Println("⚠️ " + message)
		return
	}
	p.printStyled("⚠️", message, p.theme.WarnColour)
}

// Error prints an error-styled message with nil-safe fallback to stdout.
func (p *Pretty) Error(message string) {
	if p == nil {
		fmt.Println("❌ " + message)
		return
	}
	p.printStyled("❌", message, p.theme.ErrorColour)
}

// Debug prints a debug-styled message with nil-safe fallback to stdout.
func (p *Pretty) Debug(message string) {
	if p == nil {
		fmt.Println("🐛 " + message)
		return
	}
	p.printStyled("🐛", message, p.theme.DebugColour)
}

// printStyled ignores write errors to ensure pretty printing never fails.
func (p *Pretty) printStyled(icon, message string, colour velocity.Colour) {
	buf := velocity.GetBuffer(128)
	defer velocity.PutBuffer(buf)
	buf.WriteString(colour.ANSI(true))
	buf.WriteString(icon)
	buf.WriteString(" ")
	buf.WriteString(message)
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
	_, _ = buf.WriteTo(p.writer)
}

// Section prints a titled section header with an underline.
func (p *Pretty) Section(title string) {
	if p == nil {
		fmt.Println(title)
		fmt.Println(strings.Repeat("─", 40))
		return
	}

	if p.theme == nil {
		p.theme = velocity.ThemeNightOwl
	}

	buf := velocity.GetBuffer(128)
	defer velocity.PutBuffer(buf)
	buf.WriteString(p.theme.CachedMessageFg())
	buf.WriteString(title)
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
	buf.WriteString(strings.Repeat("─", 40))
	buf.WriteString("\n")

	if p.writer == nil {
		fmt.Print(buf.String())
		return
	}

	_, _ = buf.WriteTo(p.writer)
}

// Box draws a bordered box around content, with an optional title in the top border.
func (p *Pretty) Box(title, content string) {
	buf := velocity.GetBuffer(512)
	defer velocity.PutBuffer(buf)

	// Split first so width is based on the longest line, not total content bytes.
	lines := strings.Split(content, "\n")

	// Strip trailing newline that would produce a spurious empty final line.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Width is driven by rune count so Unicode content aligns correctly.
	maxLineRunes := 0
	for _, line := range lines {
		if n := len([]rune(line)); n > maxLineRunes {
			maxLineRunes = n
		}
	}

	width := max(maxLineRunes+4, 42)
	// Ensure a long title does not produce a negative repeat count.
	if titleWidth := len([]rune(title)) + 6; titleWidth > width {
		width = titleWidth
	}

	// Top border inner width must equal width-2 to match content rows.
	// Prefix "┌─" consumes 1 dash. Non-empty title consumes runeLen(title)+1 more dashes.
	topFill := width - 2 - 1 // subtract the leading "─" from "┌─"
	if title != "" {
		topFill -= len([]rune(title)) + 1 // subtract title runes and its trailing "─"
	}

	buf.WriteString(p.theme.CachedFieldKeyFg())
	buf.WriteString("┌─")
	if title != "" {
		buf.WriteString(title)
		buf.WriteString("─")
	}
	buf.WriteString(strings.Repeat("─", topFill))
	buf.WriteString("┐")
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	for _, line := range lines {
		buf.WriteString(p.theme.CachedFieldKeyFg())
		buf.WriteString("│ ")
		buf.WriteString(velocity.Reset)
		buf.WriteString(p.theme.CachedMessageFg())
		buf.WriteString(padRightRunes(line, width-3))
		buf.WriteString(velocity.Reset)
		buf.WriteString(p.theme.CachedFieldKeyFg())
		buf.WriteString("│")
		buf.WriteString(velocity.Reset)
		buf.WriteString("\n")
	}

	buf.WriteString(p.theme.CachedFieldKeyFg())
	buf.WriteString("└")
	buf.WriteString(strings.Repeat("─", width-2))
	buf.WriteString("┘")
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	_, _ = buf.WriteTo(p.writer)
}

// Panel draws a simple bordered block with a title bar.
func (p *Pretty) Panel(title, content string) {
	buf := velocity.GetBuffer(256)
	defer velocity.PutBuffer(buf)
	buf.WriteString(p.theme.CachedMessageFg())
	if title != "" {
		buf.WriteString("▓ ")
		buf.WriteString(title)
		buf.WriteString(" ▓\n")
	}
	buf.WriteString(content)
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
	_, _ = buf.WriteTo(p.writer)
}

// Bullet prints an indented bullet point at the given nesting level.
func (p *Pretty) Bullet(level int, text string) {
	buf := velocity.GetBuffer(128)
	defer velocity.PutBuffer(buf)
	indent := strings.Repeat("  ", level)
	bullets := []string{"•", "◦", "▪", "▫"}
	bullet := bullets[level%len(bullets)]

	buf.WriteString(indent)
	buf.WriteString(p.theme.CachedFieldKeyFg())
	buf.WriteString(bullet)
	buf.WriteString(velocity.Reset)
	buf.WriteString(" ")
	buf.WriteString(p.theme.CachedMessageFg())
	buf.WriteString(text)
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
	_, _ = buf.WriteTo(p.writer)
}

// KeyValue prints a two-column key: value line with nil-safe fallback.
func (p *Pretty) KeyValue(key, value string) {
	if p == nil {
		fmt.Printf("%s: %s\n", key, value)
		return
	}

	if p.theme == nil {
		p.theme = velocity.ThemeNightOwl
	}

	buf := velocity.GetBuffer(128)
	defer velocity.PutBuffer(buf)
	buf.WriteString(p.theme.CachedFieldKeyFg())
	buf.WriteString(key)
	buf.WriteString(velocity.Reset)
	buf.WriteString(": ")
	buf.WriteString(p.theme.CachedFieldValFg())
	buf.WriteString(value)
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	if p.writer == nil {
		fmt.Print(buf.String())
		return
	}

	_, _ = buf.WriteTo(p.writer)
}

// Table renders an aligned table with auto-sized columns.
func (p *Pretty) Table(headers []string, rows [][]string) {
	if len(headers) == 0 || len(rows) == 0 {
		return
	}

	colWidths := p.calculateColumnWidths(headers, rows)
	buf := velocity.GetBuffer(1024)
	defer velocity.PutBuffer(buf)

	p.writeTableTopBorder(buf, colWidths)
	p.writeTableHeaders(buf, headers, colWidths)
	p.writeTableHeaderSeparator(buf, colWidths)
	p.writeTableRows(buf, rows, colWidths)
	p.writeTableBottomBorder(buf, colWidths)

	_, _ = buf.WriteTo(p.writer)
}

func (*Pretty) calculateColumnWidths(headers []string, rows [][]string) []int {
	colWidths := make([]int, len(headers))
	for i, header := range headers {
		colWidths[i] = len(header)
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

func (p *Pretty) writeTableTopBorder(buf *bytes.Buffer, colWidths []int) {
	buf.WriteString(p.theme.CachedFieldKeyFg())
	for i, width := range colWidths {
		buf.WriteString(strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┬")
		}
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

func (p *Pretty) writeTableHeaders(buf *bytes.Buffer, headers []string, colWidths []int) {
	buf.WriteString(p.theme.CachedFieldKeyFg())
	for i, header := range headers {
		if i > 0 {
			buf.WriteString("│")
		}
		buf.WriteString(" ")
		buf.WriteString(velocity.Reset)
		buf.WriteString(p.theme.CachedTableHeaderFg())
		buf.WriteString(padRight(header, colWidths[i]))
		buf.WriteString(velocity.Reset)
		buf.WriteString(p.theme.CachedFieldKeyFg())
		buf.WriteString(" ")
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

func (p *Pretty) writeTableHeaderSeparator(buf *bytes.Buffer, colWidths []int) {
	buf.WriteString(p.theme.CachedFieldKeyFg())
	for i, width := range colWidths {
		buf.WriteString(strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┼")
		}
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

func (p *Pretty) writeTableRows(buf *bytes.Buffer, rows [][]string, colWidths []int) {
	for _, row := range rows {
		p.writeTableRow(buf, row, colWidths)
	}
}

func (p *Pretty) writeTableRow(buf *bytes.Buffer, row []string, colWidths []int) {
	buf.WriteString(p.theme.CachedFieldKeyFg())
	for i, cell := range row {
		if i >= len(colWidths) {
			break
		}
		buf.WriteString(" ")
		buf.WriteString(p.theme.CachedMessageFg())
		buf.WriteString(padRightVisible(cell, colWidths[i]))
		buf.WriteString(velocity.Reset)
		buf.WriteString(p.theme.CachedFieldKeyFg())
		buf.WriteString(" ")
		if i < len(colWidths)-1 {
			buf.WriteString("│")
		}
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

func (p *Pretty) writeTableBottomBorder(buf *bytes.Buffer, colWidths []int) {
	buf.WriteString(p.theme.CachedFieldKeyFg())
	for i, width := range colWidths {
		buf.WriteString(strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┴")
		}
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

// Tree prints a hierarchy of TreeItem nodes with nil-safe fallback to stdout.
func (p *Pretty) Tree(nodes []TreeItem) {
	if p == nil {
		for i, node := range nodes {
			writeTreeItemStandalone(os.Stdout, node, "", i == len(nodes)-1)
		}
		return
	}

	if p.theme == nil {
		p.theme = velocity.ThemeNightOwl
	}

	buf := velocity.GetBuffer(512)
	defer velocity.PutBuffer(buf)
	for i, node := range nodes {
		p.writePrettyTreeItem(buf, node, "", i == len(nodes)-1)
	}

	if p.writer == nil {
		fmt.Print(buf.String())
		return
	}

	_, _ = buf.WriteTo(p.writer)
}

func writeTreeItemStandalone(w io.Writer, node TreeItem, prefix string, isLast bool) {
	connector := treeBranch
	if isLast {
		connector = treeCorner
	}

	if node.Value != nil {
		_, _ = fmt.Fprintf(w, "%s%s%s: %v\n", prefix, connector, node.Key, node.Value)
	} else {
		_, _ = fmt.Fprintf(w, "%s%s%s\n", prefix, connector, node.Key)
	}

	childPrefix := prefix
	if isLast {
		childPrefix += treeBlank
	} else {
		childPrefix += treePipe
	}

	for i, child := range node.Children {
		writeTreeItemStandalone(w, child, childPrefix, i == len(node.Children)-1)
	}
}

func (p *Pretty) writePrettyTreeItem(buf *bytes.Buffer, node TreeItem, prefix string, isLast bool) {
	connector := treeBranch
	if isLast {
		connector = treeCorner
	}

	buf.WriteString(prefix)
	buf.WriteString(connector)
	buf.WriteString(p.theme.CachedMessageFg())
	if node.Value != nil {
		fmt.Fprintf(buf, "%s: %v", node.Key, node.Value)
	} else {
		buf.WriteString(node.Key)
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	childPrefix := prefix
	if isLast {
		childPrefix += treeBlank
	} else {
		childPrefix += treePipe
	}

	for i, child := range node.Children {
		p.writePrettyTreeItem(buf, child, childPrefix, i == len(node.Children)-1)
	}
}

// Raw writes text directly to the writer without any formatting.
func (p *Pretty) Raw(text string) {
	_, _ = io.WriteString(p.writer, text)
}

// Banner draws a double-border box around text.
func (p *Pretty) Banner(text string) {
	buf := velocity.GetBuffer(512)
	defer velocity.PutBuffer(buf)
	lines := strings.Split(text, "\n")

	maxLen := 0
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
		runeLen := len([]rune(lines[i]))
		if runeLen > maxLen {
			maxLen = runeLen
		}
	}

	contentWidth := maxLen
	boxWidth := contentWidth + 2

	buf.WriteString(p.theme.CachedFieldKeyFg())
	buf.WriteString("╔")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("╗")
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	for _, line := range lines {
		buf.WriteString(p.theme.CachedFieldKeyFg())
		buf.WriteString("│ ")
		buf.WriteString(velocity.Reset)
		buf.WriteString(p.theme.CachedMessageFg())
		buf.WriteString(padRightRunes(line, contentWidth))
		buf.WriteString(velocity.Reset)
		buf.WriteString(p.theme.CachedFieldKeyFg())
		buf.WriteString(" │")
		buf.WriteString(velocity.Reset)
		buf.WriteString("\n")
	}

	buf.WriteString(p.theme.CachedFieldKeyFg())
	buf.WriteString("╚")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("╝")
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	_, _ = buf.WriteTo(p.writer)
}

// SystemInfo is startup/configuration metadata for display.
type SystemInfo struct {
	Title   string
	Version string
	Fields  []KeyValuePair
}

// KeyValuePair is a labelled string value.
type KeyValuePair struct {
	Key   string
	Value string
}

// SystemInfo prints a titled block of key-value pairs with nil-safe fallback.
func (p *Pretty) SystemInfo(info *SystemInfo) {
	if info == nil {
		return
	}

	if p == nil {
		if info.Title != "" {
			version := ""
			if info.Version != "" {
				version = " v" + info.Version
			}
			fmt.Printf("▓ %s%s ▓\n", info.Title, version)
		}
		for _, pair := range info.Fields {
			fmt.Printf("%-20s %s\n", pair.Key+":", pair.Value)
		}
		return
	}

	if p.theme == nil {
		p.theme = velocity.ThemeNightOwl
	}

	buf := velocity.GetBuffer(512)
	defer velocity.PutBuffer(buf)

	if info.Title != "" {
		buf.WriteString(p.theme.CachedInfoColourFg())
		buf.WriteString("▓ ")
		buf.WriteString(info.Title)
		if info.Version != "" {
			buf.WriteString(" v")
			buf.WriteString(info.Version)
		}
		buf.WriteString(" ▓")
		buf.WriteString(velocity.Reset)
		buf.WriteString("\n")
	}

	for _, pair := range info.Fields {
		buf.WriteString(p.theme.CachedFieldKeyFg())
		buf.WriteString(padRight(pair.Key+":", 20))
		buf.WriteString(velocity.Reset)
		buf.WriteString(" ")
		buf.WriteString(p.theme.CachedMessageFg())
		buf.WriteString(pair.Value)
		buf.WriteString(velocity.Reset)
		buf.WriteString("\n")
	}

	if p.writer == nil {
		fmt.Print(buf.String())
		return
	}

	_, _ = buf.WriteTo(p.writer)
}

// TreeItem represents a node in a hierarchical display tree.
type TreeItem struct {
	Key      string
	Value    any
	Children []TreeItem
}

// CreateBanner renders a double-border banner box with ASCII art, title, version, and URL.
func CreateBanner(title, version, url string, ascii []string) string {
	var b strings.Builder
	maxLen := 0

	for _, line := range ascii {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	if len(title)+len(version)+3 > maxLen {
		maxLen = len(title) + len(version) + 3
	}
	if len(url) > maxLen {
		maxLen = len(url)
	}

	boxWidth := maxLen + 4

	b.WriteString("╔")
	b.WriteString(strings.Repeat("═", boxWidth-2))
	b.WriteString("╗\n")

	for _, line := range ascii {
		b.WriteString("║ ")
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", maxLen-len(line)))
		b.WriteString(" ║\n")
	}

	if len(ascii) > 0 {
		b.WriteString("╠")
		b.WriteString(strings.Repeat("═", boxWidth-2))
		b.WriteString("╣\n")
	}

	titleLine := fmt.Sprintf("%s v%s", title, version)
	padding := (maxLen - len(titleLine)) / 2
	b.WriteString("║ ")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString(titleLine)
	b.WriteString(strings.Repeat(" ", maxLen-len(titleLine)-padding))
	b.WriteString(" ║\n")

	if url != "" {
		urlPadding := (maxLen - len(url)) / 2
		b.WriteString("║ ")
		b.WriteString(strings.Repeat(" ", urlPadding))
		b.WriteString(url)
		b.WriteString(strings.Repeat(" ", maxLen-len(url)-urlPadding))
		b.WriteString(" ║\n")
	}

	b.WriteString("╚")
	b.WriteString(strings.Repeat("═", boxWidth-2))
	b.WriteString("╝\n")

	return b.String()
}

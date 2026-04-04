package velocity

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// Pretty provides formatted output utilities for styled terminal printing.
type Pretty struct {
	writer io.Writer
	theme  *Theme
}

func NewPretty(w io.Writer, theme *Theme) *Pretty {
	if theme == nil {
		theme = ThemeNightOwl
	}
	return &Pretty{
		writer: w,
		theme:  theme,
	}
}

// Info prints with nil-safe fallback to stdout.
func (p *Pretty) Info(message string) {
	if p == nil {
		fmt.Println("ℹ️ " + message)
		return
	}
	p.printStyled("ℹ️", message, p.theme.InfoColour)
}

// Success prints with nil-safe fallback to stdout.
func (p *Pretty) Success(message string) {
	if p == nil {
		fmt.Println("✅ " + message)
		return
	}
	p.printStyled("✅", message, p.theme.InfoColour)
}

// Warn prints with nil-safe fallback to stdout.
func (p *Pretty) Warn(message string) {
	if p == nil {
		fmt.Println("⚠️ " + message)
		return
	}
	p.printStyled("⚠️", message, p.theme.WarnColour)
}

// Error prints with nil-safe fallback to stdout.
func (p *Pretty) Error(message string) {
	if p == nil {
		fmt.Println("❌ " + message)
		return
	}
	p.printStyled("❌", message, p.theme.ErrorColour)
}

// Debug prints with nil-safe fallback to stdout.
func (p *Pretty) Debug(message string) {
	if p == nil {
		fmt.Println("🐛 " + message)
		return
	}
	p.printStyled("🐛", message, p.theme.DebugColour)
}

// printStyled ignores write errors to ensure logging never fails.
func (p *Pretty) printStyled(icon, message string, colour Colour) {
	buf := &bytes.Buffer{}
	buf.WriteString(colour.ANSI(true))
	buf.WriteString(icon)
	buf.WriteString(" ")
	buf.WriteString(message)
	buf.WriteString(Reset)
	buf.WriteString("\n")
	_, _ = buf.WriteTo(p.writer)
}

// Section prints with nil-safe fallback to stdout.
func (p *Pretty) Section(title string) {
	if p == nil {
		fmt.Println(title)
		fmt.Println(strings.Repeat("─", 40))
		return
	}

	if p.theme == nil {
		p.theme = ThemeNightOwl
	}

	buf := &bytes.Buffer{}

	buf.WriteString(p.theme.MessageColour.ANSI(true))
	buf.WriteString(title)
	buf.WriteString(Reset)
	buf.WriteString("\n")

	buf.WriteString(strings.Repeat("─", 40))
	buf.WriteString("\n")

	if p.writer == nil {
		fmt.Print(buf.String())
		return
	}

	_, _ = buf.WriteTo(p.writer)
}

func (p *Pretty) Box(title, content string) {
	buf := &bytes.Buffer{}

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

	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	buf.WriteString("┌─")
	if title != "" {
		buf.WriteString(title)
		buf.WriteString("─")
	}
	buf.WriteString(strings.Repeat("─", topFill))
	buf.WriteString("┐")
	buf.WriteString(Reset)
	buf.WriteString("\n")

	for _, line := range lines {
		buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
		buf.WriteString("│ ")
		buf.WriteString(Reset)
		buf.WriteString(p.theme.MessageColour.ANSI(true))
		buf.WriteString(padRightRunes(line, width-3))
		buf.WriteString(Reset)
		buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
		buf.WriteString("│")
		buf.WriteString(Reset)
		buf.WriteString("\n")
	}

	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	buf.WriteString("└")
	buf.WriteString(strings.Repeat("─", width-2))
	buf.WriteString("┘")
	buf.WriteString(Reset)
	buf.WriteString("\n")

	_, _ = buf.WriteTo(p.writer)
}

func (p *Pretty) Panel(title, content string) {
	buf := &bytes.Buffer{}

	buf.WriteString(p.theme.MessageColour.ANSI(true))
	if title != "" {
		buf.WriteString("▓ ")
		buf.WriteString(title)
		buf.WriteString(" ▓\n")
	}
	buf.WriteString(content)
	buf.WriteString(Reset)
	buf.WriteString("\n")

	_, _ = buf.WriteTo(p.writer)
}

func (p *Pretty) Bullet(level int, text string) {
	buf := &bytes.Buffer{}

	indent := strings.Repeat("  ", level)

	bullets := []string{"•", "◦", "▪", "▫"}
	bullet := bullets[level%len(bullets)]

	buf.WriteString(indent)
	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	buf.WriteString(bullet)
	buf.WriteString(Reset)
	buf.WriteString(" ")
	buf.WriteString(p.theme.MessageColour.ANSI(true))
	buf.WriteString(text)
	buf.WriteString(Reset)
	buf.WriteString("\n")

	_, _ = buf.WriteTo(p.writer)
}

// KeyValue prints with nil-safe fallback to stdout.
func (p *Pretty) KeyValue(key, value string) {
	if p == nil {
		fmt.Printf("%s: %s\n", key, value)
		return
	}

	if p.theme == nil {
		p.theme = ThemeNightOwl
	}

	buf := &bytes.Buffer{}

	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	buf.WriteString(key)
	buf.WriteString(Reset)
	buf.WriteString(": ")
	buf.WriteString(p.theme.FieldValColour.ANSI(true))
	buf.WriteString(value)
	buf.WriteString(Reset)
	buf.WriteString("\n")

	if p.writer == nil {
		fmt.Print(buf.String())
		return
	}

	_, _ = buf.WriteTo(p.writer)
}

func (p *Pretty) Table(headers []string, rows [][]string) {
	if len(headers) == 0 || len(rows) == 0 {
		return
	}

	colWidths := p.calculateColumnWidths(headers, rows)
	buf := &bytes.Buffer{}

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
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}
	return colWidths
}

func (p *Pretty) writeTableTopBorder(buf *bytes.Buffer, colWidths []int) {
	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	for i, width := range colWidths {
		buf.WriteString(strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┬")
		}
	}
	buf.WriteString(Reset)
	buf.WriteString("\n")
}

func (p *Pretty) writeTableHeaders(buf *bytes.Buffer, headers []string, colWidths []int) {
	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	for i, header := range headers {
		if i > 0 {
			buf.WriteString("│")
		}
		buf.WriteString(" ")
		buf.WriteString(Reset)
		buf.WriteString(p.theme.TableHeader.ANSI(true))
		buf.WriteString(padRight(header, colWidths[i]))
		buf.WriteString(Reset)
		buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
		buf.WriteString(" ")
	}
	buf.WriteString(Reset)
	buf.WriteString("\n")
}

func (p *Pretty) writeTableHeaderSeparator(buf *bytes.Buffer, colWidths []int) {
	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	for i, width := range colWidths {
		buf.WriteString(strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┼")
		}
	}
	buf.WriteString(Reset)
	buf.WriteString("\n")
}

func (p *Pretty) writeTableRows(buf *bytes.Buffer, rows [][]string, colWidths []int) {
	for _, row := range rows {
		p.writeTableRow(buf, row, colWidths)
	}
}

func (p *Pretty) writeTableRow(buf *bytes.Buffer, row []string, colWidths []int) {
	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	for i, cell := range row {
		if i >= len(colWidths) {
			break
		}
		buf.WriteString(" ")
		buf.WriteString(p.theme.MessageColour.ANSI(true))
		buf.WriteString(padRight(cell, colWidths[i]))
		buf.WriteString(Reset)
		buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
		buf.WriteString(" ")
		if i < len(colWidths)-1 {
			buf.WriteString("│")
		}
	}
	buf.WriteString(Reset)
	buf.WriteString("\n")
}

func (p *Pretty) writeTableBottomBorder(buf *bytes.Buffer, colWidths []int) {
	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	for i, width := range colWidths {
		buf.WriteString(strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┴")
		}
	}
	buf.WriteString(Reset)
	buf.WriteString("\n")
}

// Tree prints a hierarchy of TreeItem nodes with nil-safe fallback to stdout.
func (p *Pretty) Tree(nodes []TreeItem) {
	if p == nil {
		for i, node := range nodes {
			writeTreeItemPrettyStandalone(os.Stdout, node, 0, i == len(nodes)-1)
		}
		return
	}

	if p.theme == nil {
		p.theme = ThemeNightOwl
	}

	buf := &bytes.Buffer{}

	for i, node := range nodes {
		p.writePrettyTreeItem(buf, node, 0, i == len(nodes)-1)
	}

	if p.writer == nil {
		fmt.Print(buf.String())
		return
	}

	_, _ = buf.WriteTo(p.writer)
}

func writeTreeItemPrettyStandalone(w io.Writer, node TreeItem, depth int, isLast bool) {
	if depth > 0 {
		for range depth - 1 {
			_, _ = fmt.Fprint(w, "    ")
		}
		if isLast {
			_, _ = fmt.Fprint(w, "└─ ")
		} else {
			_, _ = fmt.Fprint(w, "├─ ")
		}
	}

	if node.Value != nil {
		_, _ = fmt.Fprintf(w, "%s: %v\n", node.Key, node.Value)
	} else {
		_, _ = fmt.Fprintln(w, node.Key)
	}

	for i, child := range node.Children {
		writeTreeItemPrettyStandalone(w, child, depth+1, i == len(node.Children)-1)
	}
}

func (p *Pretty) writePrettyTreeItem(buf *bytes.Buffer, node TreeItem, depth int, isLast bool) {
	if depth > 0 {
		for range depth - 1 {
			buf.WriteString("    ")
		}
		if isLast {
			buf.WriteString("└─ ")
		} else {
			buf.WriteString("├─ ")
		}
	}

	buf.WriteString(p.theme.MessageColour.ANSI(true))
	if node.Value != nil {
		fmt.Fprintf(buf, "%s: %v", node.Key, node.Value)
	} else {
		buf.WriteString(node.Key)
	}
	buf.WriteString(Reset)
	buf.WriteString("\n")

	for i, child := range node.Children {
		p.writePrettyTreeItem(buf, child, depth+1, i == len(node.Children)-1)
	}
}

func (p *Pretty) Raw(text string) {
	_, _ = io.WriteString(p.writer, text)
}

func (p *Pretty) Banner(text string) {
	buf := &bytes.Buffer{}

	// Split text into lines for multi-line support
	lines := strings.Split(text, "\n")

	// Trim trailing whitespace from each line and find the longest (in runes, not bytes)
	maxLen := 0
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
		runeLen := len([]rune(lines[i]))
		if runeLen > maxLen {
			maxLen = runeLen
		}
	}

	// Box width = longest line + padding (2 spaces on each side) + borders (2)
	contentWidth := maxLen
	boxWidth := contentWidth + 2 // content + 2 for padding

	// Top border with rounded corners
	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	buf.WriteString("╔")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("╗")
	buf.WriteString(Reset)
	buf.WriteString("\n")

	// Content lines
	for _, line := range lines {
		buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
		buf.WriteString("│ ")
		buf.WriteString(Reset)
		buf.WriteString(p.theme.MessageColour.ANSI(true))
		buf.WriteString(padRightRunes(line, contentWidth))
		buf.WriteString(Reset)
		buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
		buf.WriteString(" │")
		buf.WriteString(Reset)
		buf.WriteString("\n")
	}

	// Bottom border with rounded corners
	buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
	buf.WriteString("╚")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("╝")
	buf.WriteString(Reset)
	buf.WriteString("\n")

	_, _ = buf.WriteTo(p.writer)
}

type SystemInfo struct {
	Title   string
	Version string
	Fields  []KeyValuePair
}

type KeyValuePair struct {
	Key   string
	Value string
}

// SystemInfo prints with nil-safe fallback to stdout.
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
		p.theme = ThemeNightOwl
	}

	buf := &bytes.Buffer{}

	if info.Title != "" {
		buf.WriteString(p.theme.InfoColour.ANSI(true))
		buf.WriteString("▓ ")
		buf.WriteString(info.Title)
		if info.Version != "" {
			buf.WriteString(" v")
			buf.WriteString(info.Version)
		}
		buf.WriteString(" ▓")
		buf.WriteString(Reset)
		buf.WriteString("\n")
	}

	for _, pair := range info.Fields {
		buf.WriteString(p.theme.FieldKeyColour.ANSI(true))
		buf.WriteString(padRight(pair.Key+":", 20))
		buf.WriteString(Reset)
		buf.WriteString(" ")
		buf.WriteString(p.theme.MessageColour.ANSI(true))
		buf.WriteString(pair.Value)
		buf.WriteString(Reset)
		buf.WriteString("\n")
	}

	if p.writer == nil {
		fmt.Print(buf.String())
		return
	}

	_, _ = buf.WriteTo(p.writer)
}

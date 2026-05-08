package pretty

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	velocity "github.com/tensorfoundrylabs/velocity"
)

// BoxResult holds the configuration for a Box render and implements velocity.Renderable.
type BoxResult struct {
	theme   *velocity.Theme
	title   string
	content string
}

// NewBoxResult returns a BoxResult ready to render.
func NewBoxResult(title, content string, theme *velocity.Theme) *BoxResult {
	if theme == nil {
		theme = velocity.ThemeNightOwl
	}
	return &BoxResult{title: title, content: content, theme: theme}
}

// Render writes the box to w.
func (r *BoxResult) Render(w io.Writer) error {
	buf := velocity.GetBuffer(512)
	defer velocity.PutBuffer(buf)
	renderBox(buf, r.theme, r.title, r.content)
	_, err := buf.WriteTo(w)
	return err
}

func renderBox(buf *bytes.Buffer, theme *velocity.Theme, title, content string) {
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
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	for _, line := range lines {
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString("│ ")
		buf.WriteString(velocity.Reset)
		buf.WriteString(theme.CachedMessageFg())
		buf.WriteString(padRightRunes(line, width-3))
		buf.WriteString(velocity.Reset)
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString("│")
		buf.WriteString(velocity.Reset)
		buf.WriteString("\n")
	}

	buf.WriteString(theme.CachedFieldKeyFg())
	buf.WriteString("└")
	buf.WriteString(strings.Repeat("─", width-2))
	buf.WriteString("┘")
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

// TableResult holds the configuration for a Table render and implements velocity.Renderable.
type TableResult struct {
	theme   *velocity.Theme
	headers []string
	rows    [][]string
}

// NewTableResult returns a TableResult ready to render.
func NewTableResult(headers []string, rows [][]string, theme *velocity.Theme) *TableResult {
	if theme == nil {
		theme = velocity.ThemeNightOwl
	}
	return &TableResult{headers: headers, rows: rows, theme: theme}
}

// Render writes the table to w.
func (r *TableResult) Render(w io.Writer) error {
	if len(r.headers) == 0 || len(r.rows) == 0 {
		return nil
	}
	buf := velocity.GetBuffer(1024)
	defer velocity.PutBuffer(buf)
	renderTable(buf, r.theme, r.headers, r.rows)
	_, err := buf.WriteTo(w)
	return err
}

func renderTable(buf *bytes.Buffer, theme *velocity.Theme, headers []string, rows [][]string) {
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

func writeTableTopBorder(buf *bytes.Buffer, theme *velocity.Theme, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, w := range colWidths {
		buf.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┬")
		}
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

func writeTableHeaders(buf *bytes.Buffer, theme *velocity.Theme, headers []string, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, header := range headers {
		if i > 0 {
			buf.WriteString("│")
		}
		buf.WriteString(" ")
		buf.WriteString(velocity.Reset)
		buf.WriteString(theme.CachedTableHeaderFg())
		buf.WriteString(padRight(header, colWidths[i]))
		buf.WriteString(velocity.Reset)
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString(" ")
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

func writeTableHeaderSeparator(buf *bytes.Buffer, theme *velocity.Theme, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, w := range colWidths {
		buf.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┼")
		}
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

func writeTableRow(buf *bytes.Buffer, theme *velocity.Theme, row []string, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, cell := range row {
		if i >= len(colWidths) {
			break
		}
		buf.WriteString(" ")
		buf.WriteString(theme.CachedMessageFg())
		buf.WriteString(padRightVisible(cell, colWidths[i]))
		buf.WriteString(velocity.Reset)
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString(" ")
		if i < len(colWidths)-1 {
			buf.WriteString("│")
		}
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

func writeTableBottomBorder(buf *bytes.Buffer, theme *velocity.Theme, colWidths []int) {
	buf.WriteString(theme.CachedFieldKeyFg())
	for i, w := range colWidths {
		buf.WriteString(strings.Repeat("─", w+2))
		if i < len(colWidths)-1 {
			buf.WriteString("┴")
		}
	}
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

// BannerResult holds the configuration for a Banner render and implements velocity.Renderable.
type BannerResult struct {
	theme *velocity.Theme
	text  string
}

// NewBannerResult returns a BannerResult ready to render.
func NewBannerResult(text string, theme *velocity.Theme) *BannerResult {
	if theme == nil {
		theme = velocity.ThemeNightOwl
	}
	return &BannerResult{text: text, theme: theme}
}

// Render writes the banner to w.
func (r *BannerResult) Render(w io.Writer) error {
	buf := velocity.GetBuffer(512)
	defer velocity.PutBuffer(buf)
	renderBanner(buf, r.theme, r.text)
	_, err := buf.WriteTo(w)
	return err
}

func renderBanner(buf *bytes.Buffer, theme *velocity.Theme, text string) {
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
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	for _, line := range lines {
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString("│ ")
		buf.WriteString(velocity.Reset)
		buf.WriteString(theme.CachedMessageFg())
		buf.WriteString(padRightRunes(line, contentWidth))
		buf.WriteString(velocity.Reset)
		buf.WriteString(theme.CachedFieldKeyFg())
		buf.WriteString(" │")
		buf.WriteString(velocity.Reset)
		buf.WriteString("\n")
	}

	buf.WriteString(theme.CachedFieldKeyFg())
	buf.WriteString("╚")
	buf.WriteString(strings.Repeat("─", boxWidth))
	buf.WriteString("╝")
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
}

// TreeResult holds tree nodes for rendering and implements velocity.Renderable.
type TreeResult struct {
	theme *velocity.Theme
	nodes []TreeItem
}

// NewTreeResult returns a TreeResult ready to render.
func NewTreeResult(nodes []TreeItem, theme *velocity.Theme) *TreeResult {
	if theme == nil {
		theme = velocity.ThemeNightOwl
	}
	return &TreeResult{nodes: nodes, theme: theme}
}

// Render writes the tree to w.
func (r *TreeResult) Render(w io.Writer) error {
	buf := velocity.GetBuffer(512)
	defer velocity.PutBuffer(buf)
	for i, node := range r.nodes {
		writePrettyTreeItemInto(buf, r.theme, node, "", i == len(r.nodes)-1)
	}
	_, err := buf.WriteTo(w)
	return err
}

func writePrettyTreeItemInto(buf *bytes.Buffer, theme *velocity.Theme, node TreeItem, prefix string, isLast bool) {
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
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")

	childPrefix := prefix
	if isLast {
		childPrefix += treeBlank
	} else {
		childPrefix += treePipe
	}

	for i, child := range node.Children {
		writePrettyTreeItemInto(buf, theme, child, childPrefix, i == len(node.Children)-1)
	}
}

// KeyValueResult holds a key-value pair for rendering and implements velocity.Renderable.
type KeyValueResult struct {
	theme *velocity.Theme
	key   string
	value string
}

// NewKeyValueResult returns a KeyValueResult ready to render.
func NewKeyValueResult(key, value string, theme *velocity.Theme) *KeyValueResult {
	if theme == nil {
		theme = velocity.ThemeNightOwl
	}
	return &KeyValueResult{key: key, value: value, theme: theme}
}

// Render writes the key-value pair to w.
func (r *KeyValueResult) Render(w io.Writer) error {
	buf := velocity.GetBuffer(128)
	defer velocity.PutBuffer(buf)
	buf.WriteString(r.theme.CachedFieldKeyFg())
	buf.WriteString(r.key)
	buf.WriteString(velocity.Reset)
	buf.WriteString(": ")
	buf.WriteString(r.theme.CachedFieldValFg())
	buf.WriteString(r.value)
	buf.WriteString(velocity.Reset)
	buf.WriteString("\n")
	_, err := buf.WriteTo(w)
	return err
}

// SystemInfoResult holds system info metadata for rendering and implements velocity.Renderable.
type SystemInfoResult struct {
	theme *velocity.Theme
	info  *SystemInfo
}

// NewSystemInfoResult returns a SystemInfoResult ready to render.
func NewSystemInfoResult(info *SystemInfo, theme *velocity.Theme) *SystemInfoResult {
	if theme == nil {
		theme = velocity.ThemeNightOwl
	}
	return &SystemInfoResult{info: info, theme: theme}
}

// Render writes the system info block to w.
func (r *SystemInfoResult) Render(w io.Writer) error {
	if r.info == nil {
		return nil
	}
	buf := velocity.GetBuffer(512)
	defer velocity.PutBuffer(buf)

	if r.info.Title != "" {
		buf.WriteString(r.theme.CachedInfoColourFg())
		buf.WriteString("▓ ")
		buf.WriteString(r.info.Title)
		if r.info.Version != "" {
			buf.WriteString(" v")
			buf.WriteString(r.info.Version)
		}
		buf.WriteString(" ▓")
		buf.WriteString(velocity.Reset)
		buf.WriteString("\n")
	}

	for _, pair := range r.info.Fields {
		buf.WriteString(r.theme.CachedFieldKeyFg())
		buf.WriteString(padRight(pair.Key+":", 20))
		buf.WriteString(velocity.Reset)
		buf.WriteString(" ")
		buf.WriteString(r.theme.CachedMessageFg())
		buf.WriteString(pair.Value)
		buf.WriteString(velocity.Reset)
		buf.WriteString("\n")
	}

	_, err := buf.WriteTo(w)
	return err
}


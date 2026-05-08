package pretty

import (
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

// NewFromLogger returns a Pretty whose output is routed through log's console writer,
// serialised under the same mutex as concurrent log calls to prevent interleaving.
// The pretty output is indented to align with the message column so it sits flush
// with tree-mode log fields.
// Returns nil if log is nil.
func NewFromLogger(log *velocity.Logger) *Pretty {
	if log == nil {
		return nil
	}
	return &Pretty{
		writer: &loggerWriter{log: log},
		theme:  log.Theme(),
	}
}

// loggerWriter adapts velocity.Logger to io.Writer, routing writes through Logger.RenderRaw
// so that pretty output respects the console writer's mutex.
type loggerWriter struct {
	log *velocity.Logger
}

func (lw *loggerWriter) Write(p []byte) (int, error) {
	lw.log.RenderRaw(&bytesRenderable{data: p})
	return len(p), nil
}

// bytesRenderable wraps a byte slice as a Renderable.
type bytesRenderable struct {
	data []byte
}

func (r *bytesRenderable) Render(w io.Writer) error {
	_, err := w.Write(r.data)
	return err
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
	_ = NewBoxResult(title, content, p.theme).Render(p.writer)
}

// NewBox returns a BoxResult for the given title and content using p's theme.
// Use this with Logger.Render to route the box through the logger's console writer.
func (p *Pretty) NewBox(title, content string) *BoxResult {
	return NewBoxResult(title, content, p.theme)
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

	w := p.writer
	if w == nil {
		buf := velocity.GetBuffer(128)
		defer velocity.PutBuffer(buf)
		_ = NewKeyValueResult(key, value, p.theme).Render(buf)
		fmt.Print(buf.String())
		return
	}

	_ = NewKeyValueResult(key, value, p.theme).Render(w)
}

// NewKeyValue returns a KeyValueResult using p's theme.
// Use this with Logger.Render to route through the logger's console writer.
func (p *Pretty) NewKeyValue(key, value string) *KeyValueResult {
	return NewKeyValueResult(key, value, p.theme)
}

// Table renders an aligned table with auto-sized columns.
func (p *Pretty) Table(headers []string, rows [][]string) {
	_ = NewTableResult(headers, rows, p.theme).Render(p.writer)
}

// NewTable returns a TableResult for the given data using p's theme.
// Use this with Logger.Render to route the table through the logger's console writer.
func (p *Pretty) NewTable(headers []string, rows [][]string) *TableResult {
	return NewTableResult(headers, rows, p.theme)
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

	w := p.writer
	if w == nil {
		// Nil writer is a fallback path — collect and print.
		buf := velocity.GetBuffer(512)
		defer velocity.PutBuffer(buf)
		for i, node := range nodes {
			writePrettyTreeItemInto(buf, p.theme, node, "", i == len(nodes)-1)
		}
		fmt.Print(buf.String())
		return
	}

	_ = NewTreeResult(nodes, p.theme).Render(w)
}

// NewTree returns a TreeResult for the given nodes using p's theme.
// Use this with Logger.Render to route the tree through the logger's console writer.
func (p *Pretty) NewTree(nodes []TreeItem) *TreeResult {
	return NewTreeResult(nodes, p.theme)
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


// Raw writes text directly to the writer without any formatting.
func (p *Pretty) Raw(text string) {
	_, _ = io.WriteString(p.writer, text)
}

// Banner draws a double-border box around text.
func (p *Pretty) Banner(text string) {
	_ = NewBannerResult(text, p.theme).Render(p.writer)
}

// NewBanner returns a BannerResult for the given text using p's theme.
// Use this with Logger.Render to route the banner through the logger's console writer.
func (p *Pretty) NewBanner(text string) *BannerResult {
	return NewBannerResult(text, p.theme)
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

	w := p.writer
	if w == nil {
		// Collect and print to stdout as fallback.
		buf := velocity.GetBuffer(512)
		defer velocity.PutBuffer(buf)
		_ = NewSystemInfoResult(info, p.theme).Render(buf)
		fmt.Print(buf.String())
		return
	}

	_ = NewSystemInfoResult(info, p.theme).Render(w)
}

// NewSystemInfo returns a SystemInfoResult for the given info using p's theme.
// Use this with Logger.Render to route the output through the logger's console writer.
func (p *Pretty) NewSystemInfo(info *SystemInfo) *SystemInfoResult {
	return NewSystemInfoResult(info, p.theme)
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

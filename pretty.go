package velocity

import (
	"fmt"
	"io"
	"strings"
)

// Pretty is the standalone facade for rich terminal output. It is distinct from Logger:
// CLI commands that want coloured output without a structured logging pipeline use Pretty
// directly. Both Pretty and Logger are facades over the same Renderable types in this package.
//
// Callers sharing a writer with an active Logger must serialise writes externally —
// Pretty has no mutex of its own, intentionally, because the Logger's consoleWriter
// mutex is the serialisation point when routing through NewPrettyFromLogger.
type Pretty struct {
	writer io.Writer
	theme  *Theme
}

// NewPretty returns a Pretty that writes to w using the given theme.
// If theme is nil, ThemeNightOwl is used. If w is nil, output goes to io.Discard.
func NewPretty(w io.Writer, theme *Theme) *Pretty {
	if theme == nil {
		theme = ThemeNightOwl
	}
	if w == nil {
		w = io.Discard
	}
	return &Pretty{writer: w, theme: theme}
}

// NewPrettyFromLogger returns a Pretty whose writes are serialised under the logger's
// console writer mutex, preventing interleaving with concurrent log calls.
// Returns nil if log is nil — callers can branch on presence without a nil check ladder.
func NewPrettyFromLogger(log *Logger) *Pretty {
	if log == nil {
		return nil
	}
	return &Pretty{
		writer: &prettyLoggerWriter{log: log},
		theme:  log.Theme(),
	}
}

// prettyLoggerWriter routes writes through Logger.RenderRaw so output is flush-left
// and serialised under the console writer's mutex.
type prettyLoggerWriter struct {
	log *Logger
}

func (lw *prettyLoggerWriter) Write(p []byte) (int, error) {
	lw.log.RenderRaw(&rawBytesRenderable{data: p})
	return len(p), nil
}

// rawBytesRenderable wraps a byte slice as a Renderable for use in RenderRaw.
type rawBytesRenderable struct {
	data []byte
}

func (r *rawBytesRenderable) Render(w io.Writer) error {
	_, err := w.Write(r.data)
	return err
}

// Box draws a bordered box around content, with an optional title in the top border.
func (p *Pretty) Box(title, content string) {
	if p == nil {
		return
	}
	_ = NewBox(title, content, p.theme).Render(p.writer)
}

// Table renders an aligned table with auto-sized columns.
func (p *Pretty) Table(headers []string, rows [][]string) {
	if p == nil {
		return
	}
	_ = NewTable(headers, rows, p.theme).Render(p.writer)
}

// Tree prints a hierarchy of TreeItem nodes.
func (p *Pretty) Tree(nodes []TreeItem) {
	if p == nil {
		return
	}
	_ = NewTree(nodes, p.theme).Render(p.writer)
}

// Banner draws a double-border box around text.
func (p *Pretty) Banner(text string) {
	if p == nil {
		return
	}
	_ = NewBanner(text, p.theme).Render(p.writer)
}

// KeyValue prints a two-column key: value line.
func (p *Pretty) KeyValue(key, value string) {
	if p == nil {
		fmt.Printf("%s: %s\n", key, value)
		return
	}
	_ = NewKeyValue(key, value, p.theme).Render(p.writer)
}

// Bullet prints an indented bullet point at the given nesting level.
func (p *Pretty) Bullet(level int, text string) {
	if p == nil {
		return
	}
	buf := GetBuffer(128)
	defer PutBuffer(buf)
	indent := strings.Repeat("  ", level)
	bullets := []string{"•", "◦", "▪", "▫"}
	bullet := bullets[level%len(bullets)]

	buf.WriteString(indent)
	buf.WriteString(p.theme.CachedFieldKeyFg())
	buf.WriteString(bullet)
	buf.WriteString(Reset)
	buf.WriteString(" ")
	buf.WriteString(p.theme.CachedMessageFg())
	buf.WriteString(text)
	buf.WriteString(Reset)
	buf.WriteString("\n")
	_, _ = buf.WriteTo(p.writer)
}

// SystemInfo prints a titled block of key-value pairs.
func (p *Pretty) SystemInfo(info *SystemInfoData) {
	if p == nil || info == nil {
		return
	}
	_ = NewSystemInfo(info, p.theme).Render(p.writer)
}

// Section prints a titled section header with a dashed underline.
func (p *Pretty) Section(title string) {
	if p == nil {
		fmt.Println(title)
		fmt.Println(strings.Repeat("─", 40))
		return
	}
	buf := GetBuffer(128)
	defer PutBuffer(buf)
	buf.WriteString(p.theme.CachedMessageFg())
	buf.WriteString(title)
	buf.WriteString(Reset)
	buf.WriteString("\n")
	buf.WriteString(strings.Repeat("─", 40))
	buf.WriteString("\n")
	_, _ = buf.WriteTo(p.writer)
}

// Render writes an arbitrary Renderable to the Pretty's writer.
// The canonical path for custom Renderables.
func (p *Pretty) Render(r Renderable) {
	if p == nil || r == nil {
		return
	}
	_ = r.Render(p.writer)
}

// Panel draws a simple bordered block with a title bar.
func (p *Pretty) Panel(title, content string) {
	if p == nil {
		return
	}
	buf := GetBuffer(256)
	defer PutBuffer(buf)
	buf.WriteString(p.theme.CachedMessageFg())
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

// Raw writes text directly to the writer without any formatting.
func (p *Pretty) Raw(text string) {
	if p == nil {
		return
	}
	_, _ = io.WriteString(p.writer, text)
}

// Success prints a success-styled message. Nil-safe: falls back to stdout.
func (p *Pretty) Success(message string) {
	if p == nil {
		fmt.Println("✅ " + message)
		return
	}
	p.printStyled("✅", message, p.theme.cachedLevelCode(LevelInfo))
}

// Warn prints a warning-styled message. Nil-safe: falls back to stdout.
func (p *Pretty) Warn(message string) {
	if p == nil {
		fmt.Println("⚠️ " + message)
		return
	}
	p.printStyled("⚠️", message, p.theme.cachedLevelCode(LevelWarn))
}

// Error prints an error-styled message. Nil-safe: falls back to stdout.
func (p *Pretty) Error(message string) {
	if p == nil {
		fmt.Println("❌ " + message)
		return
	}
	p.printStyled("❌", message, p.theme.cachedLevelCode(LevelError))
}

// Info prints an info-styled message. Nil-safe: falls back to stdout.
func (p *Pretty) Info(message string) {
	if p == nil {
		fmt.Println("ℹ️ " + message)
		return
	}
	p.printStyled("ℹ️", message, p.theme.cachedLevelCode(LevelInfo))
}

// Muted prints a dimmed message using the timestamp/muted colour — useful for secondary
// output that should recede visually (hints, paths, supplementary context).
func (p *Pretty) Muted(message string) {
	if p == nil {
		fmt.Println(message)
		return
	}
	p.printStyled("", message, p.theme.cachedTimestampFgStr())
}

// Debug prints a debug-styled message. Nil-safe: falls back to stdout.
func (p *Pretty) Debug(message string) {
	if p == nil {
		fmt.Println("🐛 " + message)
		return
	}
	p.printStyled("🐛", message, p.theme.cachedLevelCode(LevelDebug))
}

// printStyled writes an ANSI-coloured line. Write errors are silently dropped —
// pretty printing must never fail the caller.
func (p *Pretty) printStyled(icon, message, ansiCode string) {
	buf := GetBuffer(128)
	defer PutBuffer(buf)
	buf.WriteString(ansiCode)
	if icon != "" {
		buf.WriteString(icon)
		buf.WriteString(" ")
	}
	buf.WriteString(message)
	buf.WriteString(Reset)
	buf.WriteString("\n")
	_, _ = buf.WriteTo(p.writer)
}

// CreateBanner renders a double-border banner box with ASCII art, title, version, and URL.
// Useful for CLI splash screens. Returns a string ready to print or pass to Logger.Banner.
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

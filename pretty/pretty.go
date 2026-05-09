package pretty

import (
	"fmt"
	"strings"
)

// SystemInfo is startup/configuration metadata for display.
// Kept here so the renderable.go shim and existing callers compile until Phase 1c
// removes this package. New code should use velocity.SystemInfoData directly.
type SystemInfo struct {
	Title   string
	Version string
	Fields  []KeyValuePair
}

// KeyValuePair is a labelled string value.
// Kept here to match the SystemInfo fields slice until Phase 1c.
type KeyValuePair struct {
	Key   string
	Value string
}

// TreeItem represents a node in a hierarchical display tree.
// Kept here so renderable_test.go and examples compile until Phase 1c.
type TreeItem struct {
	Key      string
	Value    any
	Children []TreeItem
}

// CreateBanner renders a double-border banner box with ASCII art, title, version, and URL.
// Kept in pretty/ because the examples still reference it here; Phase 1c relocates it.
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

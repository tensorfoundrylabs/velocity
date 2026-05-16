package velocity

import "strings"

const (
	secureOpen  = "<secure>"
	secureClose = "</secure>"
)

// redactSecureTags replaces all <secure>...</secure> spans in s with mark.
// Called only when entry.maybeSecure is true so the string scan is pay-for-use.
// One allocation per affected message (strings.Builder).
func redactSecureTags(s, mark string) string {
	if !strings.Contains(s, secureOpen) {
		return s
	}
	var b strings.Builder
	for {
		start := strings.Index(s, secureOpen)
		if start < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		s = s[start+len(secureOpen):]
		end := strings.Index(s, secureClose)
		if end < 0 {
			// Unclosed tag — emit the mark and stop.
			b.WriteString(mark)
			break
		}
		b.WriteString(mark)
		s = s[end+len(secureClose):]
	}
	return b.String()
}

// stripSecureTags removes the <secure> and </secure> markers from s, leaving
// the content between them intact. Used by trusted TTY console writers to show
// plaintext while stripping the markup.
func stripSecureTags(s string) string {
	if !strings.Contains(s, secureOpen) {
		return s
	}
	s = strings.ReplaceAll(s, secureOpen, "")
	s = strings.ReplaceAll(s, secureClose, "")
	return s
}

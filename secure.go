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

// applySecureTags applies the entry's secure-tag policy to an arbitrary text
// payload (group item text, continuation lines). active is the entry's
// maybeSecure flag, so the string scan stays pay-for-use: no work happens on
// entries whose message and payloads never contained '<' while scanning was on.
func applySecureTags(s string, active, trusted bool, redactionMark string) string {
	if !active {
		return s
	}
	if trusted {
		return stripSecureTags(s)
	}
	return redactSecureTags(s, redactionMark)
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

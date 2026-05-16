package velocity

import (
	"os"
	"sync"
)

// HyperlinkFallback controls how Hyperlink renders when OSC 8 is not supported.
type HyperlinkFallback uint8

const (
	// HyperlinkFallbackParens renders as "text (uri)" — the default.
	HyperlinkFallbackParens HyperlinkFallback = iota
	// HyperlinkFallbackNone renders as "text" only. Zero-alloc when hyperlinks
	// are unsupported; the input text string is returned directly.
	HyperlinkFallbackNone
	// HyperlinkFallbackBrackets renders as "text [uri]".
	HyperlinkFallbackBrackets
)

// HyperlinkOption configures Hyperlink behaviour.
type HyperlinkOption func(*hyperlinkOptions)

type hyperlinkOptions struct {
	fallback HyperlinkFallback
}

// WithHyperlinkFallback sets the fallback rendering mode used when the
// terminal does not support OSC 8 hyperlinks.
func WithHyperlinkFallback(f HyperlinkFallback) HyperlinkOption {
	return func(o *hyperlinkOptions) {
		o.fallback = f
	}
}

// hyperlinkOnce guards a single env-var read; the result is stable for the
// process lifetime.  VELOCITY_HYPERLINKS=1 force-enables, =0 force-disables —
// useful in tests that cannot construct a real supporting terminal.
var (
	hyperlinkOnce      sync.Once
	hyperlinkSupported bool
)

// HyperlinksSupported reports whether the current terminal supports OSC 8.
// The result is cached after the first call. Useful for branching logic at call
// sites that need to choose between plain and hyperlinked output.
func HyperlinksSupported() bool {
	hyperlinkOnce.Do(detectHyperlinkSupport)
	return hyperlinkSupported
}

// detectHyperlinkSupport runs once. Order of precedence:
//  1. VELOCITY_HYPERLINKS=1|0 explicit override (tests + CI scripts)
//  2. $TERM_PROGRAM: iTerm.app, WezTerm, vscode, Terminus
//  3. $WT_SESSION: Windows Terminal sets this to a non-empty GUID
//  4. $KITTY_WINDOW_ID: set by kitty terminal
func detectHyperlinkSupport() {
	switch os.Getenv("VELOCITY_HYPERLINKS") {
	case "1":
		hyperlinkSupported = true
		return
	case "0":
		hyperlinkSupported = false
		return
	}

	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "Terminus":
		hyperlinkSupported = true
		return
	}

	if os.Getenv("WT_SESSION") != "" {
		hyperlinkSupported = true
		return
	}

	if os.Getenv("KITTY_WINDOW_ID") != "" {
		hyperlinkSupported = true
		return
	}
}

// Hyperlink wraps text in an OSC 8 hyperlink sequence when the terminal
// supports it, otherwise returns a fallback string.
//
// OSC 8 sequence: \x1b]8;;<uri>\x07<text>\x1b]8;;\x07
//
// Detection is cached via sync.Once on first call. Override via:
//   - VELOCITY_HYPERLINKS=1  force enable (useful in tests)
//   - VELOCITY_HYPERLINKS=0  force disable
//
// Theme colouring is intentionally NOT applied here. To produce a coloured
// hyperlink, compose with Theme.Format:
//
//	theme.Format(SlotHyperlink, velocity.Hyperlink(uri, text))
//
// Empty text returns an empty string regardless of hyperlink support — there
// is nothing meaningful to wrap. Empty uri with non-empty text falls through
// to the fallback (the URI would be empty in the OSC sequence, which most
// terminals treat as clearing the link; we skip it rather than emit noise).
func Hyperlink(uri, text string, opts ...HyperlinkOption) string {
	if text == "" {
		return ""
	}

	o := hyperlinkOptions{fallback: HyperlinkFallbackParens}
	for _, opt := range opts {
		opt(&o)
	}

	// Skip the OSC sequence when the URI is empty — a zero-length URI in the
	// sequence is valid per the spec (it closes an active link) but emitting it
	// mid-string where no link was opened produces invisible garbage in most
	// terminals. Fall through to fallback instead.
	if uri == "" || !HyperlinksSupported() {
		return fallbackHyperlink(uri, text, o.fallback)
	}

	return "\x1b]8;;" + uri + "\x07" + text + "\x1b]8;;\x07"
}

// fallbackHyperlink returns the plain-text representation for terminals that
// do not support OSC 8. HyperlinkFallbackNone is the only zero-alloc path.
func fallbackHyperlink(uri, text string, mode HyperlinkFallback) string {
	switch mode {
	case HyperlinkFallbackNone:
		return text
	case HyperlinkFallbackBrackets:
		if uri == "" {
			return text
		}
		return text + " [" + uri + "]"
	default: // HyperlinkFallbackParens
		if uri == "" {
			return text
		}
		return text + " (" + uri + ")"
	}
}

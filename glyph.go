package velocity

import (
	"os"
	"sync"
)

// glyphOnce guards a single env-var read; the result is stable for the process lifetime.
// VELOCITY_GLYPHS=1 force-enables, =0 force-disables — useful in tests.
var (
	glyphOnce      sync.Once
	glyphsDetected bool
)

// GlyphsSupported reports whether Unicode indicator glyphs (⏱, →) should be emitted.
// Result is cached after the first call. Override via VELOCITY_GLYPHS=1|0.
func GlyphsSupported() bool {
	glyphOnce.Do(detectGlyphSupport)
	return glyphsDetected
}

// detectGlyphSupport runs once. Order of precedence:
//  1. VELOCITY_GLYPHS=1|0 explicit override (tests + CI)
//  2. TERM_PROGRAM: modern terminal emulators that reliably support Unicode
//  3. WT_SESSION: Windows Terminal
//  4. KITTY_WINDOW_ID: kitty terminal
//  5. Default: disabled (safest for pipes and unknown terminals)
func detectGlyphSupport() {
	switch os.Getenv("VELOCITY_GLYPHS") {
	case "1":
		glyphsDetected = true
		return
	case "0":
		glyphsDetected = false
		return
	}

	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "Terminus":
		glyphsDetected = true
		return
	}

	if os.Getenv("WT_SESSION") != "" {
		glyphsDetected = true
		return
	}

	if os.Getenv("KITTY_WINDOW_ID") != "" {
		glyphsDetected = true
		return
	}

	// Conservative default: ASCII fallbacks for pipes, CI, unknown terminals.
}

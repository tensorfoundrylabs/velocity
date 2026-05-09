// Hyperlinks example demonstrates OSC 8 terminal hyperlinks.
//
// OSC 8 is the escape sequence that makes terminal text into a clickable link,
// like an <a href> in a browser. Terminals that support it (iTerm2, WezTerm,
// Windows Terminal, kitty, VSCode integrated terminal) show the display text
// underlined; clicking it opens the URI.
//
// Detection is automatic. Override with:
//
//	VELOCITY_HYPERLINKS=1   force enable  (always render OSC 8)
//	VELOCITY_HYPERLINKS=0   force disable (always use fallback)
//
// Run to see the default detection result:
//
//	go run ./examples/hyperlinks
//
// Force-enable to see OSC 8 sequences in a non-supporting terminal:
//
//	VELOCITY_HYPERLINKS=1 go run ./examples/hyperlinks
package main

import (
	"fmt"
	"os"

	"github.com/tensorfoundrylabs/velocity"
)

func main() {
	log := velocity.New(
		velocity.WithDevelopment(),
		velocity.WithConsoleOutput(os.Stdout),
	)

	supported := velocity.HyperlinksSupported()
	fmt.Printf("OSC 8 support detected: %v\n", supported)
	fmt.Printf("(override with VELOCITY_HYPERLINKS=1 or =0)\n")
	fmt.Println()

	// --- Plain vs hyperlinked text ---
	//
	// On a supporting terminal the second line is clickable; both lines read
	// the same in a non-supporting terminal (Parens fallback appends the URL).
	plain := "https://tensorfoundry.io/docs"
	linked := velocity.Hyperlink("https://tensorfoundry.io/docs", "velocity docs")

	fmt.Println("Plain URL:", plain)
	fmt.Println("Hyperlink:", linked)
	fmt.Println()

	// --- Fallback modes ---
	//
	// When OSC 8 is not supported (or force-disabled), Hyperlink returns plain
	// text in one of three forms. Use VELOCITY_HYPERLINKS=0 to see these live.
	// HyperlinkFallbackParens is the default; None is the zero-alloc path.
	uri := "https://tensorfoundry.io/setup"
	text := "complete setup"

	// Demonstrate the fallback output directly — independent of terminal support.
	fmt.Println("Fallback modes (seen when VELOCITY_HYPERLINKS=0 or no OSC 8 support):")
	fmt.Printf("  Parens   : %s\n", text+" ("+uri+")")
	fmt.Printf("  Brackets : %s\n", text+" ["+uri+"]")
	fmt.Printf("  None     : %s\n", text)
	fmt.Println()
	fmt.Println("Same call, current terminal:")
	fmt.Printf("  Parens   : %s\n", velocity.Hyperlink(uri, text, velocity.WithHyperlinkFallback(velocity.HyperlinkFallbackParens)))
	fmt.Printf("  Brackets : %s\n", velocity.Hyperlink(uri, text, velocity.WithHyperlinkFallback(velocity.HyperlinkFallbackBrackets)))
	fmt.Printf("  None     : %s\n", velocity.Hyperlink(uri, text, velocity.WithHyperlinkFallback(velocity.HyperlinkFallbackNone)))
	fmt.Println()

	// --- Combining with Theme.Format ---
	//
	// Hyperlink returns a plain string; wrap it with Theme.Format to apply
	// colour. The OSC 8 sequence and ANSI colour codes compose correctly in
	// all supporting terminals.
	style := log.Style()
	coloured := style.Format(velocity.SlotHyperlink, velocity.Hyperlink(uri, "Open setup page"))
	fmt.Println("Coloured hyperlink:", coloured)
	fmt.Println()

	// --- Inside a Box ---
	//
	// Renderable types treat Hyperlink output as ordinary strings, so they
	// work inside Box content, Table cells, and ContinuationBlock lines.
	setupURL := velocity.Hyperlink("https://tensorfoundry.io/setup?token=abc123", "https://tensorfoundry.io/setup?token=abc123")
	docsURL := velocity.Hyperlink("https://tensorfoundry.io/docs", "documentation")

	box := velocity.NewBox(
		"Setup Required",
		"Open the following URL to complete your installation:\n\n"+
			"  "+setupURL+"\n\n"+
			"See the "+docsURL+" for details.",
		velocity.ThemeNightOwl,
	)
	log.Render(box)
	log.Newline()

	// --- Inside a Table cell ---
	//
	// Column-width calculation strips ANSI codes but the OSC 8 sequences are
	// zero-width markup, so cell alignment is preserved.
	log.RenderRaw(velocity.NewTable(
		[]string{"Resource", "URL"},
		[][]string{
			{"API reference", velocity.Hyperlink("https://pkg.go.dev/github.com/tensorfoundrylabs/velocity", "pkg.go.dev")},
			{"Source code", velocity.Hyperlink("https://github.com/tensorfoundrylabs/velocity", "github.com")},
			{"Changelog", velocity.Hyperlink("https://github.com/tensorfoundrylabs/velocity/releases", "releases")},
		},
		velocity.ThemeNightOwl,
	))
	log.Newline()

	// --- Inside a ContinuationBlock ---
	//
	// The canonical server-startup pattern: listening address and dashboard as
	// clickable links, all grouped under one timestamped INFO entry.
	log.Continue(velocity.LevelInfo, "Server listening",
		"API:       "+velocity.Hyperlink("http://localhost:8080", "http://localhost:8080"),
		"Metrics:   "+velocity.Hyperlink("http://localhost:9090/metrics", "http://localhost:9090/metrics"),
		"Dashboard: "+velocity.Hyperlink("http://localhost:3000", "http://localhost:3000"),
	)
}

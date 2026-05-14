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
// Pipe to confirm no OSC 8 control sequences appear in non-TTY output:
//
//	go run ./examples/hyperlinks | cat
//
// Force-enable to see OSC 8 sequences in a non-supporting terminal:
//
//	VELOCITY_HYPERLINKS=1 go run ./examples/hyperlinks
//
// Note: HyperlinksSupported() is per-process, not per-fd. When stdout is a pipe
// but the parent terminal supports OSC 8, the env-based detection may still
// return true. Use WithHyperlinkFallback(HyperlinkFallbackNone) or gate on
// IsTerminalWriter(os.Stdout) when writing to potentially non-TTY destinations.
package main

import (
	"fmt"
	"os"

	"github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	// Gate OSC 8 on whether stdout is actually a terminal. HyperlinksSupported()
	// checks the env var and terminal type but does not know whether stdout has
	// been redirected — IsTerminalWriter catches the pipe/redirect case.
	stdoutIsTTY := velocity.IsTerminalWriter(os.Stdout)

	log := velocity.New(
		velocity.WithDevelopment(),
		velocity.WithConsoleOutput(os.Stdout),
	)

	supported := velocity.HyperlinksSupported() && stdoutIsTTY
	fmt.Printf("OSC 8 support detected: %v\n", velocity.HyperlinksSupported())
	fmt.Printf("stdout is a terminal: %v\n", stdoutIsTTY)
	fmt.Printf("hyperlinks active: %v\n", supported)
	fmt.Printf("(override with VELOCITY_HYPERLINKS=1 or =0)\n")
	fmt.Println()

	// --- Plain vs hyperlinked text ---
	//
	// On a supporting terminal the second line is clickable; both lines read
	// the same in a non-supporting terminal (Parens fallback appends the URL).
	plain := "https://tensorfoundry.io/docs"

	// Use HyperlinkFallbackNone when stdout is not a TTY to avoid leaking
	// OSC 8 sequences into pipes or files.
	var linked string
	if stdoutIsTTY {
		linked = velocity.Hyperlink("https://tensorfoundry.io/docs", "velocity docs")
	} else {
		linked = velocity.Hyperlink("https://tensorfoundry.io/docs", "velocity docs",
			velocity.WithHyperlinkFallback(velocity.HyperlinkFallbackNone))
	}

	fmt.Println("Plain URL:", plain)
	fmt.Println("Hyperlink:", linked)
	fmt.Println()

	// --- Fallback modes ---
	//
	// When OSC 8 is not supported (or force-disabled via VELOCITY_HYPERLINKS=0),
	// Hyperlink returns plain text in one of three forms.
	// These are shown with their literal output — independent of terminal support.
	uri := "https://tensorfoundry.io/setup"
	text := "complete setup"

	fmt.Println("Fallback modes (seen when VELOCITY_HYPERLINKS=0 or no OSC 8 support):")
	fmt.Printf("  Parens   : %s\n", text+" ("+uri+")")
	fmt.Printf("  Brackets : %s\n", text+" ["+uri+"]")
	fmt.Printf("  None     : %s\n", text)
	fmt.Println()

	// The three fallback variants are only meaningfully different when OSC 8 is
	// disabled; when it is active all three emit the same OSC 8 sequence and look
	// identical on a supporting terminal. Show them only under the disabled banner.
	if !supported {
		fmt.Println("Fallback variants (OSC 8 disabled — differences visible here):")
		fmt.Printf("  Parens   : %s\n", velocity.Hyperlink(uri, text, velocity.WithHyperlinkFallback(velocity.HyperlinkFallbackParens)))
		fmt.Printf("  Brackets : %s\n", velocity.Hyperlink(uri, text, velocity.WithHyperlinkFallback(velocity.HyperlinkFallbackBrackets)))
		fmt.Printf("  None     : %s\n", velocity.Hyperlink(uri, text, velocity.WithHyperlinkFallback(velocity.HyperlinkFallbackNone)))
		fmt.Println()
	}

	// --- Combining with Theme.Format ---
	//
	// Hyperlink returns a plain string; wrap it with Theme.Format to apply
	// colour. The OSC 8 sequence and ANSI colour codes compose correctly in
	// all supporting terminals. When stdout is not a TTY, Hyperlink returns
	// plain text so no escape sequences reach the pipe.
	style := log.Style()
	setupLink := velocity.Hyperlink(uri, "Open setup page")
	if !stdoutIsTTY {
		setupLink = velocity.Hyperlink(uri, "Open setup page", velocity.WithHyperlinkFallback(velocity.HyperlinkFallbackParens))
	}
	coloured := style.Format(velocity.SlotHyperlink, setupLink)
	fmt.Println("Coloured hyperlink:", coloured)
	fmt.Println()

	// --- Inside a Box ---
	//
	// Renderable types treat Hyperlink output as ordinary strings, so they
	// work inside Box content, Table cells, and ContinuationBlock lines.
	// Gate on stdoutIsTTY to keep OSC 8 out of piped output.
	var setupURL, docsURL string
	if stdoutIsTTY {
		setupURL = velocity.Hyperlink("https://tensorfoundry.io/setup?token=abc123", "https://tensorfoundry.io/setup?token=abc123")
		docsURL = velocity.Hyperlink("https://tensorfoundry.io/docs", "documentation")
	} else {
		setupURL = "https://tensorfoundry.io/setup?token=abc123"
		docsURL = "documentation (https://tensorfoundry.io/docs)"
	}

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
	// When stdout is a pipe, plain text is used to avoid leaking escape sequences.
	tableRows := [][]string{
		{"API reference", "https://pkg.go.dev/github.com/tensorfoundrylabs/velocity/v2"},
		{"Source code", "https://github.com/tensorfoundrylabs/velocity"},
		{"Changelog", "https://github.com/tensorfoundrylabs/velocity/releases"},
	}
	if stdoutIsTTY {
		tableRows = [][]string{
			{"API reference", velocity.Hyperlink("https://pkg.go.dev/github.com/tensorfoundrylabs/velocity/v2", "pkg.go.dev")},
			{"Source code", velocity.Hyperlink("https://github.com/tensorfoundrylabs/velocity", "github.com")},
			{"Changelog", velocity.Hyperlink("https://github.com/tensorfoundrylabs/velocity/releases", "releases")},
		}
	}
	log.RenderRaw(velocity.NewTable(
		[]string{"Resource", "URL"},
		tableRows,
		velocity.ThemeNightOwl,
	))
	log.Newline()

	// --- Inside a ContinuationBlock ---
	//
	// The canonical server-startup pattern: listening address and dashboard as
	// clickable links, all grouped under one timestamped INFO entry.
	// Plain URLs are used when stdout is piped, so no OSC 8 sequences reach the file.
	apiURL := "http://localhost:8080"
	metricsURL := "http://localhost:9090/metrics"
	dashURL := "http://localhost:3000"
	if stdoutIsTTY {
		apiURL = velocity.Hyperlink(apiURL, apiURL)
		metricsURL = velocity.Hyperlink(metricsURL, metricsURL)
		dashURL = velocity.Hyperlink(dashURL, dashURL)
	}
	log.Continue(velocity.LevelInfo, "Server listening",
		"API:       "+apiURL,
		"Metrics:   "+metricsURL,
		"Dashboard: "+dashURL,
	)
}

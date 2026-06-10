package velocity

import (
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// Format is the output format for structured (JSON) writers.
type Format int

const (
	FormatJSON Format = iota
)

func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "json"
	default:
		return "unknown"
	}
}

// FieldDisplayMode controls whether fields render inline or as a tree.
type FieldDisplayMode int

const (
	FieldDisplayInline FieldDisplayMode = iota
	FieldDisplayTree
)

func (m FieldDisplayMode) String() string {
	switch m {
	case FieldDisplayInline:
		return "inline"
	case FieldDisplayTree:
		return "tree"
	default:
		return "unknown"
	}
}

// FatalHandler is called after a Fatal log entry is written.
// Default behaviour is os.Exit(1). Override in tests to prevent process exit.
type FatalHandler func()

// inlineIndicators holds config for the opt-in compact header indicators feature.
// The zero value means everything is disabled, so existing behaviour is unchanged
// unless the caller explicitly enables it via WithComponentField / WithComponentStyling etc.
// removeFromTree defaults to true only when the feature is actively enabled — it is
// meaningless when component == false, so we derive the effective value at render time.
type inlineIndicators struct {
	componentField string
	countFields    []string // small N; linear scan is fine
	timingFields   []string
	statePairs     [][2]string

	componentWidth int

	component  bool
	showGlyphs bool
	// glyphsExplicit is true when WithInlineGlyphs was called. When false,
	// the render path uses glyphsSupported() (runtime VELOCITY_GLYPHS detection)
	// instead of the showGlyphs value.
	glyphsExplicit bool
	removeFromTree bool // true = promoted fields are hidden from the tree
}

// config holds all logger configuration. Unexported — callers configure via Options.
type config struct {
	ConsoleOutput io.Writer

	StructuredOutput io.Writer
	Sampler          Sampler

	// NotifyOutput is the destination for Notify/NotifyLines/NotifyBox calls.
	// Defaults to os.Stderr. Override via WithNotifyOutput — useful in tests
	// where stderr is not captured by the test runner.
	NotifyOutput io.Writer

	FatalHandler FatalHandler

	ConsoleTheme    *Theme
	DisplayTimezone *time.Location

	TimeFormat string

	Indicators inlineIndicators

	StructuredFormat Format

	BufferSize    int
	FieldPoolSize int

	FieldDisplayMode FieldDisplayMode
	// CallerSkip is extra frames to skip beyond the standard 4; use for wrapper functions.
	CallerSkip int

	ConsoleLevel    Level
	StructuredLevel Level

	DisableColour bool
	AddCaller     bool

	// DisableSecureTags permanently disables the <secure>...</secure> message scanner.
	// Set via WithSecureTags(false). When true, no IndexByte scan runs on any log call
	// regardless of which writers are attached. Use for extreme-perf consumers that
	// never embed sensitive data in message strings.
	DisableSecureTags bool
}

func defaultConfig() *config {
	return &config{
		ConsoleOutput:    os.Stdout,
		ConsoleLevel:     LevelDebug,
		StructuredOutput: nil,
		StructuredFormat: FormatJSON,
		StructuredLevel:  LevelInfo,
		BufferSize:       1024,
		FieldPoolSize:    100,
		TimeFormat:       "2006-01-02T15:04:05Z07:00",
		DisplayTimezone:  time.Local,
		FieldDisplayMode: FieldDisplayInline,
	}
}

// isTerminal reports whether f is connected to a terminal.
// Uses term.IsTerminal for all *os.File values (not just the three std streams)
// to match IsTerminalWriter's behaviour and avoid false-negatives on arbitrary
// file handles like those from os.OpenFile or pty wrappers.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: uintptr fd fits in int on all supported platforms
}

// resolveColourForWriter reports whether ANSI colour should be emitted to w,
// applying the standard environment overrides in priority order:
//
//  1. NO_COLOR=<non-empty>  — always disable (https://no-color.org)
//  2. FORCE_COLOR=<non-empty> — always enable
//  3. term.IsTerminal        — auto-detect from the file descriptor
//
// Windows terminal emulators (VS Code, Git Bash, Windows Terminal) often
// present stdout as a named pipe rather than a console handle, which causes
// term.IsTerminal to return false even on a real terminal. FORCE_COLOR=1 is
// the documented escape hatch for those environments.
func resolveColourForWriter(w io.Writer) bool {
	// NO_COLOR has highest priority — explicit opt-out.
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	// FORCE_COLOR overrides TTY detection — explicit opt-in.
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	// Fall back to fd-level detection.
	return IsTerminalWriter(w)
}

// IsTerminalWriter reports whether w is a terminal, using term.IsTerminal when possible.
// Used to auto-detect colour support.
//
// Note: on Windows, terminal emulators that run shells as child processes (VS Code,
// Git Bash, Windows Terminal) may proxy stdout through a pipe, causing this to return
// false even when the output is visible in a colour-capable terminal. In that case,
// set FORCE_COLOR=1 to override detection, or use resolveColourForWriter which
// handles both env vars and fd detection.
func IsTerminalWriter(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: uintptr fd fits in int on all supported platforms
	}
	return false
}

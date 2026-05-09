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

	TimeFormat       string
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
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}

	switch f {
	case os.Stdout, os.Stderr, os.Stdin:
		stat, err := f.Stat()
		if err != nil {
			return false
		}
		// ModeCharDevice is set for character devices (terminals).
		return (stat.Mode() & os.ModeCharDevice) != 0
	default:
		return false
	}
}

// IsTerminalWriter reports whether w is a terminal, using term.IsTerminal when possible.
// Used to auto-detect colour support.
func IsTerminalWriter(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: uintptr fd fits in int on all supported platforms
	}
	return false
}

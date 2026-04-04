package velocity

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

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

type Config struct {
	ConsoleOutput io.Writer

	// FatalHandler overrides the default os.Exit(1) called after Fatal().
	// Useful in tests. If nil, defaults to os.Exit(1).
	FatalHandler FatalHandler

	StructuredOutput io.Writer
	ConsoleTheme     *Theme
	Sampler          Sampler

	// Logs are always stored in UTC, but can be displayed in a different timezone
	DisplayTimezone *time.Location

	TimeFormat string

	StructuredFormat Format

	BufferSize    int
	FieldPoolSize int

	FieldDisplayMode FieldDisplayMode
	ConsoleLevel     Level

	StructuredLevel Level

	DisableColour bool

	// AddCaller enables capturing file:line and function name for each log entry
	AddCaller bool
	// CallerSkip is the number of stack frames to skip when capturing caller information
	// Default is 0, increase for wrapper functions
	CallerSkip int
}

func DefaultConfig() *Config {
	return &Config{
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

type Builder struct {
	config *Config
}

func NewConfig() *Builder {
	return &Builder{
		config: DefaultConfig(),
	}
}

func (b *Builder) WithLevel(level Level) *Builder {
	b.config.ConsoleLevel = level
	return b
}

func (b *Builder) WithFormat(format Format) *Builder {
	b.config.StructuredFormat = format
	return b
}

func (b *Builder) WithOutput(w io.Writer) *Builder {
	b.config.ConsoleOutput = w
	return b
}

func (b *Builder) WithStructuredOutput(w io.Writer) *Builder {
	b.config.StructuredOutput = w
	return b
}

func (b *Builder) WithStructuredLevel(level Level) *Builder {
	b.config.StructuredLevel = level
	return b
}

func (b *Builder) WithTheme(theme *Theme) *Builder {
	b.config.ConsoleTheme = theme
	return b
}

func (b *Builder) WithTimeFormat(format string) *Builder {
	b.config.TimeFormat = format
	return b
}

func (b *Builder) WithBufferSize(size int) *Builder {
	b.config.BufferSize = size
	return b
}

func (b *Builder) WithFieldPoolSize(size int) *Builder {
	b.config.FieldPoolSize = size
	return b
}

func (b *Builder) WithColour(enabled bool) *Builder {
	b.config.DisableColour = !enabled
	return b
}

func (b *Builder) DisableColour() *Builder {
	b.config.DisableColour = true
	return b
}

func (b *Builder) WithSampling(initial, thereafter uint32) *Builder {
	b.config.Sampler = NewCountSampler(uint64(initial), uint64(thereafter))
	return b
}

// WithDisplayTimezone sets the timezone for displaying timestamps in console output.
// Logs are always stored in UTC internally, but this controls how they're displayed.
func (b *Builder) WithDisplayTimezone(tz string) (*Builder, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return b, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	b.config.DisplayTimezone = loc
	return b, nil
}

func (b *Builder) MustWithDisplayTimezone(tz string) *Builder {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		panic(fmt.Sprintf("velocity: invalid timezone %q: %v", tz, err))
	}
	b.config.DisplayTimezone = loc
	return b
}

func (b *Builder) WithFieldDisplayMode(mode FieldDisplayMode) *Builder {
	b.config.FieldDisplayMode = mode
	return b
}

func (b *Builder) WithFatalHandler(fn FatalHandler) *Builder {
	b.config.FatalHandler = fn
	return b
}

func (b *Builder) Build() (*Config, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b.config, nil
}

func (b *Builder) MustBuild() *Config {
	cfg, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("velocity: invalid configuration: %v", err))
	}
	return cfg
}

func (b *Builder) validate() error {
	if b.config.BufferSize < 256 {
		return fmt.Errorf("buffer size must be at least 256 bytes, got %d", b.config.BufferSize)
	}
	if b.config.BufferSize > 1024*1024 {
		return fmt.Errorf("buffer size must not exceed 1MB, got %d", b.config.BufferSize)
	}

	if b.config.FieldPoolSize < 0 {
		return fmt.Errorf("field pool size must not be negative, got %d", b.config.FieldPoolSize)
	}
	if b.config.FieldPoolSize > 10000 {
		return fmt.Errorf("field pool size must not exceed 10000, got %d", b.config.FieldPoolSize)
	}

	if b.config.Sampler != nil {
		// CountSampler validation (check if it's our concrete type)
		if cs, ok := b.config.Sampler.(*CountSampler); ok {
			if cs.Initial == 0 && cs.Thereafter == 0 {
				return errors.New("sampling initial and thereafter counts must not both be zero")
			}
		}
	}

	return nil
}

func (b *Builder) Clone() *Builder {
	cfgCopy := *b.config
	// Sampler is copied by value - interface reference is shared
	// If deep copy is needed for custom samplers, implement Clone() on sampler
	return &Builder{
		config: &cfgCopy,
	}
}

// DefaultDevelopmentConfig creates a config for development: coloured console, debug level, no structured output.
func DefaultDevelopmentConfig() *Config {
	return &Config{
		ConsoleOutput:    os.Stdout,
		ConsoleTheme:     nil,
		ConsoleLevel:     LevelDebug,
		StructuredOutput: nil,
		StructuredFormat: FormatJSON,
		StructuredLevel:  LevelOff,
		BufferSize:       1024,
		FieldPoolSize:    50,
		DisableColour:    false,
		TimeFormat:       "2006-01-02 15:04:05",
		DisplayTimezone:  time.Local,
	}
}

// DefaultProductionConfig creates a config for production: JSON output, info level, no console.
func DefaultProductionConfig() *Config {
	return &Config{
		ConsoleOutput:    io.Discard,
		ConsoleTheme:     nil,
		ConsoleLevel:     LevelOff,
		StructuredOutput: nil,
		StructuredFormat: FormatJSON,
		StructuredLevel:  LevelInfo,
		BufferSize:       4096,
		FieldPoolSize:    200,
		DisableColour:    true,
		TimeFormat:       "2006-01-02T15:04:05Z07:00",
	}
}

// DefaultContainerConfig creates a config for containerised environments: JSON to stdout, info level.
func DefaultContainerConfig() *Config {
	disableColour := !isTerminal(os.Stdout)

	return &Config{
		ConsoleOutput:    nil,
		ConsoleTheme:     nil,
		ConsoleLevel:     LevelOff,
		StructuredOutput: os.Stdout,
		StructuredFormat: FormatJSON,
		StructuredLevel:  LevelInfo,
		BufferSize:       2048,
		FieldPoolSize:    100,
		DisableColour:    disableColour,
		TimeFormat:       "2006-01-02T15:04:05Z07:00",
	}
}

// DefaultTestingConfig creates a config for tests: writes to w, debug level, colours off.
func DefaultTestingConfig(w io.Writer) *Config {
	return &Config{
		ConsoleOutput:    w,
		ConsoleTheme:     nil,
		ConsoleLevel:     LevelDebug,
		StructuredOutput: nil,
		StructuredFormat: FormatJSON,
		StructuredLevel:  LevelOff,
		BufferSize:       512,
		FieldPoolSize:    25,
		DisableColour:    true,
		TimeFormat:       "15:04:05.000",
	}
}

// DefaultHighPerformanceConfig creates a config for high throughput: minimal output, sampling enabled.
func DefaultHighPerformanceConfig() *Config {
	return &Config{
		ConsoleOutput:    io.Discard,
		ConsoleTheme:     nil,
		ConsoleLevel:     LevelOff,
		StructuredOutput: os.Stderr,
		StructuredFormat: FormatJSON,
		StructuredLevel:  LevelInfo,
		BufferSize:       8192,
		FieldPoolSize:    500,
		DisableColour:    true,
		TimeFormat:       "2006-01-02T15:04:05Z07:00",
		Sampler:          NewCountSampler(uint64(1000), uint64(100)),
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

func PresetDevelopment() *Builder {
	return &Builder{config: DefaultDevelopmentConfig()}
}

func PresetProduction() *Builder {
	return &Builder{config: DefaultProductionConfig()}
}

func PresetContainer() *Builder {
	return &Builder{config: DefaultContainerConfig()}
}

func PresetTesting(w io.Writer) *Builder {
	return &Builder{config: DefaultTestingConfig(w)}
}

func PresetHighPerformance() *Builder {
	return &Builder{config: DefaultHighPerformanceConfig()}
}

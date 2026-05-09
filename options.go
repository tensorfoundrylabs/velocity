package velocity

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Option is a functional option that mutates a config during logger construction.
// Options are applied in order, so later options override earlier ones.
// Preset options (WithDevelopment, WithProduction, etc.) reset the config to a
// known baseline; layering overrides after them is the intended pattern.
type Option func(*config)

// WithDevelopment resets config to development defaults: coloured console on
// stdout, debug level, local timezone, no structured output.
func WithDevelopment() Option {
	return func(c *config) {
		c.ConsoleOutput = io.Writer(nil) // reset first, then assign
		*c = config{
			ConsoleOutput:    defaultStdout(),
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
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// WithProduction resets config to production defaults: JSON to stdout at info level,
// no console output, UTC timestamps.
func WithProduction() Option {
	return func(c *config) {
		*c = config{
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
			DisplayTimezone:  time.UTC,
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// WithContainer resets config for containerised environments: JSON to stdout at
// info level, colour disabled unless stdout is a TTY.
func WithContainer() Option {
	return func(c *config) {
		*c = config{
			ConsoleOutput:    nil,
			ConsoleTheme:     nil,
			ConsoleLevel:     LevelOff,
			StructuredOutput: defaultStdout(),
			StructuredFormat: FormatJSON,
			StructuredLevel:  LevelInfo,
			BufferSize:       2048,
			FieldPoolSize:    100,
			DisableColour:    !isTerminal(defaultStdoutFile()),
			TimeFormat:       "2006-01-02T15:04:05Z07:00",
			DisplayTimezone:  time.UTC,
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// TestingT is the subset of *testing.T needed by WithTesting.
// Defined here to avoid importing the testing package in the core library.
type TestingT interface {
	Log(args ...any)
	Cleanup(func())
	Helper()
}

// WithTesting configures a logger for use in tests. Writes via t.Log, disables
// colour, sets level to Debug, and registers t.Cleanup(logger.Close).
// Notify output is also captured via the same testingWriter so tests can assert
// on ephemeral output without stderr pollution.
// The cleanup registration happens at construction time.
func WithTesting(t TestingT) Option {
	tw := &testingWriter{t: t}
	return func(c *config) {
		*c = config{
			ConsoleOutput:    tw,
			NotifyOutput:     tw,
			ConsoleTheme:     nil,
			ConsoleLevel:     LevelDebug,
			StructuredOutput: nil,
			StructuredFormat: FormatJSON,
			StructuredLevel:  LevelOff,
			BufferSize:       512,
			FieldPoolSize:    25,
			DisableColour:    true,
			TimeFormat:       "15:04:05.000",
			DisplayTimezone:  time.Local,
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// WithNop configures a logger that discards everything. Replaces the old NopLogger().
func WithNop() Option {
	return func(c *config) {
		*c = config{
			ConsoleOutput:    io.Discard,
			ConsoleLevel:     LevelOff,
			StructuredOutput: io.Discard,
			StructuredLevel:  LevelOff,
			BufferSize:       256,
			FieldPoolSize:    0,
			TimeFormat:       "2006-01-02T15:04:05Z07:00",
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// WithHighThroughput resets config for high-throughput scenarios: JSON to stderr,
// info level, large buffer, sampling enabled (1000 initial, 100 thereafter).
func WithHighThroughput() Option {
	return func(c *config) {
		*c = config{
			ConsoleOutput:    io.Discard,
			ConsoleTheme:     nil,
			ConsoleLevel:     LevelOff,
			StructuredOutput: defaultStderr(),
			StructuredFormat: FormatJSON,
			StructuredLevel:  LevelInfo,
			BufferSize:       8192,
			FieldPoolSize:    500,
			DisableColour:    true,
			TimeFormat:       "2006-01-02T15:04:05Z07:00",
			DisplayTimezone:  time.UTC,
			FieldDisplayMode: FieldDisplayInline,
			Sampler:          NewCountSampler(1000, 100),
		}
	}
}

func WithLevel(level Level) Option {
	return func(c *config) {
		c.ConsoleLevel = level
	}
}

func WithConsoleOutput(w io.Writer) Option {
	return func(c *config) {
		c.ConsoleOutput = w
	}
}

// WithNotifyOutput redirects Notify/NotifyLines/NotifyBox output to w instead of
// os.Stderr. Useful in tests where stderr is not captured by the test runner, or
// when the operator channel should go to a specific file descriptor.
func WithNotifyOutput(w io.Writer) Option {
	return func(c *config) {
		c.NotifyOutput = w
	}
}

func WithStructuredOutput(w io.Writer) Option {
	return func(c *config) {
		c.StructuredOutput = w
	}
}

func WithFormat(format Format) Option {
	return func(c *config) {
		c.StructuredFormat = format
	}
}

func WithStructuredLevel(level Level) Option {
	return func(c *config) {
		c.StructuredLevel = level
	}
}

func WithTheme(theme *Theme) Option {
	return func(c *config) {
		c.ConsoleTheme = theme
	}
}

func WithTimeFormat(format string) Option {
	return func(c *config) {
		c.TimeFormat = format
	}
}

func WithBufferSize(size int) Option {
	return func(c *config) {
		c.BufferSize = size
	}
}

func WithFieldPoolSize(size int) Option {
	return func(c *config) {
		c.FieldPoolSize = size
	}
}

// WithColour enables or disables ANSI colour in console output.
func WithColour(enabled bool) Option {
	return func(c *config) {
		c.DisableColour = !enabled
	}
}

// WithSampling enables log sampling using a CountSampler.
// initial is the number of initial messages to log before sampling begins.
// thereafter is the sampling interval (1 in thereafter messages).
func WithSampling(initial, thereafter uint32) Option {
	return func(c *config) {
		c.Sampler = NewCountSampler(uint64(initial), uint64(thereafter))
	}
}

// WithSampler sets a sampler for the logger. Pass nil to disable sampling.
func WithSampler(s Sampler) Option {
	return func(c *config) {
		c.Sampler = s
	}
}

// WithDisplayTimezone sets the timezone for displaying timestamps in console output.
// Logs are stored in UTC but displayed in this zone. Use MustLocation to parse
// an IANA name when building options at init time.
func WithDisplayTimezone(loc *time.Location) Option {
	return func(c *config) {
		if loc != nil {
			c.DisplayTimezone = loc
		}
	}
}

// WithCaller enables or disables caller information capture (file:line and function name).
func WithCaller(enabled bool) Option {
	return func(c *config) {
		c.AddCaller = enabled
	}
}

// WithCallerSkip sets the number of extra stack frames to skip when capturing
// caller information. Use this when wrapping the logger to skip wrapper frames.
func WithCallerSkip(skip int) Option {
	return func(c *config) {
		c.CallerSkip = skip
	}
}

// WithFatalHandler overrides the function called after Fatal() writes its entry.
// Useful in tests to prevent os.Exit.
func WithFatalHandler(fn FatalHandler) Option {
	return func(c *config) {
		c.FatalHandler = fn
	}
}

// WithFieldDisplayMode sets how fields are rendered in console output.
func WithFieldDisplayMode(mode FieldDisplayMode) Option {
	return func(c *config) {
		c.FieldDisplayMode = mode
	}
}

// MustLocation parses an IANA timezone name and panics on failure.
// Intended for package-level variable initialisation.
func MustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("velocity: invalid timezone %q: %v", name, err))
	}
	return loc
}

// defaultStdout returns os.Stdout. Extracted so preset closures don't capture
// the global at the wrong moment.
func defaultStdout() *os.File {
	return os.Stdout
}

func defaultStdoutFile() *os.File {
	return os.Stdout
}

func defaultStderr() *os.File {
	return os.Stderr
}

// testingWriter adapts TestingT.Log to io.Writer so the console writer can
// forward formatted log lines into the test's output stream.
type testingWriter struct {
	t TestingT
}

func (w *testingWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	// Trim trailing newline — t.Log adds its own.
	s := string(p)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	w.t.Log(s)
	return len(p), nil
}

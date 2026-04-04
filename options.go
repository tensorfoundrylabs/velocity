package velocity

import (
	"fmt"
	"io"
	"time"
)

// Option is a functional option pattern for configuring a Logger.
// Options are applied in order, so later options override earlier ones.
type Option func(*Builder)

func WithLevel(level Level) Option {
	return func(b *Builder) {
		b.config.ConsoleLevel = level
	}
}

func WithConsoleOutput(w io.Writer) Option {
	return func(b *Builder) {
		b.config.ConsoleOutput = w
	}
}

func WithStructuredOutput(w io.Writer) Option {
	return func(b *Builder) {
		b.config.StructuredOutput = w
	}
}

func WithFormat(format Format) Option {
	return func(b *Builder) {
		b.config.StructuredFormat = format
	}
}

func WithStructuredLevel(level Level) Option {
	return func(b *Builder) {
		b.config.StructuredLevel = level
	}
}

func WithTheme(theme *Theme) Option {
	return func(b *Builder) {
		b.config.ConsoleTheme = theme
	}
}

func WithTimeFormat(format string) Option {
	return func(b *Builder) {
		b.config.TimeFormat = format
	}
}

func WithBufferSize(size int) Option {
	return func(b *Builder) {
		b.config.BufferSize = size
	}
}

func WithFieldPoolSize(size int) Option {
	return func(b *Builder) {
		b.config.FieldPoolSize = size
	}
}

func WithColourEnabled(enabled bool) Option {
	return func(b *Builder) {
		b.config.DisableColour = !enabled
	}
}

func WithColourDisabled() Option {
	return func(b *Builder) {
		b.config.DisableColour = true
	}
}

// WithSampling enables log sampling using a CountSampler.
// initial is the number of initial messages to log before sampling begins.
// thereafter is the sampling interval (1 in thereafter messages).
func WithSampling(initial, thereafter uint32) Option {
	return func(b *Builder) {
		b.config.Sampler = NewCountSampler(uint64(initial), uint64(thereafter))
	}
}

// WithSampler sets a sampler for the logger.
// Pass nil to disable sampling (default).
func WithSampler(s Sampler) Option {
	return func(b *Builder) {
		b.config.Sampler = s
	}
}

// WithDisplayTimezone sets the timezone for displaying timestamps.
// The timezone parameter should be a valid IANA timezone name (e.g., "America/New_York", "Australia/Sydney").
// Logs are always stored in UTC internally, but this controls how they're displayed in console output.
// Panics if the timezone name is invalid (use during logger initialisation).
func WithDisplayTimezone(tz string) Option {
	return func(b *Builder) {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			panic(fmt.Sprintf("velocity: invalid timezone %q: %v", tz, err))
		}
		b.config.DisplayTimezone = loc
	}
}

// WithCaller enables or disables caller information capture (file:line and function name).
func WithCaller(enabled bool) Option {
	return func(b *Builder) {
		b.config.AddCaller = enabled
	}
}

// WithCallerSkip sets the number of stack frames to skip when capturing caller information.
// Use this when wrapping the logger to skip your wrapper's frames.
func WithCallerSkip(skip int) Option {
	return func(b *Builder) {
		b.config.CallerSkip = skip
	}
}

func ApplyOptions(b *Builder, opts ...Option) *Builder {
	for _, opt := range opts {
		opt(b)
	}
	return b
}

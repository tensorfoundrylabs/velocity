package velocity

import (
	"io"
	"os"
	"time"
)

// DefaultDevelopmentConfig creates a logger configuration optimised for development environments.
// Features:
// - Pretty console output with colours
// - Debug level logging (most verbose)
// - No structured output file
// - Fast buffer initialisation
// - Console theme for visual appeal
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

		DisableColour:   false,
		TimeFormat:      "2006-01-02 15:04:05",
		DisplayTimezone: time.Local,
	}
}

// DefaultProductionConfig creates a logger configuration optimised for production environments.
// Features:
// - Structured JSON output to file
// - Info level logging (production appropriate)
// - RFC3339 timestamp format for reliable parsing
// - Larger buffer for batch processing
// - Context enabled for distributed tracing
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

		DisableColour: true,
		TimeFormat:    "2006-01-02T15:04:05Z07:00",
	}
}

// DefaultContainerConfig creates a logger configuration optimised for containerised environments.
// Features:
// - JSON output to stdout (standard for container logging systems)
// - Info level logging
// - Context enabled for distributed tracing and correlation IDs
// - Fast startup with minimal buffering
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

		DisableColour: disableColour,
		TimeFormat:    "2006-01-02T15:04:05Z07:00",
	}
}

// DefaultTestingConfig creates a logger configuration optimised for testing environments.
// Features:
// - Output to provided io.Writer (usually testing.T.Logf or a buffer)
// - Debug level logging (capture everything for debugging)
// - No structured output (testing logs are typically ephemeral)
// - Colours disabled (test output is usually parsed/captured)
// - Minimal buffering for immediate output
// - Context enabled for correlation tracking in test assertions
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

		DisableColour: true,
		TimeFormat:    "15:04:05.000",
	}
}

// DefaultHighPerformanceConfig creates a logger configuration optimised for extreme performance scenarios.
// Features:
// - Minimal allocation with large buffer pooling
// - Info level logging (avoiding debug spam)
// - Sample high-volume logs (initial 1000, then 1 in 100)
// - Structured JSON output for analysis
// - Large buffers to reduce allocations
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

		DisableColour: true,
		TimeFormat:    "2006-01-02T15:04:05Z07:00",
		Sampler:       NewCountSampler(uint64(1000), uint64(100)),
	}
}

// isTerminal checks whether the given file is connected to a terminal
// using the character device mode check.
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
		return (stat.Mode() & os.ModeCharDevice) != 0
	default:
		return false
	}
}

func PresetDevelopment() *Builder {
	cfg := DefaultDevelopmentConfig()
	return &Builder{config: cfg}
}

func PresetProduction() *Builder {
	cfg := DefaultProductionConfig()
	return &Builder{config: cfg}
}

func PresetContainer() *Builder {
	cfg := DefaultContainerConfig()
	return &Builder{config: cfg}
}

func PresetTesting(w io.Writer) *Builder {
	cfg := DefaultTestingConfig(w)
	return &Builder{config: cfg}
}

func PresetHighPerformance() *Builder {
	cfg := DefaultHighPerformanceConfig()
	return &Builder{config: cfg}
}

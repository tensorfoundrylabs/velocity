package velocity

import (
	"fmt"
	"strings"
)

type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
	LevelOff
)

const (
	LevelStrDebug = "DEBUG"
	LevelStrInfo  = "INFO"
	LevelStrWarn  = "WARN"
	LevelStrError = "ERROR"
	LevelStrFatal = "FATAL"
	LevelStrOff   = "OFF"
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return LevelStrDebug
	case LevelInfo:
		return LevelStrInfo
	case LevelWarn:
		return LevelStrWarn
	case LevelError:
		return LevelStrError
	case LevelFatal:
		return LevelStrFatal
	case LevelOff:
		return LevelStrOff
	default:
		return "UNKNOWN"
	}
}

// Short returns a single character representation for compact output.
func (l Level) Short() byte {
	switch l {
	case LevelDebug:
		return 'D'
	case LevelInfo:
		return 'I'
	case LevelWarn:
		return 'W'
	case LevelError:
		return 'E'
	case LevelFatal:
		return 'F'
	case LevelOff:
		return '-'
	}
	return '?'
}

func (l Level) Icon() string {
	switch l {
	case LevelDebug:
		return "🐛"
	case LevelInfo:
		return "ℹ️"
	case LevelWarn:
		return "⚠️"
	case LevelError:
		return "❌"
	case LevelFatal:
		return "🔥"
	case LevelOff:
		return "⏹️"
	}
	return "❓"
}

// ConciseLabel returns a 4-character concise representation of the log level for console output.
// These labels are designed to be visually balanced and easy to scan.
func (l Level) ConciseLabel() string {
	switch l {
	case LevelDebug:
		return "!DBG"
	case LevelInfo:
		return LevelStrInfo
	case LevelWarn:
		return LevelStrWarn
	case LevelError:
		return "ERR!"
	case LevelFatal:
		return "DOH!"
	case LevelOff:
		return "OFF "
	default:
		return "????"
	}
}

// ParseLevel converts a string level name to a Level constant.
// Valid levels (case-insensitive): debug, info, warn, warning, error, fatal, off.
func ParseLevel(level string) (Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	case "fatal":
		return LevelFatal, nil
	case "off":
		return LevelOff, nil
	default:
		return LevelOff, fmt.Errorf("velocity: invalid log level: %q", level)
	}
}

// MustParseLevel converts a string level to Level constant.
// Panics on invalid level (useful for config initialization).
// Valid levels (case-insensitive): debug, info, warn, error, fatal, off
func MustParseLevel(level string) Level {
	switch strings.ToLower(level) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	case "fatal":
		return LevelFatal
	case "off":
		return LevelOff
	default:
		panic("velocity: invalid log level: " + level)
	}
}

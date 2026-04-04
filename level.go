package velocity

import (
	"strings"
	"sync/atomic"
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

// AtomicLevel provides thread-safe level management with zero-cost reads.
type AtomicLevel struct {
	level int32
}

func NewAtomicLevel(l Level) *AtomicLevel {
	return &AtomicLevel{level: int32(l)}
}

func (al *AtomicLevel) Level() Level {
	return Level(atomic.LoadInt32(&al.level))
}

func (al *AtomicLevel) SetLevel(l Level) {
	atomic.StoreInt32(&al.level, int32(l))
}

// Enabled checks if the given level is enabled.
// This is the critical path - called on every log attempt.
func (al *AtomicLevel) Enabled(l Level) bool {
	return l >= Level(atomic.LoadInt32(&al.level))
}

func (al *AtomicLevel) CompareAndSwap(old, newLevel Level) bool {
	return atomic.CompareAndSwapInt32(&al.level, int32(old), int32(newLevel))
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

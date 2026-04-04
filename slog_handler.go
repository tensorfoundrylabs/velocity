package velocity

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// SlogHandler bridges log/slog to a velocity Logger.
type SlogHandler struct {
	logger *Logger
	prefix string   // cached dotted prefix from groups, computed once at construction
	attrs  []Field  // pre-converted from WithAttrs
	groups []string // nested group names
}

// NewSlogHandler creates an slog.Handler backed by a velocity Logger.
func NewSlogHandler(logger *Logger) *SlogHandler {
	return &SlogHandler{logger: logger}
}

// NewSlogLogger creates an *slog.Logger backed by a velocity Logger.
func NewSlogLogger(logger *Logger) *slog.Logger {
	return slog.New(NewSlogHandler(logger))
}

// Enabled reports whether the handler handles records at the given level.
func (h *SlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	if h.logger == nil {
		return false
	}
	return mapSlogLevel(level) >= h.logger.Level()
}

// Handle converts a slog.Record to a velocity entry and dispatches it.
func (h *SlogHandler) Handle(_ context.Context, record slog.Record) error {
	if h.logger == nil {
		return nil
	}

	level := mapSlogLevel(record.Level)

	entry := GetEntry()
	defer entry.Release()

	entry.SetLevel(level)
	entry.SetMessage(record.Message)

	t := record.Time
	if t.IsZero() {
		t = time.Now()
	}
	entry.SetTime(t)

	if len(h.attrs) > 0 {
		entry.WithFields(h.attrs...)
	}

	if record.NumAttrs() > 0 {
		// Pre-grow to avoid repeated slice growth in the common case.
		entry.Fields = growFields(entry.Fields, record.NumAttrs())
		prefix := h.prefix
		record.Attrs(func(a slog.Attr) bool {
			appendAttrToEntry(entry, prefix, a)
			return true
		})
	}

	h.logger.logEntry(entry)
	return nil
}

// WithAttrs returns a new handler with the given attributes pre-converted.
func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	newAttrs := make([]Field, 0, len(h.attrs)+len(attrs))
	newAttrs = append(newAttrs, h.attrs...)
	for _, a := range attrs {
		newAttrs = appendAttrFields(newAttrs, h.prefix, a)
	}
	return &SlogHandler{
		logger: h.logger,
		attrs:  newAttrs,
		groups: h.groups,
		prefix: h.prefix,
	}
}

// WithGroup returns a new handler with the given group name pushed onto the stack.
func (h *SlogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name
	return &SlogHandler{
		logger: h.logger,
		attrs:  h.attrs,
		groups: newGroups,
		prefix: strings.Join(newGroups, ".") + ".",
	}
}

// slog has no fatal level. Treat anything at or above error+4 as fatal.
const slogLevelFatal = slog.LevelError + 4

func mapSlogLevel(l slog.Level) Level {
	switch {
	case l < slog.LevelInfo:
		return LevelDebug
	case l < slog.LevelWarn:
		return LevelInfo
	case l < slog.LevelError:
		return LevelWarn
	case l < slogLevelFatal:
		return LevelError
	default:
		return LevelFatal
	}
}

// growFields ensures the slice has room for n more elements without repeated growth.
func growFields(fields []Field, n int) []Field {
	if cap(fields)-len(fields) >= n {
		return fields
	}
	grown := make([]Field, len(fields), len(fields)+n)
	copy(grown, fields)
	return grown
}

// appendAttrFields appends velocity Fields for a slog.Attr to the given slice.
func appendAttrFields(fields []Field, prefix string, attr slog.Attr) []Field {
	attr.Value = attr.Value.Resolve()

	if attr.Equal(slog.Attr{}) {
		return fields
	}

	key := prefix + attr.Key

	switch attr.Value.Kind() {
	case slog.KindString:
		return append(fields, StringField(key, attr.Value.String()))
	case slog.KindInt64:
		return append(fields, Int64(key, attr.Value.Int64()))
	case slog.KindUint64:
		return append(fields, Int64(key, int64(attr.Value.Uint64()))) //nolint:gosec // slog uint64 -> int64 bit cast is intentional
	case slog.KindFloat64:
		return append(fields, Float64(key, attr.Value.Float64()))
	case slog.KindBool:
		return append(fields, Bool(key, attr.Value.Bool()))
	case slog.KindTime:
		return append(fields, Time(key, attr.Value.Time()))
	case slog.KindDuration:
		return append(fields, Duration(key, attr.Value.Duration()))
	case slog.KindGroup:
		groupAttrs := attr.Value.Group()
		// Unnamed groups flatten their attrs into the current prefix.
		groupPrefix := key + "."
		if attr.Key == "" {
			groupPrefix = prefix
		}
		for _, ga := range groupAttrs {
			fields = appendAttrFields(fields, groupPrefix, ga)
		}
		return fields
	case slog.KindAny, slog.KindLogValuer:
		return append(fields, Any(key, attr.Value.Any()))
	}
	return fields
}

// appendAttrToEntry converts a slog.Attr and appends it directly to an entry's fields.
func appendAttrToEntry(entry *Entry, prefix string, attr slog.Attr) {
	entry.Fields = appendAttrFields(entry.Fields, prefix, attr)
}

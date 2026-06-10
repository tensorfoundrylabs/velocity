package velocity

import (
	"context"
	"sync"
)

type (
	contextKey       struct{}
	contextFieldsKey struct{}
)

// nopLoggerOnce ensures the singleton nop logger is constructed exactly once.
// Returning the same instance for every FromContext cache-miss avoids allocating
// a new Logger + config on every undecorated context traversal.
var (
	nopLoggerOnce     sync.Once
	nopLoggerInstance *Logger
)

func getNopLogger() *Logger {
	nopLoggerOnce.Do(func() {
		nopLoggerInstance = New(WithNop())
	})
	return nopLoggerInstance
}

// NewContext returns a new context carrying the logger.
func NewContext(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext retrieves the logger from ctx.
// If ctx carries additional fields via ContextWithFields, they are prepended
// via With() before returning.
// Returns a singleton nop logger if no logger is stored — never returns nil.
func FromContext(ctx context.Context) *Logger {
	l, ok := ctx.Value(contextKey{}).(*Logger)
	if !ok || l == nil {
		return getNopLogger()
	}
	if fields, ok := ctx.Value(contextFieldsKey{}).([]Field); ok && len(fields) > 0 {
		return l.With(fields...)
	}
	return l
}

// ContextWithFields returns a new context with additional fields that will be
// prepended to any logger retrieved via FromContext. Fields accumulate across
// middleware layers.
func ContextWithFields(ctx context.Context, fields ...Field) context.Context {
	if len(fields) == 0 {
		return ctx
	}
	existing, _ := ctx.Value(contextFieldsKey{}).([]Field)
	merged := make([]Field, len(existing)+len(fields))
	copy(merged, existing)
	copy(merged[len(existing):], fields)
	return context.WithValue(ctx, contextFieldsKey{}, merged)
}

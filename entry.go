package velocity

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"
)

// Entry represents a single log entry designed for reuse via sync.Pool.
type Entry struct {
	Time time.Time

	logger *Logger

	// Buffer for formatted output - reused across entries
	buffer *bytes.Buffer

	Message string

	Caller   string
	Function string

	Fields []Field

	Line int

	Level Level

	// written indicates whether entry.Write() was called
	// Must be atomic for safe access across goroutines
	written atomic.Uint32

	// forceTreeDisplay indicates that fields should always be displayed in tree format
	forceTreeDisplay bool

	// maybeSecure is set when the message contains '<' and scanSecure is active.
	// Writers check this to decide whether to run the <secure>...</secure> redaction pass.
	// Kept on Entry (not inlined into every Field) because the common case is false.
	maybeSecure bool

	// Reference count for pool safety
	// Starts at 1 when acquired, decremented on Release
	// Only returned to pool when count reaches 0
	refCount atomic.Int32
}

// entryPool manages Entry object reuse to minimise allocations.
// Buffer is intentionally omitted here; Buffer() lazy-allocates on first use
// so entries that never call Buffer() (the majority) pay nothing.
var entryPool = sync.Pool{
	New: func() any {
		return &Entry{
			Fields: GetFieldSlice(),
		}
	},
}

func GetEntry() *Entry {
	e, ok := entryPool.Get().(*Entry)
	if !ok || e == nil {
		// Fallback in case pool returns unexpected type
		e = &Entry{
			Fields: GetFieldSlice(),
		}
	}
	e.Reset()
	// Clear the buffer's content from the previous use without releasing the allocation.
	if e.buffer != nil {
		e.buffer.Reset()
	}
	e.refCount.Store(1)
	return e
}

// Reset prepares the Entry for reuse, clearing all fields but keeping allocated memory.
func (e *Entry) Reset() {
	e.Level = LevelInfo
	e.Time = time.Time{}
	e.Message = ""
	e.logger = nil

	e.Fields = e.Fields[:0]

	// Don't reset e.buffer — nil stays nil, allocated buffer keeps its capacity
	// and will be Reset on next Buffer() call if reused.

	e.Caller = ""
	e.Function = ""
	e.Line = 0

	e.written.Store(0)
	e.forceTreeDisplay = false
	e.maybeSecure = false
	e.refCount.Store(0)
}

// Retain increments the reference count.
// Call this before passing the entry to async handlers.
func (e *Entry) Retain() {
	e.refCount.Add(1)
}

// Release decrements the reference count and returns to pool when zero.
// Uses atomic swap to ensure only one goroutine performs cleanup and pool return.
func (e *Entry) Release() {
	// Fast path: not written yet, skip release (atomic read)
	if e.written.Load() == 0 {
		return
	}

	// Decrement ref count
	newCount := e.refCount.Add(-1)

	// If ref count > 0, other goroutines still hold references
	if newCount > 0 {
		return
	}

	// If ref count < 0, something is wrong (double-release)
	// Reset to 0 to prevent further negative counts
	if newCount < 0 {
		e.refCount.Store(0)
		return
	}

	// refCount == 0: we're the last holder, try to return to pool
	// Use CAS to ensure only ONE goroutine does this even with races
	for {
		currentCount := e.refCount.Load()
		if currentCount != 0 {
			// Another goroutine already handled cleanup
			return
		}
		// Try to swap from 0 to -1 (indicating cleanup in progress)
		if e.refCount.CompareAndSwap(0, -1) {
			break
		}
		// CAS failed, retry
	}

	// We won the race - return to pool
	// Note: Reset() handles e.Fields = e.Fields[:0] when entry is reused
	// Don't nil out Fields here as other goroutines may still be reading
	entryPool.Put(e)
}

func (e *Entry) WithField(key string, value any) *Entry {
	e.Fields = append(e.Fields, Any(key, value))
	return e
}

func (e *Entry) WithFields(fields ...Field) *Entry {
	e.Fields = append(e.Fields, fields...)
	return e
}

func (e *Entry) WithError(err error) *Entry {
	if err != nil {
		e.Fields = append(e.Fields, Error("error", err))
	}
	return e
}

func (e *Entry) SetTime(t time.Time) *Entry {
	e.Time = t
	return e
}

func (e *Entry) SetLevel(level Level) *Entry {
	e.Level = level
	return e
}

func (e *Entry) SetMessage(msg string) *Entry {
	e.Message = msg
	return e
}

// Buffer returns the entry's buffer for direct writing to avoid allocations.
func (e *Entry) Buffer() *bytes.Buffer {
	if e.buffer == nil {
		e.buffer = bytes.NewBuffer(make([]byte, 0, 256))
	}
	return e.buffer
}

// Bytes returns the formatted entry as bytes.
// The returned slice is only valid until Release() is called.
func (e *Entry) Bytes() []byte {
	if e.buffer == nil {
		return nil
	}
	return e.buffer.Bytes()
}

// String returns the formatted entry as a string.
// Allocates - avoid in hot paths.
func (e *Entry) String() string {
	if e.buffer == nil {
		return ""
	}
	return e.buffer.String()
}

func (e *Entry) Write() {
	e.written.Store(1)
}

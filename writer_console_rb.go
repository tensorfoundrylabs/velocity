package velocity

import (
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ConsoleWriterRB is a high-performance console writer using a lock-free ring buffer.
// This eliminates mutex contention and provides batched writing for better throughput.
type ConsoleWriterRB struct {
	out             io.Writer
	theme           *Theme
	bufPool         *BufferPool
	template        *Template
	displayTimezone *time.Location
	ringBuffer      *RingBuffer
	closed          atomic.Bool

	mu     sync.Mutex // Protects theme and template
	writes atomic.Uint64
	errors atomic.Uint64
}

func NewConsoleWriterRB(out io.Writer, theme *Theme, displayTimezone *time.Location, fieldMode FieldDisplayMode) *ConsoleWriterRB {
	actualOut := out
	if file, ok := out.(*os.File); ok {
		actualOut = file
	}

	// Default to local time, matching ConsoleWriter behaviour.
	if displayTimezone == nil {
		displayTimezone = time.Local
	}

	w := &ConsoleWriterRB{
		out:             actualOut,
		theme:           theme,
		bufPool:         NewBufferPool(),
		displayTimezone: displayTimezone,
	}

	w.ringBuffer = NewRingBuffer(actualOut, DefaultRingBufferSize)

	if theme != nil {
		w.template = initTemplate(&Template{
			showTime:         true,
			timeFormat:       time.RFC3339,
			showLevel:        true,
			levelStyle:       LevelStyleBadge,
			showMessage:      true,
			showFields:       true,
			fieldSep:         " ",
			fieldPairSep:     "=",
			fieldDisplayMode: fieldMode,
			useColours:       true,
		})
	}

	return w
}

// Write writes a formatted log entry using the ring buffer.
// This method is lock-free and optimised for high throughput.
func (w *ConsoleWriterRB) Write(e *Entry) error {
	if w.closed.Load() {
		return ErrWriterClosed
	}

	w.writes.Add(1)

	rawBuf := w.bufPool.Get(HintConsoleLog)
	defer w.bufPool.Put(rawBuf)

	var formattedData []byte
	w.mu.Lock()
	hasTemplate := w.template != nil
	theme := w.theme
	template := w.template
	w.mu.Unlock()

	if hasTemplate {
		tempBuf := GetTemplateBuffer()
		defer PutTemplateBuffer(tempBuf)
		template.buildWithTimezone(tempBuf, e, theme, w.displayTimezone)
		formattedData = tempBuf.Bytes()
	} else {
		buf := NewBytesBuffer(rawBuf)
		w.formatEntry(buf, e)
		_ = buf.WriteByte('\n')
		formattedData = buf.Bytes()
	}

	// Lock-free write to ring buffer
	if !w.ringBuffer.Write(formattedData) {
		w.errors.Add(1)
		// Fallback to direct write if buffer is full
		_, err := w.out.Write(formattedData)
		return err
	}

	return nil
}

func (w *ConsoleWriterRB) formatEntry(buf *BytesBuffer, e *Entry) {
	if !e.Time.IsZero() {
		// displayTimezone is always non-nil; NewConsoleWriterRB defaults nil to time.Local.
		buf.AppendTime(e.Time.In(w.displayTimezone), "2006-01-02T15:04:05.000Z07:00")
		_ = buf.WriteByte(' ')
	}

	// Fixed-width label avoids sprintf allocation; ConciseLabel returns a 4-char string.
	_ = buf.WriteByte('[')
	buf.WriteString(e.Level.ConciseLabel())
	_ = buf.WriteByte(']')
	_ = buf.WriteByte(' ')

	if e.Message != "" {
		buf.WriteString(e.Message)
	}

	if e.Caller != "" {
		buf.WriteString(" (")
		buf.WriteString(e.Caller)
		_ = buf.WriteByte(':')
		buf.WriteInt(int64(e.Line))
		_ = buf.WriteByte(')')
	}

	if len(e.Fields) > 0 {
		_ = buf.WriteByte(' ')
		for i, f := range e.Fields {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(f.Key)
			_ = buf.WriteByte('=')
			buf.WriteString(FieldValueToString(f))
		}
	}
}

func (w *ConsoleWriterRB) Close() error {
	if !w.closed.CompareAndSwap(false, true) {
		return ErrWriterClosed
	}

	return w.ringBuffer.Close()
}

func (w *ConsoleWriterRB) SetTheme(theme *Theme) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.theme = theme
	if theme != nil {
		w.template = initTemplate(&Template{
			showTime:         true,
			timeFormat:       time.RFC3339,
			showLevel:        true,
			levelStyle:       LevelStyleBadge,
			showMessage:      true,
			showFields:       true,
			fieldSep:         " ",
			fieldPairSep:     "=",
			fieldDisplayMode: FieldDisplayInline,
			useColours:       true,
		})
	}
}

func (w *ConsoleWriterRB) Metrics() (writes, errors, dropped uint64) {
	return w.writes.Load(), w.errors.Load(), w.ringBuffer.DroppedCount()
}

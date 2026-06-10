package velocity

import (
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ConsoleWriterRB is a high-performance console writer using a lock-free ring buffer.
//
// Deprecated: ConsoleWriterRB is not integrated with the standard Logger pipeline
// and has known concurrency issues (the direct-write fallback path races with the
// ring-buffer flusher). Use ConsoleWriter, which buffers output via the logger's
// own mutex and is the supported path. ConsoleWriterRB will be removed in v3.
type ConsoleWriterRB struct {
	out             io.Writer // the raw destination; writes must go through outMu
	theme           *Theme
	bufPool         *BufferPool
	template        *Template
	displayTimezone *time.Location
	ringBuffer      *RingBuffer
	closed          atomic.Bool

	// outMu serialises all writes to out — both the ring-buffer flusher's batch
	// writes and the direct-write fallback path that fires when the ring is full.
	// Without this, the two paths race on the underlying io.Writer.
	outMu sync.Mutex

	// isTTY mirrors ConsoleWriter's trust model: TTY = trusted (human terminal),
	// non-TTY = untrusted (pipe or file). The template is rendered via the secure
	// path so Secure fields are redacted when piping to a file or non-TTY sink.
	isTTY bool

	mu     sync.Mutex // Protects theme and template
	writes atomic.Uint64
	errors atomic.Uint64
}

// syncWriter wraps an io.Writer and a mutex so the ring buffer's flusher and
// the direct-write fallback in Write() use the same lock when writing to out.
type syncWriter struct {
	mu  *sync.Mutex
	out io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.Write(p)
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

	// Themes are immutable from NewTheme — ANSI codes already populated.

	w := &ConsoleWriterRB{
		out:             actualOut,
		theme:           theme,
		bufPool:         NewBufferPool(),
		displayTimezone: displayTimezone,
		// Respect NO_COLOR / FORCE_COLOR / TTY detection; don't blindly emit ANSI
		// into pipes or files (fixes L4: useColours was previously hardcoded true).
		isTTY: resolveColourForWriter(actualOut),
	}

	// Pass a syncWriter so the ring flusher's w.out.Write calls are serialised
	// via the same outMu as the direct-write fallback, eliminating the H2 race.
	sw := &syncWriter{mu: &w.outMu, out: actualOut}
	w.ringBuffer = NewRingBuffer(sw, DefaultRingBufferSize)

	if theme != nil {
		useColours := w.isTTY && !theme.noColour
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
			useColours:       useColours,
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
		// Use the trust-aware path so Secure fields are redacted on non-TTY output
		// (e.g. piped to a file). TTY writers are treated as trusted — same model
		// as ConsoleWriter which passes isTTY as the trusted flag.
		template.buildWithTimezoneSecure(tempBuf, e, theme, w.displayTimezone, w.isTTY, "[REDACTED]")
		formattedData = tempBuf.Bytes()
	} else {
		buf := NewBytesBuffer(rawBuf)
		w.formatEntry(buf, e)
		_ = buf.WriteByte('\n')
		formattedData = buf.Bytes()
	}

	// Lock-free write to ring buffer.
	if !w.ringBuffer.Write(formattedData) {
		w.errors.Add(1)
		// Fallback: ring is full; write directly but serialise via outMu so this
		// path cannot interleave with the ring flusher (which uses syncWriter).
		w.outMu.Lock()
		_, err := w.out.Write(formattedData)
		w.outMu.Unlock()
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
		// Preserve TTY/colour state from construction when updating the theme.
		useColours := w.isTTY && !theme.noColour
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
			useColours:       useColours,
		})
	}
}

func (w *ConsoleWriterRB) Metrics() (writes, errors, dropped uint64) {
	return w.writes.Load(), w.errors.Load(), w.ringBuffer.DroppedCount()
}

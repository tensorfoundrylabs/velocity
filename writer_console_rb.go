package velocity

import (
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ConsoleWriterRB is a console writer backed by a batching byte queue.
//
// Deprecated: ConsoleWriterRB is not integrated with the standard Logger
// pipeline. Use ConsoleWriter, which buffers output via the logger's own
// mutex and is the supported path. ConsoleWriterRB will be removed in v3.
type ConsoleWriterRB struct {
	out             io.Writer // the raw destination; only the drain goroutine writes to it after construction
	theme           *Theme
	bufPool         *BufferPool
	template        *Template
	displayTimezone *time.Location
	ringBuffer      *RingBuffer
	closed          atomic.Bool

	// isTTY mirrors ConsoleWriter's trust model: TTY = trusted (human terminal),
	// non-TTY = untrusted (pipe or file). The template is rendered via the secure
	// path so Secure fields are redacted when piping to a file or non-TTY sink.
	isTTY bool

	// colourAllowed is the stable colour permission resolved at construction
	// (NO_COLOR / FORCE_COLOR / terminal detection). SetTheme re-derives
	// useColours from it rather than from the previous theme, so a
	// mono-to-coloured swap restores colour when permission allows.
	colourAllowed bool

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

	// Themes are immutable from NewTheme — ANSI codes already populated.

	w := &ConsoleWriterRB{
		out:             actualOut,
		theme:           theme,
		bufPool:         NewBufferPool(),
		displayTimezone: displayTimezone,
		// Respect NO_COLOR / FORCE_COLOR / TTY detection; don't blindly emit ANSI
		// into pipes or files (fixes L4: useColours was previously hardcoded true).
		isTTY:         IsTerminalWriter(actualOut),
		colourAllowed: resolveColourForWriter(actualOut),
	}

	w.ringBuffer = NewRingBuffer(actualOut, DefaultRingBufferSize)

	if theme != nil {
		useColours := w.colourAllowed && !theme.noColour
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

// Write writes a formatted log entry through the byte queue.
// When the queue is full the entry is dropped and counted in Metrics rather
// than written directly: a direct fallback raced with Close and could overtake
// records already queued ahead of it (R10).
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

	if !w.ringBuffer.Write(formattedData) {
		// Queue full or closed. Counted as an error here and as a drop by the
		// queue; never re-ordered ahead of accepted records.
		w.errors.Add(1)
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

// Close rejects further writes and drains every accepted entry. It is safe to
// call concurrently: RingBuffer.Close is idempotent and every caller waits for
// the same drain, so no Close returns while entries are still in flight.
func (w *ConsoleWriterRB) Close() error {
	// Gate new writes before shutting the queue. A Write racing past this
	// store is rejected by the queue's own closed check and counted as a drop.
	w.closed.Store(true)
	return w.ringBuffer.Close()
}

func (w *ConsoleWriterRB) SetTheme(theme *Theme) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.theme = theme
	if theme != nil {
		// Re-derive useColours from the construction-time colour permission, not
		// from the previous template — a mono-to-coloured swap must restore
		// colour when NO_COLOR/FORCE_COLOR/TTY permission allows it.
		useColours := w.colourAllowed && !theme.noColour
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

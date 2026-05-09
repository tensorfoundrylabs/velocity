// Package velocity provides a high-performance, structured logging library with
// rich terminal output and a composable writer pipeline.
//
// # Close ownership
//
// The rule is: whichever side constructs the underlying io.Writer owns its Close.
//
//   - WithConsoleOutput(os.Stdout)      — caller owns Stdout, caller closes
//   - WithStructuredOutput(rotator)     — caller owns rotator, caller closes
//   - AddWriter("file", NewJSONWriter(f)) — caller constructed f, caller closes f
//   - AddWriter("ring", NewRingBufferWriter(...)) — logger constructs internally, logger closes
//
// Logger.Close() flushes and drains writer pipeline state (channels, ring buffers)
// but never calls Close on a caller-supplied io.Writer.
package velocity

// Reused to avoid a heap allocation per write through the io.Writer interface.
var newlineByte = []byte{'\n'}

// Writer defines the interface for log output writers.
// Implementations must be thread-safe and handle formatting independently.
type Writer interface {
	// Write delivers an entry to the writer.
	// The entry must not be modified after this call returns.
	Write(e *Entry) error

	Close() error
}

// ThemedWriter is the optional interface for writers that support runtime theme changes.
// Implemented by ConsoleWriter and ConsoleWriterRB.
type ThemedWriter interface {
	SetTheme(*Theme)
}

// LeveledWriter is the optional interface for writers that filter by level independently.
// Useful for sinks that need a different minimum level than the parent logger.
type LeveledWriter interface {
	Level() Level
	SetLevel(Level)
}

// FlushableWriter is the optional interface for writers with an internal buffer
// that can be flushed without closing the writer.
// Implemented by JSONWriter.
type FlushableWriter interface {
	Flush() error
}

// TrustedWriter is the optional interface for writers that self-report trust.
// Prefer writerOptions.isTrusted (set at AddWriter time) over this interface —
// the stored bool is cheaper than a type assertion on the hot path.
// This interface exists for writers that need to declare trust from their constructor.
type TrustedWriter interface {
	IsTrusted() bool
}

// SecureWriter is an optional capability interface for writers that handle
// field-level redaction and <secure> tag processing themselves.
// When a MultiWriter worker's underlying writer implements SecureWriter,
// WriteSecure is called instead of Write, passing the per-worker trust state
// and redaction mark without mutating the shared Entry.
//
// Built-in writers (ConsoleWriter, JSONWriter, RingBufferWriter) implement this.
// Third-party writers that don't implement it receive the entry unmodified —
// they are treated as if they are trusted (they see plaintext).
type SecureWriter interface {
	WriteSecure(e *Entry, trusted bool, redactionMark string) error
}

// WriterOption configures per-writer behaviour at AddWriter time.
type WriterOption func(*writerOptions)

type writerOptions struct {
	redactionMark string // overrides "[REDACTED]" when non-empty
	isTrusted     bool
}

// WriterTrusted marks a writer as trusted.
// Trusted writers receive unredacted field values; untrusted writers receive
// [REDACTED] for Secure fields (wired in Phase 4). Default: untrusted.
// Trust is cached at AddWriter time as a bool — no type assertion per write.
func WriterTrusted() WriterOption {
	return func(o *writerOptions) {
		o.isTrusted = true
	}
}

// WriterRedactionMark sets the string used to replace redacted values for this
// writer. Default: "[REDACTED]". Applies to Secure/SecureURL fields and
// <secure>...</secure> message content when the writer is untrusted.
func WriterRedactionMark(mark string) WriterOption {
	return func(o *writerOptions) {
		o.redactionMark = mark
	}
}

// applyWriterOptions applies opts and returns the resulting options struct.
func applyWriterOptions(opts []WriterOption) writerOptions {
	var o writerOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// effectiveRedactionMark returns the custom mark or the default "[REDACTED]".
func (o writerOptions) effectiveRedactionMark() string {
	if o.redactionMark != "" {
		return o.redactionMark
	}
	return "[REDACTED]"
}

type NoOpWriter struct{}

func (*NoOpWriter) Write(_ *Entry) error {
	return nil
}

func (*NoOpWriter) Close() error {
	return nil
}

// WriterFunc adapts a function to the Writer interface.
type WriterFunc func(*Entry) error

func (f WriterFunc) Write(e *Entry) error {
	return f(e)
}

func (WriterFunc) Close() error {
	return nil
}

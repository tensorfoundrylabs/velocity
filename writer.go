package velocity

// Reused to avoid a heap allocation per write through the io.Writer interface.
var newlineByte = []byte{'\n'}

// Writer defines the interface for log output writers.
// Implementations must be thread-safe and handle formatting independently.
type Writer interface {
	// Write writes an entry to the output.
	// The entry must not be modified after this call.
	Write(e *Entry) error

	Close() error
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

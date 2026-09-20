package velocity

import (
	"io"
	"testing"
)

type depthNopWriter struct{}

func (depthNopWriter) Write(_ *Entry) error { return nil }
func (depthNopWriter) Close() error         { return nil }

// WithWriterQueueDepth sets the per-writer channel capacity; the default (and
// any non-positive value) stays 256.
func TestMultiWriter_QueueDepthOption(t *testing.T) {
	t.Parallel()

	t.Run("default is 256", func(t *testing.T) {
		t.Parallel()
		mw := NewMultiWriter()
		defer func() { _ = mw.Close() }()
		mw.AddWriter("w", depthNopWriter{})
		if got := cap(mw.writeChans["w"]); got != 256 {
			t.Fatalf("default queue depth = %d, want 256", got)
		}
	})

	t.Run("custom depth honoured", func(t *testing.T) {
		t.Parallel()
		mw := NewMultiWriter()
		defer func() { _ = mw.Close() }()
		mw.AddWriter("w", depthNopWriter{}, WithWriterQueueDepth(4))
		if got := cap(mw.writeChans["w"]); got != 4 {
			t.Fatalf("queue depth = %d, want 4", got)
		}
	})

	t.Run("non-positive falls back to default", func(t *testing.T) {
		t.Parallel()
		mw := NewMultiWriter()
		defer func() { _ = mw.Close() }()
		for _, n := range []int{0, -1} {
			mw.AddWriter(io.Discard, depthNopWriter{}, WithWriterQueueDepth(n))
		}
		for name, ch := range mw.writeChans {
			if got := cap(ch); got != 256 {
				t.Fatalf("queue depth for %q = %d, want 256", name, got)
			}
		}
	})
}

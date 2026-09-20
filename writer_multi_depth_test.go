package velocity

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
		for i, n := range []int{0, -1} {
			mw.AddWriter(fmt.Sprintf("w%d", i), depthNopWriter{}, WithWriterQueueDepth(n))
		}
		for name, ch := range mw.writeChans {
			if got := cap(ch); got != 256 {
				t.Fatalf("queue depth for %q = %d, want 256", name, got)
			}
		}
	})
}

// lockedBuffer is a mutex-guarded bytes.Buffer so the test goroutine can poll
// a worker-owned writer's output without a data race.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}

// Depth 1 is the smallest pipeline a worker can run: one entry in service,
// one queued, and everything else dropped under the non-blocking send
// contract. Delivered + dropped must equal sent exactly, and no entry may
// arrive corrupted.
func TestMultiWriter_QueueDepth1DeliversOrDropsExactly(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	const perGoroutine = 50
	const total = goroutines * perGoroutine

	mw := NewMultiWriter()
	defer func() { _ = mw.Close() }()

	var mu sync.Mutex
	seen := make(map[string]int)
	var delivered atomic.Int64
	mw.AddWriter("w", WriterFunc(func(e *Entry) error {
		mu.Lock()
		seen[e.Message]++
		mu.Unlock()
		delivered.Add(1)
		return nil
	}), WithWriterQueueDepth(1))

	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			for i := range perGoroutine {
				e := GetEntry()
				e.SetMessage(fmt.Sprintf("q1-%02d-%03d", g, i))
				e.SetLevel(LevelInfo)
				_ = mw.Write(e)
				e.Release()
			}
		}(g)
	}
	close(start)
	wg.Wait()

	waitFor(t, func() bool {
		return delivered.Load()+int64(mw.DroppedCount()) == int64(total)
	}, 5*time.Second, time.Millisecond, "depth-1 worker to settle")

	if got := delivered.Load() + int64(mw.DroppedCount()); got != int64(total) {
		t.Fatalf("delivered %d + dropped %d != sent %d", delivered.Load(), mw.DroppedCount(), total)
	}

	// Every delivered entry must be whole: a pooled entry released while the
	// worker still held it would surface as a wrong or empty message.
	mu.Lock()
	defer mu.Unlock()
	for msg, n := range seen {
		var g, i int
		if _, err := fmt.Sscanf(msg, "q1-%02d-%03d", &g, &i); err != nil {
			t.Fatalf("corrupt entry message %q: %v", msg, err)
		}
		if n != 1 {
			t.Fatalf("message %q delivered %d times", msg, n)
		}
	}
}

// A large depth is honoured as-is; nothing clamps or rounds it.
func TestMultiWriter_LargeQueueDepthHonoured(t *testing.T) {
	t.Parallel()

	mw := NewMultiWriter()
	defer func() { _ = mw.Close() }()
	mw.AddWriter("big", depthNopWriter{}, WithWriterQueueDepth(1<<16))
	if got := cap(mw.writeChans["big"]); got != 1<<16 {
		t.Fatalf("queue depth = %d, want %d", got, 1<<16)
	}
}

// Trust and a shallow queue combine: the depth-1 channel still delivers the
// Secure field's plaintext only to the trusted writer, redacted to everyone
// else.
func TestMultiWriter_TrustedWriterWithShallowQueueSeesPlaintext(t *testing.T) {
	t.Parallel()

	var console bytes.Buffer
	logger := New(WithConsoleOutput(&console))

	var trusted, plain lockedBuffer
	logger.AddWriter("trusted", NewJSONWriter(&trusted), WithWriterQueueDepth(1), WriterTrusted())
	logger.AddWriter("plain", NewJSONWriter(&plain), WithWriterQueueDepth(1))

	logger.Info("cred", Secure("token", "s3cr3t-plaintext"))

	waitFor(t, func() bool {
		return len(trusted.bytes()) > 0 && len(plain.bytes()) > 0
	}, 5*time.Second, 5*time.Millisecond, "both structured writers to receive the record")

	if !bytes.Contains(trusted.bytes(), []byte("s3cr3t-plaintext")) {
		t.Fatalf("trusted writer did not receive Secure plaintext: %s", trusted.bytes())
	}
	if bytes.Contains(plain.bytes(), []byte("s3cr3t-plaintext")) {
		t.Fatal("untrusted writer received Secure plaintext")
	}
	if !bytes.Contains(plain.bytes(), []byte("[REDACTED]")) {
		t.Fatalf("untrusted writer did not redact the Secure field: %s", plain.bytes())
	}
}

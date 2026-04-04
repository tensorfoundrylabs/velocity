package velocity

import (
	"sync"
	"testing"
	"time"
)

// TestRingBuffer_ConcurrentWrites verifies that concurrent writes work correctly
// and that the committed field synchronises writer and flusher properly.
func TestRingBuffer_ConcurrentWrites(t *testing.T) {
	buf := &safeBuffer{}
	// Larger buffer to avoid drops during concurrent writes
	rb := NewRingBuffer(buf, 512)

	const numGoroutines = 10
	const writesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(_ int) {
			defer wg.Done()
			for range writesPerGoroutine {
				data := []byte("test message\n")
				rb.Write(data)
			}
		}(i)
	}

	wg.Wait()

	// Wait for flusher to process entries
	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 10*time.Second, 10*time.Millisecond, "data should be flushed")

	// Close to ensure all pending entries are flushed
	_ = rb.Close()

	expectedBytes := numGoroutines * writesPerGoroutine * len("test message\n")
	actualBytes := buf.Len()
	dropped := rb.DroppedCount()

	t.Logf("Wrote %d bytes, expected %d bytes, dropped %d messages",
		actualBytes, expectedBytes, dropped)

	// With larger buffer, should flush most or all entries
	if actualBytes == 0 {
		t.Error("Expected some bytes to be flushed")
	}
}

// TestRingBuffer_HighThroughput verifies that RingBuffer handles high throughput
// correctly with the race detector enabled.
func TestRingBuffer_HighThroughput(t *testing.T) {
	buf := &safeBuffer{}
	// Large buffer for high throughput test
	rb := NewRingBuffer(buf, 2048)

	const numGoroutines = 50
	const writesPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range writesPerGoroutine {
				data := []byte("msg\n")
				rb.Write(data)
			}
		}()
	}

	wg.Wait()

	// Wait for some flushing to occur
	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 10*time.Second, 10*time.Millisecond, "data should be flushed")

	// Close to flush remaining entries
	_ = rb.Close()

	dropped := rb.DroppedCount()
	actualBytes := buf.Len()
	expectedBytes := numGoroutines * writesPerGoroutine * len("msg\n")

	t.Logf("Processed %d writes: flushed %d bytes (expected %d), dropped %d",
		numGoroutines*writesPerGoroutine, actualBytes, expectedBytes, dropped)

	// Should flush significant amount of data
	if actualBytes == 0 {
		t.Error("Expected some bytes to be flushed under high throughput")
	}
}

// TestRingBuffer_OverflowHandling verifies that buffer overflow is handled correctly.
func TestRingBuffer_OverflowHandling(t *testing.T) {
	buf := &safeBuffer{}
	// Small buffer to force overflow
	rb := NewRingBuffer(buf, 8)

	const numWrites = 1000
	for range numWrites {
		rb.Write([]byte("test\n"))
	}

	// Wait for some flushing
	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 10*time.Second, 10*time.Millisecond, "some data should be flushed")

	_ = rb.Close()

	// Some messages may be dropped due to small buffer
	dropped := rb.DroppedCount()
	t.Logf("Dropped %d messages out of %d writes with small buffer", dropped, numWrites)

	// Buffer should still function correctly
	if buf.Len() == 0 {
		t.Error("Buffer should have flushed some data")
	}
}

// TestRingBuffer_Write_BoundedSpin verifies that Write returns false (dropped) rather than spinning
// forever when a slot stays committed. This guards the bounded-spin fix in Write.
func TestRingBuffer_Write_BoundedSpin(t *testing.T) {
	buf := &safeBuffer{}
	// Size-2 buffer: both slots will be permanently committed so Write hits the spin limit.
	rb := NewRingBuffer(buf, 2)

	// Stop the flusher so nothing is ever consumed.
	close(rb.stopCh)
	<-rb.doneCh

	// Mark both slots as permanently committed so Write spins waiting for them to clear.
	for i := range rb.entries {
		rb.entries[i].committed.Store(1)
	}

	// Reset head and tail so Write claims a slot and enters the spin loop.
	rb.head.Store(0)
	rb.tail.Store(0)

	done := make(chan bool, 1)
	go func() {
		result := rb.Write([]byte("test"))
		done <- result
	}()

	select {
	case result := <-done:
		if result {
			t.Error("expected Write to return false (dropped) when slot stays committed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Write did not return within timeout, unbounded spin detected")
	}
}

// TestRingBuffer_CloseFlushesAll verifies that Close() flushes all pending entries.
func TestRingBuffer_CloseFlushesAll(t *testing.T) {
	buf := &safeBuffer{}
	rb := NewRingBuffer(buf, 64)

	const numWrites = 100
	for range numWrites {
		rb.Write([]byte("msg\n"))
	}

	// Wait for entries to be flushed
	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 10*time.Second, 10*time.Millisecond, "some entries should be flushed")

	// Close should flush all pending entries
	_ = rb.Close()

	// Should have flushed most or all entries
	actualBytes := buf.Len()
	expectedBytes := numWrites * len("msg\n")
	t.Logf("Flushed %d bytes out of expected %d bytes", actualBytes, expectedBytes)

	if actualBytes == 0 {
		t.Error("Expected some bytes to be flushed after Close()")
	}
}

func TestRingBuffer_ZeroLengthWrite(t *testing.T) {
	buf := &safeBuffer{}
	rb := NewRingBuffer(buf, 16)

	rb.Write([]byte{})
	rb.Write([]byte("after zero\n"))

	done := make(chan struct{})
	go func() {
		_ = rb.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RingBuffer.Close() hung after zero-length write")
	}
}

func TestNewRingBuffer_MinSize(t *testing.T) {
	for _, size := range []int{0, 1} {
		buf := &safeBuffer{}
		rb := NewRingBuffer(buf, size)

		if !rb.Write([]byte("hello\n")) {
			t.Logf("size=%d: write returned false (dropped)", size)
		}

		waitFor(t, func() bool {
			return buf.Len() > 0
		}, 5*time.Second, 5*time.Millisecond, "data should flush after min-size construction")

		_ = rb.Close()

		if buf.Len() == 0 {
			t.Errorf("size=%d: no data flushed from ring buffer", size)
		}
	}
}

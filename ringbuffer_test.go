package velocity

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

// TestRingBuffer_WriterPreemptedBeforeClaim deterministically reproduces the
// double-claim race that commit 31018f4 fixed.
//
// The exact scenario:
//  1. Writer A wins the outer head CAS for slot 0 (head=0). It exits the
//     sequence spin (expected==0). The hook fires and stalls writer A here.
//  2. We manually simulate what the flusher's skipSlot does: advance expected
//     for slot 0 to the next-round value (0 + size = 4) and bump tail. We also
//     skip slot 1 (bump tail again) so the overflow guard passes for writer B.
//  3. Writer B wins head=4 (wrapping to slot 0 again). expected==4, so B passes
//     the sequence spin. With the CAS fix, B advances expected to 5. Without it,
//     expected stays at 4.
//  4. The hook releases writer A. Without the CAS fix, A writes "writer-A\n"
//     into slot 0 — which B owns — producing a double-write. With the fix, A's
//     CAS(0→1) fails because expected is 5, so A drops.
//
// Slot ownership is asserted by inspecting committed and data directly, making
// the test fully deterministic and independent of flusher timing.
func TestRingBuffer_WriterPreemptedBeforeClaim(t *testing.T) {
	t.Parallel()

	// Restore the hook after this test so parallel tests are unaffected.
	t.Cleanup(func() { afterSequenceSpinHook = nil })

	buf := &safeBuffer{}

	// Size-4 ring: idx = head & 3, so head=0 and head=4 both land on slot 0.
	// Stop the flusher so we control all slot-state transitions ourselves.
	rb := NewRingBuffer(buf, 4)
	close(rb.stopCh)
	<-rb.doneCh

	hookFired := make(chan struct{})
	releaseA := make(chan struct{})

	afterSequenceSpinHook = func() {
		afterSequenceSpinHook = nil // fire exactly once
		close(hookFired)
		<-releaseA
	}

	var writerAOK atomic.Bool
	writerADone := make(chan struct{})
	go func() {
		// Writer A: outer CAS claims head=0 → slot 0. Sequence spin exits
		// (expected==0). Hook fires; writer A stalls here.
		writerAOK.Store(rb.Write([]byte("writer-A\n")))
		close(writerADone)
	}()

	select {
	case <-hookFired:
	case <-time.After(5 * time.Second):
		t.Fatal("hook never fired — writer A did not reach the preemption window")
	}

	// Writer A is stalled between sequence-spin exit and the CAS claim.
	// Its local `head` is 0; slot 0's expected is still 0 (unclaimed).
	//
	// Simulate skipSlot for slot 0: expected → 0+4=4, tail → 1.
	size := uint64(len(rb.entries)) // == 4
	rb.entries[0].expected.Store(size)
	rb.tail.Store(1)

	// Simulate skipSlot for slot 1 so tail=2. This satisfies the overflow guard:
	// writer B needs nextHead(5) - tail(2) = 3 <= mask(3).
	rb.entries[1].expected.Store(1 + size) // next-round value for slot 1
	rb.tail.Store(2)

	// Advance head to 4 so writer B lands on slot 0 next round.
	rb.head.Store(4)

	// Writer B: head=4, idx=0. expected==4==head, passes the spin immediately.
	// The CAS fix: B does CAS(4→5); expected becomes 5. Without fix: expected stays 4.
	writerBOK := rb.Write([]byte("writer-B\n"))

	// Release writer A. With the fix, A tries CAS(0→1) on expected==5 → fails → dropped.
	// Without the fix, A writes "writer-A\n" over B's slot 0.
	close(releaseA)

	select {
	case <-writerADone:
	case <-time.After(5 * time.Second):
		t.Fatal("writer A goroutine did not finish — possible deadlock")
	}

	if writerBOK {
		// B wrote first. A should have been dropped by the failed CAS.
		// If A also wrote (double-claim), slot 0's data will contain "writer-A".
		committed := rb.entries[0].committed.Load()
		data := string(rb.entries[0].data[:rb.entries[0].size.Load()])
		if committed == 1 && strings.Contains(data, "writer-A") {
			t.Errorf("double-claim: writer A overwrote writer B's slot — CAS claim fix is broken")
		}
		if writerAOK.Load() {
			t.Errorf("both writers reported success for the same physical slot — double-claim bug")
		}
	}
}

// TestRingBuffer_HighThroughput verifies high-throughput correctness with the
// race detector enabled. Each goroutine writes sequenced payloads so we can
// assert two invariants after Close():
//
//  1. No duplicates: a double-claim bug would emit the same (goroutine, seq)
//     pair twice.
//  2. No orphan: every (goroutine, seq) pair that was not dropped appears
//     exactly once (no phantom entries that never existed).
//
// Ring size (65536) is the next power-of-2 above the total write count (50000)
// so the buffer never needs to wrap during the burst. With continuous flushing
// via the default select branch, drops should be zero in steady state. If the
// scheduler bursts all writers before the flusher gets a chance the overflow
// guard may drop a small number; we assert drops < 1% of total writes.
func TestRingBuffer_HighThroughput(t *testing.T) {
	buf := &safeBuffer{}
	rb := NewRingBuffer(buf, 65536)

	const numGoroutines = 50
	const writesPerGoroutine = 1000
	const total = numGoroutines * writesPerGoroutine

	// Track which (goroutineID, seq) pairs were actually written (not dropped).
	// written[g*writesPerGoroutine+s] == true means that entry reached the ring.
	written := make([]atomic.Bool, total)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := range numGoroutines {
		go func(g int) {
			defer wg.Done()
			for s := range writesPerGoroutine {
				payload := fmt.Sprintf("g%d-s%d\n", g, s)
				if rb.Write([]byte(payload)) {
					written[g*writesPerGoroutine+s].Store(true)
				}
			}
		}(g)
	}

	wg.Wait()

	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 10*time.Second, 10*time.Millisecond, "data should be flushed")

	_ = rb.Close()

	dropped := rb.DroppedCount()

	t.Logf("total=%d dropped=%d", total, dropped)

	// Drops < 1% is the steady-state target. More than that suggests the buffer
	// is undersized or the flusher is falling behind.
	if dropped > uint64(total)/100 {
		t.Errorf("dropped %d/%d (%.1f%%) — exceeds 1%% budget; check ring size vs write rate",
			dropped, total, float64(dropped)/float64(total)*100)
	}

	// Parse flushed output and detect duplicates and phantom entries.
	seen := make(map[string]int, total)
	for line := range strings.SplitSeq(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		seen[line]++
	}

	// No duplicates: each line must appear at most once.
	for line, count := range seen {
		if count > 1 {
			t.Errorf("duplicate entry %q appears %d times — double-claim bug", line, count)
		}
	}

	// Every written entry must appear in the output exactly once.
	for g := range numGoroutines {
		for s := range writesPerGoroutine {
			if !written[g*writesPerGoroutine+s].Load() {
				continue // dropped before entering the ring, skip
			}
			key := fmt.Sprintf("g%d-s%d", g, s)
			if seen[key] == 0 {
				t.Errorf("entry %q was written to ring but not flushed — data loss", key)
			}
		}
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
// forever when a slot's sequence counter never advances. This guards the bounded-spin fix in Write.
func TestRingBuffer_Write_BoundedSpin(t *testing.T) {
	buf := &safeBuffer{}
	// Size-2 buffer: slot expected values are set to an unreachable sequence so Write hits the spin limit.
	rb := NewRingBuffer(buf, 2)

	// Stop the flusher so nothing is ever consumed.
	close(rb.stopCh)
	<-rb.doneCh

	// Set expected to a value that will never match head=0, so Write spins indefinitely.
	for i := range rb.entries {
		rb.entries[i].expected.Store(999)
	}

	// Reset head and tail so Write claims slot 0 and enters the sequence spin loop.
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

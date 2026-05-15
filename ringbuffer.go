package velocity

import (
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultRingBufferSize = 1024
	DefaultBatchSize      = 64
	DefaultFlushInterval  = 10 * time.Millisecond
)

type RingBufferEntry struct {
	data      []byte
	expected  atomic.Uint64 // which head value owns this slot next
	size      atomic.Int32
	committed atomic.Uint32
}

// RingBuffer implements a lock-free ring buffer for batched writing.
// Uses atomic operations to minimise contention and provide high throughput.
type RingBuffer struct {
	writer  io.Writer
	stopCh  chan struct{}
	doneCh  chan struct{}
	entries []RingBufferEntry
	wg      sync.WaitGroup

	mask uint64 // Size - 1 for fast modulo using bitwise AND
	head atomic.Uint64
	tail atomic.Uint64

	batchSize     int
	flushInterval time.Duration

	dropped atomic.Uint64
}

// NewRingBuffer creates a new ring buffer with the specified size.
// Size must be a power of 2 for optimal performance.
func NewRingBuffer(writer io.Writer, size int) *RingBuffer {
	// Ensure size is power of 2
	if size&(size-1) != 0 {
		// Bounds check before conversion to prevent G115 overflow warning
		if size < 0 || size > (1<<31-1) {
			size = DefaultRingBufferSize
		}

		v := uint64(size) // #nosec G115 -- bounds checked above
		v--
		v |= v >> 1
		v |= v >> 2
		v |= v >> 4
		v |= v >> 8
		v |= v >> 16
		v |= v >> 32
		v++
		// Safe conversion after bounds check
		maxInt := int(^uint(0) >> 1)
		// #nosec G115 -- maxInt is derived from int type limits, always safe
		if v > uint64(maxInt) {
			size = maxInt
		} else {
			size = int(v) // #nosec G115 -- v is bounded by maxInt check above
		}
	}

	// Another bounds check before final conversion
	if size < 0 || size > (1<<31-1) {
		size = DefaultRingBufferSize
	}

	// Minimum size of 2 required for correct mask arithmetic (mask = size-1).
	if size < 2 {
		size = DefaultRingBufferSize
	}

	rb := &RingBuffer{
		entries:       make([]RingBufferEntry, size),
		mask:          uint64(size) - 1, // #nosec G115 -- size is bounded by checks above
		writer:        writer,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		batchSize:     DefaultBatchSize,
		flushInterval: DefaultFlushInterval,
	}

	for i := range rb.entries {
		rb.entries[i].data = make([]byte, 0, 512)
		rb.entries[i].expected.Store(uint64(i)) // #nosec G115 -- i is always non-negative
	}

	rb.wg.Add(1)
	go rb.flusher()

	return rb
}

// afterSequenceSpinHook is called in tests between the sequence-spin exit and
// the CAS claim to simulate preemption and expose double-claim races.
// Stored as an atomic pointer so concurrent test goroutines can read and clear
// it without a data race (package-level vars are shared across parallel tests).
var afterSequenceSpinHook atomic.Pointer[func()]

// Write adds a log entry to the ring buffer.
// Returns false if the buffer is full and the message was dropped.
func (rb *RingBuffer) Write(data []byte) bool {
	for {
		head := rb.head.Load()
		nextHead := head + 1
		tail := rb.tail.Load()

		if nextHead-tail > rb.mask {
			rb.dropped.Add(1)
			return false
		}

		if rb.head.CompareAndSwap(head, nextHead) {
			idx := head & rb.mask
			entry := &rb.entries[idx]

			// Wait until the slot's sequence counter matches our head value.
			// This prevents two writers whose head values alias the same index
			// from writing concurrently when the ring wraps.
			spins := 0
			for entry.expected.Load() != head {
				runtime.Gosched()
				spins++
				if spins > 1000 {
					rb.dropped.Add(1)
					return false
				}
			}

			// Allow tests to inject a pause between spin exit and the claim CAS,
			// reproducing the preemption window the fix targets.
			if h := afterSequenceSpinHook.Load(); h != nil {
				(*h)()
			}

			// Atomically claim the write section by advancing expected from head to
			// head+1. This prevents the flusher's bounded-spin skip from racing with
			// a preempted writer that exited the spin above but hasn't yet written
			// entry.data. The flusher's skip only fires when expected == tail (meaning
			// no writer ever claimed this slot for the current round).
			if !entry.expected.CompareAndSwap(head, head+1) {
				// Flusher already advanced expected past our round. Drop.
				rb.dropped.Add(1)
				return false
			}

			dataLen := len(data)

			if cap(entry.data) >= dataLen {
				entry.data = entry.data[:dataLen]
				copy(entry.data, data)
			} else {
				entry.data = make([]byte, dataLen)
				copy(entry.data, data)
			}

			entry.size.Store(int32(dataLen)) // #nosec G115 - dataLen is from len() which is always non-negative
			entry.committed.Store(1)

			return true
		}
	}
}

// waitForCommit spins until entry.committed == 1 or the spin limit is reached.
// Returns true if committed was observed.
func waitForCommit(entry *RingBufferEntry, limit int) bool {
	for spins := 0; entry.committed.Load() != 1 && spins < limit; spins++ {
		runtime.Gosched()
	}
	return entry.committed.Load() == 1
}

// skipSlot resets a stalled or orphaned slot and advances tail.
// Only safe to call when the slot is unclaimed (expected == tail).
func (rb *RingBuffer) skipSlot(entry *RingBufferEntry, tail uint64) {
	rb.dropped.Add(1)
	entry.size.Store(0)
	entry.committed.Store(0)
	entry.expected.Store(tail + uint64(len(rb.entries)))
	rb.tail.Store(tail + 1)
}

func (rb *RingBuffer) flusher() {
	defer rb.wg.Done()
	defer close(rb.doneCh)

	ticker := time.NewTicker(rb.flushInterval)
	defer ticker.Stop()

	// Single reusable buffer avoids per-entry and per-flush allocations.
	batchBuf := make([]byte, 0, DefaultBatchSize*512)

	for {
		select {
		case <-rb.stopCh:
			// Flush data already collected into batchBuf before draining the ring.
			if len(batchBuf) > 0 {
				if _, err := rb.writer.Write(batchBuf); err != nil {
					rb.dropped.Add(1)
				}
			}
			rb.flushAll()
			return

		case <-ticker.C:
			if len(batchBuf) > 0 {
				if _, err := rb.writer.Write(batchBuf); err != nil {
					rb.dropped.Add(1)
				}
				batchBuf = batchBuf[:0]
			}

		default:
			batchBuf = rb.collectBatch(batchBuf)

			if len(batchBuf) > 0 {
				if _, err := rb.writer.Write(batchBuf); err != nil {
					rb.dropped.Add(1)
				}
				batchBuf = batchBuf[:0]
			}

			if rb.tail.Load() >= rb.head.Load() {
				time.Sleep(100 * time.Microsecond)
			}
		}
	}
}

// collectBatch drains up to batchSize committed entries into buf and returns the
// updated slice. Stops early if the ring is empty or a stalled writer is encountered.
func (rb *RingBuffer) collectBatch(buf []byte) []byte {
	for range rb.batchSize {
		tail := rb.tail.Load()
		head := rb.head.Load()

		if tail >= head {
			break
		}

		idx := tail & rb.mask
		entry := &rb.entries[idx]

		// Load committed FIRST as the acquire barrier to ensure all writes to
		// entry.data are visible before we read them. This is critical for
		// weakly-ordered architectures like ARM.
		//
		// Spin briefly if a writer claimed the slot but has not committed yet.
		// Without a limit, a stalled writer would block the flusher forever.
		if !waitForCommit(entry, 1000) {
			// Check whether a writer atomically claimed this slot (expected == tail+1)
			// or never entered the write section (expected == tail, writer gave up in
			// its bounded spin before the claim CAS). Only skip unclaimed slots —
			// advancing expected while a writer is still active races on entry.data.
			if entry.expected.Load() == tail {
				rb.skipSlot(entry, tail)
			}
			// else: writer claimed (expected == tail+1) but is slow to commit.
			// Break and retry on the next tick rather than race the active writer.
			break
		}

		size := entry.size.Load()
		if size > 0 {
			buf = append(buf, entry.data[:size]...)
		}
		entry.size.Store(0)
		entry.committed.Store(0)
		// Advance the sequence so the next round's writer can claim this slot.
		entry.expected.Store(tail + uint64(len(rb.entries)))
		rb.tail.Store(tail + 1)
	}

	return buf
}

func (rb *RingBuffer) flushAll() {
	for {
		tail := rb.tail.Load()
		head := rb.head.Load()

		if tail >= head {
			return
		}

		idx := tail & rb.mask
		entry := &rb.entries[idx]

		// Load committed FIRST as the acquire barrier to ensure all writes to
		// entry.data are visible before we read them. This is critical for
		// weakly-ordered architectures like ARM.
		//
		// Spin briefly if a writer claimed the slot but has not committed yet.
		// Without a limit, a stalled writer would block Close() forever.
		if !waitForCommit(entry, 1000) {
			rb.handleStalledSlotOnClose(entry, tail)
			continue
		}

		size := entry.size.Load()
		if size > 0 {
			if _, err := rb.writer.Write(entry.data[:size]); err != nil {
				rb.dropped.Add(1)
			}
		}
		entry.size.Store(0)
		entry.committed.Store(0)
		// Advance the sequence so the next round's writer can claim this slot.
		entry.expected.Store(tail + uint64(len(rb.entries)))
		rb.tail.Store(tail + 1)
	}
}

// handleStalledSlotOnClose handles a slot that has not committed within the initial
// spin budget during Close(). Applies the same claimed-vs-unclaimed check as the
// batch flusher to avoid racing an active writer, but spins longer because Close()
// must drain as many entries as possible before returning.
func (rb *RingBuffer) handleStalledSlotOnClose(entry *RingBufferEntry, tail uint64) {
	if entry.expected.Load() == tail {
		// Slot unclaimed: writer gave up before the claim CAS. Safe to skip.
		rb.skipSlot(entry, tail)
		return
	}

	// Writer claimed (expected == tail+1) but is slow. Spin longer to avoid
	// losing data on shutdown — the write section is nanoseconds, so extra
	// Gosched iterations cost very little and recover the entry.
	if !waitForCommit(entry, 9000) {
		// Still not committed after extended wait. Skip to avoid hanging Close().
		rb.skipSlot(entry, tail)
		return
	}

	size := entry.size.Load()
	if size > 0 {
		if _, err := rb.writer.Write(entry.data[:size]); err != nil {
			rb.dropped.Add(1)
		}
	}
	entry.size.Store(0)
	entry.committed.Store(0)
	entry.expected.Store(tail + uint64(len(rb.entries)))
	rb.tail.Store(tail + 1)
}

func (rb *RingBuffer) Close() error {
	close(rb.stopCh)
	rb.wg.Wait()
	<-rb.doneCh
	return nil
}

func (rb *RingBuffer) DroppedCount() uint64 {
	return rb.dropped.Load()
}

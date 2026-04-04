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
	size      atomic.Int32  // Atomic to prevent races between writer and flusher
	committed atomic.Uint32 // Memory barrier to synchronise writer and flusher
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
	}

	rb.wg.Add(1)
	go rb.flusher()

	return rb
}

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

			// CAS on head ensures only one writer can claim this slot.
			// Wait for flusher to finish consuming this slot before reusing.
			// Bound the spin to avoid blocking indefinitely if the flusher stalls.
			spins := 0
			for entry.committed.Load() != 0 {
				runtime.Gosched()
				spins++
				if spins > 1000 {
					rb.dropped.Add(1)
					return false
				}
			}

			// Reuse buffer if possible to reduce allocations
			dataLen := len(data)

			if cap(entry.data) >= dataLen {
				entry.data = entry.data[:dataLen]
				copy(entry.data, data)
			} else {
				entry.data = make([]byte, dataLen)
				copy(entry.data, data)
			}

			entry.size.Store(int32(dataLen)) // #nosec G115 - dataLen is from len() which is always non-negative

			// Mark entry as committed after all data is written.
			// This is the synchronisation point for the flusher.
			entry.committed.Store(1)

			return true
		}
	}
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
			collected := 0
			for collected < rb.batchSize {
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
				spins := 0
				for entry.committed.Load() != 1 && spins < 1000 {
					runtime.Gosched()
					spins++
				}
				if entry.committed.Load() != 1 {
					// Writer stalled. Skip slot to avoid deadlock.
					rb.dropped.Add(1)
					entry.size.Store(0)
					entry.committed.Store(0)
					rb.tail.Store(tail + 1)
					break
				}
				size := entry.size.Load()
				if size > 0 {
					batchBuf = append(batchBuf, entry.data[:size]...)
				}
				// Always clear and advance for committed entries, even zero-size ones.
				// Without this, a zero-length write leaves the slot permanently occupied.
				entry.size.Store(0)
				// Memory barrier: mark as consumed, allowing writer to reuse slot.
				entry.committed.Store(0)
				collected++
				// Only advance tail when entry is consumed.
				rb.tail.Store(tail + 1)
			}

			// Flush when batch is full or when idle with pending data.
			if len(batchBuf) > 0 && (collected >= rb.batchSize || collected == 0) {
				if _, err := rb.writer.Write(batchBuf); err != nil {
					rb.dropped.Add(1)
				}
				batchBuf = batchBuf[:0]
			}

			if collected == 0 {
				time.Sleep(100 * time.Microsecond)
			}
		}
	}
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
		spins := 0
		for entry.committed.Load() != 1 && spins < 1000 {
			runtime.Gosched()
			spins++
		}
		if entry.committed.Load() != 1 {
			// Writer stalled. Skip slot to avoid hanging Close().
			rb.dropped.Add(1)
			entry.size.Store(0)
			entry.committed.Store(0)
			rb.tail.Store(tail + 1)
			continue
		}
		size := entry.size.Load()
		if size > 0 {
			if _, err := rb.writer.Write(entry.data[:size]); err != nil {
				rb.dropped.Add(1)
			}
		}
		// Always clear and advance for committed entries, even zero-size ones.
		// Without this, a zero-length write leaves the slot permanently occupied.
		entry.size.Store(0)
		// Memory barrier: mark as consumed, allowing writer to reuse slot.
		entry.committed.Store(0)
		// Only advance tail when entry is consumed.
		rb.tail.Store(tail + 1)
	}
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

package velocity

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultRingBufferSize = 1024
	DefaultBatchSize      = 64
	DefaultFlushInterval  = 10 * time.Millisecond
)

// RingBufferEntry is one queued record. The queue owns the payload copy held in
// data; producers never retain access to it after Write returns.
type RingBufferEntry struct {
	data []byte
}

// RingBuffer implements a bounded byte queue for batched writing.
//
// Producers copy their payload into queue-owned storage under a short mutex and
// never touch slot storage after that; a single draining goroutine is the only
// consumer. This replaces the previous speculative CAS/skip reclamation design,
// in which a bounded spin budget could retire slot storage still owned by a
// preempted writer (R10). Ownership is now transferred by the mutex, so no spin
// budget can lose a writer's bytes.
type RingBuffer struct {
	writer io.Writer

	// mu guards the queue fields and the closed flag. It is held only for
	// pointer/count updates and the payload copy — never across I/O.
	mu      sync.Mutex
	entries []RingBufferEntry
	mask    int // len(entries) - 1; len is a power of 2
	head    int // index of the oldest queued record
	count   int // queued record count
	closed  bool

	// stopCh is closed exactly once by the first Close; doneCh is closed by the
	// drainer after it has drained the final queue. Close blocks on doneCh, so
	// concurrent Close calls all wait for the same drain to finish.
	stopCh chan struct{}
	doneCh chan struct{}

	// wake nudges the drainer without blocking; the ticker covers a dropped
	// nudge so an entry never waits longer than one flush interval.
	wake          chan struct{}
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
		mask:          size - 1,
		writer:        writer,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		wake:          make(chan struct{}, 1),
		batchSize:     DefaultBatchSize,
		flushInterval: DefaultFlushInterval,
	}

	go rb.drainer()

	return rb
}

// Write adds a record to the queue.
// Returns false if the queue is full or closed and the record was dropped.
// On success the record's bytes are copied into queue-owned storage before
// Write returns, so the caller may reuse its slice immediately.
func (rb *RingBuffer) Write(data []byte) bool {
	rb.mu.Lock()
	if rb.closed || rb.count == len(rb.entries) {
		rb.mu.Unlock()
		rb.dropped.Add(1)
		return false
	}

	// The copy happens under the lock while the caller's bytes are still
	// valid; after this point the queue solely owns the payload.
	buf := make([]byte, len(data))
	copy(buf, data)
	rb.entries[(rb.head+rb.count)&rb.mask].data = buf
	rb.count++
	rb.mu.Unlock()

	select {
	case rb.wake <- struct{}{}:
	default:
	}

	return true
}

// drain pops up to batchSize records into buf and returns the updated buffer
// and the number of records popped. Byte copies happen under the lock, after
// which the queue releases its payload reference.
func (rb *RingBuffer) drain(buf []byte) ([]byte, int) {
	rb.mu.Lock()
	n := min(rb.batchSize, rb.count)
	for range n {
		e := &rb.entries[rb.head]
		buf = append(buf, e.data...)
		e.data = nil
		rb.head = (rb.head + 1) & rb.mask
	}
	rb.count -= n
	rb.mu.Unlock()
	return buf, n
}

// writeBatch hands one drained batch to the underlying writer. On error the
// whole batch is counted dropped in DroppedCount — records are never re-queued
// after a failed write, so a record is either delivered (possibly with a
// partial trailing record if the sink wrote some bytes before failing) or
// counted, never both. Zero-byte batches skip the sink entirely.
func (rb *RingBuffer) writeBatch(buf []byte, records int) {
	if len(buf) == 0 {
		return
	}
	if _, err := rb.writer.Write(buf); err != nil {
		// records is a batch count bounded by batchSize, always non-negative.
		rb.dropped.Add(uint64(records)) // #nosec G115 -- bounded by batchSize
	}
}

func (rb *RingBuffer) drainer() {
	defer close(rb.doneCh)

	ticker := time.NewTicker(rb.flushInterval)
	defer ticker.Stop()

	// Single reusable buffer avoids per-batch allocations.
	batchBuf := make([]byte, 0, DefaultBatchSize*512)

	for {
		// Drain everything currently queued before blocking again, so the
		// queue empties in bursts and a dropped wake nudge costs at most one
		// flush interval of latency.
		for {
			var n int
			batchBuf, n = rb.drain(batchBuf)
			if n == 0 {
				break
			}
			rb.writeBatch(batchBuf, n)
			batchBuf = batchBuf[:0]
		}

		select {
		case <-rb.stopCh:
			// Close set closed=true (rejecting further writes) before closing
			// stopCh, so the queue is now final: drain everything still queued
			// after the signal was observed, then exit. A write that enqueued
			// between the drain above and this point is caught here.
			for {
				var n int
				batchBuf, n = rb.drain(batchBuf)
				if n == 0 {
					return
				}
				rb.writeBatch(batchBuf, n)
				batchBuf = batchBuf[:0]
			}
		case <-rb.wake:
		case <-ticker.C:
		}
	}
}

// Close rejects further writes, drains every accepted record to the underlying
// writer, and is safe to call concurrently from multiple goroutines: every
// caller blocks until the same drain completes. The drain can block for as
// long as the underlying writer blocks.
func (rb *RingBuffer) Close() error {
	rb.mu.Lock()
	alreadyClosed := rb.closed
	rb.closed = true
	rb.mu.Unlock()

	if !alreadyClosed {
		close(rb.stopCh)
	}

	// Nudge so the drainer notices stopCh promptly instead of at the next tick.
	select {
	case rb.wake <- struct{}{}:
	default:
	}

	<-rb.doneCh
	return nil
}

func (rb *RingBuffer) DroppedCount() uint64 {
	return rb.dropped.Load()
}

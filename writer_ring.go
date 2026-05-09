package velocity

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

const minRingBufferWriterCapacity = 2

// EntrySnapshot is a value-typed deep copy of a log entry.
// Safe to read after the originating *Entry has been released to the pool.
type EntrySnapshot struct {
	Time    time.Time
	Message string
	Caller  string
	Fields  []FieldSnapshot
	Level   Level
}

// FieldSnapshot holds a pre-formatted field key/value pair.
// Value is the string form produced at write time, not at read time.
type FieldSnapshot struct {
	Key   string
	Value string
}

// RingStats reports the current state of a RingBufferWriter.
type RingStats struct {
	Capacity int
	Fill     int
	Drops    int64
	Total    int64
}

// ringOptions holds configuration applied via RingBufferOption.
type ringOptions struct {
	redactionMark string
}

// RingBufferOption configures a RingBufferWriter at construction time.
type RingBufferOption func(*ringOptions)

// RingRedactionMark sets the placeholder string used when Phase 4 redacts
// a Secure field for this writer. Default: "[REDACTED]".
func RingRedactionMark(s string) RingBufferOption {
	return func(o *ringOptions) {
		o.redactionMark = s
	}
}

// subscriber pairs a channel with a once-closer so neither the ctx-cancel
// goroutine nor Close() can panic on a double-close.
type subscriber struct {
	ch   chan EntrySnapshot
	once sync.Once
}

func (s *subscriber) close() {
	s.once.Do(func() { close(s.ch) })
}

// fieldSnapshotPool amortises the slice allocation for small field sets.
// Each snapshot borrows a slice, populates it, then keeps it — the pool is
// for the initial Get only; Put is called only when we discard a snapshot
// during ring overflow, not when the caller holds it via Snapshot().
var fieldSnapshotPool = sync.Pool{
	New: func() any {
		s := make([]FieldSnapshot, 0, 8)
		return &s
	},
}

// RingBufferWriter is a fixed-capacity in-process log sink.
// It stores the most recent N log entries as value-typed snapshots, making
// it safe to read after the original *Entry has been returned to its pool.
//
// Designed for the foundryos pattern: attach to a Logger, then serve
// snapshots over an HTTP debug endpoint or fan-out via Subscribe.
//
// Concurrency: a single mutex guards the ring head/tail and subscriber list.
// This is intentional — the ring writer sits off the critical path (attached
// via MultiWriter) and the per-write work (one mutex lock + one slice copy)
// is far cheaper than the CAS machinery in ringbuffer.go, which is optimised
// for byte-stream throughput, not snapshot semantics.
type RingBufferWriter struct {
	redactionMark string

	// ring is the fixed-size circular snapshot store.
	ring []EntrySnapshot

	// subscribers receive a copy of each new snapshot.
	// Slow consumers get dropped entries, not blocked writers.
	subscribers []*subscriber

	head     int // next write position
	fill     int // number of valid entries currently held
	capacity int

	drops atomic.Int64
	total atomic.Int64

	mu sync.Mutex

	// closed prevents writes after Close().
	closed bool

	// isTrusted mirrors the WriterTrusted() opt-in so IsTrusted() works
	// without the caller needing to inspect writerOptions separately.
	// Phase 4 reads this to decide whether to redact Secure fields.
	isTrusted bool
}

// NewRingBufferWriter creates a fixed-capacity snapshot ring.
// Capacity is clamped to minRingBufferWriterCapacity (2) if smaller.
func NewRingBufferWriter(capacity int, opts ...RingBufferOption) *RingBufferWriter {
	if capacity < minRingBufferWriterCapacity {
		capacity = minRingBufferWriterCapacity
	}

	o := ringOptions{
		redactionMark: "[REDACTED]",
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	return &RingBufferWriter{
		ring:          make([]EntrySnapshot, capacity),
		capacity:      capacity,
		redactionMark: o.redactionMark,
	}
}

// IsTrusted implements TrustedWriter. Returns false by default; true when the
// writer is added via AddWriter with WriterTrusted(). The trust flag is stored
// on writerOptions in MultiWriter — this method exists so callers can query
// the writer directly without going through the logger.
func (r *RingBufferWriter) IsTrusted() bool {
	return r.isTrusted
}

// SetTrusted is called by MultiWriter's AddWriter when WriterTrusted() is in
// the option set. Not part of the public API — internal plumbing for Phase 4.
func (r *RingBufferWriter) SetTrusted(v bool) {
	r.isTrusted = v
}

// Write converts the live entry to a value snapshot and appends it to the ring.
// When the ring is full the oldest entry is overwritten (drop-oldest semantics).
// Entries written after Close are silently discarded.
func (r *RingBufferWriter) Write(e *Entry) error {
	if e == nil {
		return nil
	}

	snap := toSnapshot(e)

	r.mu.Lock()

	if r.closed {
		r.mu.Unlock()
		putFieldSnapshot(snap.Fields)
		return nil
	}

	// Overwrite the oldest slot when full; the displaced snapshot's field
	// slice is returned to the pool to keep allocation churn low.
	if r.fill == r.capacity {
		putFieldSnapshot(r.ring[r.head].Fields)
		r.drops.Add(1)
	} else {
		r.fill++
	}

	r.ring[r.head] = snap
	r.head = (r.head + 1) % r.capacity

	// Fan-out to subscribers before releasing the lock so they see a
	// consistent snapshot. Non-blocking send: slow consumers drop, not block.
	for _, sub := range r.subscribers {
		select {
		case sub.ch <- snap:
		default:
			r.drops.Add(1)
		}
	}

	r.mu.Unlock()

	r.total.Add(1)
	return nil
}

// Snapshot returns the most recent n entries in chronological order (oldest first).
// n is clamped to the current fill count. Each call allocates a new slice —
// this is a diagnostic endpoint, not a hot path.
func (r *RingBufferWriter) Snapshot(n int) []EntrySnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n <= 0 || r.fill == 0 {
		return nil
	}

	if n > r.fill {
		n = r.fill
	}

	out := make([]EntrySnapshot, n)

	// tail is the index of the oldest valid entry.
	// head points to the next write slot, so the oldest is:
	//   (head - fill + capacity) % capacity
	tail := (r.head - r.fill + r.capacity) % r.capacity

	// Start from (fill - n) entries ahead of tail to get the most recent n.
	start := (tail + r.fill - n) % r.capacity

	for i := range n {
		idx := (start + i) % r.capacity
		src := r.ring[idx]
		// Deep-copy the field slice so the caller owns its memory.
		var fields []FieldSnapshot
		if len(src.Fields) > 0 {
			fields = make([]FieldSnapshot, len(src.Fields))
			copy(fields, src.Fields)
		}
		out[i] = EntrySnapshot{
			Time:    src.Time,
			Level:   src.Level,
			Message: src.Message,
			Fields:  fields,
			Caller:  src.Caller,
		}
	}

	return out
}

// Subscribe returns a channel that receives a copy of every new snapshot.
// The channel is buffered to bufSize. Slow consumers get dropped entries
// (the Drops counter is incremented). The channel closes when ctx is cancelled.
// bufSize is clamped to 1 if zero or negative.
func (r *RingBufferWriter) Subscribe(ctx context.Context, bufSize int) <-chan EntrySnapshot {
	if bufSize < 1 {
		bufSize = 1
	}

	sub := &subscriber{ch: make(chan EntrySnapshot, bufSize)}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		sub.close()
		return sub.ch
	}
	r.subscribers = append(r.subscribers, sub)
	r.mu.Unlock()

	// Unregister and close when the context is cancelled.
	// sub.close() is idempotent via sync.Once, so it is safe even if
	// Close() fired concurrently and already closed the channel.
	go func() {
		<-ctx.Done()
		r.mu.Lock()
		r.removeSubscriber(sub)
		r.mu.Unlock()
		sub.close()
	}()

	return sub.ch
}

// Stats returns a point-in-time view of ring state.
func (r *RingBufferWriter) Stats() RingStats {
	r.mu.Lock()
	fill := r.fill
	r.mu.Unlock()

	return RingStats{
		Capacity: r.capacity,
		Fill:     fill,
		Drops:    r.drops.Load(),
		Total:    r.total.Load(),
	}
}

// Close prevents further writes and closes all subscriber channels.
// Safe to call more than once.
func (r *RingBufferWriter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true

	// Close all subscriber channels. sub.close() is guarded by sync.Once,
	// so it is safe even if the ctx-cancel goroutine fires concurrently.
	for _, sub := range r.subscribers {
		sub.close()
	}
	r.subscribers = nil

	return nil
}

// toSnapshot deep-copies the live entry into a value type safe to retain
// after the entry is released. The field slice comes from the pool to reduce
// allocation pressure on the write path.
func toSnapshot(e *Entry) EntrySnapshot {
	var fields []FieldSnapshot

	if len(e.Fields) > 0 {
		ptr, ok := fieldSnapshotPool.Get().(*[]FieldSnapshot)
		if !ok || ptr == nil {
			s := make([]FieldSnapshot, 0, len(e.Fields))
			ptr = &s
		}

		fs := (*ptr)[:0]
		if cap(fs) < len(e.Fields) {
			fs = make([]FieldSnapshot, 0, len(e.Fields))
		}

		for _, f := range e.Fields {
			fs = append(fs, FieldSnapshot{
				Key:   f.Key,
				Value: FieldValueToString(f),
			})
		}
		fields = fs
	}

	return EntrySnapshot{
		Time:    e.Time,
		Level:   e.Level,
		Message: e.Message,
		Fields:  fields,
		Caller:  e.Caller,
	}
}

// removeSubscriber removes sub from the subscriber list.
// Must be called with r.mu held.
func (r *RingBufferWriter) removeSubscriber(sub *subscriber) {
	for i, s := range r.subscribers {
		if s == sub {
			// Swap with last to avoid shifting the slice.
			last := len(r.subscribers) - 1
			r.subscribers[i] = r.subscribers[last]
			r.subscribers[last] = nil
			r.subscribers = r.subscribers[:last]
			return
		}
	}
}

// putFieldSnapshot returns a field slice to the pool when it is no longer
// referenced (e.g. when a ring slot is overwritten). Only called for slices
// that the ring itself owns, never for slices returned by Snapshot().
func putFieldSnapshot(fs []FieldSnapshot) {
	if fs == nil {
		return
	}
	if cap(fs) > 64 {
		return
	}
	fs = fs[:0]
	fieldSnapshotPool.Put(&fs)
}

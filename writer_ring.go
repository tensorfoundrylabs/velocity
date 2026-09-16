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

// fieldSnapshotPtrPool recycles the *[]FieldSnapshot wrapper handles so
// putFieldSnapshot doesn't allocate one per call — the same pattern as
// slicePtrPool in pool.go. Taking an address of the local parameter escapes
// and heap-allocates; borrowing the handle avoids that.
var fieldSnapshotPtrPool = sync.Pool{
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
	// closedCh is closed exactly once by Close. Subscription cleanup goroutines
	// select on it so a context.Background subscriber cannot strand a goroutine
	// after the ring is closed — cleanup must finish on EITHER context
	// cancellation or ring closure.
	closedCh chan struct{}

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

	// isTrusted mirrors the WriterTrusted() opt-in so IsTrusted() works
	// without the caller needing to inspect writerOptions separately.
	// Phase 4 reads this to decide whether to redact Secure fields.
	isTrusted atomic.Bool

	// closed prevents writes after Close().
	closed bool
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
		closedCh:      make(chan struct{}),
	}
}

// IsTrusted implements TrustedWriter. Returns false by default; true when the
// writer is added via AddWriter with WriterTrusted(). The trust flag is stored
// on writerOptions in MultiWriter — this method exists so callers can query
// the writer directly without going through the logger.
func (r *RingBufferWriter) IsTrusted() bool {
	return r.isTrusted.Load()
}

// SetTrusted is called by MultiWriter's AddWriter when WriterTrusted() is in
// the option set. Not part of the public API — internal plumbing for Phase 4.
func (r *RingBufferWriter) SetTrusted(v bool) {
	r.isTrusted.Store(v)
}

// Write converts the live entry to a value snapshot and appends it to the ring.
// When the ring is full the oldest entry is overwritten (drop-oldest semantics).
// Entries written after Close are silently discarded.
// Secure fields are redacted unless the writer was registered with WriterTrusted().
func (r *RingBufferWriter) Write(e *Entry) error {
	return r.WriteSecure(e, r.isTrusted.Load(), r.redactionMark)
}

// WriteSecure implements SecureWriter. trusted controls whether Secure field
// plaintext is stored in the snapshot. Call via MultiWriter which passes the
// per-worker trust state; direct Write() uses the writer's own isTrusted flag.
func (r *RingBufferWriter) WriteSecure(e *Entry, trusted bool, redactionMark string) error {
	if e == nil {
		return nil
	}

	snap := toSnapshotSecure(e, trusted, redactionMark)

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
		// A channel handoff transfers ownership. The ring retains snap.Fields and
		// will reuse it on overflow, therefore each subscriber needs its own copy.
		delivered := cloneEntrySnapshot(snap)
		select {
		case sub.ch <- delivered:
		default:
			r.drops.Add(1)
		}
	}

	r.mu.Unlock()

	r.total.Add(1)
	return nil
}

func cloneEntrySnapshot(src EntrySnapshot) EntrySnapshot {
	dst := src
	if len(src.Fields) != 0 {
		dst.Fields = append([]FieldSnapshot(nil), src.Fields...)
	}
	return dst
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

	// Unregister and close when EITHER the context is cancelled or the ring is
	// closed — a context.Background subscription must not strand this goroutine
	// after Close. sub.close() is idempotent via sync.Once, so concurrent
	// firing of both paths is safe.
	go func() {
		select {
		case <-ctx.Done():
		case <-r.closedCh:
		}
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
	// Signal every subscription cleanup goroutine; idempotent via the closed
	// flag (Close is only ever run once past this point under r.mu).
	close(r.closedCh)

	// Close all subscriber channels. sub.close() is guarded by sync.Once,
	// so it is safe even if the ctx-cancel goroutine fires concurrently.
	for _, sub := range r.subscribers {
		sub.close()
	}
	r.subscribers = nil

	return nil
}

// toSnapshotSecure deep-copies the entry, applying trust-aware field serialisation.
// When trusted is false, Secure/SecureURL fields emit their redacted form and
// Redacted fields emit redactionMark.
func toSnapshotSecure(e *Entry, trusted bool, redactionMark string) EntrySnapshot {
	var fields []FieldSnapshot

	if len(e.Fields) > 0 {
		ptr, ok := fieldSnapshotPool.Get().(*[]FieldSnapshot)
		if !ok || ptr == nil {
			s := make([]FieldSnapshot, 0, len(e.Fields))
			ptr = &s
		}
		fs := (*ptr)[:0]

		// The wrapper handle is no longer needed now that the slice value is
		// taken: recycle it for putFieldSnapshot's next Put instead of letting
		// that allocate a fresh one. fs already carries the array pointer, so
		// a later borrower overwriting *ptr cannot alias this snapshot's
		// storage. Must happen after fs is read — after the Put, *ptr is
		// shared property.
		fieldSnapshotPtrPool.Put(ptr)

		if cap(fs) < len(e.Fields) {
			fs = make([]FieldSnapshot, 0, len(e.Fields))
		}

		for _, f := range e.Fields {
			val := fieldSnapshotValue(f, trusted, redactionMark)
			fs = append(fs, FieldSnapshot{Key: f.Key, Value: val})
		}
		fields = fs
	}

	msg := e.Message
	if e.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}

	return EntrySnapshot{
		Time:    e.Time,
		Level:   e.Level,
		Message: msg,
		Fields:  fields,
		Caller:  e.Caller,
	}
}

// fieldSnapshotValue converts a field to its snapshot string form,
// respecting trust state for Secure/SecureURL/Redacted types.
func fieldSnapshotValue(f Field, trusted bool, redactionMark string) string {
	switch f.Type {
	case FieldTypeSecure, FieldTypeSecureURL:
		if trusted && f.value != nil {
			return (*secureValue)(f.value).plain
		}
		if f.value != nil {
			return (*secureValue)(f.value).redacted
		}
		return redactionMark
	case FieldTypeRedacted:
		return redactionMark
	case FieldTypeTruncated:
		if f.value != nil {
			return *(*string)(f.value)
		}
		return ""
	default:
		return FieldValueToString(f)
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
	clear(fs)
	fs = fs[:0]
	// Borrow a wrapper handle rather than allocating &fs (the local would
	// escape to the heap). See fieldSnapshotPtrPool.
	p, _ := fieldSnapshotPtrPool.Get().(*[]FieldSnapshot)
	if p == nil {
		s := make([]FieldSnapshot, 0, cap(fs))
		p = &s
	}
	*p = fs
	fieldSnapshotPool.Put(p)
}

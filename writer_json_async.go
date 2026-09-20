package velocity

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// AsyncPolicy selects what a logging goroutine does when the asynchronous
// structured queue is full.
type AsyncPolicy int

const (
	// AsyncBlock back-pressures the caller until the drainer makes room.
	// Lossless: the queue can slow logging down but never silently lose
	// records. The zero value, so AsyncConfig{} is the conservative default.
	AsyncBlock AsyncPolicy = iota
	// AsyncDrop discards the formatted record, returns immediately, and
	// counts the loss in DroppedCount. Use for paths where logging must never
	// stall the request (the reason async output exists) and an occasional
	// dropped line under sustained sink stalls is acceptable.
	AsyncDrop
)

// DefaultAsyncQueue is the queue depth used when AsyncConfig.Queue is unset.
//
// 8192 records absorb roughly 130-400ms of burst at tens of thousands of
// lines per second (at 60k lines/s — two INFO lines per request at 30k req/s —
// it is ~140ms of headroom), which rides out scheduler spikes and short page
// cache flushes without the caller noticing. The memory bound is honest
// because the queue holds pooled byte buffers, not entries: worst case every
// slot holds a buffer grown past its 2KiB pool capacity and is capped at
// 32KiB, but typical JSON lines sit in reused 2KiB buffers, so steady-state
// retention is a few MiB, not 8192 x 32KiB.
const DefaultAsyncQueue = 8192

// AsyncConfig configures the opt-in asynchronous structured output. Supply it
// through WithAsyncOutput; without that option the JSON writer stays fully
// synchronous and byte-for-byte identical to previous behaviour.
type AsyncConfig struct {
	// Queue is the bounded queue depth between logging goroutines and the
	// single drainer. Non-positive values select DefaultAsyncQueue.
	Queue int

	// OnFull selects block or drop behaviour when the queue is full.
	OnFull AsyncPolicy
}

// jsonAsyncItem is one slot in the drainer's queue: a formatted record, an
// optional barrier to acknowledge once everything ahead of it is written, and
// the stop flag that retires the drainer during Close.
type jsonAsyncItem struct {
	buf     *bytes.Buffer // nil on barrier-only and stop items
	barrier chan struct{} // closed after the item (and all before it) is written
	stop    bool          // drainer exit sentinel; only sent by Close
}

// jsonAsync is the async state of a JSONWriter. Constructed once, before the
// writer is published to its readers, and immutable afterwards except for the
// dropped counter.
type jsonAsync struct {
	ch     chan jsonAsyncItem
	policy AsyncPolicy
	dropped atomic.Uint64

	// done closes when the drainer exits, so Close can prove no write is in
	// progress before flushing the underlying sink.
	done chan struct{}

	// writeMu serialises the drainer's writes (and Flush's underlying flush)
	// and is NEVER held across admission: w.mu stays a short admission lock
	// in async mode so callers cannot queue behind a syscall, which is the
	// contention this whole file exists to remove.
	writeMu sync.Mutex

	// The drainer cannot return write errors to callers, so the first failure
	// is recorded here and surfaced through Close.
	writeErrMu sync.Mutex
	writeErr   error
}

// NewAsyncJSONWriter builds a JSONWriter whose records are formatted on the
// caller's goroutine exactly as in the synchronous writer, then handed to a
// single background drainer that performs the write. No syscall — and no
// mutex held across one — ever runs on the logging goroutine.
func NewAsyncJSONWriter(out io.Writer, cfg AsyncConfig) *JSONWriter {
	w := NewJSONWriter(out)
	w.startAsync(cfg)
	return w
}

func (w *JSONWriter) startAsync(cfg AsyncConfig) {
	depth := cfg.Queue
	if depth <= 0 {
		depth = DefaultAsyncQueue
	}
	w.async = &jsonAsync{
		ch:     make(chan jsonAsyncItem, depth),
		policy: cfg.OnFull,
		done:   make(chan struct{}),
	}
	go w.drainAsync()
}

// enqueueAsync hands a completed buffer to the drainer. Reliable records
// (Fatal, and Flush's drain barrier) are never dropped and block until the
// drainer has written everything accepted before them, preserving ordering
// with earlier async entries.
func (w *JSONWriter) enqueueAsync(rawBuf *bytes.Buffer, reliable bool) {
	a := w.async
	if reliable {
		b := make(chan struct{})
		a.ch <- jsonAsyncItem{buf: rawBuf, barrier: b}
		<-b
		return
	}
	if a.policy == AsyncDrop {
		select {
		case a.ch <- jsonAsyncItem{buf: rawBuf}:
			return
		default:
			a.dropped.Add(1)
			w.putJSONBuffer(rawBuf)
			return
		}
	}
	a.ch <- jsonAsyncItem{buf: rawBuf}
}

// drainAsync owns the underlying writer for the async lifetime. It is the only
// goroutine that takes writeMu, so caller goroutines never queue on a mutex
// held across a syscall. Exits only on the Close sentinel, by which point
// admission has stopped and every admitted enqueue has landed.
func (w *JSONWriter) drainAsync() {
	a := w.async
	defer close(a.done)
	for {
		item := <-a.ch
		if item.buf != nil {
			a.writeMu.Lock()
			_, err := w.out.Write(item.buf.Bytes())
			a.writeMu.Unlock()
			w.putJSONBuffer(item.buf)
			if err != nil {
				a.writeErrMu.Lock()
				if a.writeErr == nil {
					a.writeErr = fmt.Errorf("json write failed: %w", err)
				}
				a.writeErrMu.Unlock()
			}
		}
		if item.barrier != nil {
			close(item.barrier)
		}
		if item.stop {
			return
		}
	}
}

// DroppedCount reports how many records AsyncDrop has discarded. It is always
// 0 on a synchronous writer, mirroring MultiWriter.DroppedCount.
func (w *JSONWriter) DroppedCount() uint64 {
	if a := w.async; a != nil {
		return a.dropped.Load()
	}
	return 0
}

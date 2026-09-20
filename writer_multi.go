package velocity

import (
	"errors"
	"maps"
	"sync"
	"sync/atomic"
)

// workerState holds per-writer state cached at AddWriter time.
// isTrusted and redactionMark are stored here rather than re-derived on every
// write, keeping the per-entry cost to a simple bool read in the fan-out loop.
type workerState struct {
	w             Writer
	sw            SecureWriter // non-nil when w implements SecureWriter; cached to avoid type assertion per write
	redactionMark string
	isTrusted     bool
}

type MultiWriter struct {
	// workers carries both the writer and its cached trust flag, keyed by name.
	workers map[string]workerState

	// Buffered channels prevent one slow writer from blocking others
	writeChans map[string]chan *Entry

	shutdownChan chan struct{}

	closeDone chan struct{}

	closeErrs []error

	wg sync.WaitGroup

	// dropped counts entries silently discarded because a worker channel was full.
	// Atomic so DroppedCount() can be read without acquiring any lock.
	dropped atomic.Uint64

	// mu guards closed, writeChans, and workers. Write() takes RLock (reads
	// writeChans without modifying them); AddWriter/RemoveWriter/Close take the
	// full write lock. This is safe: Close sets closed=true under the write lock,
	// so any Write() that holds RLock will see closed=false and finish its channel
	// send before Close closes those channels.
	mu sync.RWMutex

	shutdownOnce sync.Once

	// Family close lifecycle for this MultiWriter alone: the drain runs once,
	// every concurrent Close waits for the same completion and returns the same
	// recorded result (worker close errors joined).
	closeOnce sync.Once

	// Worker Close errors, recorded by each worker goroutine as it exits and
	// returned by Close after the drain. Guarded by closeErrMu because workers
	// finish concurrently with the Close caller.
	closeErrMu sync.Mutex

	closed bool
}

func NewMultiWriter() *MultiWriter {
	return &MultiWriter{
		workers:      make(map[string]workerState),
		writeChans:   make(map[string]chan *Entry),
		shutdownChan: make(chan struct{}),
		closeDone:    make(chan struct{}),
	}
}

// AddWriter registers a named writer with the given options.
// Replaces any existing writer with the same name.
// Thread-safe; no-op after Close.
func (mw *MultiWriter) AddWriter(name string, w Writer, opts ...WriterOption) {
	o := applyWriterOptions(opts)

	mw.mu.Lock()
	defer mw.mu.Unlock()

	if mw.closed {
		return
	}

	if old, ok := mw.writeChans[name]; ok {
		// Close the channel so the old worker drains and closes its writer.
		close(old)
	}

	ws := workerState{
		w:             w,
		isTrusted:     o.isTrusted,
		redactionMark: o.effectiveRedactionMark(),
	}
	// Cache the SecureWriter assertion at registration time so the hot path
	// pays only a bool comparison, not a type assertion per entry.
	if sw, ok := w.(SecureWriter); ok {
		ws.sw = sw
	}
	mw.workers[name] = ws

	// Buffer size trades latency vs blocking: smaller = less latency, larger
	// = less blocking. Depth is configurable per writer; non-positive options
	// fall back to the historical default.
	depth := o.queueDepth
	if depth <= 0 {
		depth = defaultWriterQueueDepth
	}
	ch := make(chan *Entry, depth)
	mw.writeChans[name] = ch

	mw.wg.Add(1)
	go mw.writerWorker(ws, ch)
}

// RemoveWriter removes the named writer and returns it so the caller can close it
// if needed. Returns nil if the name is not registered.
// Thread-safe; no-op after Close.
func (mw *MultiWriter) RemoveWriter(name string) Writer {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	state, exists := mw.workers[name]
	if !exists {
		return nil
	}

	if ch, ok := mw.writeChans[name]; ok {
		// If Close() has already set closed=true, it holds a snapshot of this
		// channel and will close it itself. Closing here too would panic.
		if !mw.closed {
			// Close channel so the worker drains and closes the writer.
			close(ch)
		}
		delete(mw.writeChans, name)
	}

	delete(mw.workers, name)
	return state.w
}

// WriterByName returns the writer registered under name, or nil.
// Useful for inspecting capabilities without removing the writer.
func (mw *MultiWriter) WriterByName(name string) Writer {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	return mw.workers[name].w
}

// IsTrusted reports whether the writer registered under name was added with WriterTrusted().
// Returns false for unknown names.
func (mw *MultiWriter) IsTrusted(name string) bool {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	return mw.workers[name].isTrusted
}

func (mw *MultiWriter) Write(e *Entry) error {
	// RLock is sufficient here: Write only reads writeChans and closed, it never
	// modifies them. Close() takes the write lock and sets closed=true before
	// closing channels, so a concurrent Write() holding RLock will see
	// closed=true (or will already have sent on the open channel) before the
	// channel is closed — no send-on-closed-channel is possible.
	mw.mu.RLock()
	defer mw.mu.RUnlock()

	if mw.closed {
		return ErrWriterClosed
	}

	// Non-blocking send prevents backpressure from slow writers.
	// Retain() before send; Release() on channel-full drop.
	for _, ch := range mw.writeChans {
		e.Retain()
		select {
		case ch <- e:
		default:
			// Worker channel is full; releasing here rather than blocking keeps
			// fast writers from stalling behind a slow consumer.
			mw.dropped.Add(1)
			e.Release()
		}
	}

	return nil
}

// WriteReliable delivers e to every registered worker and blocks until each
// has processed it — and therefore everything accepted before it (channels are
// FIFO with a single consumer). It never takes the queue-full drop branch:
// sends block until the worker makes room. Used by Logger.Fatal so the fatal
// entry is ordered behind all accepted entries and cannot be dropped before
// the FatalHandler or exit decision.
//
// The acknowledgement is tied to the specific queue position: immediately
// after e, a barrier sentinel is enqueued on the same channel, and the worker
// closes the sentinel's channel when it reaches it. Aggregate counters are
// deliberately not used — a producer can be preempted between its channel
// send and publishing any count it maintains, letting already-processed
// entries satisfy the wait while the barrier item is still queued (the F1
// finding from the independent finish review).
//
// Limitation, by design: a generic io.Writer cannot be cancelled, so this
// waits for a stalled writer indefinitely. No timeout is faked and no
// goroutine is spawned — the caller (Fatal or Close) simply blocks.
//
// Entry lifetime: the entry is Retained per channel and Released by the worker
// before the sentinel behind it is acknowledged, so returning guarantees every
// reference is gone.
func (mw *MultiWriter) WriteReliable(e *Entry) error {
	if e == nil {
		return nil
	}

	// Snapshot channels under RLock and perform the blocking sends while still
	// holding it: Close marks closed and closes channels under the write lock,
	// so holding RLock across the sends makes a send-on-closed-channel
	// impossible. Workers never take mw.mu, so this cannot self-deadlock.
	mw.mu.RLock()
	if mw.closed {
		mw.mu.RUnlock()
		return ErrWriterClosed
	}
	chans := make([]chan *Entry, 0, len(mw.writeChans))
	for _, ch := range mw.writeChans {
		chans = append(chans, ch)
	}
	barriers := make([]chan struct{}, 0, len(chans))
	for _, ch := range chans {
		e.Retain()
		ch <- e // blocking: wait for room rather than drop
		// The sentinel rides directly behind e on the same FIFO channel; its
		// processing by the single consumer proves e itself was processed and
		// released first.
		b := make(chan struct{})
		ch <- &Entry{barrier: b}
		barriers = append(barriers, b)
	}
	mw.mu.RUnlock()

	// Wait outside the lock: each sentinel closes only after its channel
	// consumed the entry ahead of it.
	for _, b := range barriers {
		<-b
	}
	return nil
}

func (mw *MultiWriter) writerWorker(ws workerState, ch chan *Entry) {
	defer mw.wg.Done()
	// Worker owns the writer lifecycle. Closing here ensures no concurrent
	// Write() calls happen after the worker exits, regardless of why it stopped.
	// The close error is recorded so Close can report AddWriter sink failures.
	defer func() {
		if err := ws.w.Close(); err != nil {
			mw.closeErrMu.Lock()
			mw.closeErrs = append(mw.closeErrs, err)
			mw.closeErrMu.Unlock()
		}
	}()

	write := func(e *Entry) {
		if ws.sw != nil {
			_ = ws.sw.WriteSecure(e, ws.isTrusted, ws.redactionMark)
		} else {
			_ = ws.w.Write(e)
		}
		// Fatal entries flush flushable sinks in-line: the reliable-write
		// barrier guarantees this has happened before the FatalHandler runs.
		// Non-fatal entries keep the ordinary nonblocking contract.
		if e.Level == LevelFatal {
			if f, ok := ws.w.(FlushableWriter); ok {
				_ = f.Flush()
			}
		}
	}

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.barrier != nil {
				// WriteReliable sentinel: everything ahead of it on this FIFO
				// channel (including the entry it was sent behind) has been
				// written and released. Not pooled, never retained — just ack.
				close(e.barrier)
				continue
			}
			write(e)
			// CRITICAL: Balance the Retain() from Write()/WriteReliable().
			e.Release()

		case <-mw.shutdownChan:
			// Drain all remaining entries. Close() guarantees ch will be closed
			// after shutdownChan, so range terminates once the channel is empty
			// and closed. Sentinels queued by an in-flight WriteReliable still
			// acknowledge so that barrier cannot hang on shutdown.
			for e := range ch {
				if e.barrier != nil {
					close(e.barrier)
					continue
				}
				write(e)
				e.Release()
			}
			return
		}
	}
}

func (mw *MultiWriter) Close() error {
	// The drain runs exactly once; concurrent Close callers all wait for the
	// same completion and receive the same recorded result instead of racing a
	// flag and reporting success early.
	mw.closeOnce.Do(func() {
		// Always close closeDone when the body finishes, even on the (currently
		// unreachable) already-closed path, so no Close caller can block forever.
		defer close(mw.closeDone)

		mw.mu.Lock()
		mw.closed = true
		channels := make(map[string]chan *Entry)
		maps.Copy(channels, mw.writeChans)
		mw.mu.Unlock()

		mw.shutdownOnce.Do(func() {
			close(mw.shutdownChan)
		})

		for _, ch := range channels {
			close(ch)
		}

		// Workers close their own writers when they exit, so wg.Wait() ensures
		// all writers are flushed and closed before we record the result. The
		// wait runs with no mw.mu held: workers never need it.
		mw.wg.Wait()
	})

	<-mw.closeDone
	mw.closeErrMu.Lock()
	defer mw.closeErrMu.Unlock()
	return errors.Join(mw.closeErrs...)
}

func (mw *MultiWriter) Stats() map[string]int {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	stats := make(map[string]int)

	for name, ch := range mw.writeChans {
		stats[name] = len(ch)
	}

	return stats
}

// DroppedCount returns the total number of entries discarded because a worker's
// buffered channel was full at the moment Write was called. Mirrors the same
// metric on RingBuffer so callers can observe back-pressure from slow writers.
// Safe to call concurrently; the counter is updated atomically without any lock.
func (mw *MultiWriter) DroppedCount() uint64 {
	if mw == nil {
		return 0
	}
	return mw.dropped.Load()
}

type FilteredWriter struct {
	w  Writer
	fn func(*Entry) bool
}

func NewFilteredWriter(w Writer, fn func(*Entry) bool) *FilteredWriter {
	return &FilteredWriter{
		w:  w,
		fn: fn,
	}
}

func (fw *FilteredWriter) Write(e *Entry) error {
	if !fw.fn(e) {
		return nil
	}
	return fw.w.Write(e)
}

func (fw *FilteredWriter) Close() error {
	return fw.w.Close()
}

package velocity

import (
	"maps"
	"sync"
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

	wg sync.WaitGroup

	shutdownOnce sync.Once
	// mu guards closed, writeChans, and workers. Write() takes RLock (reads
	// writeChans without modifying them); AddWriter/RemoveWriter/Close take the
	// full write lock. This is safe: Close sets closed=true under the write lock,
	// so any Write() that holds RLock will see closed=false and finish its channel
	// send before Close closes those channels.
	mu sync.RWMutex

	closed bool
}

func NewMultiWriter() *MultiWriter {
	return &MultiWriter{
		workers:      make(map[string]workerState),
		writeChans:   make(map[string]chan *Entry),
		shutdownChan: make(chan struct{}),
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

	if ch, ok := mw.writeChans[name]; ok {
		// Close the channel so the old worker drains and closes its writer.
		close(ch)
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

	// Buffer size trades latency vs blocking: smaller = less latency, larger = less blocking
	ch := make(chan *Entry, 256)
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
			e.Release()
		}
	}

	return nil
}

func (mw *MultiWriter) writerWorker(ws workerState, ch chan *Entry) {
	defer mw.wg.Done()
	// Worker owns the writer lifecycle. Closing here ensures no concurrent
	// Write() calls happen after the worker exits, regardless of why it stopped.
	defer func() { _ = ws.w.Close() }()

	write := func(e *Entry) {
		if ws.sw != nil {
			_ = ws.sw.WriteSecure(e, ws.isTrusted, ws.redactionMark)
		} else {
			_ = ws.w.Write(e)
		}
	}

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			write(e)
			// CRITICAL: Balance the Retain() from Write()
			e.Release()

		case <-mw.shutdownChan:
			// Drain all remaining entries. Close() guarantees ch will be closed
			// after shutdownChan, so range terminates once the channel is empty and closed.
			for e := range ch {
				write(e)
				e.Release()
			}
			return
		}
	}
}

func (mw *MultiWriter) Close() error {
	mw.mu.Lock()

	if mw.closed {
		mw.mu.Unlock()
		return nil
	}

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
	// all writers are flushed and closed before we return.
	mw.wg.Wait()

	return nil
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

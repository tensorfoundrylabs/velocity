package velocity

import (
	"maps"
	"sync"
)

type MultiWriter struct {
	writers map[string]Writer

	// Buffered channels prevent one slow writer from blocking others
	writeChans map[string]chan *Entry

	shutdownChan chan struct{}

	wg sync.WaitGroup

	shutdownOnce sync.Once
	mu           sync.Mutex

	closed bool
}

func NewMultiWriter() *MultiWriter {
	return &MultiWriter{
		writers:      make(map[string]Writer),
		writeChans:   make(map[string]chan *Entry),
		shutdownChan: make(chan struct{}),
	}
}

func (mw *MultiWriter) AddWriter(name string, w Writer) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	if mw.closed {
		return
	}

	if ch, ok := mw.writeChans[name]; ok {
		// Close the channel so the old worker drains and closes its writer.
		close(ch)
	}

	mw.writers[name] = w

	// Buffer size trades latency vs blocking: smaller = less latency, larger = less blocking
	ch := make(chan *Entry, 256)
	mw.writeChans[name] = ch

	mw.wg.Add(1)
	go mw.writerWorker(name, w, ch)
}

func (mw *MultiWriter) RemoveWriter(name string) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	if ch, ok := mw.writeChans[name]; ok {
		// If Close() has already set closed=true, it holds a snapshot of this
		// channel and will close it itself. Closing here too would panic.
		if !mw.closed {
			// Close channel so the worker drains and closes the writer.
			close(ch)
		}
		delete(mw.writeChans, name)
	}

	delete(mw.writers, name)
}

func (mw *MultiWriter) Write(e *Entry) error {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	if mw.closed {
		return ErrWriterClosed
	}

	// Non-blocking send prevents backpressure from slow writers
	// CRITICAL: Call Retain() before send, Release() on failure
	// This is more defensive and idiomatic
	for _, ch := range mw.writeChans {
		e.Retain() // Prevent pool reclamation during async processing
		select {
		case ch <- e:
			// Successfully sent, Retain() will be balanced by Release() in worker
		default:
			// Channel full - release and skip write to prevent blocking
			e.Release()
		}
	}

	return nil
}

func (mw *MultiWriter) writerWorker(_ string, w Writer, ch chan *Entry) {
	defer mw.wg.Done()
	// Worker owns the writer lifecycle. Closing here ensures no concurrent
	// Write() calls happen after the worker exits, regardless of why it stopped.
	defer func() { _ = w.Close() }()

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}

			// Errors silently dropped to prevent panics
			_ = w.Write(e)
			// CRITICAL: Balance the Retain() from Write()
			// Safe to call Release() here even if Write() failed
			e.Release()

		case <-mw.shutdownChan:
			// Drain all remaining entries. Close() guarantees ch will be closed
			// after shutdownChan, so range terminates once the channel is empty and closed.
			for e := range ch {
				_ = w.Write(e)
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

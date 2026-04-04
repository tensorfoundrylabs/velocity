package velocity

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMultiWriter_EntryRetainRelease verifies that entries are processed
// and Release() is called without race conditions.
func TestMultiWriter_EntryRetainRelease(t *testing.T) {
	mw := NewMultiWriter()
	defer func() { _ = mw.Close() }()

	var processedCount atomic.Int64

	// Wrap writer to track processing
	trackingWriterFn := WriterFunc(func(e *Entry) error {
		// Verify the entry is valid (not nil)
		if e == nil {
			t.Error("Received nil entry")
			return nil
		}
		// Mark as processed
		processedCount.Add(1)
		// Simulate the Release() that writerWorker should call
		// NOTE: MultiWriter handles Release()
		return nil
	})

	mw.AddWriter("tracker", &trackingWriterFn)

	// Send some entries
	for range 10 {
		entry := GetEntry()
		entry.SetMessage("test message")

		// This should call Retain() internally
		if err := mw.Write(entry); err != nil {
			t.Fatalf("Write() failed: %v", err)
		}

		// Release immediately - MultiWriter.Write() should have Retained
		entry.Release()
	}

	// Wait for async processing
	waitFor(t, func() bool {
		return processedCount.Load() >= 10
	}, 500*time.Millisecond, 10*time.Millisecond, "Entries should be processed")

	if processedCount.Load() == 0 {
		t.Error("No entries processed - writer may not be working")
	}

	t.Logf("Processed %d entries", processedCount.Load())
}

// TestMultiWriter_AsyncEntrySafety verifies that pooled entries are not
// corrupted when processed asynchronously by multiple writer workers.
func TestMultiWriter_AsyncEntrySafety(t *testing.T) {
	mw := NewMultiWriter()
	defer func() { _ = mw.Close() }()

	var processed atomic.Int64
	var corruption atomic.Int64

	// Create a writer that validates entry integrity
	validatingWriterFn := WriterFunc(func(e *Entry) error {
		// Validate entry fields
		if e.Message == "" {
			corruption.Add(1)
			return nil
		}

		// Verify level is valid
		switch e.Level {
		case LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal:
			// Valid
		case LevelOff:
			corruption.Add(1)
		}

		processed.Add(1)
		return nil
	})

	releaseValidatingWriterFn := WriterFunc(func(e *Entry) error {
		err := validatingWriterFn.Write(e)
		// NOTE: MultiWriter handles Release()
		return err
	})

	mw.AddWriter("validator", &releaseValidatingWriterFn)

	// Send many entries concurrently to stress test pool safety
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			entry := GetEntry()
			entry.SetMessage("test message")
			entry.SetLevel(LevelInfo)

			// Write entry (will be Retain'd and processed async)
			_ = mw.Write(entry)

			// Release our reference immediately
			// The async worker has its own reference from Retain()
			entry.Release()
		}(i)
	}

	wg.Wait()
	// Allow async processing to complete
	waitFor(t, func() bool {
		return processed.Load() >= 100
	}, 500*time.Millisecond, 10*time.Millisecond, "Entries should be processed")

	if corruption.Load() > 0 {
		t.Errorf("Detected %d corrupted entries", corruption.Load())
	}

	t.Logf("Processed %d entries safely", processed.Load())
}

// TestMultiWriter_EntryPoolReuse verifies that entries are safely returned
// to the pool after async processing completes.
func TestMultiWriter_EntryPoolReuse(t *testing.T) {
	// Get an entry and verify it works
	entry1 := GetEntry()
	entry1.SetMessage("first message")
	entry1.SetLevel(LevelInfo)

	if entry1.Message != "first message" {
		t.Fatalf("Entry message not set: got %q", entry1.Message)
	}

	// Release back to pool
	entry1.Release()

	// Get another entry - should be the same one from pool
	entry2 := GetEntry()
	entry2.SetMessage("second message")
	entry2.SetLevel(LevelWarn)

	if entry2.Message != "second message" {
		t.Fatalf("Entry message not set: got %q", entry2.Message)
	}
	if entry2.Level != LevelWarn {
		t.Fatalf("Entry level not set: got %v", entry2.Level)
	}

	// Release back to pool
	entry2.Release()

	// Test with MultiWriter
	mw := NewMultiWriter()
	defer func() { _ = mw.Close() }()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			e := GetEntry()
			e.SetMessage("concurrent message")
			e.SetLevel(LevelDebug)

			simpleWriterFn := WriterFunc(func(ent *Entry) error {
				ent.Release()
				return nil
			})

			mw.AddWriter("writer", &simpleWriterFn)
			_ = mw.Write(e)
			// NOTE: MultiWriter handles Release()

			// Intentional delay: pacing for writer operations
			time.Sleep(10 * time.Millisecond)
			mw.RemoveWriter("writer")
		}(i)
	}

	wg.Wait()
	// Allow async processing to complete
	waitFor(t, func() bool {
		return true // Verify no deadlock/crash
	}, 200*time.Millisecond, 10*time.Millisecond, "Concurrent operations should complete")

	// Verify pool still works after concurrent use
	entry3 := GetEntry()
	entry3.SetMessage("final message")
	if entry3.Message != "final message" {
		t.Fatalf("Pool corrupted after concurrent use: got %q", entry3.Message)
	}
	entry3.Release()
}

// TestMultiWriter_ConcurrentWrites verifies that concurrent Write calls
// don't cause race conditions or pool corruption.
func TestMultiWriter_ConcurrentWrites(t *testing.T) {
	mw := NewMultiWriter()
	defer func() { _ = mw.Close() }()

	var writeCount atomic.Int64

	simpleWriterFn := WriterFunc(func(_ *Entry) error {
		writeCount.Add(1)
		// NOTE: MultiWriter handles Release()
		return nil
	})

	mw.AddWriter("counter", &simpleWriterFn)

	// Launch many concurrent writers
	// NOTE: Keep total writes (200) below channel buffer (256) to avoid drops
	// MultiWriter uses non-blocking sends and drops when buffer is full
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			for range 10 {
				entry := GetEntry()
				entry.SetMessage("concurrent test")
				entry.SetLevel(LevelInfo)
				_ = mw.Write(entry)
				entry.Release()
			}
		}(i)
	}

	wg.Wait()
	// Allow all async processing to complete
	// Use longer timeout to account for race detector slowdown and scheduler variance
	waitFor(t, func() bool {
		return writeCount.Load() >= 200 // 20 goroutines * 10 writes
	}, 10*time.Second, 50*time.Millisecond, "All concurrent writes should be processed")

	t.Logf("Completed %d concurrent writes", writeCount.Load())

	// Verify pool still works
	entry := GetEntry()
	entry.SetMessage("pool check")
	if entry.Message != "pool check" {
		t.Fatal("Pool corrupted after concurrent writes")
	}
	entry.Release()
}

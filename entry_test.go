package velocity

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestEntry_ReferenceCount_PreventsEarlyPoolReturn(t *testing.T) {
	entry := GetEntry()
	entry.SetMessage("test")
	entry.written.Store(1)

	// Simulate async handler retaining entry
	entry.Retain()

	// Primary release
	entry.Release()

	// Entry should still be valid (not returned to pool)
	if entry.Message != "test" {
		t.Errorf("Expected 'test', got '%s'", entry.Message)
	}

	// Final release
	entry.Release()
}

func TestEntry_DoubleRelease_DoesNotPanic(t *testing.T) {
	entry := GetEntry()
	entry.SetMessage("test")
	entry.written.Store(1)

	entry.Release()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Double release caused panic: %v", r)
		}
	}()

	entry.Release()
}

func TestEntry_ConcurrentRetainRelease(_ *testing.T) {
	var wg sync.WaitGroup
	const iterations = 1000

	for i := range iterations {
		entry := GetEntry()
		entry.SetMessage(fmt.Sprintf("test-%d", i))
		entry.written.Store(1)

		// Simulate multiple handlers retaining
		for range 5 {
			entry.Retain()
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Intentional delay to simulate async work (not for synchronization)
				time.Sleep(time.Microsecond)
				entry.Release()
			}()
		}

		// Primary release
		entry.Release()
	}

	wg.Wait()
}

func TestEntry_GetEntry_SetsRefCountToOne(t *testing.T) {
	entry := GetEntry()

	if entry.refCount.Load() != 1 {
		t.Errorf("Expected refCount to be 1, got %d", entry.refCount.Load())
	}
}

func TestEntry_Reset_ClearsRefCount(t *testing.T) {
	entry := GetEntry()
	entry.Retain()

	entry.Reset()

	if entry.refCount.Load() != 0 {
		t.Errorf("Expected refCount to be 0 after Reset, got %d", entry.refCount.Load())
	}
}

func TestEntry_ReleaseWithoutWritten_DoesNotReturnToPool(t *testing.T) {
	entry := GetEntry()
	entry.SetMessage("test")
	// Don't set written = true

	entry.Release()

	// Entry should not have been returned to pool
	// refCount should still be 1
	if entry.refCount.Load() != 1 {
		t.Errorf("Expected refCount to still be 1, got %d", entry.refCount.Load())
	}
}

func TestEntry_MultipleRetains_RequiresMultipleReleases(t *testing.T) {
	entry := GetEntry()
	entry.SetMessage("test")
	entry.written.Store(1)

	entry.Retain()
	entry.Retain()

	entry.Release()
	if entry.refCount.Load() != 2 {
		t.Errorf("Expected refCount to be 2, got %d", entry.refCount.Load())
	}

	entry.Release()
	if entry.refCount.Load() != 1 {
		t.Errorf("Expected refCount to be 1, got %d", entry.refCount.Load())
	}

	entry.Release()
	if entry.refCount.Load() != -1 {
		t.Errorf("Expected refCount to be -1 (pooled marker), got %d", entry.refCount.Load())
	}
}

func TestEntry_RaceCondition_SimulateAsyncHandler(t *testing.T) {
	var wg sync.WaitGroup
	const iterations = 100

	for i := range iterations {
		entry := GetEntry()
		entry.SetMessage(fmt.Sprintf("iteration-%d", i))
		entry.written.Store(1)

		// Simulate an async handler that needs to access the entry
		entry.Retain()
		wg.Add(1)
		go func(e *Entry, iteration int) {
			defer wg.Done()
			defer e.Release()

			// Intentional delay to simulate processing time (not for synchronization)
			time.Sleep(time.Microsecond * 10)

			// Access entry fields
			msg := e.Message
			expectedMsg := fmt.Sprintf("iteration-%d", iteration)
			if msg != expectedMsg {
				t.Errorf("Message corrupted: expected '%s', got '%s'", expectedMsg, msg)
			}
		}(entry, i)

		// Main goroutine releases immediately
		entry.Release()
	}

	wg.Wait()
}

func TestEntry_PoolReuse_AfterRelease(t *testing.T) {
	// Get first entry
	entry1 := GetEntry()
	entry1.SetMessage("first")
	entry1.written.Store(1)
	entry1.Release()

	// Get second entry - might be the same entry from pool
	entry2 := GetEntry()

	// Should have been reset
	if entry2.Message != "" {
		t.Errorf("Expected empty message after pool reuse, got '%s'", entry2.Message)
	}

	if entry2.refCount.Load() != 1 {
		t.Errorf("Expected refCount to be 1 for new entry, got %d", entry2.refCount.Load())
	}

	entry2.written.Store(1)
	entry2.Release()
}

func BenchmarkEntry_GetRelease(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		entry := GetEntry()
		entry.SetMessage("benchmark")
		entry.written.Store(1)
		entry.Release()
	}
}

func BenchmarkEntry_GetReleaseWithRetain(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		entry := GetEntry()
		entry.SetMessage("benchmark")
		entry.Retain()
		entry.written.Store(1)
		entry.Release()
		entry.Release()
	}
}

func BenchmarkEntry_ConcurrentRetainRelease(b *testing.B) {
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			entry := GetEntry()
			entry.SetMessage("benchmark")
			entry.written.Store(1)

			var wg sync.WaitGroup
			for range 3 {
				entry.Retain()
				wg.Add(1)
				go func() {
					defer wg.Done()
					entry.Release()
				}()
			}

			entry.Release()
			wg.Wait()
		}
	})
}

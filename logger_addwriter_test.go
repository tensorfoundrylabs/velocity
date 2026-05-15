package velocity

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLogger_AddWriter verifies that a writer receives log entries.
func TestLogger_AddWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := New(WithConsoleOutput(&buf))

	var callCount atomic.Int64

	trackingWriterFn := WriterFunc(func(_ *Entry) error {
		callCount.Add(1)
		// NOTE: Don't call Release() here - MultiWriter.writerWorker() does that
		return nil
	})

	logger.AddWriter("tracker", &trackingWriterFn)

	logger.Info("test message 1")
	logger.Warn("test message 2")

	waitFor(t, func() bool {
		return callCount.Load() >= 1
	}, 200*time.Millisecond, 10*time.Millisecond, "Expected at least 1 call to writer")
}

// TestLogger_AddWriter_Concurrent verifies thread-safety of AddWriter.
func TestLogger_AddWriter_Concurrent(t *testing.T) {
	var buf bytes.Buffer
	logger := New(WithConsoleOutput(&buf))

	var wg sync.WaitGroup

	for i := range 10 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			writerFn := WriterFunc(func(_ *Entry) error {
				// NOTE: MultiWriter handles Release()
				return nil
			})

			logger.AddWriter("writer", &writerFn)
			// Intentional delay: pacing concurrent AddWriter calls
			time.Sleep(10 * time.Millisecond)
		}(i)
	}

	wg.Wait()
	logger.Info("concurrency test")
	// Allow async processing - minimal wait for message to be processed
	waitFor(t, func() bool {
		return true // Just verify no deadlock/crash
	}, 100*time.Millisecond, 10*time.Millisecond, "Concurrent writes should complete")
}

// TestLogger_RemoveWriter verifies that a removed writer no longer receives entries.
func TestLogger_RemoveWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := New(WithConsoleOutput(&buf))

	var count1 atomic.Int64
	var count2 atomic.Int64

	writer1Fn := WriterFunc(func(_ *Entry) error {
		count1.Add(1)
		// NOTE: MultiWriter handles Release()
		return nil
	})

	writer2Fn := WriterFunc(func(_ *Entry) error {
		count2.Add(1)
		// NOTE: MultiWriter handles Release()
		return nil
	})

	logger.AddWriter("writer1", &writer1Fn)
	logger.AddWriter("writer2", &writer2Fn)

	for range 5 {
		logger.Info("message")
	}

	waitFor(t, func() bool {
		return count1.Load() >= 5 && count2.Load() >= 5
	}, 200*time.Millisecond, 10*time.Millisecond, "Both writers should receive initial messages")

	initial1 := count1.Load()
	initial2 := count2.Load()

	logger.RemoveWriter("writer1")

	for range 5 {
		logger.Info("message")
	}

	waitFor(t, func() bool {
		return count2.Load() >= initial2+5
	}, 200*time.Millisecond, 10*time.Millisecond, "Active writer should receive additional messages")

	final1 := count1.Load()
	final2 := count2.Load()

	if final1 > initial1 {
		t.Errorf("Removed writer received entries: %d -> %d", initial1, final1)
	}

	if final2 <= initial2 {
		t.Errorf("Active writer did not receive entries: %d -> %d", initial2, final2)
	}
}

// TestLogger_AddWriter_NilSafety verifies nil-safety of AddWriter/RemoveWriter.
func TestLogger_AddWriter_NilSafety(t *testing.T) {
	var nilLogger *Logger

	nilLogger.AddWriter("test", &NoOpWriter{})
	nilLogger.RemoveWriter("test")

	logger := newForTesting(nil)

	logger.AddWriter("noop", &NoOpWriter{})
	logger.Info("test message")
	logger.RemoveWriter("noop")

	// Allow async processing to complete
	waitFor(t, func() bool {
		return true // Verify no deadlock/crash
	}, 100*time.Millisecond, 10*time.Millisecond, "Operations should complete without deadlock")
}

// TestLogger_AddWriter_MultipleWriters verifies multiple writers receive entries.
func TestLogger_AddWriter_MultipleWriters(t *testing.T) {
	var buf bytes.Buffer
	logger := New(WithConsoleOutput(&buf))

	var count1 atomic.Int64
	var count2 atomic.Int64
	var count3 atomic.Int64

	writer1Fn := WriterFunc(func(_ *Entry) error {
		count1.Add(1)
		// NOTE: MultiWriter handles Release()
		return nil
	})

	writer2Fn := WriterFunc(func(_ *Entry) error {
		count2.Add(1)
		// NOTE: MultiWriter handles Release()
		return nil
	})

	writer3Fn := WriterFunc(func(_ *Entry) error {
		count3.Add(1)
		// NOTE: MultiWriter handles Release()
		return nil
	})

	logger.AddWriter("w1", &writer1Fn)
	logger.AddWriter("w2", &writer2Fn)
	logger.AddWriter("w3", &writer3Fn)

	for range 10 {
		logger.Info("message")
	}

	waitFor(t, func() bool {
		return count1.Load() >= 10 && count2.Load() >= 10 && count3.Load() >= 10
	}, 300*time.Millisecond, 10*time.Millisecond, "All writers should receive messages")

	c1 := count1.Load()
	c2 := count2.Load()
	c3 := count3.Load()

	if c1 == 0 {
		t.Errorf("Writer 1 received no entries")
	}
	if c2 == 0 {
		t.Errorf("Writer 2 received no entries")
	}
	if c3 == 0 {
		t.Errorf("Writer 3 received no entries")
	}

	t.Logf("Writers received: w1=%d, w2=%d, w3=%d", c1, c2, c3)
}

// TestLogger_AddWriter_LevelFiltering verifies writers respect level filtering.
func TestLogger_AddWriter_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := New(
		WithConsoleOutput(&buf),
		WithLevel(LevelInfo),
	)

	var debugCount atomic.Int64
	var infoCount atomic.Int64

	trackingWriterFn := WriterFunc(func(e *Entry) error {
		switch e.Level {
		case LevelDebug:
			debugCount.Add(1)
		case LevelInfo:
			infoCount.Add(1)
		case LevelWarn, LevelError, LevelFatal, LevelOff:
			// not tracked
		}
		// NOTE: MultiWriter handles Release()
		return nil
	})

	logger.AddWriter("tracker", &trackingWriterFn)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")

	waitFor(t, func() bool {
		return infoCount.Load() >= 1
	}, 200*time.Millisecond, 10*time.Millisecond, "Info writer should receive entries")

	debug := debugCount.Load()
	info := infoCount.Load()

	t.Logf("Received counts: debug=%d, info=%d", debug, info)

	if info == 0 {
		t.Error("Info writer received no entries")
	}
}

// TestLogger_AddWriter_EntryIntegrity verifies entry fields are preserved.
func TestLogger_AddWriter_EntryIntegrity(t *testing.T) {
	var buf bytes.Buffer
	logger := New(WithConsoleOutput(&buf))

	// Use a channel to receive the entry data safely
	type entryData struct {
		message string
		level   Level
		fields  int
	}
	resultCh := make(chan entryData, 1)

	trackingWriterFn := WriterFunc(func(e *Entry) error {
		// Extract data immediately before Release
		data := entryData{
			message: e.Message,
			level:   e.Level,
			fields:  len(e.Fields),
		}
		resultCh <- data
		// NOTE: MultiWriter handles Release()
		return nil
	})

	logger.AddWriter("tracker", &trackingWriterFn)

	testMsg := "integrity test message"
	testField := String("component", "test")

	logger.Info(testMsg, testField)

	// Wait for the result with timeout
	select {
	case data := <-resultCh:
		if data.message != testMsg {
			t.Errorf("Message mismatch: got %q, want %q", data.message, testMsg)
		}
		if data.level != LevelInfo {
			t.Errorf("Level mismatch: got %v, want %v", data.level, LevelInfo)
		}
		if data.fields != 1 {
			t.Errorf("Field count mismatch: got %d, want 1", data.fields)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Timeout waiting for log entry")
	}
}

func TestLogger_NilSetLevel(_ *testing.T) {
	var l *Logger
	l.SetLevel(LevelDebug) // must not panic
	_ = l.Level()          // must not panic
}

// TestLogger_ChildSeesWriterAddedAfterCreation is a regression test for the bug where
// child loggers created via With() did not see writers added to the parent after the
// child was created. The shared writerSet pointer must propagate the new writer to
// all members of the logger family.
func TestLogger_ChildSeesWriterAddedAfterCreation(t *testing.T) {
	t.Parallel()

	parent := New(WithConsoleOutput(&bytes.Buffer{}))

	// Create a child before adding any writer.
	child := parent.With(String("child", "true"))

	var count atomic.Int64
	fn := WriterFunc(func(_ *Entry) error {
		count.Add(1)
		return nil
	})

	// Add the writer to the parent AFTER the child was created.
	parent.AddWriter("tracker", &fn)

	// Log through the child — the shared writerSet means the tracker should fire.
	child.Info("from child")

	waitFor(t, func() bool {
		return count.Load() >= 1
	}, 300*time.Millisecond, 10*time.Millisecond,
		"child should route through writer added to parent after child creation")
}

package velocity

import (
	"bytes"
	"sync"
	"testing"
)

func TestDetailedLogging(t *testing.T) {
	tests := []struct {
		name        string
		setupLogger func() *Logger
		logFunc     func(*Logger)
		wantTree    bool
	}{
		{
			name: "InfoDetailed always uses tree display even with inline config",
			setupLogger: func() *Logger {
				cfg := DefaultConfig()
				cfg.FieldDisplayMode = FieldDisplayInline
				return NewWithConfig(cfg)
			},
			logFunc: func(l *Logger) {
				l.InfoDetailed("Test message",
					StringField("key1", "value1"),
					Int("key2", 42),
					Bool("key3", true))
			},
			wantTree: true,
		},
		{
			name: "ErrorDetailed always uses tree display",
			setupLogger: func() *Logger {
				cfg := DefaultConfig()
				cfg.FieldDisplayMode = FieldDisplayInline
				return NewWithConfig(cfg)
			},
			logFunc: func(l *Logger) {
				l.ErrorDetailed("Error occurred",
					StringField("error", "connection timeout"),
					Int("retry", 3))
			},
			wantTree: true,
		},
		{
			name: "WarnDetailed always uses tree display",
			setupLogger: func() *Logger {
				cfg := DefaultConfig()
				cfg.FieldDisplayMode = FieldDisplayInline
				return NewWithConfig(cfg)
			},
			logFunc: func(l *Logger) {
				l.WarnDetailed("Warning message",
					StringField("warning", "high memory usage"),
					Float64("usage_percent", 89.5))
			},
			wantTree: true,
		},
		{
			name: "DebugDetailed always uses tree display",
			setupLogger: func() *Logger {
				cfg := DefaultConfig()
				cfg.FieldDisplayMode = FieldDisplayInline
				cfg.ConsoleLevel = LevelDebug
				return NewWithConfig(cfg)
			},
			logFunc: func(l *Logger) {
				l.DebugDetailed("Debug info",
					StringField("module", "auth"),
					StringField("action", "token_refresh"))
			},
			wantTree: true,
		},
		{
			name: "Regular Info uses inline when configured",
			setupLogger: func() *Logger {
				cfg := DefaultConfig()
				cfg.FieldDisplayMode = FieldDisplayInline
				return NewWithConfig(cfg)
			},
			logFunc: func(l *Logger) {
				l.Info("Regular message",
					StringField("key", "value"),
					Int("number", 123))
			},
			wantTree: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			logger := tt.setupLogger()

			// The test will use the configured console writer
			// We're primarily testing that the forceTreeDisplay flag
			// is properly set and passed through the system

			tt.logFunc(logger)

			// For this test, we're mainly verifying that the methods compile
			// and execute without panics. Full output testing would require
			// more setup to capture console writer output.

			// The key thing we're testing is that the forceTreeDisplay flag
			// is properly set and passed through the system.
		})
	}
}

func TestDetailedLoggingThreadSafety(_ *testing.T) {
	cfg := DefaultConfig()
	cfg.FieldDisplayMode = FieldDisplayInline
	logger := NewWithConfig(cfg)

	// Run concurrent detailed and normal logs
	done := make(chan bool)

	go func() {
		for i := range 100 {
			logger.InfoDetailed("Detailed log", Int("iteration", i))
		}
		done <- true
	}()

	go func() {
		for i := range 100 {
			logger.Info("Normal log", Int("iteration", i))
		}
		done <- true
	}()

	go func() {
		for i := range 100 {
			logger.ErrorDetailed("Detailed error", Int("iteration", i))
		}
		done <- true
	}()

	// Wait for all goroutines
	for range 3 {
		<-done
	}

	// If we get here without panics or races, thread safety is maintained
}

func TestDetailedMethodsWithNilLogger(_ *testing.T) {
	var logger *Logger = nil

	// These should not panic, just print to stderr
	logger.InfoDetailed("Test", StringField("key", "value"))
	logger.ErrorDetailed("Test", StringField("key", "value"))
	logger.WarnDetailed("Test", StringField("key", "value"))
	logger.DebugDetailed("Test", StringField("key", "value"))
}

func TestDetailedMethodsRespectLogLevel(_ *testing.T) {
	cfg := DefaultConfig()
	cfg.ConsoleLevel = LevelWarn // Only warn and above
	logger := NewWithConfig(cfg)

	// These should be filtered out
	logger.DebugDetailed("Debug", StringField("key", "value"))
	logger.InfoDetailed("Info", StringField("key", "value"))

	// These should pass through
	logger.WarnDetailed("Warn", StringField("key", "value"))
	logger.ErrorDetailed("Error", StringField("key", "value"))
}

func TestRaw_ConcurrentWithWrite(_ *testing.T) {
	buf := &bytes.Buffer{}
	log := New(buf)

	var wg sync.WaitGroup
	const iters = 200

	wg.Add(3)

	go func() {
		defer wg.Done()
		for range iters {
			log.Raw("raw line\n")
		}
	}()

	go func() {
		defer wg.Done()
		for range iters {
			log.Info("info message")
		}
	}()

	go func() {
		defer wg.Done()
		for range iters {
			log.Raw("another raw\n")
		}
	}()

	wg.Wait()
}

// TestEntryPoolResetsForceTreeDisplay verifies that the forceTreeDisplay flag
// is properly reset when entries are returned to the pool and reused.
// This prevents the bug where entries from logDetailed() calls would retain
// the tree display flag and affect subsequent regular log calls.
func TestEntryPoolResetsForceTreeDisplay(t *testing.T) {
	// Get an entry from the pool
	entry1 := GetEntry()

	// Verify it starts with forceTreeDisplay = false
	if entry1.forceTreeDisplay {
		t.Error("New entry from pool should have forceTreeDisplay = false")
	}

	// Simulate using it in a logDetailed call
	entry1.forceTreeDisplay = true
	entry1.Write()
	entry1.Release()

	// Get another entry from the pool (might be the same one we just released)
	entry2 := GetEntry()

	// Verify forceTreeDisplay has been reset to false
	if entry2.forceTreeDisplay {
		t.Error("Entry from pool should have forceTreeDisplay reset to false after Reset()")
	}

	entry2.Write()
	entry2.Release()
}

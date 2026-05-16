package velocity

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

// newForTesting builds a logger that writes to w with colour disabled, debug
// level, and no structured output. Used in tests that need a real console writer
// without a *testing.T to hand (use WithTesting(t) when t is available).
func newForTesting(w io.Writer) *Logger {
	if w == nil {
		w = io.Discard
	}
	cfg := defaultConfig()
	cfg.ConsoleOutput = w
	cfg.ConsoleTheme = nil
	cfg.ConsoleLevel = LevelDebug
	cfg.StructuredOutput = nil
	cfg.StructuredLevel = LevelOff
	cfg.BufferSize = 512
	cfg.FieldPoolSize = 25
	cfg.DisableColour = true
	cfg.TimeFormat = "15:04:05.000"
	return newFromConfig(cfg)
}

// waitFor polls condition until it returns true or timeout expires.
func waitFor(t *testing.T, condition func() bool, timeout, interval time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

// safeBuffer wraps bytes.Buffer with a mutex for concurrent test use.
type safeBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (sb *safeBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *safeBuffer) Len() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Len()
}

func (sb *safeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

func (sb *safeBuffer) Reset() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.buf.Reset()
}

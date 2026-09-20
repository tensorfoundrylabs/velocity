package velocity

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// asyncTestWriter is an io.Writer for the structured sink that can sleep per
// Write, gate on a release channel, and record every delivered line. It exists
// because JSONWriter consumes io.Writer (not velocity.Writer), so the
// MultiWriter fixtures in bench_sink_test.go do not apply here.
type asyncTestWriter struct {
	mu      sync.Mutex
	lines   []string
	sleep   time.Duration
	release chan struct{} // non-nil: every Write blocks until closed
}

func (w *asyncTestWriter) Write(p []byte) (int, error) {
	if w.release != nil {
		<-w.release
	}
	if w.sleep > 0 {
		time.Sleep(w.sleep)
	}
	w.mu.Lock()
	w.lines = append(w.lines, strings.TrimRight(string(p), "\n"))
	w.mu.Unlock()
	return len(p), nil
}

func (w *asyncTestWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.lines)
}

func (w *asyncTestWriter) snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.lines))
	copy(out, w.lines)
	return out
}

// newAsyncTestLogger builds a production-shaped logger whose only real output
// is the structured sink w, with the async option applied.
func newAsyncTestLogger(w io.Writer, acfg AsyncConfig) *Logger {
	return New(WithProduction(), WithStructuredOutput(w), WithAsyncOutput(acfg))
}

// A log call under WithAsyncOutput must return without waiting for the
// underlying write syscall: with a sink that sleeps 50ms per write, ten calls
// complete well inside the time a single synchronous write would take.
func TestAsyncOutput_ReturnsWithoutWaitingForSlowWriter(t *testing.T) {
	t.Parallel()

	w := &asyncTestWriter{sleep: 50 * time.Millisecond}
	logger := newAsyncTestLogger(w, AsyncConfig{Queue: 256, OnFull: AsyncBlock})
	defer func() { _ = logger.Close() }()

	start := time.Now()
	for range 10 {
		logger.Info("fast")
	}
	asyncElapsed := time.Since(start)

	if asyncElapsed >= 45*time.Millisecond {
		t.Fatalf("async caller waited for the sink: %v for 10 calls (sink sleeps 50ms/write)", asyncElapsed)
	}

	// Without the option the caller inherits the sink's latency.
	syncW := &asyncTestWriter{sleep: 50 * time.Millisecond}
	syncLogger := New(WithProduction(), WithStructuredOutput(syncW))
	defer func() { _ = syncLogger.Close() }()

	start = time.Now()
	syncLogger.Info("slow")
	syncElapsed := time.Since(start)
	if syncElapsed < 45*time.Millisecond {
		t.Fatalf("sync caller did not inherit sink latency: %v (expected >= 45ms)", syncElapsed)
	}
}

// Without the option the JSON writer must not grow async machinery at all, so
// default behaviour is byte-for-byte the synchronous path.
func TestAsyncOutput_AbsentByDefault(t *testing.T) {
	t.Parallel()

	// A real sink: newFromConfig treats io.Discard as "no output" and never
	// constructs the JSON writer at all.
	logger := New(WithProduction(), WithStructuredOutput(&asyncTestWriter{}))
	if logger.jsonWriter.async != nil {
		t.Fatal("async state constructed without WithAsyncOutput")
	}
	if got := logger.StructuredDroppedCount(); got != 0 {
		t.Fatalf("StructuredDroppedCount = %d on a sync logger, want 0", got)
	}
}

// Concurrent callers must observe per-goroutine submission order, with every
// accepted line delivered whole: no lost, no torn records.
func TestAsyncOutput_PerGoroutineOrderingPreserved(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	const perGoroutine = 50

	w := &asyncTestWriter{}
	// A deliberately shallow queue forces heavy interleaving and frequent
	// blocking handoffs through the single drainer.
	logger := newAsyncTestLogger(w, AsyncConfig{Queue: 16, OnFull: AsyncBlock})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			for i := range perGoroutine {
				logger.Info(fmt.Sprintf("g%02d-%03d", g, i))
			}
		}(g)
	}
	close(start)
	wg.Wait()

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := w.snapshot()
	if len(lines) != goroutines*perGoroutine {
		t.Fatalf("delivered %d lines, want %d", len(lines), goroutines*perGoroutine)
	}

	last := make([]int, goroutines)
	for n, line := range lines {
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			t.Fatalf("line %d torn or not a JSON object: %q", n, line)
		}
		_, rest, found := strings.Cut(line, `"message":"g`)
		if !found {
			t.Fatalf("line %d has no message field: %q", n, line)
		}
		var g, seq int
		if _, err := fmt.Sscanf(rest, "%02d-%03d", &g, &seq); err != nil {
			t.Fatalf("line %d message unparseable: %q", n, line)
		}
		if seq != last[g] {
			t.Fatalf("goroutine %d out of order: got seq %d after %d", g, seq, last[g])
		}
		last[g] = seq + 1
	}
}

// Drop policy: when the queue is full the caller returns immediately, the drop
// is counted, and after Close drains, written + dropped == logged.
func TestAsyncOutput_DropPolicyCountsAndNeverBlocks(t *testing.T) {
	t.Parallel()

	w := &asyncTestWriter{release: make(chan struct{})}
	logger := newAsyncTestLogger(w, AsyncConfig{Queue: 4, OnFull: AsyncDrop})

	const total = 20
	start := time.Now()
	for i := range total {
		logger.Info(fmt.Sprintf("drop-%02d", i))
	}
	elapsed := time.Since(start)

	// The sink is gated shut; if the caller blocked on it this bound fails
	// (or the test deadlocks, which is also a failure).
	if elapsed >= 2*time.Second {
		t.Fatalf("drop policy blocked the caller for %v while the sink was gated", elapsed)
	}

	close(w.release)
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	written := w.count()
	dropped := logger.StructuredDroppedCount()
	//nolint:gosec // test-only accounting; dropped is bounded by the 20 logged entries
	if int64(written)+int64(dropped) != total {
		t.Fatalf("written %d + dropped %d != logged %d", written, dropped, total)
	}
	if dropped == 0 {
		t.Fatal("expected drops with a gated sink and queue depth 4 for 20 entries")
	}
}

// Block policy: callers back-pressure once the queue is full and nothing is
// lost.
func TestAsyncOutput_BlockPolicyBackpressuresAndLosesNothing(t *testing.T) {
	t.Parallel()

	w := &asyncTestWriter{release: make(chan struct{})}
	logger := newAsyncTestLogger(w, AsyncConfig{Queue: 4, OnFull: AsyncBlock})

	const total = 20
	timer := time.AfterFunc(150*time.Millisecond, func() { close(w.release) })
	defer timer.Stop()

	start := time.Now()
	for i := range total {
		logger.Info(fmt.Sprintf("bp-%02d", i))
	}
	elapsed := time.Since(start)

	// Queue depth 4 (+1 in the drainer) means call ~6 onwards must wait for
	// the gate. Anything well under the 150ms hold means no back-pressure.
	if elapsed < 100*time.Millisecond {
		t.Fatalf("block policy did not back-pressure: %v for 20 calls against a gated sink", elapsed)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := w.count(); got != total {
		t.Fatalf("delivered %d lines, want all %d", got, total)
	}
	if got := logger.StructuredDroppedCount(); got != 0 {
		t.Fatalf("dropped %d entries under block policy, want 0", got)
	}
}

// Close drains everything enqueued before it; post-Close writes are rejected
// per the family-close model.
func TestAsyncOutput_CloseDrainsEverythingEnqueued(t *testing.T) {
	t.Parallel()

	w := &asyncTestWriter{}
	logger := newAsyncTestLogger(w, AsyncConfig{Queue: 64, OnFull: AsyncBlock})

	const total = 100
	for i := range total {
		logger.Info(fmt.Sprintf("drain-%03d", i))
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := w.count(); got != total {
		t.Fatalf("Close drained %d lines, want %d", got, total)
	}

	// After the family is closed, admission is gone for good.
	logger.Info("post-close")
	if got := w.count(); got != total {
		t.Fatalf("write accepted after Close: %d lines, want %d", got, total)
	}
}

// Fatal in async mode must be reliably written, ordered behind every earlier
// accepted entry, and complete before the FatalHandler runs.
func TestAsyncOutput_FatalWrittenInOrderBeforeHandler(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var events []string
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, s)
	}
	// Route sink lines through the shared event log so handler timing is
	// comparable in one sequence.
	sink := eventWriter{fn: record}
	logger := New(
		WithProduction(),
		WithStructuredOutput(&sink),
		WithAsyncOutput(AsyncConfig{Queue: 32, OnFull: AsyncBlock}),
		WithFatalHandler(func() { record("HANDLER") }),
	)

	const total = 50
	for i := range total {
		logger.Info(fmt.Sprintf("pre-%03d", i))
	}
	logger.Fatal("boom")

	mu.Lock()
	defer mu.Unlock()
	if len(events) != total+2 {
		t.Fatalf("recorded %d events, want %d entries + fatal + handler", len(events), total+2)
	}
	for i := range total {
		want := fmt.Sprintf("pre-%03d", i)
		if !strings.Contains(events[i], want) {
			t.Fatalf("event %d = %q, want entry %q", i, events[i], want)
		}
	}
	if !strings.Contains(events[total], "boom") {
		t.Fatalf("event %d = %q, want the fatal entry", total, events[total])
	}
	if events[total+1] != "HANDLER" {
		t.Fatalf("event %d = %q, want the fatal handler last", total+1, events[total+1])
	}
}

// eventWriter adapts a callback into an io.Writer.
type eventWriter struct {
	fn func(string)
}

func (e *eventWriter) Write(p []byte) (int, error) {
	e.fn(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

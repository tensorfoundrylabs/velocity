package velocity

// Permanent WP2 regression coverage: family-wide close, concurrent Close
// barriers, named-writer close errors, post-close admission, reliable fatal
// delivery and fatal sampling parity. Promoted from the adversarial batteries
// in .verify/scratch/wp3 (subscription lifetime) and .verify/scratch/wp4
// (close, fatal), restructured where the new barrier semantics changed the
// deterministic handshakes (gates must now be opened by independent goroutines,
// never by the FatalHandler, which runs after delivery).

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- fixtures ----------------------------------------------------------------

// wp2GateEntryWriter blocks every Write on gate until it is closed, then
// records the entry. entered signals each Write arrival so tests can prove the
// worker is parked mid-write without sleeping.
type wp2GateEntryWriter struct {
	gate    chan struct{}
	entered chan struct{}
	// sink, when non-nil, receives "message\n" for every processed entry —
	// used by the subprocess fatal test to prove file delivery before exit.
	sink    io.Writer
	mu      sync.Mutex
	msgs    []string
	entries []*Entry
	writes  atomic.Int64
	closers atomic.Int64
}

func newWP2GateEntryWriter() *wp2GateEntryWriter {
	return &wp2GateEntryWriter{
		gate:    make(chan struct{}),
		entered: make(chan struct{}, 1024),
	}
}

func (w *wp2GateEntryWriter) Write(e *Entry) error {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.gate
	w.mu.Lock()
	w.msgs = append(w.msgs, e.Message)
	w.entries = append(w.entries, e)
	w.mu.Unlock()
	if w.sink != nil {
		_, _ = fmt.Fprintf(w.sink, "%s\n", e.Message)
	}
	w.writes.Add(1)
	return nil
}

func (w *wp2GateEntryWriter) Close() error {
	w.closers.Add(1)
	return nil
}

func (w *wp2GateEntryWriter) Snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.msgs))
	copy(out, w.msgs)
	return out
}

func (w *wp2GateEntryWriter) WriteCount() int64 { return w.writes.Load() }

// wp2CaptureWriter records every entry message and whether it was closed.
type wp2CaptureWriter struct {
	mu     sync.Mutex
	msgs   []string
	closed bool
}

func (w *wp2CaptureWriter) Write(e *Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, e.Message)
	return nil
}

func (w *wp2CaptureWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *wp2CaptureWriter) Messages() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.msgs))
	copy(out, w.msgs)
	return out
}

func (w *wp2CaptureWriter) Closed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

var errWP2Flush = errors.New("wp2 flush failure")

// wp2FlushFailWriter accepts writes but fails Flush.
type wp2FlushFailWriter struct{ failFlush bool }

func (w *wp2FlushFailWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *wp2FlushFailWriter) Flush() error {
	if w.failFlush {
		return errWP2Flush
	}
	return nil
}

// wp2CloseErrWriter fails Close so Logger.Close must surface named-writer
// close errors.
type wp2CloseErrWriter struct{ err error }

func (w *wp2CloseErrWriter) Write(*Entry) error { return nil }
func (w *wp2CloseErrWriter) Close() error       { return w.err }

// wp2GoroutineCount returns the number of live goroutines whose stack contains
// marker — a deterministic lifecycle signal, not a process-wide count.
//
//nolint:unparam // the single marker is the point: it identifies subscription cleanup goroutines
func wp2GoroutineCount(marker string) int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	count := 0
	for stack := range bytes.SplitSeq(buf[:n], []byte("\n\n")) {
		if bytes.Contains(stack, []byte(marker)) {
			count++
		}
	}
	return count
}

func wp2Poll(cond func() bool, deadline time.Duration) bool {
	deadlineAt := time.Now().Add(deadline)
	for time.Now().Before(deadlineAt) {
		if cond() {
			return true
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// ---- family close ------------------------------------------------------------

// Close state is family-wide: closing any member closes the family for every
// sibling, and AddWriter on any member (parent or child created before the
// close) cannot revive output or spawn an orphan worker.
func TestClose_FamilySharedAndChildCannotRevive(t *testing.T) {
	l := newForTesting(io.Discard)
	child := l.With(String("svc", "child"))

	first := &wp2CaptureWriter{}
	l.AddWriter("first", first)
	l.Info("wp2-before-close")

	if err := l.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}
	waitFor(t, first.Closed, 5*time.Second, time.Millisecond, "first writer closed by drain")

	// Child admission: family is closed for the child too.
	child.Info("wp2-child-after-close")
	second := &wp2CaptureWriter{}
	child.AddWriter("second", second) // must be rejected on any member
	child.Info("wp2-child-revive-attempt")
	l.Info("wp2-parent-revive-attempt")

	if got := second.Messages(); len(got) != 0 {
		t.Errorf("AddWriter after family Close revived output via child: %v", got)
	}
	if got := first.Messages(); len(got) != 1 {
		t.Errorf("closed first writer received extra entries: %v", got)
	}

	// No orphan worker: the rejected AddWriter must not have built a new
	// MultiWriter. (Internal reach-in — the observable proxy is above.)
	l.writers.mu.RLock()
	mw := l.writers.mw
	l.writers.mu.RUnlock()
	if mw != nil {
		t.Error("AddWriter after Close created a new MultiWriter — orphaned worker goroutine")
	}

	// A later Close from the child returns the same recorded result without
	// reviving or erroring.
	if err := child.Close(); err != nil {
		t.Errorf("second family Close returned error: %v", err)
	}
}

// A second concurrent Close must wait behind the first Close's gated drain and
// return the same recorded result (including named-writer close errors), not
// early-return nil on a flag.
func TestClose_SecondConcurrentCloseWaitsForSameCompletion(t *testing.T) {
	gw := newWP2GateEntryWriter()
	cw := &wp2CloseErrWriter{err: errors.New("wp2 named-writer close failure")}
	l := newForTesting(io.Discard)
	l.AddWriter("gated", gw)
	l.AddWriter("errsink", cw)
	mwRef := l.writers.mw

	const n = 5
	for i := range n {
		l.Info(fmt.Sprintf("wp2-close-drain-%d", i))
	}
	// Worker parks on the gated writer holding entry 0; the rest queue behind.
	waitFor(t, func() bool { return mwRef.Stats()["gated"] == n-1 }, 5*time.Second, time.Millisecond, "worker parked on gated writer")

	errA := make(chan error, 1)
	go func() { errA <- l.Close() }()

	// First Close reaches MultiWriter shutdown and blocks in the drain.
	waitFor(t, func() bool {
		mwRef.mu.Lock()
		defer mwRef.mu.Unlock()
		return mwRef.closed
	}, 5*time.Second, time.Millisecond, "first Close to reach MultiWriter shutdown")

	errB := make(chan error, 1)
	go func() { errB <- l.Close() }()

	// Drain provably cannot progress while the gate is held. If the second
	// Close returns inside this window it did not wait for the completion.
	secondReturnedEarly := false
	select {
	case <-errB:
		secondReturnedEarly = true
	case <-time.After(250 * time.Millisecond):
	}

	close(gw.gate)

	var firstErr error
	select {
	case firstErr = <-errA:
	case <-time.After(30 * time.Second):
		t.Fatal("first Close never returned after gate release")
	}
	select {
	case got := <-errB:
		if secondReturnedEarly {
			t.Fatal("second concurrent Close returned while the gate was still held and the drain had made no progress")
		}
		if !errors.Is(got, cw.err) {
			t.Errorf("second Close returned %v, want the recorded named-writer close error %v", got, cw.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("second Close never returned after gate release")
	}
	if !errors.Is(firstErr, cw.err) {
		t.Errorf("first Close returned %v, want the named-writer close error %v", firstErr, cw.err)
	}

	// Both waited for the same completion: full drain happened.
	if msgs := gw.Snapshot(); len(msgs) != n {
		t.Errorf("expected %d delivered entries after close, got %d: %v", n, len(msgs), msgs)
	}
}

// Named-writer Close errors surface from Logger.Close (they were previously
// discarded by the worker's deferred close).
func TestClose_NamedWriterCloseErrorPropagated(t *testing.T) {
	sentinel := errors.New("wp2 sink close failed")
	l := newForTesting(io.Discard)
	l.AddWriter("sink", &wp2CloseErrWriter{err: sentinel})

	if err := l.Close(); !errors.Is(err, sentinel) {
		t.Errorf("Logger.Close returned %v, want named-writer close error %v", err, sentinel)
	}
	// Recorded result: a repeat Close returns the same error, not a fresh drain.
	if err := l.Close(); !errors.Is(err, sentinel) {
		t.Errorf("second Logger.Close returned %v, want the same recorded error", err)
	}
}

// After Close returns, no further writes reach the underlying writer on any
// dispatch path — direct calls, LogEntry and render helpers.
func TestClose_NoWritesAfterCloseReturns(t *testing.T) {
	gw := newWP2GateEntryWriter()
	l := newForTesting(io.Discard)
	l.AddWriter("gated", gw)
	mwRef := l.writers.mw

	for range 3 {
		l.Info("wp2-postclose-fill")
	}
	waitFor(t, func() bool { return mwRef.Stats()["gated"] == 2 }, 5*time.Second, time.Millisecond, "worker parked on gated writer")
	close(gw.gate)
	waitFor(t, func() bool { return gw.WriteCount() == 3 }, 5*time.Second, time.Millisecond, "entries delivered")

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitFor(t, func() bool { return gw.closers.Load() == 1 }, 5*time.Second, time.Millisecond, "worker closed its writer")

	before := gw.WriteCount()
	l.Info("wp2-postclose-direct")
	e := GetEntry()
	e.SetLevel(LevelInfo)
	e.SetMessage("wp2-postclose-logentry")
	l.LogEntry(e)
	e.Release()
	l.Box("title", "wp2-postclose-box")

	if after := gw.WriteCount(); after != before {
		t.Errorf("%d write(s) reached the underlying writer after Close returned", after-before)
	}
}

// Render, RenderRaw, Newline and BannerLines write straight to
// consoleWriter.out under the console mutex, bypassing ConsoleWriter's closed
// check — so the family-close admission gate in the Logger methods is the only
// thing keeping post-close bytes off the console sink. The sink here is the
// console output itself, not a named writer (the named-writer drain is
// covered by TestClose_NoWritesAfterCloseReturns).
func TestClose_NoConsoleWritesAfterCloseFromRenderHelpers(t *testing.T) {
	var sink bytes.Buffer
	l := New(WithConsoleOutput(&sink), WithLevel(LevelDebug))
	l.Info("wp2-pre-close-line")
	l.Render(NewBox("t", "wp2-pre-close-box", l.Style()))
	if sink.Len() == 0 {
		t.Fatal("precondition broken: nothing reached the console sink before close")
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	before := sink.Len()
	l.Render(NewBox("t", "wp2-post-close-render", l.Style()))
	l.RenderRaw(NewBox("t", "wp2-post-close-renderraw", l.Style()))
	l.Newline()
	l.BannerLines("wp2-post-close-banner")
	l.Render(NewBanner("wp2-post-close-banner-render", l.Style()))

	if after := sink.Len(); after != before {
		grew := sink.String()[before:]
		t.Errorf("%d bytes reached the console sink after Close via a render helper: %q", after-before, grew)
	}
}

// Bufio-backed console and JSON sinks are flushed by Close; flush failures
// propagate. The logger never closes the caller-owned underlying writer.
func TestClose_BufioFlushedAndErrorsPropagated(t *testing.T) {
	t.Run("console flushed", func(t *testing.T) {
		var raw bytes.Buffer
		bw := bufio.NewWriter(&raw)
		l := newForTesting(bw)
		l.Info("wp2-bufio-console-line")
		if raw.Len() != 0 {
			t.Fatalf("precondition broken: line left bufio early (%d bytes)", raw.Len())
		}
		if err := l.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if !strings.Contains(raw.String(), "wp2-bufio-console-line") {
			t.Errorf("console Close did not flush bufio.Writer")
		}
		if err := l.Close(); err != nil {
			t.Errorf("second Close returned error: %v", err)
		}
	})

	t.Run("json flushed", func(t *testing.T) {
		var raw bytes.Buffer
		bw := bufio.NewWriter(&raw)
		l := New(WithConsoleOutput(io.Discard), WithStructuredOutput(bw), WithLevel(LevelDebug))
		l.Info("wp2-bufio-json-line")
		if raw.Len() != 0 {
			t.Fatalf("precondition broken: line left bufio early (%d bytes)", raw.Len())
		}
		if err := l.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if !strings.Contains(raw.String(), "wp2-bufio-json-line") {
			t.Errorf("JSON Close did not flush bufio.Writer")
		}
	})

	t.Run("console flush error", func(t *testing.T) {
		ff := &wp2FlushFailWriter{failFlush: true}
		l := newForTesting(ff)
		l.Info("wp2-flush-err-console")
		if err := l.Close(); !errors.Is(err, errWP2Flush) {
			t.Errorf("console Close returned %v, want flush error %v", err, errWP2Flush)
		}
	})

	t.Run("json flush error", func(t *testing.T) {
		ff := &wp2FlushFailWriter{failFlush: true}
		l := New(WithConsoleOutput(io.Discard), WithStructuredOutput(ff), WithLevel(LevelDebug))
		l.Info("wp2-flush-err-json")
		if err := l.Close(); !errors.Is(err, errWP2Flush) {
			t.Errorf("JSON Close returned %v, want flush error %v", err, errWP2Flush)
		}
	})
}

// Writers versus closers under -race: no panic, no race, and the surviving
// state is fully drained by the time the final Close returns.
func TestClose_ConcurrentCloseVsInFlightWrites(t *testing.T) {
	capw := &wp2CaptureWriter{}
	l := newForTesting(io.Discard)
	l.AddWriter("cap", capw)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for g := range 4 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			for i := range 300 {
				l.Info(fmt.Sprintf("wp2-race-%d-%d", g, i))
			}
		}(g)
	}
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// Let real traffic flow first so the close genuinely races
			// in-flight writes rather than landing before the first entry.
			deadline := time.Now().Add(5 * time.Second)
			for len(capw.Messages()) < 50 && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			_ = l.Close()
		}()
	}

	close(start)
	wg.Wait()

	if err := l.Close(); err != nil {
		t.Errorf("final Close returned error: %v", err)
	}
	waitFor(t, capw.Closed, 5*time.Second, time.Millisecond, "capture writer closed after final Close")
	// Everything admitted before shutdown drained: nothing was lost after the
	// final Close returned.
	final := len(capw.Messages())
	time.Sleep(20 * time.Millisecond) //nolint:staticcheck // detection window only; assertion is on final-count stability
	if got := len(capw.Messages()); got != final {
		t.Errorf("writes reached the writer after completed close (%d -> %d)", final, got)
	}
}

// ---- subscription lifetime (R02) ----------------------------------------------

const wp2SubscribeMarker = "RingBufferWriter).Subscribe"

// Context cancellation terminates the cleanup goroutine and closes the channel.
func TestSubscribeLifetime_ContextCancelTerminatesCleanup(t *testing.T) {
	base := wp2GoroutineCount(wp2SubscribeMarker)

	ring := NewRingBufferWriter(2)
	ctx, cancel := context.WithCancel(context.Background())
	ch := ring.Subscribe(ctx, 4)
	if got := wp2GoroutineCount(wp2SubscribeMarker); got != base+1 {
		t.Fatalf("expected exactly one cleanup goroutine after Subscribe (%d -> %d)", base, got)
	}
	cancel()

	if !wp2Poll(func() bool { return wp2GoroutineCount(wp2SubscribeMarker) == base }, 5*time.Second) {
		t.Fatal("cleanup goroutine still running after context cancellation")
	}
	if _, ok := <-ch; ok {
		t.Error("subscription channel should be closed after cancellation")
	}
	_ = ring.Close()
}

// Ring Close terminates the subscription cleanup goroutine even when the
// subscriber used context.Background, whose Done channel never fires.
func TestSubscribeLifetime_CloseWithBackgroundContextTerminatesCleanup(t *testing.T) {
	base := wp2GoroutineCount(wp2SubscribeMarker)

	ring := NewRingBufferWriter(2)
	ch := ring.Subscribe(context.Background(), 4)
	if got := wp2GoroutineCount(wp2SubscribeMarker); got != base+1 {
		t.Fatalf("expected exactly one cleanup goroutine after Subscribe (%d -> %d)", base, got)
	}

	if err := ring.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := <-ch; ok {
		t.Error("subscription channel should be closed after ring Close")
	}
	if !wp2Poll(func() bool { return wp2GoroutineCount(wp2SubscribeMarker) == base }, 5*time.Second) {
		t.Fatal("subscription cleanup goroutine leaked after Close with context.Background")
	}
}

// Second Close is idempotent; Subscribe after Close returns an already-closed
// channel without starting a cleanup goroutine.
func TestSubscribeLifetime_DoubleCloseAndSubscribeAfterClose(t *testing.T) {
	ring := NewRingBufferWriter(2)
	if err := ring.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := ring.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	before := wp2GoroutineCount(wp2SubscribeMarker)
	ch := ring.Subscribe(context.Background(), 4)
	if _, ok := <-ch; ok {
		t.Error("Subscribe after Close should return an already-closed channel")
	}
	if got := wp2GoroutineCount(wp2SubscribeMarker); got != before {
		t.Errorf("Subscribe after Close started a cleanup goroutine (%d -> %d)", before, got)
	}
}

// ---- fatal delivery (R04) ------------------------------------------------------

// Exhausted sampler still delivers a direct Fatal: fatal is never sampled.
func TestFatal_SamplerExhaustedStillDelivered(t *testing.T) {
	buf := &bytes.Buffer{}
	l := New(
		WithConsoleOutput(buf),
		WithColour(false),
		WithLevel(LevelDebug),
		WithSampler(NewCountSampler(1, 0)),
		WithFatalHandler(func() {}),
	)

	// Burn the sampler budget at fatal level through the adapter path first.
	for range 5 {
		e := GetEntry()
		e.SetLevel(LevelFatal)
		e.SetMessage("wp2-adapter-fatal-warmup")
		l.LogEntry(e)
		e.Release()
	}

	l.Fatal("wp2-fatal-never-sampled")

	if !strings.Contains(buf.String(), "wp2-fatal-never-sampled") {
		t.Error("exhausted sampler suppressed a direct Fatal (fatal is never sampled)")
	}
}

// Adapter-routed fatal entries bypass the sampler too (parity with the direct
// path) — and never exit the process: only Logger.Fatal has exit semantics.
func TestFatal_AdapterFatalNotSampledAndDoesNotExit(t *testing.T) {
	buf := &bytes.Buffer{}
	l := New(
		WithConsoleOutput(buf),
		WithColour(false),
		WithLevel(LevelDebug),
		WithSampler(NewCountSampler(1, 0)),
	)

	delivered := 0
	for i := range 5 {
		e := GetEntry()
		e.SetLevel(LevelFatal)
		e.SetMessage(fmt.Sprintf("wp2-adapter-fatal-%d", i))
		l.LogEntry(e)
		e.Release()
		if strings.Contains(buf.String(), fmt.Sprintf("wp2-adapter-fatal-%d", i)) {
			delivered++
		}
	}

	if delivered != 5 {
		t.Errorf("adapter-routed fatal entries were sampled — %d of 5 delivered; fatal must bypass the sampler on every dispatch path", delivered)
	}
	// Reaching here at all proves LogEntry fatal did not exit the process.
}

// The FatalHandler runs only after the fatal entry (and everything accepted
// before it) has been delivered: the barrier, not the handler, orders output.
// The gate is opened by an independent goroutine — a handler that opened the
// gate itself would now deadlock, which is exactly the ordering guarantee.
func TestFatal_HandlerObservesDeliveryComplete(t *testing.T) {
	gw := newWP2GateEntryWriter()

	var atHandler []string
	done := make(chan struct{})
	l := New(
		WithConsoleOutput(io.Discard),
		WithLevel(LevelDebug),
		WithFatalHandler(func() {
			atHandler = gw.Snapshot()
			close(done)
		}),
	)
	l.AddWriter("gated", gw)

	l.Info("wp2-pre-fatal-entry")
	<-gw.entered // worker parked mid-write on the gate

	fatalReturned := make(chan struct{})
	go func() {
		defer close(fatalReturned)
		l.Fatal("wp2-fatal-barrier-msg")
	}()

	// Wait until the fatal entry is queued behind the parked worker, then let
	// the drain proceed. Delivery cannot complete before this. Depth 2: the
	// fatal entry plus the barrier sentinel WriteReliable sends behind it.
	waitFor(t, func() bool { return l.writers.mw.Stats()["gated"] == 2 }, 5*time.Second, time.Millisecond, "fatal entry enqueued")
	close(gw.gate)

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("FatalHandler never ran — fatal delivery barrier stuck")
	}
	select {
	case <-fatalReturned:
	case <-time.After(30 * time.Second):
		t.Fatal("Fatal never returned after the handler")
	}

	joined := strings.Join(atHandler, "\n")
	if !strings.Contains(joined, "wp2-pre-fatal-entry") {
		t.Errorf("handler observed delivery before the preceding entry: %v", atHandler)
	}
	if !strings.Contains(joined, "wp2-fatal-barrier-msg") {
		t.Errorf("FatalHandler ran before the fatal entry was delivered: %v", atHandler)
	}

	// A returning custom handler leaves the logger reusable.
	l.Info("wp2-after-fatal-reusable")
	waitFor(t, func() bool {
		return slices.Contains(gw.Snapshot(), "wp2-after-fatal-reusable")
	}, 5*time.Second, time.Millisecond, "post-fatal entry delivered")
	if err := l.Close(); err != nil {
		t.Errorf("Close after fatal: %v", err)
	}
}

// Entry references are released before the reliable barrier completes: the
// fatal entry is back in the pool (refCount reset path taken, message cleared)
// by the time Fatal's handler decision runs.
func TestFatal_ReliablePathReleasesEntryReferences(t *testing.T) {
	gw := newWP2GateEntryWriter()
	l := New(WithConsoleOutput(io.Discard), WithLevel(LevelDebug), WithFatalHandler(func() {}))
	l.AddWriter("gated", gw)

	l.Info("wp2-pre")
	<-gw.entered

	fatalReturned := make(chan struct{})
	go func() {
		defer close(fatalReturned)
		l.Fatal("wp2-fatal-refcount")
	}()
	waitFor(t, func() bool { return l.writers.mw.Stats()["gated"] == 2 }, 5*time.Second, time.Millisecond, "fatal entry enqueued")
	close(gw.gate)
	<-fatalReturned

	gw.mu.Lock()
	entries := append([]*Entry(nil), gw.entries...)
	gw.mu.Unlock()
	if len(entries) != 2 {
		t.Fatalf("expected 2 captured entries, got %d", len(entries))
	}
	for i, e := range entries {
		if rc := e.refCount.Load(); rc > 0 {
			t.Errorf("entry %d still holds references (refCount=%d) after Fatal returned", i, rc)
		}
		if e.Message != "" {
			t.Errorf("entry %d message not cleared on final release: %q", i, e.Message)
		}
	}
	_ = l.Close()
}

// Subprocess proof: with the async queue provably full behind a gated file
// writer, the fatal line reaches the file before os.Exit.
const (
	wp2FatalSentinelEnv = "VELOCITY_WP2_FATAL_SUBPROC"
	wp2FatalFileEnv     = "VELOCITY_WP2_FATAL_FILE"
	wp2FatalMsg         = "WP2_FATAL_SENTINEL_MESSAGE"
)

func TestFatal_FullQueueDeliveredBeforeSubprocessExit(t *testing.T) {
	if os.Getenv(wp2FatalSentinelEnv) == "1" {
		wp2FatalSubprocessMain(t)
		return
	}

	tmp := t.TempDir()
	path := tmp + "/fatal-output.log"
	if err := os.WriteFile(path, nil, 0o600); err != nil { //nolint:gosec // G304: test-controlled path
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestFatal_FullQueueDeliveredBeforeSubprocessExit$", "-test.v") //nolint:gosec // G204: test binary path
	cmd.Env = append(os.Environ(), wp2FatalSentinelEnv+"=1", wp2FatalFileEnv+"="+path)
	var childOut bytes.Buffer
	cmd.Stdout = &childOut
	cmd.Stderr = &childOut
	if err := cmd.Run(); err != nil {
		t.Logf("subprocess exited with: %v (os.Exit(1) from the default fatal handler is expected)", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // G304: test-controlled path
	if err != nil {
		t.Fatalf("reading subprocess log: %v", err)
	}
	content := string(data)
	fillerLines := strings.Count(content, "wp2-filler-")

	if fillerLines == 0 {
		t.Fatalf("subprocess wrote no filler lines; harness broken. Child output:\n%s", childOut.String())
	}
	if !strings.Contains(content, wp2FatalMsg) {
		t.Errorf("fatal entry was NOT delivered before subprocess exit — %d filler entries arrived but the fatal entry was dropped (queue-full branch)", fillerLines)
	}
	// Ordering: the fatal line is the last delivered entry.
	if !strings.HasSuffix(strings.TrimRight(content, "\n"), wp2FatalMsg) {
		t.Errorf("fatal entry is not the final line in the file:\n%s", tailWP2(content, 3))
	}
}

func tailWP2(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// wp2FatalSubprocessMain fills the queue behind a gated file writer (drops
// observed = queue provably full), lets an independent goroutine open the gate
// once the fatal entry is enqueued, then calls Fatal with the default handler
// (os.Exit(1)). Delivery must complete before the exit.
func wp2FatalSubprocessMain(t *testing.T) {
	path := os.Getenv(wp2FatalFileEnv)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // G304: test-provided path
	if err != nil {
		panic(err)
	}
	defer func() { _ = f.Close() }()

	gw := newWP2GateEntryWriter()
	gw.sink = f
	l := New(WithConsoleOutput(io.Discard), WithLevel(LevelDebug))
	l.AddWriter("file", gw)

	// Fill until the first drop: the 256-slot queue is provably full and the
	// worker is parked on the gate.
	// Internal reach-in: Logger does not expose the drop counter; the
	// MultiWriter's does for exactly this observability purpose.
	dropped := func() uint64 {
		l.writers.mu.RLock()
		mw := l.writers.mw
		l.writers.mu.RUnlock()
		if mw == nil {
			return 0
		}
		return mw.DroppedCount()
	}
	sent := 0
	for dropped() == 0 {
		l.Info(fmt.Sprintf("wp2-filler-%03d", sent))
		sent++
	}
	t.Logf("queue full after %d sends (%d accepted)", sent, sent-1)

	// Open the gate shortly after the fatal call starts. The gate cannot be
	// opened by the FatalHandler (which now runs only after delivery) and it
	// cannot wait for the fatal enqueue (the fatal send blocks precisely
	// because the queue is full). The short delay puts Fatal inside its
	// blocking send; the delivery assertion holds in every interleaving.
	fatalStarted := make(chan struct{})
	go func() {
		<-fatalStarted
		time.Sleep(50 * time.Millisecond)
		close(gw.gate)
	}()
	close(fatalStarted)

	// Default FatalHandler: os.Exit(1) runs only after the barrier completes.
	l.Fatal(wp2FatalMsg, String("row", "fatal"))
}

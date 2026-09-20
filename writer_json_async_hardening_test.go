package velocity

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gatedSink is a structured sink for the deterministic async tests: every
// Write parks on the release channel, delivered lines are recorded, and
// writes can fail from a chosen index with distinct errors. entered closes on
// the first Write arrival so a test can prove the drainer has picked up an
// item and is parked on the sink before it asserts on queue state.
type gatedSink struct {
	mu    sync.Mutex
	lines []string
	n     int // completed writes, 1-based, drives failFrom

	release    chan struct{}
	entered    chan struct{}
	enteredOne sync.Once

	failFrom int               // writes from this 1-based index fail
	failWith func(n int) error // distinct error per failing write

	// onLine, when set, runs after a line is recorded (outside the sink
	// mutex) so tests can interleave sink observations with handler events.
	onLine func(line string)
}

func newGatedSink() *gatedSink {
	return &gatedSink{release: make(chan struct{}), entered: make(chan struct{})}
}

func (s *gatedSink) Write(p []byte) (int, error) {
	s.enteredOne.Do(func() { close(s.entered) })
	if s.release != nil {
		<-s.release
	}
	s.mu.Lock()
	s.n++
	fail := s.failFrom > 0 && s.n >= s.failFrom && s.failWith != nil
	if !fail {
		s.lines = append(s.lines, strings.TrimRight(string(p), "\n"))
	}
	s.mu.Unlock()
	if fail {
		return len(p), s.failWith(s.n)
	}
	if s.onLine != nil {
		s.onLine(strings.TrimRight(string(p), "\n"))
	}
	return len(p), nil
}

func (s *gatedSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lines)
}

func (s *gatedSink) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.lines))
	copy(out, s.lines)
	return out
}

// asyncQueue reports the live fill level and capacity of a logger's async
// queue. Channel len/cap are safe to poll concurrently with sends.
func asyncQueue(l *Logger) (fill, depth int) {
	ch := l.jsonWriter.async.ch
	return len(ch), cap(ch)
}

// waitForQueueSettled blocks until the async queue sits at its saturation
// high-water mark against a gated sink: cap when the drainer has not yet
// dequeued, cap-1 once it has taken a record and parked. Call only after
// <-sink.entered and after every send has completed, so the level is final.
func waitForQueueSettled(t *testing.T, l *Logger) {
	t.Helper()
	waitFor(t, func() bool {
		n, c := asyncQueue(l)
		return n == c || n == c-1
	}, 5*time.Second, time.Millisecond, "async queue to settle at saturation")
}

// waitForParkedSend polls goroutine stacks until some goroutine is PARKED in
// a channel send inside the named function. Unlike waitForStack it ties the
// [chan send] state to the function frame. The match is process-wide, so
// callers must ensure no other running test can park a sender in fn: use
// from sequential tests only (parallel tests are paused while a sequential
// test runs). Used as the deterministic handshake proving a caller's record
// is already admitted (admit runs before the send) before Close starts
// racing admission.
func waitForParkedSend(t *testing.T, fn string, deadline time.Duration) bool {
	t.Helper()
	deadlineAt := time.Now().Add(deadline)
	for time.Now().Before(deadlineAt) {
		stacks := make([]byte, 1<<20)
		n := runtime.Stack(stacks, true)
		for block := range strings.SplitSeq(string(stacks[:n]), "\n\n") {
			if strings.Contains(block, "[chan send]") && strings.Contains(block, fn) {
				return true
			}
		}
		runtime.Gosched()
	}
	return false
}

// jsonLine is the minimal shape the integrity checks need from a delivered
// structured record.
type jsonLine struct {
	Message string `json:"message"`
}

// parseWholeJSONLines asserts every delivered line is complete, un-torn JSON:
// pool reuse that handed a buffer back while it was still being written, or
// interleaved fragments from two records, would fail the Unmarshal here.
func parseWholeJSONLines(t *testing.T, lines []string) []jsonLine {
	t.Helper()
	out := make([]jsonLine, len(lines))
	for n, line := range lines {
		if err := json.Unmarshal([]byte(line), &out[n]); err != nil {
			t.Fatalf("line %d is not complete JSON (torn or interleaved): %q: %v", n, line, err)
		}
	}
	return out
}

// checkPerGoroutineOrder verifies per-goroutine submission order for messages
// shaped "<prefix>-gg-iii": within each goroutine the sequence numbers must
// arrive strictly ascending. Gaps are expected under the drop policy (a
// dropped record simply never arrives); repeats or regressions are ordering
// defects.
func checkPerGoroutineOrder(t *testing.T, msgs []string, prefix string, goroutines int) {
	t.Helper()
	last := make([]int, goroutines)
	for i := range last {
		last[i] = -1
	}
	for n, msg := range msgs {
		var g, seq int
		if _, err := fmt.Sscanf(msg, prefix+"-%02d-%03d", &g, &seq); err != nil {
			t.Fatalf("message %d = %q does not parse as %s-gg-iii: %v", n, msg, prefix, err)
		}
		if g < 0 || g >= goroutines {
			t.Fatalf("message %d references goroutine %d outside [0,%d): %q", n, g, goroutines, msg)
		}
		if seq <= last[g] {
			t.Fatalf("goroutine %d out of order: seq %d after %d (message %d)", g, seq, last[g], n)
		}
		last[g] = seq
	}
}

// Queue depth 1 is the smallest possible pipeline: the drainer holds one
// record on the sink, one more occupies the queue, and every further caller
// blocks. Interleaved enqueue/drain must not deadlock, ordering must hold,
// and Close must still drain the blocked caller's record (its in-flight
// registration holds the Close drain open until the send lands).
//
// Deliberately NOT t.Parallel: waitForParkedSend matches any goroutine
// parked in a chan send inside enqueueAsync, and the other async tests park
// their own senders there. A parallel neighbour can satisfy the handshake
// before this test's caller has admitted its final record, letting Close
// win the admission race and legitimately reject it. Sequential tests run
// alone (parallel ones are paused), so the park is provably ours.
func TestAsyncOutput_QueueDepth1_InterleavingAndCloseStillDrains(t *testing.T) {
	sink := newGatedSink()
	logger := newAsyncTestLogger(sink, AsyncConfig{Queue: 1, OnFull: AsyncBlock})

	logged := make(chan struct{})
	go func() {
		defer close(logged)
		for i := range 3 {
			logger.Info(fmt.Sprintf("d1-%03d", i))
		}
	}()

	// Item 0 is in the drainer (parked on the gate); item 1 fills the queue.
	// With the gate shut and the queue full, the only send the caller can be
	// parked on is item 2's, so waiting for that park proves all three records
	// are admitted before Close starts racing admission — without it Close can
	// win the admission lock and reject item 2 per the family-close model.
	<-sink.entered
	waitForQueueSettled(t, logger)
	if !waitForParkedSend(t, "enqueueAsync", 5*time.Second) {
		t.Fatal("caller never parked on the depth-1 send")
	}

	closed := make(chan error, 1)
	go func() { closed <- logger.Close() }()

	// Opening the gate releases the whole chain: drainer writes 0, caller
	// unblocks on item 2, Close's sentinel lands behind it and drains.
	close(sink.release)

	select {
	case <-logged:
	case <-time.After(10 * time.Second):
		t.Fatal("caller deadlocked against a depth-1 queue")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close deadlocked against a depth-1 queue")
	}

	lines := parseWholeJSONLines(t, sink.snapshot())
	if len(lines) != 3 {
		t.Fatalf("delivered %d lines, want all 3", len(lines))
	}
	for i, want := range []string{"d1-000", "d1-001", "d1-002"} {
		if lines[i].Message != want {
			t.Fatalf("line %d = %q, want %q: depth-1 ordering broken", i, lines[i].Message, want)
		}
	}
}

// Drop policy under real contention: many goroutines against a gated sink.
// written + dropped must equal logged exactly (no double count, no
// under-count), the counter must never go backwards, and it must be frozen
// once Close has completed.
func TestAsyncOutput_DropPolicyUnderContention_AccountsExactly(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	const perGoroutine = 25
	const total = goroutines * perGoroutine

	sink := newGatedSink()
	logger := newAsyncTestLogger(sink, AsyncConfig{Queue: 8, OnFull: AsyncDrop})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			for i := range perGoroutine {
				logger.Info(fmt.Sprintf("dc-%02d-%03d", g, i))
			}
		}(g)
	}
	close(start)
	wg.Wait() // drop policy: no caller can block on the gated sink

	// Saturation is deterministic while the gate is shut: one record is in
	// the drainer, eight occupy the queue, everything else must have dropped.
	midDropped := logger.StructuredDroppedCount()
	if midDropped == 0 {
		t.Fatalf("no drops against a gated sink with queue depth 8 for %d records", total)
	}

	close(sink.release)
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	written := sink.count()
	dropped := logger.StructuredDroppedCount()
	//nolint:gosec // test-only accounting against a fixed record count
	if int64(written)+int64(dropped) != int64(total) {
		t.Fatalf("written %d + dropped %d != logged %d: accounting broken", written, dropped, total)
	}
	if dropped < midDropped {
		t.Fatalf("dropped counter went backwards: %d before Close, %d after", midDropped, dropped)
	}
	if again := logger.StructuredDroppedCount(); again != dropped {
		t.Fatalf("dropped counter moved after Close: %d then %d", dropped, again)
	}

	// Line integrity under drops: buffers that were dropped went back to the
	// pool while the drainer kept writing; no reused-in-flight buffer may
	// surface as a torn or interleaved line.
	parsed := parseWholeJSONLines(t, sink.snapshot())
	msgs := make([]string, len(parsed))
	for i, p := range parsed {
		msgs[i] = p.Message
	}
	checkPerGoroutineOrder(t, msgs, "dc", goroutines)
}

// Close racing live writers: a wave of goroutines that finished before Close
// must be fully written (admitted pre-Close, block policy loses nothing),
// the racing wave may be partially rejected per the family-close model but
// never torn or reordered, the drainer must retire, and neither Close may
// hang or panic.
func TestAsyncOutput_ConcurrentCloseRacingLiveWriters(t *testing.T) {
	t.Parallel()

	const stable = 4
	const racing = 4
	const perGoroutine = 20

	sink := &asyncTestWriter{}
	logger := newAsyncTestLogger(sink, AsyncConfig{Queue: 32, OnFull: AsyncBlock})

	var stableWG sync.WaitGroup
	for g := range stable {
		stableWG.Add(1)
		go func(g int) {
			defer stableWG.Done()
			for i := range perGoroutine {
				logger.Info(fmt.Sprintf("sc-%02d-%03d", g, i))
			}
		}(g)
	}
	stableWG.Wait() // happens-before Close: all stable records admitted

	closeErrs := make(chan error, 2)
	go func() { closeErrs <- logger.Close() }()
	go func() { closeErrs <- logger.Close() }() // concurrent second Close shares the drain

	var racingWG sync.WaitGroup
	start := make(chan struct{})
	for g := range racing {
		racingWG.Add(1)
		go func(g int) {
			defer racingWG.Done()
			<-start
			for i := range perGoroutine {
				logger.Info(fmt.Sprintf("rc-%02d-%03d", g, i))
			}
		}(g)
	}
	close(start)
	racingWG.Wait()

	for range 2 {
		select {
		case err := <-closeErrs:
			if err != nil {
				t.Fatalf("Close: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Close did not complete while writers raced it")
		}
	}

	lines := sink.snapshot()
	if len(lines) < stable*perGoroutine {
		t.Fatalf("delivered %d lines, want at least the %d admitted before Close", len(lines), stable*perGoroutine)
	}
	if len(lines) > (stable+racing)*perGoroutine {
		t.Fatalf("delivered %d lines, want at most %d", len(lines), (stable+racing)*perGoroutine)
	}

	// The drainer must have retired by the time Close returned.
	select {
	case <-logger.jsonWriter.async.done:
	default:
		t.Fatal("drainer goroutine still running after Close returned")
	}

	parsed := parseWholeJSONLines(t, lines)
	scMsgs, rcMsgs := []string{}, []string{}
	for _, p := range parsed {
		switch {
		case strings.HasPrefix(p.Message, "sc-"):
			scMsgs = append(scMsgs, p.Message)
		case strings.HasPrefix(p.Message, "rc-"):
			rcMsgs = append(rcMsgs, p.Message)
		default:
			t.Fatalf("unexpected message %q in structured output", p.Message)
		}
	}
	if len(scMsgs) != stable*perGoroutine {
		t.Fatalf("stable wave delivered %d of %d records admitted before Close", len(scMsgs), stable*perGoroutine)
	}
	checkPerGoroutineOrder(t, scMsgs, "sc", stable)
	checkPerGoroutineOrder(t, rcMsgs, "rc", racing)
}

// Fatal under a saturated drop queue: the reliable path must never take the
// drop branch, must be ordered behind every earlier accepted record, and the
// FatalHandler must run only after the fatal line is on the sink.
func TestAsyncOutput_FatalSurvivesSaturatedDropQueue(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var events []string
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, s)
	}

	sink := newGatedSink()
	sink.onLine = func(line string) {
		if strings.Contains(line, "fatal-boom") {
			record("FATAL_LINE")
		}
	}
	logger := New(
		WithProduction(),
		WithStructuredOutput(sink),
		WithAsyncOutput(AsyncConfig{Queue: 8, OnFull: AsyncDrop}),
		WithFatalHandler(func() { record("HANDLER") }),
	)

	// Saturate: the drainer parks on the gated first write and the queue
	// stays full, so everything beyond the first nine records drops.
	for i := range 30 {
		logger.Info(fmt.Sprintf("sat-%03d", i))
	}
	<-sink.entered
	waitForQueueSettled(t, logger)
	if dropped := logger.StructuredDroppedCount(); dropped == 0 {
		t.Fatal("expected saturation drops before Fatal")
	}

	fatalDone := make(chan struct{})
	go func() {
		defer close(fatalDone)
		logger.Fatal("fatal-boom")
	}()

	close(sink.release)

	select {
	case <-fatalDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Fatal never completed: reliable path dropped or deadlocked on a saturated drop queue")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "FATAL_LINE" || events[1] != "HANDLER" {
		t.Fatalf("events = %v, want the fatal line written before the handler", events)
	}

	// Ordering: the fatal record is the last delivered line, behind every
	// accepted saturation record, and the drops never removed a line that
	// made it into the queue ahead of it.
	lines := sink.snapshot()
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "fatal-boom") {
		t.Fatalf("fatal record is not the last delivered line (delivered %d lines)", len(lines))
	}
	parsed := parseWholeJSONLines(t, lines[:len(lines)-1])
	lastSeq := -1
	for n, p := range parsed {
		var seq int
		if _, err := fmt.Sscanf(p.Message, "sat-%03d", &seq); err != nil {
			t.Fatalf("line %d is neither a saturation record nor parseable: %q", n, p.Message)
		}
		if seq <= lastSeq {
			t.Fatalf("accepted saturation records out of order: %d after %d", seq, lastSeq)
		}
		lastSeq = seq
	}
}

// Flush in async mode is a queue barrier: when it returns, every record
// accepted before it has been written to the sink, and the accounting against
// the drop counter still closes exactly.
func TestAsyncOutput_FlushBarrierDrainsAcceptedRecords(t *testing.T) {
	t.Parallel()

	const total = 20

	sink := newGatedSink()
	logger := newAsyncTestLogger(sink, AsyncConfig{Queue: 8, OnFull: AsyncDrop})

	for i := range total {
		logger.Info(fmt.Sprintf("fl-%03d", i))
	}
	<-sink.entered
	waitForQueueSettled(t, logger)

	flushErr := make(chan error, 1)
	go func() { flushErr <- logger.jsonWriter.Flush() }()

	// The barrier send blocks while the queue is full; opening the gate lets
	// the drainer write everything ahead of the barrier, ack it, and only
	// then does Flush return.
	close(sink.release)

	select {
	case err := <-flushErr:
		if err != nil {
			t.Fatalf("Flush: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Flush barrier deadlocked behind a full queue")
	}

	lines := sink.snapshot()
	dropped := logger.StructuredDroppedCount()
	//nolint:gosec // test-only accounting against a fixed record count
	if int64(len(lines))+int64(dropped) != int64(total) {
		t.Fatalf("written %d + dropped %d != logged %d after Flush returned", len(lines), dropped, total)
	}
	// Ascending with gaps (drops), never repeated or regressed.
	parsed := parseWholeJSONLines(t, lines)
	lastSeq := -1
	for n, p := range parsed {
		var seq int
		if _, err := fmt.Sscanf(p.Message, "fl-%03d", &seq); err != nil {
			t.Fatalf("unexpected message %q in structured output", p.Message)
		}
		if seq <= lastSeq {
			t.Fatalf("delivered records out of order: line %d has seq %d after %d", n, seq, lastSeq)
		}
		lastSeq = seq
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Flush on a closed family is a no-op that returns nil, not a hang.
	if err := logger.jsonWriter.Flush(); err != nil {
		t.Fatalf("Flush after Close = %v, want nil", err)
	}
}

// Child loggers share the async writer through writerSet: one queue, one
// drainer, one drop counter visible from every member, and a parent Close
// drains the children's records too. Post-close writes are rejected, not
// counted as drops.
func TestAsyncOutput_ChildrenShareAsyncWriterAndFamilyClose(t *testing.T) {
	t.Parallel()

	sink := newGatedSink()
	logger := newAsyncTestLogger(sink, AsyncConfig{Queue: 4, OnFull: AsyncDrop})
	child := logger.With(String("module", "auth"))
	grandchild := child.Detailed()

	if child.jsonWriter != logger.jsonWriter || grandchild.jsonWriter != logger.jsonWriter {
		t.Fatal("children do not share the parent's async structured writer")
	}

	// Saturate against the shut gate so the drop counter is non-zero.
	for i := range 20 {
		logger.Info(fmt.Sprintf("p-%03d", i))
	}
	waitForQueueSettled(t, logger)
	parentDropped := logger.StructuredDroppedCount()
	if parentDropped == 0 {
		t.Fatal("expected drops against the gated sink")
	}
	if got := child.StructuredDroppedCount(); got != parentDropped {
		t.Fatalf("child drop count %d != parent %d: counter not shared", got, parentDropped)
	}

	// Open the gate, then log from both children; the parent Close below
	// must drain them with the parent's records.
	close(sink.release)
	for i := range 3 {
		child.Info(fmt.Sprintf("c-%03d", i))
	}
	grandchild.Info("gc-000")

	if err := logger.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}

	const total = 20 + 3 + 1
	lines := sink.snapshot()
	dropped := logger.StructuredDroppedCount()
	//nolint:gosec // test-only accounting against a fixed record count
	if int64(len(lines))+int64(dropped) != int64(total) {
		t.Fatalf("written %d + dropped %d != logged %d across the family", len(lines), dropped, total)
	}

	// Family closed for children too: no revival, and rejected writes are
	// not drops.
	before := sink.count()
	child.Info("post-close")
	grandchild.Info("post-close")
	if got := sink.count(); got != before {
		t.Fatal("child logger wrote after the parent closed the family")
	}
	if got := child.StructuredDroppedCount(); got != dropped {
		t.Fatalf("post-close rejected writes counted as drops: %d -> %d", dropped, got)
	}
}

// failSeqSink succeeds on the first ok writes and fails every later one with
// a distinct error, so "first drainer error wins" is observable in Close's
// result.
type failSeqSink struct {
	mu sync.Mutex
	n  int
	ok int
}

func (s *failSeqSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if s.n > s.ok {
		return len(p), fmt.Errorf("sink failure #%d", s.n)
	}
	return len(p), nil
}

// The drainer runs on its own goroutine, so Write errors cannot reach the
// caller; they must surface in Close's result, the first error must win, and
// a second Close must return the same recorded error. The stop sentinel rides
// behind every queued record, so all failures are observed before Close reads
// the recorded error — no sleeps needed.
func TestAsyncOutput_DrainerWriteErrorSurfacesInClose(t *testing.T) {
	t.Parallel()

	sink := &failSeqSink{ok: 3}
	logger := newAsyncTestLogger(sink, AsyncConfig{Queue: 16, OnFull: AsyncBlock})

	for range 10 {
		logger.Info("boom fodder")
	}

	err := logger.Close()
	if err == nil {
		t.Fatal("drainer write errors were swallowed: Close returned nil")
	}
	if !strings.Contains(err.Error(), "sink failure #4") {
		t.Fatalf("Close error = %v, want the first failure (sink failure #4)", err)
	}
	for n := 5; n <= 10; n++ {
		if strings.Contains(err.Error(), fmt.Sprintf("sink failure #%d", n)) {
			t.Fatalf("Close error = %v: a later failure clobbered the first", err)
		}
	}
	if err2 := logger.Close(); err2 == nil || err2.Error() != err.Error() {
		t.Fatalf("second Close = %v, want the same recorded error %v", err2, err)
	}
}

// Status, Group and Continue records ride the same async queue as plain
// entries; none of the rich paths may tear a line or escape the FIFO.
func TestAsyncOutput_RichRecordsStayWholeAndOrdered(t *testing.T) {
	t.Parallel()

	sink := &asyncTestWriter{}
	logger := newAsyncTestLogger(sink, AsyncConfig{Queue: 4, OnFull: AsyncBlock})

	logger.Info("plain-0")
	logger.Status(LevelWarn, StatusFail, "status-1", String("detail", "gate"))
	logger.Info("plain-2")
	logger.Group(LevelInfo, "group-3", GroupItem{Marker: "a", Text: "1"}, GroupItem{Marker: "b", Text: "2"})
	logger.Info("plain-4")
	logger.Continue(LevelInfo, "cont-5", "line one", "line two")
	logger.Info("plain-6")

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := parseWholeJSONLines(t, sink.snapshot())
	if len(lines) != 7 {
		t.Fatalf("delivered %d lines, want 7", len(lines))
	}
	wantMsgs := []string{"plain-0", "status-1", "plain-2", "group-3", "plain-4", "cont-5", "plain-6"}
	for i, want := range wantMsgs {
		if !strings.HasPrefix(lines[i].Message, want) {
			t.Fatalf("line %d message %q, want prefix %q: rich records broke FIFO ordering", i, lines[i].Message, want)
		}
	}
}

// flushSpySink detects concurrent entry into Flush: Close's final flush must
// be exclusive with a still-running async Flush that admitted before close
// (review finding F1 — the race detector flags the overlap, the counter makes
// it observable without one).
type flushSpySink struct {
	mu       sync.Mutex
	lines    []string
	inFlush  atomic.Int32
	flushMax atomic.Int32
}

func (s *flushSpySink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.lines = append(s.lines, strings.TrimRight(string(p), "\n"))
	s.mu.Unlock()
	return len(p), nil
}

func (s *flushSpySink) Flush() error {
	now := s.inFlush.Add(1)
	for {
		old := s.flushMax.Load()
		if now <= old || s.flushMax.CompareAndSwap(old, now) {
			break
		}
	}
	time.Sleep(time.Millisecond)
	s.inFlush.Add(-1)
	return nil
}

func TestAsyncOutput_CloseFlushExclusiveWithConcurrentFlush(t *testing.T) {
	t.Parallel()

	for range 25 {
		sink := &flushSpySink{}
		logger := newAsyncTestLogger(sink, AsyncConfig{Queue: 8, OnFull: AsyncBlock})
		logger.Info("record")

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = logger.jsonWriter.Flush()
		}()
		go func() {
			defer wg.Done()
			_ = logger.Close()
		}()
		wg.Wait()

		if got := sink.flushMax.Load(); got > 1 {
			t.Fatalf("Flush overlapped %d deep with Close's final flush", got)
		}
		if len(sink.lines) != 1 {
			t.Fatalf("delivered %d lines, want 1", len(sink.lines))
		}
	}
}

// TestAsyncOutput_ParallelSteadyStateAllocations is the allocation gate for
// the async buffer handoff: default queue depth, parallel producers against
// a sink charging a real serialised cost, measured once the one-off ramp
// (one 2KiB buffer per queue slot warming sync.Pool) has been paid. Steady
// state on the pool allocates nothing; a per-record allocation on the handoff
// path allocates a buffer pair per record and lands far over the budget.
// Runs under make ready's test phase, unlike a benchmark-body assertion.
func TestAsyncOutput_ParallelSteadyStateAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation forces sync.Pool through pinSlow, which allocates per operation; the strict window runs in make ready's plain test phase")
	}
	sink := &slowSink{}
	logger := New(WithProduction(), WithStructuredOutput(sink), WithAsyncOutput(AsyncConfig{}))
	fields := fiveFields()

	// Ramp: 8 producers until the queue has been full, so the buffer
	// population exists and the pool is warm before the measured window.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 12000 {
				logger.Info("request completed", fields...)
			}
		}()
	}
	wg.Wait()

	old := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(old)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5000 {
				logger.Info("request completed", fields...)
			}
		}()
	}
	wg.Wait()
	runtime.ReadMemStats(&after)

	const measured = 4 * 5000
	// A per-record regression allocates one buffer pair per record (20000
	// over this window); the pool serves steady state with nothing but rare
	// scheduler-driven misses, so the budget is a sliver of the records.
	if mallocs := after.Mallocs - before.Mallocs; mallocs > measured/1000*5 {
		t.Fatalf("async parallel steady state allocated %d times over %d records; buffer pooling regressed", mallocs, measured)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

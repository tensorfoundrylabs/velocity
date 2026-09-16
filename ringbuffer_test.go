package velocity

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rbRecordWriter is a mutex-guarded io.Writer for byte-queue output.
type rbRecordWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *rbRecordWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *rbRecordWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// rbGateWriter blocks the first flush until gate is closed, signalling via
// entered that the drainer has parked inside the underlying Write. Used to
// hold the drain open so queue state is deterministic.
type rbGateWriter struct {
	gate    chan struct{}
	entered chan struct{}
	mu      sync.Mutex
	buf     []byte
}

func newRBGateWriter() *rbGateWriter {
	return &rbGateWriter{gate: make(chan struct{}), entered: make(chan struct{}, 1)}
}

func (w *rbGateWriter) Write(p []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.gate
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	w.mu.Unlock()
	return len(p), nil
}

func (w *rbGateWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// rbFailWriter always fails, to exercise flush error accounting.
type rbFailWriter struct{ calls atomic.Int64 }

func (w *rbFailWriter) Write([]byte) (int, error) {
	w.calls.Add(1)
	return 0, errors.New("rb sink failure")
}

// rbSplitLines splits s on newlines, keeping a trailing segment after a final
// newline only when it is non-empty.
func rbSplitLines(s string) []string {
	var out []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// TestRingBuffer_TinyQueue_ConcurrentProducers_ExactOnce: a size-2 queue with
// 8 concurrent producers wrapping it repeatedly must deliver every ACCEPTED
// record exactly once with its exact bytes, never deliver a rejected record,
// and count every rejection.
func TestRingBuffer_TinyQueue_ConcurrentProducers_ExactOnce(t *testing.T) {
	t.Parallel()

	const (
		producers = 8
		per       = 400
		ringSize  = 2
	)

	rec := &rbRecordWriter{}
	rb := NewRingBuffer(rec, ringSize)

	accepted := make([][]bool, producers)
	var wg sync.WaitGroup
	for g := range producers {
		accepted[g] = make([]bool, per)
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range per {
				// Unique id per record; exact bytes verified on delivery.
				payload := fmt.Appendf(nil, "id=%d.%d\n", g, i)
				accepted[g][i] = rb.Write(payload)
			}
		}(g)
	}
	wg.Wait()

	if err := rb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Every delivered line must be a well-formed id record.
	counts := make(map[string]int)
	delivered := 0
	for _, line := range rbSplitLines(rec.String()) {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "id=") {
			t.Fatalf("corrupt record delivered: %q", line)
		}
		counts[line]++
		delivered++
	}

	acceptedTotal, rejectedTotal := 0, 0
	for g := range producers {
		for i := range per {
			key := fmt.Sprintf("id=%d.%d", g, i)
			n := counts[key]
			if accepted[g][i] {
				acceptedTotal++
				if n != 1 {
					t.Errorf("accepted record %s delivered %d times (want exactly 1)", key, n)
				}
			} else {
				rejectedTotal++
				if n != 0 {
					t.Errorf("rejected record %s was delivered anyway", key)
				}
			}
		}
	}

	if delivered != acceptedTotal {
		t.Errorf("delivered %d records but %d were accepted (lost or duplicated output)", delivered, acceptedTotal)
	}

	if got := rb.DroppedCount(); got < uint64(rejectedTotal) {
		t.Errorf("DroppedCount=%d but %d writes were rejected — rejections must be counted", got, rejectedTotal)
	}
	t.Logf("accepted=%d rejected=%d delivered=%d DroppedCount=%d", acceptedTotal, rejectedTotal, delivered, rb.DroppedCount())
}

// TestRingBuffer_CallerMutationCannotAlterDeliveredBytes: after a successful
// Write the queue owns the bytes; immediately poisoning the caller's slice must
// not change what the consumer receives.
//
// Determinism: the sink is gated so the drainer parks inside its first
// underlying Write after popping exactly one record; with the drainer parked,
// exactly capacity further writes are guaranteed acceptance — no retry loop
// that can starve (or be starved by) the drainer under parallel test load.
func TestRingBuffer_CallerMutationCannotAlterDeliveredBytes(t *testing.T) {
	t.Parallel()

	const queueCap = 64
	gw := newRBGateWriter()
	rb := NewRingBuffer(gw, queueCap)

	first := []byte("payload-0000-original")
	if !rb.Write(first) {
		t.Fatal("first write rejected on an empty queue")
	}
	// Caller reuses its buffer the moment Write returns.
	for j := range first {
		first[j] = 'X'
	}
	want := []string{"payload-0000-original"}

	select {
	case <-gw.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("drainer never reached the underlying writer")
	}

	for i := 1; i <= queueCap; i++ {
		payload := fmt.Appendf(nil, "payload-%04d-original", i)
		if !rb.Write(payload) {
			t.Fatalf("write %d rejected while the drainer was parked and the queue had space", i)
		}
		want = append(want, string(payload))
		for j := range payload {
			payload[j] = 'X'
		}
	}

	close(gw.gate)
	if err := rb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := gw.String(); got != strings.Join(want, "") {
		t.Errorf("delivered bytes differ from what was written — caller mutation altered queue contents\nwant prefix: %.80q\ngot  prefix: %.80q", strings.Join(want, ""), got)
	}
}

// TestRingBuffer_AcceptedOrderPreserved: records from a single producer must
// reach the underlying writer in acceptance order, across many drain batches.
//
// Determinism: same gated-sink handshake as the mutation test — the drainer
// parks after popping exactly one record, so exactly capacity further writes
// are guaranteed acceptance. 513 records exceeds one batch (64), so ordering
// is verified across batch boundaries.
func TestRingBuffer_AcceptedOrderPreserved(t *testing.T) {
	t.Parallel()

	const queueCap = 512
	gw := newRBGateWriter()
	rb := NewRingBuffer(gw, queueCap)

	if !rb.Write(fmt.Appendf(nil, "seq-%04d\n", 0)) {
		t.Fatal("first write rejected on an empty queue")
	}
	select {
	case <-gw.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("drainer never reached the underlying writer")
	}

	const n = queueCap + 1 // +1 for the record already inside the parked batch
	for i := 1; i < n; i++ {
		if !rb.Write(fmt.Appendf(nil, "seq-%04d\n", i)) {
			t.Fatalf("write %d rejected while the drainer was parked and the queue had space", i)
		}
	}

	close(gw.gate)
	if err := rb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := rbSplitLines(gw.String())
	if len(lines) != n {
		t.Fatalf("delivered %d records, want %d", len(lines), n)
	}
	for i, line := range lines {
		if want := fmt.Sprintf("seq-%04d", i); line != want {
			t.Fatalf("order broken at %d: got %q, want %q", i, line, want)
		}
	}
}

// TestRingBuffer_FullQueueDropAccounting: with the drain gated open, the queue
// accepts exactly its capacity, rejects the rest, counts every rejection, and
// delivers exactly the accepted records once ungated.
func TestRingBuffer_FullQueueDropAccounting(t *testing.T) {
	t.Parallel()

	gw := newRBGateWriter()
	const size = 4
	rb := NewRingBuffer(gw, size)

	// First write parks the drainer inside the underlying writer.
	if !rb.Write([]byte("first\n")) {
		t.Fatal("first write rejected on an empty queue")
	}
	select {
	case <-gw.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("drainer never reached the underlying writer")
	}

	accepted, rejected := 0, 0
	for i := range 20 {
		if rb.Write(fmt.Appendf(nil, "full-%02d\n", i)) {
			accepted++
		} else {
			rejected++
		}
	}
	if rejected == 0 {
		t.Fatal("no rejections with the drain gated open — queue capacity not bounded")
	}
	if got := rb.DroppedCount(); got != uint64(rejected) {
		t.Errorf("DroppedCount=%d, want exactly %d (rejections only)", got, rejected)
	}

	close(gw.gate)
	if err := rb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	lines := rbSplitLines(gw.String())
	if len(lines) != accepted+1 { // +1 for the gated first record
		t.Errorf("delivered %d records, want %d (accepted + gated first)", len(lines), accepted+1)
	}
	if got := rb.DroppedCount(); got != uint64(rejected) {
		t.Errorf("DroppedCount changed during drain: %d, want %d", got, rejected)
	}
}

// TestRingBuffer_DoubleClose_Idempotent: Close must be concurrently idempotent
// — no panic, no error, and both callers wait for the same drain.
func TestRingBuffer_DoubleClose_Idempotent(t *testing.T) {
	t.Parallel()

	gw := newRBGateWriter()
	rb := NewRingBuffer(gw, 4)

	if !rb.Write([]byte("drain-me\n")) {
		t.Fatal("write rejected on an empty queue")
	}
	select {
	case <-gw.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("drainer never reached the underlying writer")
	}

	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- rb.Close() }()
	}

	// Both Closes must still be blocked while the sink is gated.
	select {
	case err := <-errs:
		t.Fatalf("Close returned (%v) while the underlying writer was still blocked", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(gw.gate)
	for range 2 {
		select {
		case err := <-errs:
			if err != nil {
				t.Errorf("Close returned error: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Close never returned after the sink unblocked")
		}
	}

	if !strings.Contains(gw.String(), "drain-me") {
		t.Error("accepted record lost during double close")
	}
}

// TestRingBuffer_WriteAfterClose_Rejected: Close must reject new writes. A
// post-Close Write returning true would mean the entry is committed to a dead
// queue: never delivered and silently uncounted.
func TestRingBuffer_WriteAfterClose_Rejected(t *testing.T) {
	t.Parallel()

	rec := &rbRecordWriter{}
	rb := NewRingBuffer(rec, 4)
	if err := rb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	before := rb.DroppedCount()
	if rb.Write([]byte("late-write")) {
		t.Error("Write after Close returned true — entry committed to a closed queue")
	}
	if got := rec.String(); strings.Contains(got, "late-write") {
		t.Error("late write unexpectedly delivered")
	}
	if got := rb.DroppedCount(); got != before+1 {
		t.Errorf("post-close rejection not counted: DroppedCount=%d, want %d", got, before+1)
	}
}

// TestRingBuffer_ConcurrentCloseVsWrite: producers writing while Close runs
// must not panic or race, everything delivered must be a real record, and every
// accepted record must be delivered — Close cannot lose queue contents.
func TestRingBuffer_ConcurrentCloseVsWrite(t *testing.T) {
	t.Parallel()

	rec := &rbRecordWriter{}
	rb := NewRingBuffer(rec, 8)

	var wg sync.WaitGroup
	start := make(chan struct{})
	var accepted, rejected atomic.Int64

	for g := range 4 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			for i := range 500 {
				if rb.Write(fmt.Appendf(nil, "id=%d.%d\n", g, i)) {
					accepted.Add(1)
				} else {
					rejected.Add(1)
				}
			}
		}(g)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_ = rb.Close()
	}()

	close(start)
	wg.Wait()

	counts := make(map[string]int)
	for _, line := range rbSplitLines(rec.String()) {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "id=") {
			t.Errorf("corrupt record delivered during close race: %q", line)
		}
		counts[line]++
	}
	for line, n := range counts {
		if n > 1 {
			t.Errorf("record %q delivered %d times during close race", line, n)
		}
	}
	if got := int64(len(counts)); got != accepted.Load() {
		t.Errorf("delivered %d unique records but %d were accepted — records lost across Close", got, accepted.Load())
	}
	t.Logf("close race: accepted=%d rejected=%d delivered=%d dropped=%d",
		accepted.Load(), rejected.Load(), len(counts), rb.DroppedCount())
}

// TestRingBuffer_BlockedUnderlyingWriter_CloseWaitsThenCompletes: Close must
// not return while the underlying writer is blocked, and once unblocked it
// must deliver the accepted entries.
func TestRingBuffer_BlockedUnderlyingWriter_CloseWaitsThenCompletes(t *testing.T) {
	t.Parallel()

	gw := newRBGateWriter()
	rb := NewRingBuffer(gw, 4)

	if !rb.Write([]byte("one")) || !rb.Write([]byte("two")) {
		t.Fatal("precondition: writes not accepted")
	}

	// Handshake: the drainer has parked inside the underlying Write.
	select {
	case <-gw.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("drainer never reached the underlying writer")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- rb.Close() }()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned (%v) while the underlying writer was still blocked", err)
	case <-time.After(200 * time.Millisecond):
		// Close is still blocked in the drain, as required while the sink stalls.
	}

	close(gw.gate)

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close returned error after unblock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned after the underlying writer unblocked")
	}

	if got := gw.String(); got != "onetwo" {
		t.Errorf("delivered %q after close, want %q", got, "onetwo")
	}
}

// TestRingBuffer_FlushErrorAccounted: records in a batch whose underlying
// write fails must be observable through DroppedCount, and Close must still
// complete against a permanently failing sink.
func TestRingBuffer_FlushErrorAccounted(t *testing.T) {
	t.Parallel()

	fw := &rbFailWriter{}
	rb := NewRingBuffer(fw, 8)

	const n = 5
	for i := range n {
		if !rb.Write(fmt.Appendf(nil, "doomed-%d\n", i)) {
			t.Fatalf("write %d not accepted", i)
		}
	}

	waitFor(t, func() bool { return rb.DroppedCount() >= n }, 5*time.Second, time.Millisecond,
		"failed flush records to be counted in DroppedCount")
	t.Logf("after failed flushes: DroppedCount=%d, underlying writes attempted=%d", rb.DroppedCount(), fw.calls.Load())

	if err := rb.Close(); err != nil {
		t.Fatalf("Close against failing sink: %v", err)
	}
}

// TestRingBuffer_ZeroLengthWrite: zero-length records must be accepted without
// hanging the drain or Close.
func TestRingBuffer_ZeroLengthWrite(t *testing.T) {
	t.Parallel()

	rec := &rbRecordWriter{}
	rb := NewRingBuffer(rec, 16)

	rb.Write([]byte{})
	rb.Write([]byte("after zero\n"))

	done := make(chan struct{})
	go func() {
		_ = rb.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RingBuffer.Close() hung after zero-length write")
	}
}

// TestNewRingBuffer_SizeNormalization: non-power-of-2 and sub-minimum sizes
// must normalise to a working queue rather than breaking mask arithmetic.
func TestNewRingBuffer_SizeNormalization(t *testing.T) {
	t.Parallel()

	for _, size := range []int{-1, 0, 1, 3, 100, 1025} {
		rec := &rbRecordWriter{}
		rb := NewRingBuffer(rec, size)

		ok := rb.Write([]byte("hello\n"))
		waitFor(t, func() bool { return rec.String() != "" }, 5*time.Second, 5*time.Millisecond,
			fmt.Sprintf("size=%d: data should flush", size))

		if err := rb.Close(); err != nil {
			t.Errorf("size=%d: Close: %v", size, err)
		}
		if !ok {
			t.Errorf("size=%d: first write rejected on an empty queue", size)
		}
		if got := rec.String(); got != "hello\n" {
			t.Errorf("size=%d: delivered %q, want %q", size, got, "hello\n")
		}
		// Post-close writes must be rejected for every normalised size.
		if rb.Write([]byte("late")) {
			t.Errorf("size=%d: post-close write accepted", size)
		}
	}
}

// TestRingBuffer_ConcurrentWrites verifies that concurrent writes work
// correctly and that output is flushed under load.
func TestRingBuffer_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	rec := &rbRecordWriter{}
	// Larger queue to avoid drops during concurrent writes.
	rb := NewRingBuffer(rec, 512)

	const numGoroutines = 10
	const writesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(_ int) {
			defer wg.Done()
			for range writesPerGoroutine {
				rb.Write([]byte("test message\n"))
			}
		}(i)
	}

	wg.Wait()

	// Close drains everything accepted, so the byte count must be exact.
	_ = rb.Close()

	total := uint64(numGoroutines * writesPerGoroutine)
	dropped := rb.DroppedCount()
	if dropped > total {
		t.Fatalf("dropped %d exceeds total writes %d", dropped, total)
	}
	expectedBytes := (total - dropped) * uint64(len("test message\n"))
	if got := uint64(len(rec.String())); got != expectedBytes {
		t.Errorf("flushed %d bytes, expected %d (dropped=%d)", got, expectedBytes, dropped)
	}
}

// TestRingBuffer_HighThroughput verifies high-throughput correctness: every
// accepted (goroutine, seq) payload is delivered exactly once — no duplicates
// (double-delivery) and no orphans (accepted-but-lost).
func TestRingBuffer_HighThroughput(t *testing.T) {
	t.Parallel()

	rec := &rbRecordWriter{}
	rb := NewRingBuffer(rec, 65536)

	const numGoroutines = 50
	const writesPerGoroutine = 1000
	const total = numGoroutines * writesPerGoroutine

	// Track which (goroutineID, seq) pairs were actually written (not dropped).
	written := make([]atomic.Bool, total)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := range numGoroutines {
		go func(g int) {
			defer wg.Done()
			for s := range writesPerGoroutine {
				payload := fmt.Sprintf("g%d-s%d\n", g, s)
				if rb.Write([]byte(payload)) {
					written[g*writesPerGoroutine+s].Store(true)
				}
			}
		}(g)
	}

	wg.Wait()

	_ = rb.Close()

	dropped := rb.DroppedCount()
	t.Logf("total=%d dropped=%d", total, dropped)

	// Drops < 1% is the steady-state target. More than that suggests the queue
	// is undersized or the drainer is falling behind.
	if dropped > uint64(total)/100 {
		t.Errorf("dropped %d/%d (%.1f%%) — exceeds 1%% budget; check queue size vs write rate",
			dropped, total, float64(dropped)/float64(total)*100)
	}

	seen := make(map[string]int, total)
	for line := range strings.SplitSeq(strings.TrimRight(rec.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		seen[line]++
	}

	// No duplicates: each line must appear at most once.
	for line, count := range seen {
		if count > 1 {
			t.Errorf("duplicate entry %q appears %d times", line, count)
		}
	}

	// Every accepted entry must appear in the output exactly once.
	for g := range numGoroutines {
		for s := range writesPerGoroutine {
			if !written[g*writesPerGoroutine+s].Load() {
				continue // rejected before entering the queue, skip
			}
			key := fmt.Sprintf("g%d-s%d", g, s)
			if seen[key] == 0 {
				t.Errorf("entry %q was accepted but not delivered — data loss", key)
			}
		}
	}
}

// TestRingBuffer_OverflowHandling verifies that a small queue under sustained
// load keeps functioning and delivers everything it accepted.
func TestRingBuffer_OverflowHandling(t *testing.T) {
	t.Parallel()

	rec := &rbRecordWriter{}
	// Small queue to force overflow.
	rb := NewRingBuffer(rec, 8)

	const numWrites = 1000
	accepted := 0
	for range numWrites {
		if rb.Write([]byte("test\n")) {
			accepted++
		}
	}

	_ = rb.Close()

	dropped := rb.DroppedCount()
	t.Logf("Dropped %d messages out of %d writes with small buffer", dropped, numWrites)

	if rec.String() == "" {
		t.Error("queue should have flushed some data")
	}
	if got := len(rbSplitLines(rec.String())); got != accepted {
		t.Errorf("delivered %d records, want %d accepted", got, accepted)
	}
}

// TestRingBuffer_CloseFlushesAll verifies that Close() drains all pending
// entries: every accepted record is delivered even when the sink stalls until
// Close time. Determinism: gated-sink handshake parks the drainer after one
// record, so exactly capacity further writes are guaranteed accepted — no
// retry loop that can be starved under parallel test load.
func TestRingBuffer_CloseFlushesAll(t *testing.T) {
	t.Parallel()

	const queueCap = 64
	gw := newRBGateWriter()
	rb := NewRingBuffer(gw, queueCap)

	if !rb.Write([]byte("msg\n")) {
		t.Fatal("first write rejected on an empty queue")
	}
	select {
	case <-gw.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("drainer never reached the underlying writer")
	}

	const numWrites = queueCap + 1 // +1 for the record in the parked batch
	for i := 1; i < numWrites; i++ {
		if !rb.Write([]byte("msg\n")) {
			t.Fatalf("write %d rejected while the drainer was parked and the queue had space", i)
		}
	}

	// Unblocking the sink inside Close: the drain must still deliver every
	// accepted record before Close returns.
	close(gw.gate)
	if err := rb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := len(rbSplitLines(gw.String())); got != numWrites {
		t.Errorf("Close flushed %d records, want %d", got, numWrites)
	}
}

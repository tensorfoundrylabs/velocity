package velocity

import (
	"context"
	"sync"
	"testing"
	"time"
)

// makeEntry produces a minimal *Entry suitable for ring writer tests.
// The entry is not pool-managed — tests own it directly.
func makeEntry(level Level, msg string, fields ...Field) *Entry {
	e := &Entry{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
		Fields:  fields,
	}
	e.written.Store(1) // mark written so Release is safe if called
	e.refCount.Store(1)
	return e
}

// --- Construction ---

func TestRingBufferWriter_CapacityEnforced(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   int
		want int
	}{
		{0, minRingBufferWriterCapacity},
		{-5, minRingBufferWriterCapacity},
		{1, minRingBufferWriterCapacity},
		{2, 2},
		{100, 100},
	}

	for _, tc := range cases {
		r := NewRingBufferWriter(tc.in)
		if r.capacity != tc.want {
			t.Errorf("capacity(%d): got %d, want %d", tc.in, r.capacity, tc.want)
		}
	}
}

func TestRingBufferWriter_RedactionMark(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4, RingRedactionMark("***"))
	if r.redactionMark != "***" {
		t.Errorf("got %q, want %q", r.redactionMark, "***")
	}

	// Default
	r2 := NewRingBufferWriter(4)
	if r2.redactionMark != redactedMark {
		t.Errorf("got %q, want %q", r2.redactionMark, redactedMark)
	}
}

// --- Write and wrapping ---

func TestRingBufferWriter_WritePopulatesRing(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)

	e := makeEntry(LevelWarn, "hello", String("k", "v"))
	if err := r.Write(e); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := r.Stats()
	if stats.Fill != 1 {
		t.Errorf("fill: got %d, want 1", stats.Fill)
	}
	if stats.Total != 1 {
		t.Errorf("total: got %d, want 1", stats.Total)
	}
	// Confirm the level round-trips correctly through the snapshot.
	snaps := r.Snapshot(1)
	if len(snaps) == 0 || snaps[0].Level != LevelWarn {
		t.Errorf("snapshot level: got %v, want %v", snaps[0].Level, LevelWarn)
	}
}

func TestRingBufferWriter_NilWriteIsNoop(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)
	if err := r.Write(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Stats().Total != 0 {
		t.Error("nil write should not increment total")
	}
}

func TestRingBufferWriter_WrapsAtCapacity(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(3)

	for i := range 5 {
		e := makeEntry(LevelInfo, "msg")
		e.Fields = []Field{Int("i", i)}
		_ = r.Write(e)
	}

	stats := r.Stats()
	if stats.Fill != 3 {
		t.Errorf("fill: got %d, want 3", stats.Fill)
	}
	// Two entries were displaced.
	if stats.Drops != 2 {
		t.Errorf("drops: got %d, want 2", stats.Drops)
	}
	if stats.Total != 5 {
		t.Errorf("total: got %d, want 5", stats.Total)
	}

	// Most recent 3 entries should have i=2,3,4
	snaps := r.Snapshot(3)
	if len(snaps) != 3 {
		t.Fatalf("snapshot len: got %d, want 3", len(snaps))
	}
	for idx, want := range []string{"2", "3", "4"} {
		if len(snaps[idx].Fields) == 0 || snaps[idx].Fields[0].Value != want {
			t.Errorf("snap[%d].Fields[0].Value: got %q, want %q", idx, snaps[idx].Fields[0].Value, want)
		}
	}
}

// --- Snapshot ---

func TestRingBufferWriter_SnapshotReturnsRecentN(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(10)

	for i := range 7 {
		_ = r.Write(makeEntry(LevelInfo, "msg", Int("i", i)))
	}

	snaps := r.Snapshot(3)
	if len(snaps) != 3 {
		t.Fatalf("len: got %d, want 3", len(snaps))
	}
	// Should be entries i=4, i=5, i=6
	for idx, want := range []string{"4", "5", "6"} {
		got := snaps[idx].Fields[0].Value
		if got != want {
			t.Errorf("snap[%d]: got %q, want %q", idx, got, want)
		}
	}
}

func TestRingBufferWriter_SnapshotClampsToFill(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(10)
	_ = r.Write(makeEntry(LevelInfo, "only"))

	snaps := r.Snapshot(100)
	if len(snaps) != 1 {
		t.Errorf("len: got %d, want 1", len(snaps))
	}
}

func TestRingBufferWriter_SnapshotZeroOrEmptyReturnsNil(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)

	if s := r.Snapshot(0); s != nil {
		t.Errorf("expected nil for n=0, got %v", s)
	}
	if s := r.Snapshot(5); s != nil {
		t.Errorf("expected nil for empty ring, got %v", s)
	}
}

func TestRingBufferWriter_SnapshotDeepCopy(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)
	_ = r.Write(makeEntry(LevelInfo, "original", String("key", "value")))

	snaps := r.Snapshot(1)

	// Write new entries to potentially overwrite the old ring slot.
	for range 5 {
		_ = r.Write(makeEntry(LevelInfo, "new"))
	}

	// The snapshot should still reflect the original.
	if snaps[0].Message != "original" {
		t.Errorf("snapshot message mutated: %q", snaps[0].Message)
	}
	if len(snaps[0].Fields) == 0 || snaps[0].Fields[0].Value != "value" {
		t.Errorf("snapshot fields mutated: %v", snaps[0].Fields)
	}
}

// --- Subscribe ---

func TestRingBufferWriter_SubscribeReceivesEntries(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(10)
	ctx := t.Context()

	ch := r.Subscribe(ctx, 8)

	messages := []string{"alpha", "beta", "gamma"}
	for _, msg := range messages {
		_ = r.Write(makeEntry(LevelInfo, msg))
	}

	received := make([]string, 0, len(messages))
	timeout := time.After(time.Second)
	for range messages {
		select {
		case snap := <-ch:
			received = append(received, snap.Message)
		case <-timeout:
			t.Fatalf("timed out waiting for subscriber entry; received %v", received)
		}
	}

	for i, want := range messages {
		if received[i] != want {
			t.Errorf("received[%d]: got %q, want %q", i, received[i], want)
		}
	}
}

func TestRingBufferWriter_SubscribeClosesOnCtxCancel(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(10)
	ctx, cancel := context.WithCancel(context.Background())

	ch := r.Subscribe(ctx, 4)
	cancel()

	// Channel must close; don't block forever.
	select {
	case _, ok := <-ch:
		if ok {
			// drain any buffered entries and wait for close
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close after ctx cancel")
	}
}

func TestRingBufferWriter_SubscribeDropsOnSlowConsumer(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(20)
	ctx := t.Context()

	// bufSize=1 means the channel fills after one unread entry.
	ch := r.Subscribe(ctx, 1)

	// Write enough entries to overflow the channel buffer while the consumer
	// is not reading — most will be dropped.
	for range 10 {
		_ = r.Write(makeEntry(LevelInfo, "flood"))
	}

	// At least some drops must have occurred.
	if r.drops.Load() == 0 {
		t.Error("expected at least one drop for slow consumer")
	}

	_ = ch // suppress unused warning; consumer intentionally not reading
}

// --- Stats ---

func TestRingBufferWriter_Stats(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(5)

	for range 3 {
		_ = r.Write(makeEntry(LevelInfo, "x"))
	}

	s := r.Stats()

	if s.Capacity != 5 {
		t.Errorf("Capacity: got %d, want 5", s.Capacity)
	}
	if s.Fill != 3 {
		t.Errorf("Fill: got %d, want 3", s.Fill)
	}
	if s.Total != 3 {
		t.Errorf("Total: got %d, want 3", s.Total)
	}
	if s.Drops != 0 {
		t.Errorf("Drops: got %d, want 0", s.Drops)
	}
}

// --- IsTrusted ---

func TestRingBufferWriter_IsTrustedDefaultFalse(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)
	if r.IsTrusted() {
		t.Error("default IsTrusted should be false")
	}
}

func TestRingBufferWriter_SetTrustedFlipsFlag(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)
	r.SetTrusted(true)
	if !r.IsTrusted() {
		t.Error("IsTrusted should be true after SetTrusted(true)")
	}
}

// Verify that WriterTrusted() integration works end-to-end via the logger.
// The trust flag must survive the AddWriter path so IsTrusted() reflects it.
func TestRingBufferWriter_TrustedViaLoggerAddWriter(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)

	log := New(WithNop())
	log.AddWriter("ring", r, WriterTrusted())

	// The MultiWriter stores isTrusted on the worker, not on the writer itself.
	// SetTrusted() is NOT called by AddWriter — that is intentional: MultiWriter
	// holds the flag. IsTrusted() on the writer remains false unless the caller
	// explicitly sets it via SetTrusted. This matches the design: trust lives in
	// writerOptions, not in the writer struct.
	//
	// Verify MultiWriter's IsTrusted accessor instead.
	if !log.additionalWriters.IsTrusted("ring") {
		t.Error("writer registered with WriterTrusted() should report trusted in MultiWriter")
	}

	_ = log.Close()
}

// --- Close ---

func TestRingBufferWriter_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)
	if err := r.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestRingBufferWriter_PostCloseWriteDrops(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)
	_ = r.Write(makeEntry(LevelInfo, "before"))

	_ = r.Close()

	if err := r.Write(makeEntry(LevelInfo, "after")); err != nil {
		t.Fatalf("write after close should not error: %v", err)
	}

	// Total should not have increased after close.
	if r.Stats().Total != 1 {
		t.Errorf("total after post-close write: got %d, want 1", r.Stats().Total)
	}
}

func TestRingBufferWriter_CloseClosesSubscribers(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(4)
	ctx := t.Context()

	ch := r.Subscribe(ctx, 4)
	_ = r.Close()

	// Close() shuts down subscriber channels; the channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			// Drain remaining.
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel not closed after writer Close()")
	}
}

// --- Concurrency ---

func TestRingBufferWriter_ConcurrentWriteSnapshotSubscribe(t *testing.T) {
	t.Parallel()

	r := NewRingBufferWriter(64)
	ctx, cancel := context.WithCancel(context.Background())

	ch := r.Subscribe(ctx, 32)

	// Separate WaitGroups so we can cancel ctx after writers finish,
	// then wait for the subscriber drainer which exits once ch closes.
	var producersWg sync.WaitGroup
	var consumerWg sync.WaitGroup

	const numWriters = 8
	const perWriter = 200

	// Concurrent writers.
	for w := range numWriters {
		producersWg.Add(1)
		go func(id int) {
			defer producersWg.Done()
			for i := range perWriter {
				_ = r.Write(makeEntry(LevelInfo, "concurrent", Int("w", id), Int("i", i)))
			}
		}(w)
	}

	// Concurrent snapshotter — counted with producers since it finishes before cancel.
	producersWg.Add(1)
	go func() {
		defer producersWg.Done()
		for range numWriters * perWriter / 10 {
			_ = r.Snapshot(10)
		}
	}()

	// Subscriber drainer: exits when ch closes (after cancel).
	consumerWg.Add(1)
	go func() {
		defer consumerWg.Done()
		for range ch { //nolint:revive // intentionally discarding subscriber entries
		}
	}()

	// Cancel after all writes and snapshots are done so the subscriber
	// goroutine inside Subscribe exits and closes ch.
	producersWg.Wait()
	cancel()
	consumerWg.Wait()

	s := r.Stats()
	if s.Total != numWriters*perWriter {
		t.Errorf("total: got %d, want %d", s.Total, numWriters*perWriter)
	}
}

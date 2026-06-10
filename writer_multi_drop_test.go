package velocity

import (
	"sync"
	"testing"
	"time"
)

// TestMultiWriter_DroppedCount_NilReceiver verifies that DroppedCount on a nil
// MultiWriter returns zero rather than panicking.
func TestMultiWriter_DroppedCount_NilReceiver(t *testing.T) {
	t.Parallel()

	var mw *MultiWriter
	if got := mw.DroppedCount(); got != 0 {
		t.Errorf("DroppedCount on nil: want 0, got %d", got)
	}
}

// TestMultiWriter_DroppedCount_NoDrops verifies the counter stays at zero when
// no drops occur under normal load (fewer entries than channel capacity).
func TestMultiWriter_DroppedCount_NoDrops(t *testing.T) {
	t.Parallel()

	mw := NewMultiWriter()
	defer func() { _ = mw.Close() }()

	var received int64
	fn := WriterFunc(func(_ *Entry) error {
		return nil
	})
	mw.AddWriter("sink", &fn)

	// Well below channel capacity (256) so nothing should be dropped.
	for range 10 {
		e := GetEntry()
		e.SetMessage("ok")
		_ = mw.Write(e)
		e.Release()
	}

	// Let the worker drain.
	waitFor(t, func() bool {
		_ = received // keep the closure live
		return mw.Stats()["sink"] == 0
	}, 500*time.Millisecond, 5*time.Millisecond, "channel should drain")

	if got := mw.DroppedCount(); got != 0 {
		t.Errorf("want 0 drops on lightly-loaded writer, got %d", got)
	}
}

// TestMultiWriter_DroppedCount_ChannelFull verifies that DroppedCount increments
// when a worker's channel is saturated and Write falls through to the default branch.
func TestMultiWriter_DroppedCount_ChannelFull(t *testing.T) {
	t.Parallel()

	mw := NewMultiWriter()
	defer func() { _ = mw.Close() }()

	// blockCh holds the worker in its write call so the buffered channel fills up.
	blockCh := make(chan struct{})
	// ready signals that the worker has picked up the first entry and is now blocked.
	ready := make(chan struct{})

	var once sync.Once
	fn := WriterFunc(func(_ *Entry) error {
		// Signal the test on first entry then stall until released.
		once.Do(func() { close(ready) })
		<-blockCh
		return nil
	})
	mw.AddWriter("blocked", &fn)

	// Send the first entry so the worker is occupied and signals ready.
	first := GetEntry()
	first.SetMessage("first")
	_ = mw.Write(first)
	first.Release()

	// Wait until the worker is inside its write call and stalled.
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not signal ready in time")
	}

	// Flood the channel beyond capacity (256) so sends start dropping.
	// The worker is blocked so the buffer fills quickly.
	const flood = 512
	for range flood {
		e := GetEntry()
		e.SetMessage("flood")
		_ = mw.Write(e)
		e.Release()
	}

	// Unblock the worker so Close() can drain.
	close(blockCh)

	dropped := mw.DroppedCount()
	if dropped == 0 {
		t.Error("expected DroppedCount > 0 after flooding a blocked worker channel, got 0")
	}
	t.Logf("DroppedCount = %d after flooding %d entries into a blocked channel", dropped, flood)
}

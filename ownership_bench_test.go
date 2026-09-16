package velocity

import (
	"bytes"
	"testing"
	"time"
)

// R07 ownership-cost benchmarks: Formatter/BytesBuffer.String return stable
// copies (one allocation each by design — see the ownership tests) and ring
// subscriber delivery clones the FieldSnapshot slice so each subscriber owns
// its memory. These numbers are the honest cost of those guarantees and are
// reported as absolute B/op and allocs/op in the hardening-finish record.

func BenchmarkOwnership_Formatter_String(b *testing.B) {
	f := NewFormatter(256)
	f.WriteString("request completed service=bench status=200")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = f.String()
	}
}

func BenchmarkOwnership_BytesBuffer_String(b *testing.B) {
	buf := NewBytesBuffer(bytes.NewBuffer(make([]byte, 0, 256)))
	buf.WriteString("request completed service=bench status=200")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = buf.String()
	}
}

func benchOwnershipEntry() *Entry {
	e := GetEntry()
	e.SetLevel(LevelInfo)
	e.SetMessage("request completed")
	e.SetTime(time.Now())
	e.WithFields(String("service", "bench"), Int("status", 200))
	return e
}

// BenchmarkOwnership_Ring_SubscriberDelivery: one subscriber receiving a
// cloned field snapshot per entry. The clone is the R07 ownership fix; its
// allocations are expected and must be reported, not optimised away.
func BenchmarkOwnership_Ring_SubscriberDelivery(b *testing.B) {
	r := NewRingBufferWriter(1024)
	ch := r.Subscribe(b.Context(), 64)
	e := benchOwnershipEntry()
	defer e.Release()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e.Retain()
		_ = r.WriteSecure(e, false, "<secure>")
		<-ch
		e.Release()
	}
}

// BenchmarkOwnership_Ring_Write_NoSubscriber: the no-subscriber ring write
// for contrast — the ring retains its own snapshot but no per-subscriber
// clone is needed.
func BenchmarkOwnership_Ring_Write_NoSubscriber(b *testing.B) {
	r := NewRingBufferWriter(1024)
	e := benchOwnershipEntry()
	defer e.Release()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e.Retain()
		_ = r.WriteSecure(e, false, "<secure>")
		e.Release()
	}
}

// BenchmarkOwnership_Snapshot: the diagnostic Snapshot(n) path — each call
// deep-copies field slices so the caller owns its memory.
func BenchmarkOwnership_Snapshot(b *testing.B) {
	r := NewRingBufferWriter(256)
	e := benchOwnershipEntry()
	for range 256 {
		_ = r.WriteSecure(e, false, "<secure>")
	}
	e.Release()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = r.Snapshot(64)
	}
}

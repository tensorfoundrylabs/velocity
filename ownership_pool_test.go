package velocity

// R07 ownership regressions, promoted from the verification battery: final
// Entry release clears pointer-bearing state before pooling, non-final
// releases keep it, oversized storage is capped, and BytesBuffer/Formatter
// String results remain stable across modification, Reset and pool reuse.

import (
	"bytes"
	"sync"
	"testing"
)

// TestOwnership_FinalReleaseClearsPointerFields: the final release must clear
// message, caller, function, logger and every populated Field slot so a pooled
// entry cannot hold references (including secrets) to a previous use.
// Not parallel: the test inspects the entry after it has returned to the
// shared pool, which is only race-free when no other test pulls from the pool.
func TestOwnership_FinalReleaseClearsPointerFields(t *testing.T) {
	e := GetEntry()
	e.SetLevel(LevelWarn)
	e.SetMessage("previous message")
	e.Caller = "previous.go"
	e.Function = "previous.Func"
	e.Line = 42
	e.logger = nil
	e.WithFields(
		String("s", "previous-string"),
		Secure("sec", "previous-secret"),
		Int("n", 7),
	)
	prevFields := e.Fields

	e.Release() // final: refCount was 1

	if e.Message != "" {
		t.Errorf("Message not cleared after final release: %q", e.Message)
	}
	if e.Caller != "" {
		t.Errorf("Caller not cleared after final release: %q", e.Caller)
	}
	if e.Function != "" {
		t.Errorf("Function not cleared after final release: %q", e.Function)
	}
	if e.logger != nil {
		t.Error("logger reference not cleared after final release")
	}
	for i, f := range prevFields {
		if f.Key != "" {
			t.Errorf("field %d Key not cleared: %q", i, f.Key)
		}
		if f.value != nil {
			t.Errorf("field %d value pointer not cleared", i)
		}
		if f.num != 0 {
			t.Errorf("field %d num not cleared: %d", i, f.num)
		}
		if f.Type != FieldTypeUnknown {
			t.Errorf("field %d Type not cleared: %d", i, f.Type)
		}
	}
}

// TestOwnership_NonFinalReleaseKeepsFields: a release while another owner
// still holds the entry must not clear anything — the remaining owner needs
// the contents.
func TestOwnership_NonFinalReleaseKeepsFields(t *testing.T) {
	t.Parallel()

	e := GetEntry()
	e.SetMessage("still referenced")
	e.WithFields(String("s", "value"), Secure("sec", "plaintext"))

	e.Retain() // second owner (refCount 2)
	e.Release()

	if e.Message != "still referenced" {
		t.Errorf("Message cleared on non-final release: %q", e.Message)
	}
	if got := e.Fields[0].Key; got != "s" {
		t.Errorf("field cleared on non-final release: %q", got)
	}
	if e.Fields[1].value == nil {
		t.Error("secure field pointer cleared on non-final release")
	}

	e.Release() // now final; balance the pool
}

// TestOwnership_FinalReleaseDropsOversizedFieldSlice: field storage beyond the
// 64-slot pool cap is dropped rather than pooled indefinitely. Not parallel:
// post-release inspection.
func TestOwnership_FinalReleaseDropsOversizedFieldSlice(t *testing.T) {
	e := GetEntry()
	for range 100 {
		e.WithFields(String("f", "x"))
	}
	if cap(e.Fields) <= 64 {
		t.Fatalf("precondition: expected field capacity > 64, got %d", cap(e.Fields))
	}
	e.Release()
	if e.Fields != nil {
		t.Errorf("oversized field slice retained after final release (len=%d cap=%d)",
			len(e.Fields), cap(e.Fields))
	}
}

// TestOwnership_ConcurrentRelease_LastOwnerClears: with two owners releasing
// concurrently, exactly one performs the final clear and the cleared state is
// race-free under -race. Not parallel: post-release reads.
func TestOwnership_ConcurrentRelease_LastOwnerClears(t *testing.T) {
	const rounds = 500
	for r := range rounds {
		e := GetEntry()
		e.SetMessage("secret payload " + string(rune('A'+r%26)))
		e.WithFields(String("s", "v"))
		e.Retain() // second owner: refCount is now 2

		var start sync.WaitGroup
		start.Add(2)
		var done sync.WaitGroup
		done.Add(2)
		for range 2 {
			go func() {
				start.Done()
				start.Wait() // both goroutines hold a reference before releasing
				e.Release()
				done.Done()
			}()
		}
		done.Wait()

		if e.Message != "" {
			t.Fatalf("round %d: entry not cleared after all owners released: %q", r, e.Message)
		}
	}
}

// TestOwnership_BytesBufferStringStable: BytesBuffer.String returns a copy, so
// the string survives later writes and a Reset of the underlying buffer. This
// is the R07 stable-string contract; BenchmarkOwnership_BytesBuffer_String
// reports its allocation cost.
func TestOwnership_BytesBufferStringStable(t *testing.T) {
	t.Parallel()

	raw := bytes.NewBuffer(make([]byte, 0, 512))
	b := NewBytesBuffer(raw)

	b.WriteString("first-entry-message")
	s1 := b.String()
	b.WriteString("-appended")
	s2 := b.String()

	if s1 != "first-entry-message" {
		t.Errorf("s1 mutated by later writes: %q", s1)
	}
	if s2 != "first-entry-message-appended" {
		t.Errorf("s2 wrong: %q", s2)
	}

	raw.Reset()
	if got := b.String(); got != "" {
		t.Errorf("after Reset String should be empty, got %q", got)
	}
	if s1 != "first-entry-message" {
		t.Errorf("s1 mutated by Reset: %q", s1)
	}
}

const stablePrefixResult = "stable-prefix42"

// TestOwnership_FormatterStringStable: Formatter.String results remain stable
// across further writes, Release and pool reuse by a new formatter.
func TestOwnership_FormatterStringStable(t *testing.T) {
	t.Parallel()

	f := NewFormatter(HintConsoleLog)
	f.WriteString("stable-prefix").WriteInt(42)
	s := f.String()

	f.WriteString("more-data-overwrites-nothing")
	if s != stablePrefixResult {
		t.Errorf("String result changed after further writes: %q", s)
	}

	f.Release() // buffer returns to the pool and may be reused
	if s != stablePrefixResult {
		t.Errorf("String result changed after Release: %q", s)
	}

	// Even after the pool hands the same buffer to a new formatter, the
	// previously returned string must be untouched.
	g := NewFormatter(HintConsoleLog)
	g.WriteString("XXXXXXXX")
	if s != stablePrefixResult {
		t.Errorf("String result changed after pool reuse: %q", s)
	}
	g.Release()
}

// TestOwnership_SnapshotPoolClearsOnReturn: ring field-snapshot slices are
// cleared through their populated storage before returning to the pool, so a
// pooled slice holds no stale string references (including redacted plaintext
// on trusted rings). Verifies the helper directly since the ring only recycles
// slots on overflow.
func TestOwnership_SnapshotPoolClearsOnReturn(t *testing.T) {
	t.Parallel()

	fs := []FieldSnapshot{
		{Key: "secret", Value: "classified"},
		{Key: "k2", Value: "v2"},
	}
	putFieldSnapshot(fs)

	// Whatever the pool hands back must not expose the returned keys within
	// its populated length; putFieldSnapshot truncates to zero and cleared.
	got := testGetFieldSnapshots(2)
	for i, f := range got {
		if f.Key != "" || f.Value != "" {
			t.Errorf("stale snapshot slot %d after pool return: %+v", i, f)
		}
	}
}

// testGetFieldSnapshots borrows a snapshot slice from the shared pool. The
// pool may hand back a fresh slice, in which case the stale-check above is
// vacuously true — the regression it guards only fails when the same backing
// array comes back uncleared.
func testGetFieldSnapshots(n int) []FieldSnapshot {
	ptr, ok := fieldSnapshotPool.Get().(*[]FieldSnapshot)
	if !ok || ptr == nil {
		return nil
	}
	fs := *ptr
	// Re-extend over the previously populated storage: putFieldSnapshot
	// truncates to zero length after clearing, so the stale references — if
	// any survived — sit between len and cap.
	if cap(fs) >= n {
		fs = fs[:n]
	}
	return fs
}

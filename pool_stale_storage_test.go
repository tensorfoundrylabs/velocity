package velocity

// Regression coverage for the field-pool helpers: PutFieldSlice must clear
// through the full backing array (Field carries unsafe.Pointer values, so
// populated storage between len and cap would survive pooling). The snapshot
// wrapper-pool allocation test lives in pool_snapshot_alloc_norace_test.go —
// the race detector's own bookkeeping would skew a heap measurement.

import "testing"

// PutFieldSlice clears the entire backing array, including stale storage
// between len and cap left by earlier re-slicing — the deterministic check
// observes the caller's alias, so it does not depend on pool identity.
func TestPutFieldSlice_ClearsThroughCapacity(t *testing.T) {
	t.Parallel()

	backing := make([]Field, 6)
	for i := range backing {
		backing[i] = String("stale"+string(rune('0'+i)), "TOPSECRETPASSWORD")
	}
	// Hand the pool a slice whose len under-reports the populated extent.
	PutFieldSlice(backing[:2])

	for i := range backing {
		if backing[i].Key != "" || backing[i].value != nil {
			t.Errorf("slot %d survived pooling: Key=%q value=%v — Field storage between len and cap must be cleared", i, backing[i].Key, backing[i].value)
		}
	}

	// Whatever the pool lends next starts empty.
	got := GetFieldSlice()
	if len(got) != 0 {
		t.Errorf("GetFieldSlice returned non-empty slice: len=%d", len(got))
	}

	// Boundary behaviour: nil and oversize slices are no-ops, not panics.
	PutFieldSlice(nil)
	PutFieldSlice(make([]Field, 65))
}

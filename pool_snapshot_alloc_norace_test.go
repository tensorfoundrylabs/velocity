//go:build !race

package velocity

// The snapshot wrapper pool must make the get/put cycle allocation-free.
// Built without the race detector only: the detector's shadow bookkeeping
// allocates per operation and would drown out the signal being measured.

import (
	"runtime"
	"runtime/debug"
	"testing"
)

func TestFieldSnapshotPool_CycleIsAllocationFree(t *testing.T) {
	e := &Entry{
		Level:   LevelInfo,
		Message: "wp-cycle",
		Fields: []Field{
			String("k1", "v1"),
			String("k2", "v2"),
		},
	}

	// Warm both pools past their cold-start allocations.
	for range 100 {
		snap := toSnapshotSecure(e, false, "[REDACTED]")
		putFieldSnapshot(snap.Fields)
	}

	// Pin to one P: sync.Pool keeps per-P slots, so with the goroutine free to
	// migrate the cycle would show unrelated pool-churn misses, not wrapper
	// escapes. On a single P the private slot round-trips deterministically.
	oldP := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(oldP)
	oldGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGC)
	runtime.GC()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 1000 {
		snap := toSnapshotSecure(e, false, "[REDACTED]")
		putFieldSnapshot(snap.Fields)
	}
	runtime.ReadMemStats(&after)

	// The bound is counts, not bytes, and allows a small residue: sync.Pool's
	// internal chain nodes cost a handful of allocations per thousand
	// round-trips. The regression this guards — one heap allocation per
	// putFieldSnapshot from &fs escaping — would be >= 1000.
	if grew := after.Mallocs - before.Mallocs; grew >= 100 {
		t.Errorf("snapshot get/put cycle made %d allocations over 1000 iterations; wrapper handles are escaping the pool", grew)
	}
}

package velocity

import "sync/atomic"

// Sampler determines whether a log entry should be emitted.
// Implementations must be safe for concurrent use.
type Sampler interface {
	// Sample returns true if the entry should be logged.
	// Called on every log attempt when sampling is enabled.
	Sample(level Level, msg string) bool
}

// CountSampler logs the first N entries, then every Mth entry thereafter.
// This is useful for high-volume logging where you want to capture
// initial entries but reduce volume over time.
type CountSampler struct {
	// Initial is the number of entries to log initially per level.
	// After Initial entries, only every Thereafter-th entry is logged.
	Initial uint64

	// Thereafter logs every Nth entry after Initial is exhausted.
	// If 0, no entries are logged after Initial.
	Thereafter uint64

	// counters per level (use array indexed by Level)
	counters [8]atomic.Uint64 // Supports levels 0-7
}

// Sample implements Sampler.
func (s *CountSampler) Sample(level Level, _ string) bool {
	if s == nil {
		return true // nil sampler = log everything
	}

	idx := int(level)
	if idx < 0 || idx >= len(s.counters) {
		return true // unknown level, log it
	}

	count := s.counters[idx].Add(1)

	// Log first Initial entries
	if count <= s.Initial {
		return true
	}

	// After Initial, log every Thereafter-th entry
	if s.Thereafter == 0 {
		return false
	}

	return (count-s.Initial)%s.Thereafter == 0
}

// NewCountSampler creates a sampler that logs the first `initial` entries,
// then every `thereafter`-th entry. Common values: initial=100, thereafter=100.
func NewCountSampler(initial, thereafter uint64) *CountSampler {
	return &CountSampler{
		Initial:    initial,
		Thereafter: thereafter,
	}
}

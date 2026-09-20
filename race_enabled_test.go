//go:build race

package velocity

// raceEnabled reports that the race detector is active. Its instrumentation
// forces sync.Pool through pinSlow, which allocates on every operation, so
// allocation-window assertions cannot hold under -race.
const raceEnabled = true

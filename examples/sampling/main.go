// Sampling example. In high-throughput services, logging every event is too
// expensive. CountSampler lets through the first N entries unconditionally, then
// every Mth entry after that. This keeps early signal while preventing log flooding.
package main

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	// Count every entry that actually reaches a writer. We use an atomic
	// so there are no races if the async MultiWriter dispatches concurrently.
	var written atomic.Int64
	counter := velocity.WriterFunc(func(_ *velocity.Entry) error {
		written.Add(1)
		return nil
	})

	// Log the first 5 messages, then every 100th one after that.
	// This is a common pattern: capture the burst at startup, then sample steady-state noise.
	sampler := velocity.NewCountSampler(5, 100)

	log := velocity.New(
		velocity.WithConsoleOutput(os.Stdout),
		velocity.WithLevel(velocity.LevelInfo),
		velocity.WithSampler(sampler),
	)
	log.AddWriter("counter", counter)

	const total = 1000

	for i := range total {
		log.Info("Cache miss", velocity.Int("key", i))
	}

	// Flush the async counter writer before reading the tally.
	if err := log.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close error: %v\n", err)
	}

	n := written.Load()
	ratio := float64(total-n) / float64(total) * 100

	fmt.Printf("\nSampling results:\n")
	fmt.Printf("  Attempted : %d\n", total)
	fmt.Printf("  Written   : %d\n", n)
	fmt.Printf("  Dropped   : %d (%.1f%% reduction)\n", total-n, ratio)
}

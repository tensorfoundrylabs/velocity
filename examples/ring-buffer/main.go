// Ring buffer writer example. Shows how to attach a RingBufferWriter to a
// logger for in-process log capture — the pattern foundryos uses to serve
// recent log entries over an HTTP debug endpoint.
//
// Two access patterns are demonstrated:
//  1. Snapshot — pull the most recent N entries on demand (HTTP handler style)
//  2. Subscribe — push every new entry to a channel (live tail / alerting style)
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	// Attach a ring that holds the last 100 entries.
	// Untrusted by default — Phase 4 will redact Secure fields here.
	ring := velocity.NewRingBufferWriter(100)

	log := velocity.New(
		velocity.WithDevelopment(),
		velocity.WithConsoleOutput(os.Stdout),
	)
	log.AddWriter("ring", ring)
	defer func() { _ = log.Close() }()

	log.Info("Logger ready, ring attached")

	// Simulate a few requests so the ring has something to show.
	routes := []string{"/api/users", "/api/orders", "/api/health"}
	for i, route := range routes {
		log.Info("Request handled",
			velocity.String("route", route),
			velocity.Int("status", 200),
			velocity.Duration("latency", time.Duration(i+1)*15*time.Millisecond),
		)
	}
	log.Warn("Slow query detected", velocity.Duration("elapsed", 320*time.Millisecond))
	log.Error("Upstream timeout", velocity.String("service", "payments"))

	fmt.Println()

	// --- Pattern 1: snapshot (HTTP debug endpoint) ---
	//
	// Grab the last 3 entries. In a real service this is called inside an
	// http.HandlerFunc and the result is JSON-encoded into the response.
	snaps := ring.Snapshot(3)
	fmt.Printf("=== Snapshot: last %d entries ===\n", len(snaps))
	for _, s := range snaps {
		fmt.Printf("  [%s] %s", s.Level, s.Message)
		for _, f := range s.Fields {
			fmt.Printf("  %s=%s", f.Key, f.Value)
		}
		fmt.Println()
	}

	fmt.Println()

	// --- Pattern 2: subscribe (live tail) ---
	//
	// A background goroutine receives every new snapshot as it arrives.
	// The channel buffers 16 entries; slow consumers drop, not block.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := ring.Subscribe(ctx, 16)

	done := make(chan struct{})
	go func() {
		defer close(done)
		fmt.Println("=== Subscriber: live tail ===")
		for snap := range ch {
			fmt.Printf("  -> [%s] %s\n", snap.Level, snap.Message)
		}
		fmt.Println("  subscriber done")
	}()

	// Log a few more entries while the subscriber is active.
	for _, msg := range []string{"Cron job started", "Cron job finished"} {
		log.Info(msg)
	}

	// Wait for the subscriber goroutine to finish draining.
	<-done

	s := ring.Stats()
	fmt.Printf("\nRing stats: capacity=%d fill=%d total=%d drops=%d\n",
		s.Capacity, s.Fill, s.Total, s.Drops)
}

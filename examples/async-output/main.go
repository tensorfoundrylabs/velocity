// Async structured output example. WithAsyncOutput moves JSON writes off the
// logging goroutine: the caller formats each record, then hands the finished
// bytes to a bounded queue drained by a single background writer, so a slow
// sink never parks the request path on a syscall. When the queue fills,
// AsyncBlock back-pressures (nothing is lost) and AsyncDrop keeps the caller
// moving (losses are counted in StructuredDroppedCount). Close drains
// everything accepted before it returns.
package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
)

// slowSink simulates a laggy destination: a network shipper, a contended
// file, or a collector that is briefly behind.
type slowSink struct {
	mu     sync.Mutex
	sleep  time.Duration
	writes int
}

func (s *slowSink) Write(p []byte) (int, error) {
	time.Sleep(s.sleep)
	s.mu.Lock()
	s.writes++
	s.mu.Unlock()
	return len(p), nil
}

func (s *slowSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

func main() {
	// --- AsyncBlock: lossless back-pressure ------------------------------
	//
	// The queue rides out short stalls; once it fills, callers wait for the
	// drainer instead of losing records.
	const blockRecords = 300

	blockSink := &slowSink{sleep: time.Millisecond}
	blockLog := velocity.New(
		velocity.WithProduction(),
		velocity.WithStructuredOutput(blockSink),
		// Must come after WithProduction: presets reset the whole config.
		velocity.WithAsyncOutput(velocity.AsyncConfig{
			Queue:  256,
			OnFull: velocity.AsyncBlock,
		}),
	)

	start := time.Now()
	for i := range blockRecords {
		blockLog.Info("request handled", velocity.Int("seq", i))
	}
	elapsed := time.Since(start)

	fmt.Println("=== AsyncBlock: lossless back-pressure ===")
	fmt.Printf("logged %d records in %s while the sink slept 1ms per write\n",
		blockRecords, elapsed.Round(time.Millisecond))
	fmt.Printf("(synchronously the same calls would inherit ~%s of sink latency)\n\n",
		time.Duration(blockRecords)*time.Millisecond)

	// Close drains everything accepted: after it returns, every record is on
	// the sink and the drainer is retired.
	if err := blockLog.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close error: %v\n", err)
	}
	fmt.Printf("after Close: %d written, %d dropped\n\n",
		blockSink.count(), blockLog.StructuredDroppedCount())

	// --- AsyncDrop: never stall the caller --------------------------------
	//
	// A shallow queue plus drop policy: overflow is discarded and counted
	// rather than blocking the request path.
	const dropRecords = 5000

	dropSink := &slowSink{sleep: 2 * time.Millisecond}
	dropLog := velocity.New(
		velocity.WithProduction(),
		velocity.WithStructuredOutput(dropSink),
		velocity.WithAsyncOutput(velocity.AsyncConfig{
			Queue:  16,
			OnFull: velocity.AsyncDrop,
		}),
	)

	fmt.Println("=== AsyncDrop: never stall the caller ===")
	start = time.Now()
	for i := range dropRecords {
		dropLog.Info("request handled", velocity.Int("seq", i))

		// Poll the loss counter mid-stream, the way a metrics endpoint would.
		if (i+1)%1000 == 0 {
			fmt.Printf("  after %4d calls: %d dropped so far\n",
				i+1, dropLog.StructuredDroppedCount())
		}
	}
	elapsed = time.Since(start)

	fmt.Printf("logged %d records in %s while the sink slept 2ms per write\n",
		dropRecords, elapsed.Round(time.Millisecond))
	fmt.Printf("(synchronously the same calls would inherit ~%s of sink latency)\n",
		2*time.Duration(dropRecords)*time.Millisecond)

	if err := dropLog.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close error: %v\n", err)
	}

	written, dropped := dropSink.count(), dropLog.StructuredDroppedCount()
	fmt.Printf("after Close: %d written + %d dropped = %d logged\n",
		written, dropped, dropRecords)
}

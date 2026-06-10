// status-items demonstrates StatusItem rendering and Logger.Status routing.
//
// Run it directly for a TTY console with coloured badges:
//
//	go run ./examples/status-items
//
// Pipe it to see the plain (non-TTY) badge form:
//
//	go run ./examples/status-items | cat
//
// Add -json to write JSON to a file alongside the console output:
//
//	go run ./examples/status-items -json
package main

import (
	"flag"
	"os"
	"time"

	velocity "github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	jsonOut := flag.Bool("json", false, "also write JSON to status.log")
	flag.Parse()

	opts := []velocity.Option{
		velocity.WithDevelopment(),
	}
	if *jsonOut {
		f, err := os.Create("status.log")
		if err != nil {
			panic(err)
		}
		defer func() {
			if err := f.Close(); err != nil {
				panic(err)
			}
		}()
		opts = append(opts, velocity.WithStructuredOutput(f))
	}

	log := velocity.New(opts...)
	defer func() {
		if err := log.Close(); err != nil {
			panic(err)
		}
	}()

	log.Info("starting service health check")
	log.Newline()

	// Startup checklist: six services, mixed outcomes.
	log.Status(
		velocity.LevelInfo, velocity.StatusOK, "postgres connected",
		velocity.String("host", "db.internal"),
		velocity.Duration("latency", 4*time.Millisecond),
	)

	log.Status(
		velocity.LevelInfo, velocity.StatusOK, "redis connected",
		velocity.String("host", "cache.internal"),
		velocity.Duration("latency", 1*time.Millisecond),
	)

	log.Status(
		velocity.LevelInfo, velocity.StatusWarn, "object storage degraded",
		velocity.String("bucket", "assets-prod"),
		velocity.String("region", "ap-southeast-2"),
		velocity.Duration("latency", 320*time.Millisecond),
	)

	log.Status(
		velocity.LevelError, velocity.StatusFail, "payment gateway unreachable",
		velocity.String("provider", "stripe"),
		velocity.Error("reason", os.ErrDeadlineExceeded),
	)

	log.Status(
		velocity.LevelInfo, velocity.StatusPending, "feature flags syncing",
		velocity.String("remote", "flags.internal"),
	)

	log.Status(
		velocity.LevelInfo, velocity.StatusSkipped, "telemetry export",
		velocity.String("reason", "disabled in config"),
	)

	log.Newline()

	// Standalone StatusItem rendered via Logger.Render for inline display.
	// TTY is detected automatically from the console writer at render time.
	log.Info("re-checking payment gateway")
	item := velocity.NewStatusItem(
		velocity.StatusOK,
		"payment gateway recovered",
		log.Style(),
		velocity.String("provider", "stripe"),
		velocity.Duration("rtt", 12*time.Millisecond),
	)
	log.Render(item)

	log.Newline()
	log.Info("health check complete")
}

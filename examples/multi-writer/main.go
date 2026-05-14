// Multi-writer example. Shows how to route log entries to different
// destinations at runtime: a human-readable console, a JSON stream, and a
// filtered sink that only captures errors. We also demonstrate WriterFunc
// as a lightweight adapter for custom processing, and WriterTrusted() to
// opt a writer into receiving unredacted Secure fields.
package main

import (
	"bytes"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	// Main logger writes to stdout for human-readable output.
	log := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(os.Stdout))
	log.Info("Logger ready, adding dynamic writers")

	// A JSON buffer lets us inspect what the JSON writer received after logging.
	// In a real service this would be a file or a network socket.
	// Marked trusted: this writer receives unredacted Secure fields.
	jsonBuf := &bytes.Buffer{}
	jsonWriter := velocity.NewJSONWriter(jsonBuf)
	log.AddWriter("json", jsonWriter, velocity.WriterTrusted())

	// FilteredWriter wraps another writer and only forwards entries that pass
	// the predicate. Here we capture anything at Error or above into a separate
	// buffer so we can ship it to an alerting system later.
	// Not trusted: this sink is for alerting, Secure values should be redacted.
	errorBuf := &bytes.Buffer{}
	errorSink := velocity.NewFilteredWriter(
		velocity.NewJSONWriter(errorBuf),
		func(e *velocity.Entry) bool {
			return e.Level >= velocity.LevelError
		},
	)
	log.AddWriter("errors", errorSink)

	// WriterFunc is an easy way to attach arbitrary logic without a full struct.
	// Here we just count every entry that gets through.
	var counter atomic.Int64
	log.AddWriter("counter", velocity.WriterFunc(func(_ *velocity.Entry) error {
		counter.Add(1)
		return nil
	}))

	// Log at a variety of levels so we can see which writers receive what.
	log.Debug("Debug detail", velocity.String("subsystem", "cache"))
	log.Info("User signed in", velocity.String("user", "ada"), velocity.Int("session", 1))
	log.Warn("Slow query", velocity.Duration("elapsed", 320*time.Millisecond))
	log.Error("Payment gateway timeout", velocity.String("gateway", "stripe"), velocity.Int("attempt", 3))
	log.Info("Background job completed", velocity.String("job", "email-digest"))

	// RemoveWriter returns the writer so the caller can flush or close it.
	removed := log.RemoveWriter("counter")
	if removed != nil {
		_ = removed.Close()
	}
	log.Info("Counter writer removed, this line won't be counted")

	// Logger.Writer lets you inspect a registered writer without removing it.
	if w := log.Writer("json"); w != nil {
		if fw, ok := w.(velocity.FlushableWriter); ok {
			_ = fw.Flush()
		}
	}

	// Flush async writers before reading the buffers.
	if err := log.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close error: %v\n", err)
	}

	log.Newline()
	fmt.Printf("--- JSON writer received %d bytes ---\n", jsonBuf.Len())
	fmt.Println(jsonBuf.String())

	fmt.Printf("--- Error-only sink received %d bytes ---\n", errorBuf.Len())
	fmt.Println(errorBuf.String())

	fmt.Printf("--- Counter captured %d entries ---\n", counter.Load())
}

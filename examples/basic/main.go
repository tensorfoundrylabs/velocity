// Package main demonstrates the basic usage of the velocity logging library.
// This is the best starting point for new users.
package main

import (
	"errors"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	// The simplest way to get a logger. WithDevelopment resets to sensible
	// development defaults: debug level, coloured output, local timezone.
	log := velocity.New(
		velocity.WithDevelopment(),
		velocity.WithConsoleOutput(os.Stdout),
	)

	log.Info("velocity logging library - basic example")

	// Logging at different levels. Each one has a distinct colour in the terminal
	// so you can spot warnings and errors at a glance.
	log.Debug("checking internal state", velocity.String("component", "startup"))
	log.Info("server is ready", velocity.String("addr", ":8080"))
	log.Warn("configuration missing, using defaults", velocity.String("key", "timeout"))
	log.Error("failed to connect to cache", velocity.String("host", "redis:6379"))

	// Typed field constructors keep allocations off the heap on hot paths.
	// Use the specific constructor when you know the type.
	log.Info("request processed",
		velocity.String("method", "GET"),
		velocity.String("path", "/api/users"),
		velocity.Int("status", 200),
		velocity.Float64("latency_ms", 12.4),
		velocity.Bool("cached", true),
		velocity.Duration("elapsed", 12*time.Millisecond),
		velocity.Error("err", nil),
	)

	// With() returns a child logger that stamps every subsequent entry with
	// the given fields. Great for scoping a logger to a request or component.
	reqLog := log.With(
		velocity.String("request_id", "req-abc-123"),
		velocity.String("user", "alice"),
	)
	reqLog.Info("handling request")
	reqLog.Debug("fetching user record", velocity.Int("user_id", 7))
	reqLog.Warn("rate limit approaching", velocity.Int("remaining", 5))

	// Nested errors show up nicely with ErrorField.
	dbErr := errors.New("connection refused")
	reqLog.Error("database query failed",
		velocity.String("query", "SELECT * FROM users"),
		velocity.Error("err", dbErr),
	)

	// SetLevel changes the minimum level dynamically. Anything below the new
	// threshold is dropped without allocating an entry.
	log.Info("raising level to WARN - debug and info will be suppressed from now")
	log.SetLevel(velocity.LevelWarn)
	log.Debug("this debug message won't appear")
	log.Info("this info message won't appear either")
	log.Warn("this warning still gets through")

	// Drop back to debug so we can show Detailed().
	log.SetLevel(velocity.LevelDebug)

	// Detailed() returns a child logger that forces tree-format for every call.
	// Easier to read when there are many fields or values are long.
	log.Detailed().Info("deployment summary",
		velocity.String("environment", "staging"),
		velocity.String("version", "2.4.1"),
		velocity.Int("replicas", 3),
		velocity.Duration("rollout_duration", 47*time.Second),
		velocity.Bool("health_checks_passed", true),
	)

	// WithDevelopment() as the only option keeps it simple.
	devLog := velocity.New(velocity.WithDevelopment())
	devLog.Info("development preset logger is ready",
		velocity.String("preset", "development"),
	)
}

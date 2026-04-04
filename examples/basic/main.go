// Package main demonstrates the basic usage of the velocity logging library.
// This is the best starting point for new users.
package main

import (
	"errors"
	"os"
	"time"

	"github.com/tensorfoundrylabs/velocity"
)

func main() {
	// The simplest way to get a logger. It writes coloured output to stdout
	// using the default Night Owl theme and debug level.
	log := velocity.New(os.Stdout)

	log.Info("velocity logging library - basic example")

	// Logging at different levels. Each one has a distinct colour in the terminal
	// so you can spot warnings and errors at a glance.
	log.Debug("checking internal state", velocity.StringField("component", "startup"))
	log.Info("server is ready", velocity.StringField("addr", ":8080"))
	log.Warn("configuration missing, using defaults", velocity.StringField("key", "timeout"))
	log.Error("failed to connect to cache", velocity.StringField("host", "redis:6379"))

	// Typed field constructors keep allocations off the heap on hot paths.
	// Use the specific constructor when you know the type; F() is fine for
	// less critical code where convenience matters more.
	log.Info("request processed",
		velocity.StringField("method", "GET"),
		velocity.StringField("path", "/api/users"),
		velocity.Int("status", 200),
		velocity.Float64("latency_ms", 12.4),
		velocity.Bool("cached", true),
		velocity.Duration("elapsed", 12*time.Millisecond),
		velocity.ErrorField("err", nil),
	)

	// F() detects the type automatically. Handy for quick instrumentation
	// but the typed constructors are faster in tight loops.
	log.Info("generic field constructor",
		velocity.F("user_id", 42),
		velocity.F("role", "admin"),
		velocity.F("active", true),
	)

	// With() returns a child logger that stamps every subsequent entry with
	// the given fields. Great for scoping a logger to a request or component.
	reqLog := log.With(
		velocity.StringField("request_id", "req-abc-123"),
		velocity.StringField("user", "alice"),
	)
	reqLog.Info("handling request")
	reqLog.Debug("fetching user record", velocity.Int("user_id", 7))
	reqLog.Warn("rate limit approaching", velocity.Int("remaining", 5))

	// Nested errors show up nicely with ErrorField.
	dbErr := errors.New("connection refused")
	reqLog.Error("database query failed",
		velocity.StringField("query", "SELECT * FROM users"),
		velocity.ErrorField("err", dbErr),
	)

	// SetLevel changes the minimum level dynamically. Anything below the new
	// threshold is dropped without allocating an entry.
	log.Info("raising level to WARN - debug and info will be suppressed from now")
	log.SetLevel(velocity.LevelWarn)
	log.Debug("this debug message won't appear")
	log.Info("this info message won't appear either")
	log.Warn("this warning still gets through")

	// Drop back to debug so we can show InfoDetailed.
	log.SetLevel(velocity.LevelDebug)

	// InfoDetailed forces a tree-format display for the fields, which is much
	// easier to read when there are many fields or values are long.
	log.InfoDetailed("deployment summary",
		velocity.StringField("environment", "staging"),
		velocity.StringField("version", "2.4.1"),
		velocity.Int("replicas", 3),
		velocity.Duration("rollout_duration", 47*time.Second),
		velocity.Bool("health_checks_passed", true),
	)

	// NewDevelopment() is a convenience preset with sensible defaults for
	// local development: debug level, local timezone, coloured output.
	devLog := velocity.NewDevelopment()
	devLog.Info("development preset logger is ready",
		velocity.StringField("preset", "development"),
	)
}

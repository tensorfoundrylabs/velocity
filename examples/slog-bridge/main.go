// slog bridge example. Teams migrating to velocity don't have to rewrite
// everything at once. Wrap a velocity logger with NewSlogLogger, set it as
// the default, and existing slog.Info/Warn/Error calls just work.
// Structured attributes, groups, and level filtering all behave as expected.
package main

import (
	"log/slog"
	"os"

	"github.com/tensorfoundrylabs/velocity"
	slogbridge "github.com/tensorfoundrylabs/velocity/slogbridge"
)

func main() {
	// Start with a standard velocity logger that writes to stdout.
	vlog := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(os.Stdout))
	vlog.Info("Velocity logger initialised")

	// Wrap it so existing slog callers don't need any changes.
	sl := slogbridge.NewLogger(vlog)
	slog.SetDefault(sl)

	// From here on, all slog calls route through velocity's writers.
	slog.Info("Server starting", "addr", ":8080", "env", "production")
	slog.Warn("Rate limit approaching", "requests_per_min", 950, "limit", 1000)

	// slog.With creates a child logger with persistent attributes. Useful for
	// request-scoped logging where you want to carry a request ID on every line.
	reqLog := slog.With("request_id", "req-abc123", "user_id", 42)
	reqLog.Info("Request received", "method", "POST", "path", "/api/orders")
	reqLog.Info("Order validated", "order_id", "ord-789")

	// Groups prefix every key with the group name and a dot, which is handy for
	// namespacing fields from different subsystems without collisions.
	dbLog := slog.With(slog.Group("db", "host", "postgres:5432", "pool_size", 10))
	dbLog.Info("Query executed", "query", "SELECT * FROM orders", "rows", 42)

	// Nested groups work too. The keys come out as "http.req.method", etc.
	slog.Info("Handling request",
		slog.Group("http",
			slog.Group("req", "method", "GET", "path", "/health"),
			slog.Group("resp", "status", 200, "bytes", 128),
		),
	)

	// Now show level filtering. Set the velocity logger to Warn and check that
	// an slog.Info call is silently dropped. This is how you reduce noise in
	// production without touching caller code.
	vlog.SetLevel(velocity.LevelWarn)
	slog.Info("This info line is filtered out (velocity level = Warn)")
	slog.Warn("This warning still gets through", "code", "DISK_SPACE_LOW")
	slog.Error("Critical failure", "component", "scheduler", "error", "deadline exceeded")
}

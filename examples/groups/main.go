// groups demonstrates Logger.Group for count-headed indented blocks.
//
// This pattern comes from olla's translator-route registration, where each
// registered route needs to be visible at startup but kept out of the hot-log path.
//
// Run directly for a TTY console with coloured count token:
//
//	go run ./examples/groups
//
// Pipe to see the non-TTY plain form:
//
//	go run ./examples/groups | cat
//
// Add -json to write structured output alongside the console output:
//
//	go run ./examples/groups -json
package main

import (
	"flag"
	"os"

	velocity "github.com/tensorfoundrylabs/velocity/v2"
)

func main() {
	jsonOut := flag.Bool("json", false, "also write JSON to groups.log")
	flag.Parse()

	opts := []velocity.Option{
		velocity.WithDevelopment(),
	}
	if *jsonOut {
		f, err := os.Create("groups.log")
		if err != nil {
			panic(err)
		}
		defer func() {
			if err := f.Close(); err != nil {
				panic(err)
			}
		}()
		opts = append(
			opts,
			velocity.WithStructuredOutput(f),
			velocity.WithStructuredLevel(velocity.LevelDebug),
		)
	}

	log := velocity.New(opts...)
	defer func() {
		if err := log.Close(); err != nil {
			panic(err)
		}
	}()

	log.Info("translator service starting")
	log.Newline()

	// Olla's route-registration pattern: show which routes were bound.
	log.Group(
		velocity.LevelInfo, "Registering translator routes",
		velocity.GroupItem{Text: "GET  /translate"},
		velocity.GroupItem{Text: "POST /translate"},
		velocity.GroupItem{Text: "GET  /languages"},
		velocity.GroupItem{Text: "GET  /health"},
	)

	log.Newline()

	// Explicit markers: useful for check-list style output (pass / fail).
	log.Group(
		velocity.LevelInfo, "Config validation",
		velocity.GroupItem{Marker: "✓", Text: "API key present"},
		velocity.GroupItem{Marker: "✓", Text: "Rate limit configured"},
		velocity.GroupItem{Marker: "✓", Text: "Target language list loaded"},
		velocity.GroupItem{Marker: "~", Text: "Cache warm (optional, skipped)"},
	)

	log.Newline()

	// Empty group: count shows (0), no item lines emitted.
	log.Group(velocity.LevelInfo, "Pending background tasks")

	log.Newline()

	// The follow-up call is a normal Info — Group does not include a footer.
	log.Info("finished registering translator routes")
}

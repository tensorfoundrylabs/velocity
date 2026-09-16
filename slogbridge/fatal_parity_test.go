package slogbridge

// WP2 regression coverage: adapter-routed fatal entries share the direct
// path's sampler exemption (fatal is never sampled on any dispatch path) and
// never carry process-control semantics — only velocity.Logger.Fatal exits.

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/tensorfoundrylabs/velocity/v2"
)

// An exhausted sampler must not suppress slog-routed fatal records, and those
// records must not exit the process (the test reaching its assertions is the
// no-exit proof).
func TestSlogHandler_FatalNotSampledAndDoesNotExit(t *testing.T) {
	var buf bytes.Buffer
	log := velocity.New(
		velocity.WithConsoleOutput(&buf),
		velocity.WithColour(false),
		velocity.WithLevel(velocity.LevelDebug),
		velocity.WithSampler(velocity.NewCountSampler(1, 0)),
	)
	handler := NewHandler(log)
	slogLogger := slog.New(handler)

	delivered := 0
	for i := range 5 {
		slogLogger.Error("wp2-slog-fatal-warmup")
		slogLogger.Log(t.Context(), slog.LevelError+4, "wp2-slog-fatal-record",
			slog.String("i", string(rune('0'+i))))
		if strings.Count(buf.String(), "wp2-slog-fatal-record") == i+1 {
			delivered++
		}
	}

	if delivered != 5 {
		t.Errorf("slog-routed fatal records were sampled — %d of 5 delivered; fatal must bypass the sampler on every dispatch path", delivered)
	}
}

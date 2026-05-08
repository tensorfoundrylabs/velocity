package pretty_test

import (
	"bytes"
	"strings"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity"
	"github.com/tensorfoundrylabs/velocity/pretty"
)

func TestNewFromLogger_RoutesToLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cfg := velocity.DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = velocity.ThemeNightOwl
	cfg.StructuredOutput = nil

	log := velocity.NewWithConfig(cfg)
	p := pretty.NewFromLogger(log)

	if p == nil {
		t.Fatal("NewFromLogger returned nil for non-nil logger")
	}

	p.KeyValue("key", "value")

	out := buf.String()
	if !strings.Contains(out, "key") || !strings.Contains(out, "value") {
		t.Errorf("expected key-value in output, got: %s", out)
	}
}

func TestNewFromLogger_NilLogger_ReturnsNil(t *testing.T) {
	t.Parallel()

	p := pretty.NewFromLogger(nil)
	if p != nil {
		t.Error("expected nil Pretty for nil logger")
	}
}

package velocity_test

import (
	"bytes"
	"strings"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity"
)

func TestNewPrettyFromLogger_RoutesToLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := velocity.New(
		velocity.WithConsoleOutput(&buf),
		velocity.WithTheme(velocity.ThemeNightOwl),
		velocity.WithLevel(velocity.LevelDebug),
	)
	p := velocity.NewPrettyFromLogger(log)

	if p == nil {
		t.Fatal("NewPrettyFromLogger returned nil for non-nil logger")
	}

	p.KeyValue("key", "value")

	out := buf.String()
	if !strings.Contains(out, "key") || !strings.Contains(out, "value") {
		t.Errorf("expected key-value in output, got: %s", out)
	}
}

func TestNewPrettyFromLogger_NilLogger_ReturnsNil(t *testing.T) {
	t.Parallel()

	p := velocity.NewPrettyFromLogger(nil)
	if p != nil {
		t.Error("expected nil Pretty for nil logger")
	}
}

func TestNewPretty_NilWriter_UsesDiscard(t *testing.T) {
	t.Parallel()

	// nil writer must not panic — output goes to io.Discard
	p := velocity.NewPretty(nil, velocity.ThemeNightOwl)
	p.Box("title", "content")
	p.Section("section")
	p.KeyValue("k", "v")
	p.Success("ok")
	p.Warn("warn")
	p.Error("err")
	p.Info("info")
	p.Muted("muted")
	p.Debug("debug")
}

func TestPretty_NilReceiver_DoesNotPanic(t *testing.T) {
	t.Parallel()

	// Every method must tolerate a nil receiver.
	var p *velocity.Pretty
	p.Box("t", "c")
	p.Table([]string{"h"}, [][]string{{"v"}})
	p.Tree([]velocity.TreeItem{{Key: "k"}})
	p.Banner("b")
	p.KeyValue("k", "v")
	p.Bullet(0, "text")
	p.SystemInfo(&velocity.SystemInfoData{Title: "T"})
	p.Section("s")
	p.Render(nil)
	p.Panel("title", "body")
	p.Raw("raw")
	p.Success("ok")
	p.Warn("warn")
	p.Error("err")
	p.Info("info")
	p.Muted("muted")
	p.Debug("debug")
}

func TestPretty_Box_WritesToWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	p := velocity.NewPretty(&buf, velocity.ThemeNightOwl)
	p.Box("My Title", "line one\nline two")

	out := buf.String()
	if !strings.Contains(out, "My Title") {
		t.Errorf("expected title in box output, got: %s", out)
	}
}

func TestPretty_Section_WritesUnderline(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	p := velocity.NewPretty(&buf, velocity.ThemeNightOwl)
	p.Section("Deployment")

	out := buf.String()
	if !strings.Contains(out, "Deployment") {
		t.Errorf("expected section title in output, got: %s", out)
	}
	if !strings.Contains(out, "─") {
		t.Errorf("expected dashed underline in output, got: %s", out)
	}
}

func TestPretty_Render_CustomRenderable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	p := velocity.NewPretty(&buf, nil)
	box := velocity.NewBox("custom", "body", nil)
	p.Render(box)

	if buf.Len() == 0 {
		t.Error("expected output from Render, got nothing")
	}
}

func TestPretty_Muted_UsesTimestampColour(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	p := velocity.NewPretty(&buf, velocity.ThemeNightOwl)
	p.Muted("secondary info")

	out := buf.String()
	if !strings.Contains(out, "secondary info") {
		t.Errorf("expected message in muted output, got: %s", out)
	}
}

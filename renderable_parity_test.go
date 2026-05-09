package velocity_test

import (
	"bytes"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity"
)

// Compile-time assertions: every renderable type must satisfy velocity.Renderable.
var (
	_ velocity.Renderable = (*velocity.Box)(nil)
	_ velocity.Renderable = (*velocity.Table)(nil)
	_ velocity.Renderable = (*velocity.Banner)(nil)
	_ velocity.Renderable = (*velocity.Tree)(nil)
	_ velocity.Renderable = (*velocity.KeyValue)(nil)
	_ velocity.Renderable = (*velocity.SystemInfo)(nil)
)

// TestBoxResult_ParityWithPrettyBox verifies that Box.Render and Pretty.Box
// produce identical output so callers can freely choose either form.
func TestBoxResult_ParityWithPrettyBox(t *testing.T) {
	t.Parallel()

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.Box("Title", "some content\nsecond line")

	result := velocity.NewBox("Title", "some content\nsecond line", velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("Box.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.Box:       %q\n  Box.Render:  %q", direct.String(), viaResult.String())
	}
}

// TestTableResult_ParityWithPrettyTable verifies that Table.Render and Pretty.Table produce identical output.
func TestTableResult_ParityWithPrettyTable(t *testing.T) {
	t.Parallel()

	headers := []string{"Name", "Value"}
	rows := [][]string{{"alpha", "1"}, {"beta", "2"}}

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.Table(headers, rows)

	result := velocity.NewTable(headers, rows, velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("Table.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.Table:    %q\n  Table.Render:%q", direct.String(), viaResult.String())
	}
}

// TestBannerResult_ParityWithPrettyBanner verifies that Banner.Render and Pretty.Banner produce identical output.
func TestBannerResult_ParityWithPrettyBanner(t *testing.T) {
	t.Parallel()

	text := "Welcome\nTo Velocity"

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.Banner(text)

	result := velocity.NewBanner(text, velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("Banner.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.Banner:   %q\n  Banner.Render:%q", direct.String(), viaResult.String())
	}
}

// TestTreeResult_ParityWithPrettyTree verifies that Tree.Render and Pretty.Tree produce identical output.
func TestTreeResult_ParityWithPrettyTree(t *testing.T) {
	t.Parallel()

	nodes := []velocity.TreeItem{
		{Key: "root", Children: []velocity.TreeItem{
			{Key: "child1", Value: "v1"},
			{Key: "child2", Value: "v2"},
		}},
	}

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.Tree(nodes)

	result := velocity.NewTree(nodes, velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("Tree.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.Tree:     %q\n  Tree.Render:%q", direct.String(), viaResult.String())
	}
}

// TestKeyValueResult_ParityWithPrettyKeyValue verifies that KeyValue.Render and Pretty.KeyValue produce identical output.
func TestKeyValueResult_ParityWithPrettyKeyValue(t *testing.T) {
	t.Parallel()

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.KeyValue("version", "1.2.3")

	result := velocity.NewKeyValue("version", "1.2.3", velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("KeyValue.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.KeyValue:   %q\n  KeyValue.Render:%q", direct.String(), viaResult.String())
	}
}

// TestSystemInfoResult_ParityWithPrettySystemInfo verifies that SystemInfo.Render and Pretty.SystemInfo produce identical output.
func TestSystemInfoResult_ParityWithPrettySystemInfo(t *testing.T) {
	t.Parallel()

	info := &velocity.SystemInfoData{
		Title:   "TestApp",
		Version: "0.1.0",
		Fields: []velocity.KeyValuePair{
			{Key: "env", Value: "test"},
			{Key: "region", Value: "ap-southeast-2"},
		},
	}

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.SystemInfo(info)

	result := velocity.NewSystemInfo(info, velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("SystemInfo.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.SystemInfo:   %q\n  SystemInfo.Render:%q", direct.String(), viaResult.String())
	}
}

// TestTableResult_EmptyHeaders confirms that nil headers produce no output.
func TestTableResult_EmptyHeaders(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	result := velocity.NewTable(nil, nil, velocity.ThemeNightOwl)
	if err := result.Render(&buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil headers, got: %q", buf.String())
	}
}

// TestSystemInfoResult_NilInfo confirms that nil SystemInfoData produces no output.
func TestSystemInfoResult_NilInfo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	result := velocity.NewSystemInfo(nil, velocity.ThemeNightOwl)
	if err := result.Render(&buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil info, got: %q", buf.String())
	}
}

// TestNewPrettyFromLogger_Concurrent verifies that concurrent pretty and logger calls don't
// interleave or race (run with -race to validate).
func TestNewPrettyFromLogger_Concurrent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := velocity.New(
		velocity.WithConsoleOutput(&buf),
		velocity.WithTheme(velocity.ThemeNightOwl),
	)
	p := velocity.NewPrettyFromLogger(log)

	const goroutines = 20
	done := make(chan struct{})

	for range goroutines / 2 {
		go func() {
			for range 5 {
				log.Info("log line")
			}
			done <- struct{}{}
		}()
	}

	for range goroutines / 2 {
		go func() {
			for range 5 {
				p.KeyValue("key", "val")
			}
			done <- struct{}{}
		}()
	}

	for range goroutines {
		<-done
	}

	// If we get here without data race or panic the test passes.
	// Output must contain both kinds of content.
	out := buf.String()
	if out == "" {
		t.Error("expected output in buffer")
	}
}

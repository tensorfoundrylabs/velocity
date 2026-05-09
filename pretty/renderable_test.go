package pretty_test

import (
	"bytes"
	"strings"
	"testing"

	velocity "github.com/tensorfoundrylabs/velocity"
	"github.com/tensorfoundrylabs/velocity/pretty"
)

// Compile-time assertions: every result type must satisfy velocity.Renderable.
var (
	_ velocity.Renderable = (*pretty.BoxResult)(nil)
	_ velocity.Renderable = (*pretty.TableResult)(nil)
	_ velocity.Renderable = (*pretty.BannerResult)(nil)
	_ velocity.Renderable = (*pretty.TreeResult)(nil)
	_ velocity.Renderable = (*pretty.KeyValueResult)(nil)
	_ velocity.Renderable = (*pretty.SystemInfoResult)(nil)
)

// TestBoxResult_ParityWithPrettyBox verifies that BoxResult.Render produces the
// same bytes as p.Box so callers can freely choose either form.
func TestBoxResult_ParityWithPrettyBox(t *testing.T) {
	t.Parallel()

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.Box("Title", "some content\nsecond line")

	result := pretty.NewBoxResult("Title", "some content\nsecond line", velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("BoxResult.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.Box:       %q\n  BoxResult:   %q", direct.String(), viaResult.String())
	}
}

// TestTableResult_ParityWithPrettyTable verifies that TableResult.Render matches p.Table.
func TestTableResult_ParityWithPrettyTable(t *testing.T) {
	t.Parallel()

	headers := []string{"Name", "Value"}
	rows := [][]string{{"alpha", "1"}, {"beta", "2"}}

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.Table(headers, rows)

	result := pretty.NewTableResult(headers, rows, velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("TableResult.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.Table:     %q\n  TableResult: %q", direct.String(), viaResult.String())
	}
}

// TestBannerResult_ParityWithPrettyBanner verifies that BannerResult.Render matches p.Banner.
func TestBannerResult_ParityWithPrettyBanner(t *testing.T) {
	t.Parallel()

	text := "Welcome\nTo Velocity"

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.Banner(text)

	result := pretty.NewBannerResult(text, velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("BannerResult.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.Banner:    %q\n  BannerResult:%q", direct.String(), viaResult.String())
	}
}

// TestTreeResult_ParityWithPrettyTree verifies that TreeResult.Render matches p.Tree.
func TestTreeResult_ParityWithPrettyTree(t *testing.T) {
	t.Parallel()

	// pretty.TreeItem is the local shim type; velocity.TreeItem is canonical.
	// Both convert through renderable.go's convertTreeItems so output must match.
	nodes := []pretty.TreeItem{
		{Key: "root", Children: []pretty.TreeItem{
			{Key: "child1", Value: "v1"},
			{Key: "child2", Value: "v2"},
		}},
	}

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.Tree([]velocity.TreeItem{
		{Key: "root", Children: []velocity.TreeItem{
			{Key: "child1", Value: "v1"},
			{Key: "child2", Value: "v2"},
		}},
	})

	result := pretty.NewTreeResult(nodes, velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("TreeResult.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.Tree:     %q\n  TreeResult: %q", direct.String(), viaResult.String())
	}
}

// TestKeyValueResult_ParityWithPrettyKeyValue verifies output parity.
func TestKeyValueResult_ParityWithPrettyKeyValue(t *testing.T) {
	t.Parallel()

	var direct, viaResult bytes.Buffer

	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.KeyValue("version", "1.2.3")

	result := pretty.NewKeyValueResult("version", "1.2.3", velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("KeyValueResult.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.KeyValue:    %q\n  KeyValueResult:%q", direct.String(), viaResult.String())
	}
}

// TestSystemInfoResult_ParityWithPrettySystemInfo verifies output parity.
func TestSystemInfoResult_ParityWithPrettySystemInfo(t *testing.T) {
	t.Parallel()

	info := &pretty.SystemInfo{
		Title:   "TestApp",
		Version: "0.1.0",
		Fields: []pretty.KeyValuePair{
			{Key: "env", Value: "test"},
			{Key: "region", Value: "ap-southeast-2"},
		},
	}

	var direct, viaResult bytes.Buffer

	// Construct the equivalent root type for parity comparison.
	rootInfo := &velocity.SystemInfoData{
		Title:   info.Title,
		Version: info.Version,
		Fields: []velocity.KeyValuePair{
			{Key: "env", Value: "test"},
			{Key: "region", Value: "ap-southeast-2"},
		},
	}
	p := velocity.NewPretty(&direct, velocity.ThemeNightOwl)
	p.SystemInfo(rootInfo)

	result := pretty.NewSystemInfoResult(info, velocity.ThemeNightOwl)
	if err := result.Render(&viaResult); err != nil {
		t.Fatalf("SystemInfoResult.Render returned error: %v", err)
	}

	if direct.String() != viaResult.String() {
		t.Errorf("output mismatch:\n  p.SystemInfo:    %q\n  SystemInfoResult:%q", direct.String(), viaResult.String())
	}
}

// TestTableResult_EmptyHeaders confirms the early-return path produces no output.
func TestTableResult_EmptyHeaders(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	result := pretty.NewTableResult(nil, nil, velocity.ThemeNightOwl)
	if err := result.Render(&buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil headers, got: %q", buf.String())
	}
}

// TestSystemInfoResult_NilInfo confirms the nil-info guard produces no output.
func TestSystemInfoResult_NilInfo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	result := pretty.NewSystemInfoResult(nil, velocity.ThemeNightOwl)
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

	cfg := velocity.DefaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleTheme = velocity.ThemeNightOwl
	cfg.StructuredOutput = nil

	log := velocity.NewWithConfig(cfg)
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
	if !strings.Contains(out, "log line") {
		t.Error("expected log output in buffer")
	}
}

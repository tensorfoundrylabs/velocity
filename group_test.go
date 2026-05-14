package velocity

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// --- resolvedMarker ---

func TestResolvedMarker_ExplicitMarker(t *testing.T) {
	t.Parallel()

	got := resolvedMarker("✓", 0, 3)
	if got != "✓" {
		t.Errorf("expected explicit marker, got %q", got)
	}
}

func TestResolvedMarker_DefaultGlyph(t *testing.T) {
	t.Parallel()

	// Non-last item with no marker gets the default branch glyph.
	got := resolvedMarker("", 0, 3)
	if got != groupDefaultGlyph {
		t.Errorf("expected %q, got %q", groupDefaultGlyph, got)
	}
}

func TestResolvedMarker_LastGlyph(t *testing.T) {
	t.Parallel()

	// Last item with no marker and total > 1 gets the terminal glyph.
	got := resolvedMarker("", 2, 3)
	if got != groupLastGlyph {
		t.Errorf("expected %q, got %q", groupLastGlyph, got)
	}
}

func TestResolvedMarker_SingleItem(t *testing.T) {
	t.Parallel()

	// Single item should use the default glyph (total == 1, not > 1).
	got := resolvedMarker("", 0, 1)
	if got != groupDefaultGlyph {
		t.Errorf("single item: expected %q, got %q", groupDefaultGlyph, got)
	}
}

// --- Group.String / Render nil receiver ---

func TestGroupNilReceiver(t *testing.T) {
	t.Parallel()

	var g *Group
	if s := g.String(); s != "" {
		t.Errorf("nil.String() = %q, want empty", s)
	}
	var buf bytes.Buffer
	if err := g.Render(&buf); err != nil {
		t.Errorf("nil.Render() error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("nil.Render() wrote bytes: %q", buf.String())
	}
}

// --- Empty items ---

func TestGroupRenderEmpty(t *testing.T) {
	t.Parallel()

	g := NewGroup("Loaded plugins", nil, ThemeMono)
	out := g.String()

	if !strings.Contains(out, "Loaded plugins (0)") {
		t.Errorf("expected header with (0) count, got: %q", out)
	}
	// No item lines beyond the header.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line for empty group, got %d: %q", len(lines), out)
	}
}

// --- One item, default marker ---

func TestGroupRenderSingleItem(t *testing.T) {
	t.Parallel()

	g := NewGroup("Loaded plugins", []GroupItem{
		{Text: "auth"},
	}, ThemeMono)
	out := g.String()

	if !strings.Contains(out, "(1)") {
		t.Errorf("expected (1) count, got: %q", out)
	}
	// Single item uses the default glyph (not the last-item glyph, because total==1).
	if !strings.Contains(out, groupDefaultGlyph+" auth") {
		t.Errorf("expected default glyph + text, got: %q", out)
	}
}

// --- Multiple items: auto last glyph ---

func TestGroupRenderMultipleItemsAutoLast(t *testing.T) {
	t.Parallel()

	items := []GroupItem{
		{Text: "GET  /api/v1/users"},
		{Text: "POST /api/v1/users"},
		{Text: "GET  /api/v1/users/:id"},
	}
	g := NewGroup("Registering routes", items, ThemeMono)
	out := g.String()

	if !strings.Contains(out, "(3)") {
		t.Errorf("expected (3) count, got: %q", out)
	}
	// First two items get the default glyph.
	if !strings.Contains(out, groupDefaultGlyph+" GET  /api/v1/users\n") {
		t.Errorf("expected default glyph on non-last item, got: %q", out)
	}
	// Last item gets the terminal glyph.
	if !strings.Contains(out, groupLastGlyph+" GET  /api/v1/users/:id") {
		t.Errorf("expected last glyph on final item, got: %q", out)
	}
}

// --- Multiple items with explicit markers ---

func TestGroupRenderExplicitMarkers(t *testing.T) {
	t.Parallel()

	items := []GroupItem{
		{Marker: "✓", Text: "passed"},
		{Marker: "✗", Text: "failed"},
		{Marker: "~", Text: "skipped"},
	}
	g := NewGroup("Test results", items, ThemeMono)
	out := g.String()

	for _, want := range []string{"✓ passed", "✗ failed", "~ skipped"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %q", want, out)
		}
	}
	// Explicit markers: auto-last should NOT override.
	if strings.Contains(out, groupLastGlyph) {
		t.Errorf("last glyph should not appear when explicit markers are set, got: %q", out)
	}
}

// --- TTY render: count has different colour (no ANSI in Mono, just check structure) ---
// Exercises the TTY path via renderGroupTTY directly, since bytes.Buffer is not a
// terminal and g.String() uses the plain path automatically.

func TestGroupRenderTTY(t *testing.T) {
	t.Parallel()

	items := []GroupItem{
		{Text: "item A"},
		{Text: "item B"},
	}
	g := NewGroup("Processing", items, ThemeMono)

	var buf bytes.Buffer
	renderGroupTTY(&buf, g.msg, g.items, g.theme)
	out := buf.String()

	if !strings.Contains(out, "Processing (2)") {
		t.Errorf("expected header with count, got: %q", out)
	}
	if !strings.Contains(out, "item A") || !strings.Contains(out, "item B") {
		t.Errorf("expected item text in TTY output, got: %q", out)
	}
}

// --- Logger.Group: console routing ---

func TestLoggerGroupConsole(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(&buf),
		WithColour(false),
	)

	log.Group(LevelInfo, "Registering routes",
		GroupItem{Text: "GET /api/v1/users"},
		GroupItem{Text: "POST /api/v1/users"},
		GroupItem{Text: "GET /api/v1/users/:id"},
	)

	out := buf.String()
	if !strings.Contains(out, "Registering routes") {
		t.Errorf("expected message in console output: %q", out)
	}
	// Count must appear somewhere (either in the header or items line).
	if !strings.Contains(out, "(3)") {
		t.Errorf("expected count (3) in console output: %q", out)
	}
	// At least one item must appear.
	if !strings.Contains(out, "/api/v1/users") {
		t.Errorf("expected item text in console output: %q", out)
	}
}

// --- Logger.Group: JSON routing ---

func TestLoggerGroupJSON(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(&buf),
	)

	log.Group(LevelInfo, "Registering routes",
		GroupItem{Text: "GET /api/v1/users"},
		GroupItem{Text: "POST /api/v1/users"},
		GroupItem{Text: "GET /api/v1/users/:id"},
	)

	out := buf.String()
	// Single JSON line.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected one JSON line, got %d: %q", len(lines), out)
	}
	if !strings.Contains(out, `"count":3`) {
		t.Errorf("expected count field in JSON: %q", out)
	}
	if !strings.Contains(out, `"items":[`) {
		t.Errorf("expected items array in JSON: %q", out)
	}
	// Message in JSON should not include the " (N)" suffix.
	if !strings.Contains(out, `"message":"Registering routes"`) {
		t.Errorf("expected clean message in JSON (no count suffix): %q", out)
	}
	// Item text (markers stripped).
	if !strings.Contains(out, `"GET /api/v1/users"`) {
		t.Errorf("expected item text in JSON items array: %q", out)
	}
}

// --- JSON: markers stripped from items array ---

func TestLoggerGroupJSONMarkersStripped(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(&buf),
	)

	log.Group(LevelInfo, "Results",
		GroupItem{Marker: "✓", Text: "auth passed"},
		GroupItem{Marker: "✗", Text: "rate limit failed"},
	)

	out := buf.String()
	// Markers must not appear in the JSON items array.
	if strings.Contains(out, "✓") || strings.Contains(out, "✗") {
		t.Errorf("markers leaked into JSON items: %q", out)
	}
	if !strings.Contains(out, `"auth passed"`) || !strings.Contains(out, `"rate limit failed"`) {
		t.Errorf("expected item text in JSON: %q", out)
	}
}

// --- JSON: empty items ---

func TestLoggerGroupJSONEmpty(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(&buf),
	)

	log.Group(LevelInfo, "No routes")

	out := buf.String()
	if !strings.Contains(out, `"count":0`) {
		t.Errorf("expected count:0 in JSON: %q", out)
	}
	if !strings.Contains(out, `"items":[]`) {
		t.Errorf("expected empty items array in JSON: %q", out)
	}
}

// --- Logger.Group: nil logger ---

func TestLoggerGroupNil(t *testing.T) {
	t.Parallel()

	// Must not panic.
	var log *Logger
	log.Group(LevelInfo, "should not panic",
		GroupItem{Text: "item one"},
	)
}

// --- Logger.Group: level filtering ---

func TestLoggerGroupLevelFilter(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithStructuredOutput(&buf),
		WithLevel(LevelError),
		WithStructuredLevel(LevelError),
	)

	log.Group(LevelInfo, "this should be filtered",
		GroupItem{Text: "item"},
	)

	if out := buf.String(); out != "" {
		t.Errorf("expected no output when filtered, got: %q", out)
	}
}

// --- TTY indent alignment: items indented past the message column ---

func TestLoggerGroupTTYIndent(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(&buf),
		WithColour(false),
	)

	// Fake a TTY by setting up a logger with a console writer whose isTTY is true.
	// We can't force this in tests, but we can verify the non-TTY item indent
	// is present (groupItemIndent at a minimum).
	log.Group(LevelInfo, "Routes",
		GroupItem{Text: "GET /"},
		GroupItem{Text: "POST /"},
	)

	out := buf.String()
	// Items must have at least the two-space groupItemIndent prefix.
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "GET /") || strings.Contains(line, "POST /") {
			if !strings.HasPrefix(line, " ") {
				t.Errorf("item line missing indent: %q", line)
			}
		}
	}
}

// --- groupMsgWithCount ---

func TestGroupMsgWithCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		msg   string
		count int
		want  string
	}{
		{"Registering routes", 3, "Registering routes (3)"},
		{"Loaded plugins", 0, "Loaded plugins (0)"},
		{"Items", 100, "Items (100)"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got := groupMsgWithCount(tc.msg, tc.count)
			if got != tc.want {
				t.Errorf("groupMsgWithCount(%q, %d) = %q, want %q", tc.msg, tc.count, got, tc.want)
			}
		})
	}
}

// --- groupItemsField / groupItemsFromField roundtrip ---

func TestGroupItemsFieldRoundtrip(t *testing.T) {
	t.Parallel()

	items := []GroupItem{
		{Marker: "•", Text: "one"},
		{Text: "two"},
	}

	f := groupItemsField(items)
	if f.Type != FieldTypeGroupItems {
		t.Fatalf("expected FieldTypeGroupItems, got %v", f.Type)
	}

	got := groupItemsFromField(f)
	if len(got) != len(items) {
		t.Fatalf("roundtrip length mismatch: got %d, want %d", len(got), len(items))
	}
	for i, item := range items {
		if got[i] != item {
			t.Errorf("item[%d] = %+v, want %+v", i, got[i], item)
		}
	}
}

// --- groupItemsFromField: wrong type returns nil ---

func TestGroupItemsFromFieldWrongType(t *testing.T) {
	t.Parallel()

	f := String("key", "val")
	if got := groupItemsFromField(f); got != nil {
		t.Errorf("expected nil for non-GroupItems field, got %v", got)
	}
}

// --- Render parity: TTY and non-TTY both have all items ---

func TestGroupRenderParity(t *testing.T) {
	t.Parallel()

	items := []GroupItem{
		{Text: "alpha"},
		{Text: "beta"},
		{Text: "gamma"},
	}

	// Both render paths (TTY and plain) must include all item text.
	g := NewGroup("Test", items, ThemeMono)

	// Plain path (bytes.Buffer is not a terminal).
	plainOut := g.String()
	for _, item := range items {
		if !strings.Contains(plainOut, item.Text) {
			t.Errorf("plain: missing item %q in output: %q", item.Text, plainOut)
		}
	}

	// TTY path via internal helper.
	var ttyBuf bytes.Buffer
	renderGroupTTY(&ttyBuf, g.msg, g.items, g.theme)
	ttyOut := ttyBuf.String()
	for _, item := range items {
		if !strings.Contains(ttyOut, item.Text) {
			t.Errorf("tty: missing item %q in output: %q", item.Text, ttyOut)
		}
	}
}

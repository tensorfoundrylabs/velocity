package velocity

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// --- StatusKind.String() ---

func TestStatusKindString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind StatusKind
		want string
	}{
		{StatusOK, "OK"},
		{StatusFail, "FAIL"},
		{StatusWarn, "WARN"},
		{StatusInfo, "INFO"},
		{StatusPending, "PENDING"},
		{StatusSkipped, "SKIP"},
		// Unknown value falls back to "INFO".
		{StatusKind(200), "INFO"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.kind.String(); got != tc.want {
				t.Errorf("StatusKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

// --- StatusKind.Slot() ---

func TestStatusKindSlot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind StatusKind
		want StyleSlot
	}{
		{StatusOK, SlotStatusOK},
		{StatusFail, SlotStatusFail},
		{StatusWarn, SlotStatusWarn},
		{StatusInfo, SlotStatusInfo},
		// Pending reuses Info slot (no dedicated slot).
		{StatusPending, SlotStatusInfo},
		// Skipped reuses Muted slot (de-emphasised / not an outcome).
		{StatusSkipped, SlotMuted},
	}

	for _, tc := range cases {
		t.Run(tc.kind.String(), func(t *testing.T) {
			t.Parallel()
			if got := tc.kind.Slot(); got != tc.want {
				t.Errorf("StatusKind(%d).Slot() = %v, want %v", tc.kind, got, tc.want)
			}
		})
	}
}

// --- StatusItem.Render (TTY path) ---

func TestStatusItemRenderTTY(t *testing.T) {
	t.Parallel()

	// Use ThemeMono so there are no ANSI codes to strip in assertions.
	item := NewStatusItem(StatusOK, "user signed in", ThemeMono, true,
		Int("user_id", 42),
		Duration("took", 18*1000*1000), // 18ms
	)

	var buf bytes.Buffer
	if err := item.Render(&buf); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	out := buf.String()
	// Badge must be present with correct padding.
	if !strings.Contains(out, "[OK     ]") {
		t.Errorf("expected badge [OK     ], got: %q", out)
	}
	if !strings.Contains(out, "user signed in") {
		t.Errorf("expected message in output, got: %q", out)
	}
	if !strings.Contains(out, "user_id=42") {
		t.Errorf("expected user_id field in output, got: %q", out)
	}
}

// TestStatusItemBadgeAlignment verifies that all six status kinds produce
// badges of identical visible width so consecutive items align in a terminal.
func TestStatusItemBadgeAlignment(t *testing.T) {
	t.Parallel()

	kinds := []StatusKind{
		StatusOK, StatusFail, StatusWarn, StatusInfo, StatusPending, StatusSkipped,
	}

	// Find visible badge width for each kind by stripping everything after the ']'.
	badgeWidths := make([]int, 0, len(kinds))
	for _, k := range kinds {
		item := NewStatusItem(k, "msg", ThemeMono, true)
		var buf bytes.Buffer
		_ = item.Render(&buf)
		line := buf.String()
		end := strings.Index(line, "]")
		if end < 0 {
			t.Fatalf("kind %s: no ']' found in output %q", k.String(), line)
		}
		// +1 to include the ']' itself.
		badgeWidths = append(badgeWidths, end+1)
	}

	for i := 1; i < len(badgeWidths); i++ {
		if badgeWidths[i] != badgeWidths[0] {
			t.Errorf("badge width mismatch: %s=%d vs %s=%d",
				kinds[0].String(), badgeWidths[0], kinds[i].String(), badgeWidths[i])
		}
	}
}

// --- StatusItem.Render (plain / non-TTY path) ---

func TestStatusItemRenderPlain(t *testing.T) {
	t.Parallel()

	item := NewStatusItem(StatusFail, "payment refused", ThemeMono, false,
		String("reason", "card expired"),
	)

	var buf bytes.Buffer
	if err := item.Render(&buf); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[FAIL   ]") {
		t.Errorf("expected badge [FAIL   ], got: %q", out)
	}
	if !strings.Contains(out, "payment refused") {
		t.Errorf("expected message in output, got: %q", out)
	}
	if !strings.Contains(out, `reason="card expired"`) {
		t.Errorf("expected reason field in output, got: %q", out)
	}
}

// --- StatusItem.String() ---

func TestStatusItemString(t *testing.T) {
	t.Parallel()

	item := NewStatusItem(StatusWarn, "slow query", ThemeMono, false)
	s := item.String()
	if !strings.Contains(s, "slow query") {
		t.Errorf("String() missing message: %q", s)
	}
	if !strings.Contains(s, "[WARN   ]") {
		t.Errorf("String() missing badge: %q", s)
	}
}

// --- StatusItem: nil receiver ---

func TestStatusItemNilReceiver(t *testing.T) {
	t.Parallel()

	var item *StatusItem
	if s := item.String(); s != "" {
		t.Errorf("nil.String() = %q, want empty", s)
	}
	var buf bytes.Buffer
	if err := item.Render(&buf); err != nil {
		t.Errorf("nil.Render() error: %v", err)
	}
}

// --- StatusItem: no fields ---

func TestStatusItemNoFields(t *testing.T) {
	t.Parallel()

	item := NewStatusItem(StatusPending, "waiting for upstream", ThemeMono, false)
	var buf bytes.Buffer
	_ = item.Render(&buf)
	out := buf.String()
	if !strings.Contains(out, "waiting for upstream") {
		t.Errorf("expected message: %q", out)
	}
	// No stray field separators.
	if strings.Contains(out, "=") {
		t.Errorf("unexpected '=' (field) in no-field output: %q", out)
	}
}

// --- StatusItem: five fields ---

func TestStatusItemFiveFields(t *testing.T) {
	t.Parallel()

	item := NewStatusItem(StatusInfo, "ready", ThemeMono, false,
		String("svc", "auth"),
		Int("port", 8080),
		Bool("tls", true),
		Duration("startup", 120*1000*1000),
		String("env", "prod"),
	)
	var buf bytes.Buffer
	_ = item.Render(&buf)
	out := buf.String()
	for _, want := range []string{"svc=", "port=", "tls=", "startup=", "env="} {
		if !strings.Contains(out, want) {
			t.Errorf("expected field %q in output: %q", want, out)
		}
	}
}

// --- Logger.Status routing: console writer ---

func TestLoggerStatusConsole(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(&buf),
		WithColour(false),
	)

	log.Status(LevelInfo, StatusOK, "database connected", String("host", "localhost"))

	out := buf.String()
	// On non-TTY console without colour the standard template path fires,
	// which won't include the badge. That's the expected fallback behaviour —
	// the test validates that Status routes through the writer without panicking
	// and that the message and field appear.
	if !strings.Contains(out, "database connected") {
		t.Errorf("expected message in console output: %q", out)
	}
	if !strings.Contains(out, "host") {
		t.Errorf("expected host field in console output: %q", out)
	}
}

// --- Logger.Status routing: JSON writer ---

func TestLoggerStatusJSON(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(&buf),
	)

	log.Status(LevelInfo, StatusOK, "service started", Int("pid", 1234))

	out := buf.String()
	if !strings.Contains(out, `"status":"ok"`) {
		t.Errorf("expected status field in JSON output: %q", out)
	}
	if !strings.Contains(out, `"message":"service started"`) {
		t.Errorf("expected message in JSON output: %q", out)
	}
	if !strings.Contains(out, `"pid":1234`) {
		t.Errorf("expected pid field in JSON output: %q", out)
	}
}

// TestLoggerStatusAllKindsJSON ensures all six kinds serialise correctly as JSON.
func TestLoggerStatusAllKindsJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind      StatusKind
		wantField string
	}{
		{StatusOK, `"status":"ok"`},
		{StatusFail, `"status":"fail"`},
		{StatusWarn, `"status":"warn"`},
		{StatusInfo, `"status":"info"`},
		{StatusPending, `"status":"pending"`},
		{StatusSkipped, `"status":"skip"`},
	}

	for _, tc := range cases {
		t.Run(tc.kind.String(), func(t *testing.T) {
			t.Parallel()

			var buf safeBuffer
			log := New(
				WithConsoleOutput(io.Discard),
				WithStructuredOutput(&buf),
			)
			log.Status(LevelInfo, tc.kind, "test", String("k", "v"))
			out := buf.String()
			if !strings.Contains(out, tc.wantField) {
				t.Errorf("kind %s: want %q in %q", tc.kind.String(), tc.wantField, out)
			}
		})
	}
}

// --- Logger.Status: nil logger fallback ---

func TestLoggerStatusNil(t *testing.T) {
	t.Parallel()

	// Must not panic. Output goes to stderr; we can't capture it here but the
	// nil path is safe.
	var log *Logger
	log.Status(LevelInfo, StatusOK, "should not panic")
}

// --- Logger.Status: level filtering ---

func TestLoggerStatusLevelFilter(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithStructuredOutput(&buf),
		// Raise both levels so Info entries are dropped.
		WithLevel(LevelError),
		WithStructuredLevel(LevelError),
	)

	// Status at Info should be filtered out.
	log.Status(LevelInfo, StatusOK, "this should not appear")

	if out := buf.String(); out != "" {
		t.Errorf("expected no output when filtered, got: %q", out)
	}
}

// TestStatusJSONNoMessageBadge ensures the badge text never appears in the JSON message.
func TestStatusJSONNoMessageBadge(t *testing.T) {
	t.Parallel()

	var buf safeBuffer
	log := New(
		WithConsoleOutput(io.Discard),
		WithStructuredOutput(&buf),
	)
	log.Status(LevelInfo, StatusFail, "clean message", String("code", "404"))
	out := buf.String()

	// The FAIL token must not be embedded in the message string.
	if strings.Contains(out, `"message":"[FAIL`) {
		t.Errorf("badge text leaked into JSON message: %q", out)
	}
}

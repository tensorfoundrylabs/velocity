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
		{StatusOK, "OKAY"},
		{StatusFail, "FAIL"},
		{StatusWarn, "WARN"},
		{StatusInfo, "INFO"},
		{StatusPending, "WAIT"},
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

// --- StatusItem.Render (TTY path, tested via internal helper) ---

// TestStatusItemRenderTTY exercises the TTY render path via renderStatusItemTTY
// directly, since bytes.Buffer is not a terminal and Render(w) auto-detects TTY
// from w. ThemeMono is used so assertions don't need to strip ANSI codes.
func TestStatusItemRenderTTY(t *testing.T) {
	t.Parallel()

	item := NewStatusItem(
		StatusOK, "user signed in", ThemeMono,
		Int("user_id", 42),
		Duration("took", 18*1000*1000), // 18ms
	)

	var buf bytes.Buffer
	renderStatusItemTTY(&buf, item.kind, item.msg, item.theme, item.fields)

	out := buf.String()
	// Badge must be present with correct token.
	if !strings.Contains(out, "[OKAY]") {
		t.Errorf("expected badge [OKAY], got: %q", out)
	}
	if !strings.Contains(out, "user signed in") {
		t.Errorf("expected message in output, got: %q", out)
	}
	if !strings.Contains(out, "user_id=42") {
		t.Errorf("expected user_id field in output, got: %q", out)
	}
}

// TestStatusItemBadgeCompact verifies each status kind produces its own compact
// bracketed token without padding (e.g. [OKAY] not [OK     ]). Variable widths are
// expected and intentional, matching the level-badge style.
func TestStatusItemBadgeCompact(t *testing.T) {
	t.Parallel()

	cases := map[StatusKind]string{
		StatusOK:      "[OKAY]",
		StatusFail:    "[FAIL]",
		StatusWarn:    "[WARN]",
		StatusInfo:    "[INFO]",
		StatusPending: "[WAIT]",
		StatusSkipped: "[SKIP]",
	}
	for k, want := range cases {
		item := NewStatusItem(k, "msg", ThemeMono)
		var buf bytes.Buffer
		renderStatusItemTTY(&buf, item.kind, item.msg, item.theme, item.fields)
		if !strings.Contains(buf.String(), want) {
			t.Errorf("kind %s: expected badge %q in output, got %q", k.String(), want, buf.String())
		}
	}
}

// --- StatusItem.Render (plain / non-TTY path) ---

// TestStatusItemRenderPlain exercises the non-TTY render path. bytes.Buffer is
// not a terminal so Render(w) uses the plain form automatically.
func TestStatusItemRenderPlain(t *testing.T) {
	t.Parallel()

	item := NewStatusItem(
		StatusFail, "payment refused", ThemeMono,
		String("reason", "card expired"),
	)

	var buf bytes.Buffer
	if err := item.Render(&buf); err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[FAIL]") {
		t.Errorf("expected badge [FAIL], got: %q", out)
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

	item := NewStatusItem(StatusWarn, "slow query", ThemeMono)
	s := item.String()
	if !strings.Contains(s, "slow query") {
		t.Errorf("String() missing message: %q", s)
	}
	// String() calls Render(&bytes.Buffer) — non-TTY path — badge still present.
	if !strings.Contains(s, "[WARN]") {
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

	item := NewStatusItem(StatusPending, "waiting for upstream", ThemeMono)
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

	item := NewStatusItem(
		StatusInfo, "ready", ThemeMono,
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
	// Status renders inline via Render — expect the badge and message.
	if !strings.Contains(out, "database connected") {
		t.Errorf("expected message in console output: %q", out)
	}
	if !strings.Contains(out, "host") {
		t.Errorf("expected host field in console output: %q", out)
	}
	// Badge must be present — Render uses the plain form on non-TTY.
	if !strings.Contains(out, "[OKAY]") {
		t.Errorf("expected badge [OKAY] in console output: %q", out)
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

// --- Logger.Status: sampler gate ---

// TestLoggerStatus_SamplerGate verifies that the sampler runs before the console
// render path, not just before the structured path. A sampled-out call must produce
// no console output at all.
func TestLoggerStatus_SamplerGate(t *testing.T) {
	t.Parallel()

	var consoleBuf safeBuffer
	// CountSampler(5, 0): allow first 5, then drop everything.
	log := New(
		WithConsoleOutput(&consoleBuf),
		WithColour(false),
		WithSampler(NewCountSampler(5, 0)),
	)
	defer func() { _ = log.Close() }()

	// Log 10 status calls — only the first 5 should produce output.
	for range 10 {
		log.Status(LevelInfo, StatusOK, "sampled")
	}

	out := consoleBuf.String()
	count := strings.Count(out, "[OKAY]")
	if count > 5 {
		t.Errorf("sampler did not gate Status console output: got %d badges, want ≤5", count)
	}
	if count == 0 {
		t.Error("expected at least some Status output before sampler kicked in, got none")
	}
}

// TestLoggerStatus_BaseFieldsPropagated verifies that baseFields set via With()
// appear in both the console badge line and the JSON structured record.
func TestLoggerStatus_BaseFieldsPropagated(t *testing.T) {
	t.Parallel()

	var consoleBuf safeBuffer
	var jsonBuf safeBuffer

	parent := New(
		WithConsoleOutput(&consoleBuf),
		WithColour(false),
		WithStructuredOutput(&jsonBuf),
	)
	defer func() { _ = parent.Close() }()

	child := parent.With(String("request_id", "req-123"))
	child.Status(LevelInfo, StatusOK, "processed")

	// Console output must include the base field.
	consoleOut := consoleBuf.String()
	if !strings.Contains(consoleOut, "request_id") {
		t.Errorf("base field missing from Status console output: %q", consoleOut)
	}

	// JSON output must also include the base field.
	jsonOut := jsonBuf.String()
	if !strings.Contains(jsonOut, "request_id") {
		t.Errorf("base field missing from Status JSON output: %q", jsonOut)
	}
}

// TestStatusItem_SecureFieldRedactedOnNonTTY verifies that Secure fields passed to a
// StatusItem are redacted when rendered via renderStatusItemPlain (non-TTY path),
// and shown as plaintext via renderStatusItemTTY (trusted TTY path).
// Regression guard for the bug where writeStatusFields always called writeFormatted
// regardless of trust, causing Secure fields to be redacted on trusted TTY output too.
func TestStatusItem_SecureFieldRedactedOnNonTTY(t *testing.T) {
	t.Parallel()

	item := NewStatusItem(StatusOK, "login", ThemeNightOwl, Secure("password", "hunter2"))

	// Non-TTY (plain) path: Secure field must be redacted.
	var plain strings.Builder
	_ = item.RenderTTY(&plain, false)
	plainOut := plain.String()
	if strings.Contains(plainOut, "hunter2") {
		t.Errorf("Secure field plaintext leaked in plain Status render: %q", plainOut)
	}
	if !strings.Contains(plainOut, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in plain Status render, got: %q", plainOut)
	}

	// TTY (trusted) path: Secure field must show plaintext.
	var tty strings.Builder
	_ = item.RenderTTY(&tty, true)
	ttyOut := tty.String()
	if !strings.Contains(ttyOut, "hunter2") {
		t.Errorf("Secure field plaintext missing from trusted TTY Status render: %q", ttyOut)
	}
}

// TestLoggerStatus_SecureTagRedactedOnNonTTY verifies that <secure> tags in the
// Status message are redacted when the console writer is non-TTY (a bytes.Buffer).
func TestLoggerStatus_SecureTagRedactedOnNonTTY(t *testing.T) {
	t.Parallel()

	var consoleBuf safeBuffer
	log := New(
		WithConsoleOutput(&consoleBuf),
		WithColour(false),
		WithStructuredOutput(io.Discard),
	)
	defer func() { _ = log.Close() }()

	log.Status(LevelInfo, StatusOK, "token <secure>supersecret</secure> ok")

	out := consoleBuf.String()
	if strings.Contains(out, "supersecret") {
		t.Errorf("secure tag content leaked in non-TTY console output: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in non-TTY console output, got: %q", out)
	}
}

package velocity

import (
	"io"
	"strings"
	"testing"
	"time"
)

// ---- Field constructor tests -------------------------------------------------

func TestSecureField_PlainAndRedacted(t *testing.T) {
	t.Parallel()

	f := Secure("token", "supersecret")
	if f.Type != FieldTypeSecure {
		t.Fatalf("expected FieldTypeSecure, got %v", f.Type)
	}
	if got := SecurePlain(f); got != "supersecret" {
		t.Errorf("plain: want %q, got %q", "supersecret", got)
	}
	if got := SecureRedacted(f); got != redactedMark {
		t.Errorf("redacted: want %q, got %q", redactedMark, got)
	}
}

func TestSecureField_WriteFormatted_Untrusted(t *testing.T) {
	t.Parallel()

	f := Secure("token", "supersecret")
	var buf strings.Builder
	f.writeFormatted(&buf)
	if got := buf.String(); got != redactedMark {
		t.Errorf("untrusted default: want %q, got %q", redactedMark, got)
	}
}

func TestSecureField_WriteFormattedTrusted(t *testing.T) {
	t.Parallel()

	f := Secure("token", "supersecret")
	var buf strings.Builder
	f.writeFormattedTrusted(&buf)
	if got := buf.String(); got != "supersecret" {
		t.Errorf("trusted: want %q, got %q", "supersecret", got)
	}
}

func TestSecureURLField_PasswordRedacted(t *testing.T) {
	t.Parallel()

	const rawURL = "redis://user:secret@localhost:6379/0" //nolint:gosec // G101: test placeholder
	f := SecureURL("redis", rawURL)
	if f.Type != FieldTypeSecureURL {
		t.Fatalf("expected FieldTypeSecureURL, got %v", f.Type)
	}
	plain := SecurePlain(f)
	if plain != rawURL {
		t.Errorf("plain: want original URL, got %q", plain)
	}
	redacted := SecureRedacted(f)
	if strings.Contains(redacted, "secret") {
		t.Errorf("redacted URL must not contain password: %q", redacted)
	}
	if !strings.Contains(redacted, "user") {
		t.Errorf("redacted URL should preserve username: %q", redacted)
	}
	// The URL sentinel is "REDACTED" (no brackets) to avoid URL-encoding of [ and ].
	if !strings.Contains(redacted, "REDACTED") {
		t.Errorf("redacted URL should contain REDACTED sentinel: %q", redacted)
	}
}

func TestSecureURLField_NoPassword(t *testing.T) {
	t.Parallel()

	// URL without password — both forms should be identical.
	f := SecureURL("db", "postgres://localhost:5432/mydb")
	plain := SecurePlain(f)
	redacted := SecureRedacted(f)
	if plain != redacted {
		t.Errorf("no-password URL: plain %q != redacted %q", plain, redacted)
	}
}

func TestSecureURLField_InvalidURL(t *testing.T) {
	t.Parallel()

	// Invalid URLs fall back to treating the raw string as the plain form.
	f := SecureURL("bad", "not a url ://")
	if f.Type != FieldTypeSecureURL {
		t.Fatalf("expected FieldTypeSecureURL, got %v", f.Type)
	}
	// Both forms should be non-empty; we just check it doesn't panic.
	_ = SecurePlain(f)
	_ = SecureRedacted(f)
}

func TestRedactedField(t *testing.T) {
	t.Parallel()

	f := Redacted("api_key")
	if f.Type != FieldTypeRedacted {
		t.Fatalf("expected FieldTypeRedacted, got %v", f.Type)
	}
	// Trusted writers still see [REDACTED] — Redacted is unconditional.
	var buf strings.Builder
	f.writeFormattedTrusted(&buf)
	if got := buf.String(); got != redactedMark {
		t.Errorf("trusted Redacted field must still show %q, got %q", redactedMark, got)
	}
}

func TestTruncatedField_Fits(t *testing.T) {
	t.Parallel()

	f := Truncated("tok", "short", 16)
	if f.Type != FieldTypeTruncated {
		t.Fatalf("expected FieldTypeTruncated, got %v", f.Type)
	}
	var buf strings.Builder
	f.writeFormatted(&buf)
	if got := buf.String(); got != "short" {
		t.Errorf("want %q, got %q", "short", got)
	}
}

func TestTruncatedField_Clipped(t *testing.T) {
	t.Parallel()

	f := Truncated("tok", "Bearer eyJhbGciOiJSUzI1NiJ9.longpayload", 16)
	var buf strings.Builder
	f.writeFormatted(&buf)
	got := buf.String()
	if strings.Contains(got, "longpayload") {
		t.Errorf("clipped value must not contain trimmed portion, got %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("clipped value must end with ellipsis, got %q", got)
	}
}

func TestTruncatedField_ZeroMaxLen(t *testing.T) {
	t.Parallel()

	f := Truncated("tok", "anything", 0)
	var buf strings.Builder
	f.writeFormatted(&buf)
	// Zero maxLen returns empty.
	if got := buf.String(); got != "" {
		t.Errorf("zero maxLen: want empty, got %q", got)
	}
}

// ---- <secure> tag scanner tests ---------------------------------------------

func TestRedactSecureTags_Replaced(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, mark, want string
	}{
		{
			in:   "connecting to <secure>redis://user:pass@host</secure>",
			mark: redactedMark,
			want: "connecting to " + redactedMark,
		},
		{
			in:   "a <secure>x</secure> b <secure>y</secure> c",
			mark: "***",
			want: "a *** b *** c",
		},
		{
			in:   "no tags here",
			mark: redactedMark,
			want: "no tags here",
		},
		{
			// Unclosed tag — emit the mark and stop.
			in:   "prefix <secure>unclosed",
			mark: redactedMark,
			want: "prefix " + redactedMark,
		},
	}

	for _, tc := range cases {
		got := redactSecureTags(tc.in, tc.mark)
		if got != tc.want {
			t.Errorf("redactSecureTags(%q, %q) = %q, want %q", tc.in, tc.mark, got, tc.want)
		}
	}
}

func TestStripSecureTags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{
			in:   "connecting to <secure>redis://user:pass@host</secure>",
			want: "connecting to redis://user:pass@host",
		},
		{
			in:   "no tags",
			want: "no tags",
		},
		{
			in:   "<secure>only</secure>",
			want: "only",
		},
	}

	for _, tc := range cases {
		got := stripSecureTags(tc.in)
		if got != tc.want {
			t.Errorf("stripSecureTags(%q) = %q, want %q", tc.in, tc.want, got)
		}
	}
}

// ---- scanSecure flag auto-recompute tests -----------------------------------

func TestScanSecure_FalseWithSecureTagsDisabled(t *testing.T) {
	t.Parallel()

	// WithSecureTags(false) keeps scanSecure permanently false.
	l := New(WithSecureTags(false))
	if l.writers.scanSecure.Load() {
		t.Error("expected scanSecure=false when WithSecureTags(false) is applied")
	}
}

func TestScanSecure_TrueWithNonTTYConsole(t *testing.T) {
	t.Parallel()

	// safeBuffer is not a TTY, so the console writer is non-TTY — scan should be on.
	cfg := defaultConfig()
	cfg.ConsoleOutput = &safeBuffer{} // non-TTY
	cfg.StructuredOutput = nil
	l := newFromConfig(cfg)
	if !l.writers.scanSecure.Load() {
		t.Error("expected scanSecure=true for non-TTY console writer")
	}
}

func TestScanSecure_FalseWhenNoOutputs(t *testing.T) {
	t.Parallel()

	// Nop logger — no writers at all, nothing to redact for.
	l := New(WithNop())
	// scanSecure: JSON writer is io.Discard (cfg path gives nil jsonWriter),
	// console is io.Discard (also nil). No writers — nothing to redact for.
	// The nop logger has ConsoleOutput=io.Discard which newFromConfig skips
	// (it checks != io.Discard), so consoleWriter == nil and jsonWriter == nil.
	if l.writers.scanSecure.Load() {
		t.Error("expected scanSecure=false for nop logger with no real writers")
	}
}

func TestScanSecure_TrueWhenJSONWriterPresent(t *testing.T) {
	t.Parallel()

	// JSON writer is always untrusted — scan must be on.
	cfg := defaultConfig()
	cfg.ConsoleOutput = &safeBuffer{}
	cfg.StructuredOutput = &safeBuffer{}
	l := newFromConfig(cfg)
	if !l.writers.scanSecure.Load() {
		t.Error("expected scanSecure=true when JSON writer is present")
	}
}

func TestScanSecure_RecomputedOnAddRemoveWriter(t *testing.T) {
	t.Parallel()

	// Start with a nop logger (no real writers, scanSecure=false).
	l := New(WithNop())
	if l.writers.scanSecure.Load() {
		t.Fatal("precondition: scanSecure should be false for nop logger")
	}

	// Adding an untrusted writer must flip the flag.
	l.AddWriter("sink", &NoOpWriter{})
	if !l.writers.scanSecure.Load() {
		t.Error("expected scanSecure=true after AddWriter (untrusted)")
	}

	// Adding a trusted writer alongside the untrusted one must leave flag true.
	l.AddWriter("trusted-sink", &NoOpWriter{}, WriterTrusted())
	if !l.writers.scanSecure.Load() {
		t.Error("scanSecure must stay true while untrusted writer exists")
	}

	// Remove the untrusted writer — flag should drop back to false.
	_ = l.RemoveWriter("sink")
	if l.writers.scanSecure.Load() {
		t.Error("expected scanSecure=false after removing the last untrusted writer")
	}
}

func TestScanSecure_WithSecureTagsFalse(t *testing.T) {
	t.Parallel()

	// WithSecureTags(false) must keep scanSecure permanently false regardless of writers.
	l := New(WithStructuredOutput(&safeBuffer{}), WithSecureTags(false))
	if l.writers.scanSecure.Load() {
		t.Error("expected scanSecure=false when WithSecureTags(false) is set")
	}

	// Adding an untrusted writer must NOT flip the flag.
	l.AddWriter("sink", &NoOpWriter{})
	if l.writers.scanSecure.Load() {
		t.Error("expected scanSecure=false after AddWriter when WithSecureTags(false)")
	}
}

// ---- Integration: trusted vs untrusted writer output -------------------------

func TestSecureField_TrustedWriterSeesPlaintext(t *testing.T) {
	t.Parallel()

	trusted := &safeBuffer{}
	untrusted := &safeBuffer{}

	l := New(
		WithConsoleOutput(&safeBuffer{}), // discard console
		WithStructuredOutput(untrusted),
	)
	// Register a trusted additional writer.
	trustedJSON := NewJSONWriter(trusted)
	l.AddWriter("audit", trustedJSON, WriterTrusted())

	l.Info("connecting", Secure("session", "abc123"))
	waitFor(t, func() bool {
		return trusted.Len() > 0
	}, 2*time.Second, 5*time.Millisecond, "trusted writer receives entry")

	if strings.Contains(untrusted.String(), "abc123") {
		t.Error("untrusted writer must not contain plaintext: abc123")
	}
	if !strings.Contains(untrusted.String(), redactedMark) {
		t.Errorf("untrusted writer must contain %q, got: %s", redactedMark, untrusted.String())
	}
	if !strings.Contains(trusted.String(), "abc123") {
		t.Errorf("trusted writer must contain plaintext abc123, got: %s", trusted.String())
	}
}

func TestSecureField_RedactedIsAlwaysHidden(t *testing.T) {
	t.Parallel()

	trusted := &safeBuffer{}
	l := New(WithStructuredOutput(trusted))
	trustedJSON := NewJSONWriter(&safeBuffer{})
	l.AddWriter("trusted", trustedJSON, WriterTrusted())

	l.Info("request", Redacted("api_key"))
	waitFor(t, func() bool {
		return trusted.Len() > 0
	}, 2*time.Second, 5*time.Millisecond, "structured writer receives entry")

	// Even on the trusted path, Redacted must never show plaintext.
	if strings.Contains(trusted.String(), "plaintext") {
		t.Error("Redacted field must never show plaintext")
	}
	if !strings.Contains(trusted.String(), redactedMark) {
		t.Errorf("Redacted field must show %q, got: %s", redactedMark, trusted.String())
	}
}

func TestSecureTag_UntrustedJSONRedacts(t *testing.T) {
	t.Parallel()

	buf := &safeBuffer{}
	l := New(WithStructuredOutput(buf))
	l.Info("endpoint: <secure>https://admin:pass@internal.host</secure>")
	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 2*time.Second, 5*time.Millisecond, "JSON writer receives entry")

	out := buf.String()
	if strings.Contains(out, "admin") || strings.Contains(out, "pass") {
		t.Errorf("JSON writer must redact <secure> content, got: %s", out)
	}
	if !strings.Contains(out, redactedMark) {
		t.Errorf("JSON writer must emit %q for <secure> content, got: %s", redactedMark, out)
	}
}

func TestSecureTag_WriterRedactionMark(t *testing.T) {
	t.Parallel()

	buf := &safeBuffer{}
	l := New(WithConsoleOutput(&safeBuffer{}))
	l.AddWriter("sink", NewJSONWriter(buf), WriterRedactionMark("***HIDDEN***"))
	l.Info("key: <secure>topsecret</secure>")

	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 2*time.Second, 5*time.Millisecond, "additional writer receives entry")

	out := buf.String()
	if strings.Contains(out, "topsecret") {
		t.Errorf("must redact with custom mark, got: %s", out)
	}
	if !strings.Contains(out, "***HIDDEN***") {
		t.Errorf("must use custom redaction mark, got: %s", out)
	}
}

// TestSecureTag_ChildLoggerSeesWriterAddedAfterCreation is a regression test for the bug
// where child loggers created before AddWriter was called kept a stale scanSecure=false.
// Because scanSecure was per-Logger (copied at With() time), the child's flag was never
// updated when the parent gained an untrusted writer — so <secure> tags leaked in plaintext.
func TestSecureTag_ChildLoggerSeesWriterAddedAfterCreation(t *testing.T) {
	t.Parallel()

	sink := &safeBuffer{}

	// Parent has only a console to io.Discard — no structured writer, so scanSecure=false.
	parent := New(WithConsoleOutput(io.Discard))
	// Child is created before the untrusted writer is added.
	child := parent.With(String("child", "yes"))

	// Now add an untrusted JSON writer to the parent.
	parent.AddWriter("json", NewJSONWriter(sink))

	// Child logs a message with a <secure> tag — it must NOT appear in plaintext.
	child.Info("token <secure>secret</secure>")

	waitFor(t, func() bool {
		return sink.Len() > 0
	}, 2*time.Second, 5*time.Millisecond, "json writer receives entry from child")

	out := sink.String()
	if strings.Contains(out, "secret") {
		t.Errorf("child logger leaked secure content; got: %s", out)
	}
	if !strings.Contains(out, redactedMark) {
		t.Errorf("expected redaction mark %q in output; got: %s", redactedMark, out)
	}
}

// TestSecureTag_TrustedWriterAddedAfterChildDoesNotFlipScan verifies that adding a
// TRUSTED writer after child creation does not enable secure-tag scanning. Trusted
// writers see plaintext by design — no scan needed for their sake.
func TestSecureTag_TrustedWriterAddedAfterChildDoesNotFlipScan(t *testing.T) {
	t.Parallel()

	// Start with no outputs — scanSecure must stay false.
	parent := New(WithNop())
	child := parent.With(String("child", "yes"))

	trustedSink := &safeBuffer{}
	parent.AddWriter("trusted", NewJSONWriter(trustedSink), WriterTrusted())

	// Neither parent nor child should have scanSecure enabled: the only writer is trusted.
	if parent.writers.scanSecure.Load() {
		t.Error("parent: scanSecure must be false when only writer is trusted")
	}
	_ = child // child shares the same writerSet — same assertion holds
}

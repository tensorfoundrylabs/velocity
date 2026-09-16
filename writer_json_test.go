package velocity

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"unsafe"
)

func TestJSONWriter_NilErrorField(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	var errVal error
	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "test",
		Fields: []Field{{
			Key:   "err",
			Type:  FieldTypeError,
			value: unsafe.Pointer(&errVal),
		}},
	}

	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "null") {
		t.Errorf("expected null for nil error, got: %s", buf.String())
	}
}

func TestJSONWriter_NilStringerField(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "test",
		Fields: []Field{{
			Key:  "s",
			Type: FieldTypeStringer,
		}},
	}

	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "null") {
		t.Errorf("expected null for nil stringer, got: %s", buf.String())
	}
}

func TestJSONWriter_AddCaller(t *testing.T) {
	var buf bytes.Buffer

	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true

	log := newFromConfig(cfg)
	log.Info("caller test")

	output := buf.String()
	if !strings.Contains(output, `"caller"`) {
		t.Fatalf("expected JSON output to contain caller field, got: %s", output)
	}
	// caller is now a separate string field; line is a separate numeric field
	if !strings.Contains(output, `"line"`) {
		t.Fatalf("expected JSON output to contain line field, got: %s", output)
	}
	if !strings.Contains(output, `_test.go"`) {
		t.Fatalf("expected caller to reference a test file, got: %s", output)
	}
}

// TestJSONWriter_ControlCharEscaping verifies that control characters in strings
// are encoded as \uXXXX sequences rather than raw bytes.
func TestJSONWriter_ControlCharEscaping(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	// Build message with raw control chars at runtime so the source stays clean.
	msg := "ctrl:" + string([]byte{0x01, 0x1f})

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: msg,
	}

	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	output := buf.String()
	// The JSON encoder must produce  and , not raw control bytes.
	if !strings.Contains(output, "\\u0001") {
		t.Errorf("expected \\u0001 escape in JSON output, got: %s", output)
	}
	if !strings.Contains(output, "\\u001f") {
		t.Errorf("expected \\u001f escape in JSON output, got: %s", output)
	}
}

func TestJSONWriter_CallerEscaping(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "escaping test",
		Caller:  `path\to\file.go`,
		Line:    42,
	}

	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	output := buf.String()

	// Must be valid JSON.
	// Manually verify required fields are present and properly escaped.
	if !strings.Contains(output, `"caller"`) {
		t.Errorf("expected caller field, got: %s", output)
	}
	if !strings.Contains(output, `"line"`) {
		t.Errorf("expected line field, got: %s", output)
	}
	// Backslashes must be escaped as \\ in JSON.
	if !strings.Contains(output, `path\\to\\file.go`) {
		t.Errorf("expected escaped backslashes in caller, got: %s", output)
	}
	// line must be a bare number, not quoted.
	if !strings.Contains(output, `"line":42`) {
		t.Errorf("expected numeric line value, got: %s", output)
	}
}

// TestConsoleOnlyLogger_LevelGate verifies that a console-only logger with a high
// console level does not process entries below that level. Previously, the default
// StructuredLevel (Info) was included in the effective-level min even when no
// structured output was configured, causing Debug entries to not be filtered when
// the console level was set to Warn.
func TestConsoleOnlyLogger_LevelGate(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = &buf
	cfg.ConsoleLevel = LevelWarn
	cfg.StructuredOutput = nil // no structured output
	cfg.StructuredLevel = LevelInfo
	log := newFromConfig(cfg)

	log.Info("should not appear")
	log.Debug("should not appear")

	if buf.Len() != 0 {
		t.Errorf("expected no output for sub-Warn entries on console-only logger, got: %q", buf.String())
	}

	log.Warn("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Errorf("expected Warn entry to appear, got: %q", buf.String())
	}
}

// TestStatus_CallerPoints_ToCallSite verifies that captureCaller is invoked
// with the right skip depth for Status, so the reported caller is the test
// function itself, not an internal dispatch helper.
func TestStatus_CallerPoints_ToCallSite(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true
	log := newFromConfig(cfg)

	log.Status(LevelInfo, StatusOK, "caller check") //nolint:testableexamples // line number pinned
	out := buf.String()
	if !strings.Contains(out, `writer_json_test.go`) {
		t.Errorf("Status caller should point to this test file, got: %s", out)
	}
}

// TestGroup_CallerPoints_ToCallSite verifies that captureCaller is invoked
// with the right skip depth for Group.
func TestGroup_CallerPoints_ToCallSite(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true
	log := newFromConfig(cfg)

	log.Group(LevelInfo, "caller check", GroupItem{Text: "item"})
	out := buf.String()
	if !strings.Contains(out, `writer_json_test.go`) {
		t.Errorf("Group caller should point to this test file, got: %s", out)
	}
}

// TestContinue_CallerPoints_ToCallSite verifies that captureCaller is invoked
// with the right skip depth for Continue.
func TestContinue_CallerPoints_ToCallSite(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.ConsoleOutput = nil
	cfg.StructuredOutput = &buf
	cfg.StructuredLevel = LevelDebug
	cfg.AddCaller = true
	log := newFromConfig(cfg)

	log.Continue(LevelInfo, "caller check", "line one")
	out := buf.String()
	if !strings.Contains(out, `writer_json_test.go`) {
		t.Errorf("Continue caller should point to this test file, got: %s", out)
	}
}

// jsonRefString round-trips s through encoding/json to obtain the exact string
// the reference encoder yields for the same bytes: each malformed byte becomes
// one U+FFFD, valid runes survive unchanged.
func jsonRefString(t *testing.T, s string) string {
	t.Helper()
	ref, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal reference: %v", err)
	}
	var want string
	if err := json.Unmarshal(ref, &want); err != nil {
		t.Fatalf("json.Unmarshal reference: %v", err)
	}
	return want
}

// TestJSONWriter_InvalidUTF8Replacement pins the shared JSON string encoder to
// encoding/json's malformed-UTF-8 behaviour: every undecodable byte becomes one
// U+FFFD so the stream stays valid UTF-8 for strict consumers. json.Unmarshal
// tolerates malformed UTF-8, so validity is asserted with utf8.Valid directly
// and decoded values are compared against encoding/json's own output.
func TestJSONWriter_InvalidUTF8Replacement(t *testing.T) {
	t.Parallel()

	// Error text carrying malformed UTF-8, built at runtime to keep the source
	// printable.
	errVal := errors.New("boom " + string([]byte{0xff, 0xfe}) + " end")

	cases := []struct {
		name     string
		refInput string
		build    func() *Entry
		pluck    func(t *testing.T, m map[string]any) string
		wantFFFD int
	}{
		{
			name:     "message isolated continuation byte",
			refInput: "broken \x80 byte",
			build: func() *Entry {
				return &Entry{Message: "broken \x80 byte"}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				return jsonStringField(t, m, "message")
			},
			wantFFFD: 1,
		},
		{
			name:     "message truncated three-byte sequence",
			refInput: "trunc \xe2\x82",
			build: func() *Entry {
				return &Entry{Message: "trunc \xe2\x82"}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				return jsonStringField(t, m, "message")
			},
			// Both bytes of the truncated sequence are individually undecodable.
			wantFFFD: 2,
		},
		{
			name:     "message malformed multibyte sequence",
			refInput: "bad \xc3(A",
			build: func() *Entry {
				return &Entry{Message: "bad \xc3(A"}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				return jsonStringField(t, m, "message")
			},
			// \xc3 lacks its continuation byte; "(A" survives.
			wantFFFD: 1,
		},
		{
			name:     "string field value with invalid bytes",
			refInput: "value \x81\x82 here",
			build: func() *Entry {
				v := "value \x81\x82 here"
				return &Entry{Fields: []Field{{
					Key:   "v",
					Type:  FieldTypeString,
					value: unsafe.Pointer(&v),
				}}}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				return jsonStringField(t, m, "v")
			},
			wantFFFD: 2,
		},
		{
			name:     "field key with invalid bytes",
			refInput: "k\xff",
			build: func() *Entry {
				v := "v"
				return &Entry{Fields: []Field{{
					Key:   "k\xff",
					Type:  FieldTypeString,
					value: unsafe.Pointer(&v),
				}}}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				for k := range m {
					if k[0] == 'k' {
						return k
					}
				}
				t.Fatalf("no key starting with 'k' in decoded object: %v", m)
				return ""
			},
			wantFFFD: 1,
		},
		{
			name:     "error text with invalid bytes",
			refInput: errVal.Error(),
			build: func() *Entry {
				return &Entry{Fields: []Field{{
					Key:   "err",
					Type:  FieldTypeError,
					value: unsafe.Pointer(&errVal),
				}}}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				return jsonStringField(t, m, "err")
			},
			wantFFFD: 2,
		},
		{
			name:     "message valid emoji passes through",
			refInput: "done \U0001F389 well 日本語",
			build: func() *Entry {
				return &Entry{Message: "done \U0001F389 well 日本語"}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				return jsonStringField(t, m, "message")
			},
			wantFFFD: 0,
		},
		{
			name:     "message literal U+FFFD passes through",
			refInput: "lit \uFFFD end",
			build: func() *Entry {
				return &Entry{Message: "lit \uFFFD end"}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				return jsonStringField(t, m, "message")
			},
			// The one U+FFFD present came from the input, not from replacement.
			wantFFFD: 1,
		},
		{
			name:     "message U+2028 passes through",
			refInput: "line1\u2028line2",
			build: func() *Entry {
				return &Entry{Message: "line1\u2028line2"}
			},
			pluck: func(t *testing.T, m map[string]any) string {
				return jsonStringField(t, m, "message")
			},
			wantFFFD: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			w := NewJSONWriter(&buf)

			e := tc.build()
			e.Time = time.Now()
			e.Level = LevelInfo
			if err := w.Write(e); err != nil {
				t.Fatalf("Write returned error: %v", err)
			}

			raw := buf.Bytes()
			if !utf8.Valid(raw) {
				t.Fatalf("output is not valid UTF-8: %q", raw)
			}

			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("output is not parseable JSON: %v; raw: %s", err, raw)
			}

			got := tc.pluck(t, decoded)
			if want := jsonRefString(t, tc.refInput); got != want {
				t.Errorf("decoded value %q does not match encoding/json reference %q", got, want)
			}
			if n := strings.Count(got, "\uFFFD"); n != tc.wantFFFD {
				t.Errorf("U+FFFD count = %d, want %d (value %q)", n, tc.wantFFFD, got)
			}
		})
	}
}

// TestJSONWriter_ValidUnicodePassthrough asserts valid multibyte content keeps
// its original bytes: velocity leaves U+2028 unescaped (legal in JSON strings;
// escaping it like encoding/json is optional) and a literal U+FFFD from the
// input is neither doubled nor escaped.
func TestJSONWriter_ValidUnicodePassthrough(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "emoji \U0001F389 ls \u2028 repl \uFFFD end",
	}
	if err := w.Write(e); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	raw := buf.String()
	if !utf8.ValidString(raw) {
		t.Fatalf("output is not valid UTF-8: %q", raw)
	}
	for _, frag := range []string{"\U0001F389", "\u2028", "\uFFFD"} {
		if !strings.Contains(raw, frag) {
			t.Errorf("expected %q to pass through as raw bytes, got: %s", frag, raw)
		}
	}
	// Exactly one U+FFFD: the literal one, with no replacement added.
	if n := strings.Count(raw, "\uFFFD"); n != 1 {
		t.Errorf("U+FFFD count = %d, want 1 (the literal from the message)", n)
	}
}

func jsonStringField(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key].(string)
	if !ok {
		t.Fatalf("decoded object has no string field %q: %v", key, m)
	}
	return v
}

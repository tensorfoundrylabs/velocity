package velocity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// An Any field holding an error must render its message text, not the {} that
// json.Marshal produces for values whose content lives in unexported fields
// (errors.New, fmt.Errorf, every runtime.Error including recovered panics).
func TestJSONWriter_AnyErrorRendersMessage(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("write panicked: %v", r)
		}
	}()

	// A recovered index-out-of-range is a runtime.Error; its Error() carries
	// the panic message json.Marshal would discard.
	var panicErr error
	func() {
		defer func() { panicErr, _ = recover().(error) }()
		s := []int{1}
		_ = s[5] //nolint:gosec // the out-of-range index is the point: it yields a runtime.Error
	}()
	if panicErr == nil {
		t.Fatal("recover did not yield a runtime error")
	}

	inner := errors.New("inner failure")
	var nilErr *typedNilError
	var nilStr *typedNilStringer
	cases := []struct {
		key string
		val any
	}{
		{"plain", errors.New("plain failure")},
		{"wrapped", fmt.Errorf("outer: %w", inner)},
		{"panic", panicErr},
		{"nilErr", nilErr},
		{"nilStr", nilStr},
	}
	fields := make([]Field, 0, len(cases))
	for _, c := range cases {
		fields = append(fields, Any(c.key, c.val))
	}

	e := &Entry{Time: time.Now(), Level: LevelInfo, Message: "any errors", Fields: fields}
	if err := w.Write(e); err != nil {
		t.Fatalf("write: %v", err)
	}

	line := buf.String()
	for _, want := range []string{
		"plain failure",
		"outer: inner failure",
		"index out of range",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q does not contain %q: error message lost in Any rendering", line, want)
		}
	}

	// Typed nils must fall through to json.Marshal and render null, never
	// call a method on the nil receiver.
	if !strings.Contains(line, `"nilErr":null`) || !strings.Contains(line, `"nilStr":null`) {
		t.Fatalf("line %q does not render typed nils as null", line)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

// typedNilError dereferences its receiver, so calling Error() on the typed
// nil panics; exactly the case the guard must keep away from the method.
type typedNilError struct {
	msg string
}

func (e *typedNilError) Error() string { return e.msg }

type typedNilStringer struct {
	msg string
}

func (s *typedNilStringer) String() string { return s.msg }

// A Stringer whose marshaled form is an empty object must render String()
// rather than {}.
func TestJSONWriter_AnyStringerEmptyMarshalPrefersString(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	type opaque struct{ secret int }
	fields := []Field{Any("v", stringerVal{opaque{7}})}

	e := &Entry{Time: time.Now(), Level: LevelInfo, Message: "stringer", Fields: fields}
	if err := w.Write(e); err != nil {
		t.Fatalf("write: %v", err)
	}

	line := buf.String()
	if !strings.Contains(line, "opaque value 7") {
		t.Fatalf("line %q does not contain the Stringer text", line)
	}
}

type stringerVal struct {
	inner any
}

func (s stringerVal) String() string { return "opaque value 7" }

// structuredDeployError carries both an Error() message and an explicit JSON form;
// the explicit form must win so structured errors keep their shape.
type structuredDeployError struct {
	Code  int
	Stage string
}

func (e *structuredDeployError) Error() string {
	return "stage " + e.Stage + " failed with code 42"
}

func (e *structuredDeployError) MarshalJSON() ([]byte, error) {
	return []byte(`{"code":42,"stage":"` + e.Stage + `"}`), nil
}

func TestJSONWriter_AnyMarshalerErrorKeepsJSONForm(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "marshaler error",
		Fields:  []Field{Any("err", &structuredDeployError{Code: 42, Stage: "deploy"})},
	}
	if err := w.Write(e); err != nil {
		t.Fatalf("write: %v", err)
	}

	line := buf.String()
	if !strings.Contains(line, `"code":42`) {
		t.Fatalf("line %q does not carry the MarshalJSON form", line)
	}
	if strings.Contains(line, "failed with code") {
		t.Fatalf("line %q rendered Error() over the explicit JSON form", line)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

// marshalFailError's MarshalJSON always fails; the ladder must fall through
// to Error() rather than emitting a marshal diagnostic.
type marshalFailError struct{}

func (e *marshalFailError) Error() string { return "marshal failed but message survives" }

func (e *marshalFailError) MarshalJSON() ([]byte, error) {
	return nil, errors.New("unserialisable")
}

func TestJSONWriter_AnyFailingMarshalJSONFallsBackToError(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	e := &Entry{
		Time:    time.Now(),
		Level:   LevelInfo,
		Message: "failing marshaler",
		Fields:  []Field{Any("err", &marshalFailError{})},
	}
	if err := w.Write(e); err != nil {
		t.Fatalf("write: %v", err)
	}

	line := buf.String()
	if !strings.Contains(line, "marshal failed but message survives") {
		t.Fatalf("line %q lost the Error() message behind a failing MarshalJSON", line)
	}
	if strings.Contains(line, "marshal failed: unserialisable") {
		t.Fatalf("line %q rendered the marshal diagnostic instead of falling back to Error()", line)
	}
}

// rawJSONMarshaler lets the JSON-lines tests exercise custom MarshalJSON
// implementations alongside json.RawMessage, which uses the same interface.
type rawJSONMarshaler []byte

func (m rawJSONMarshaler) MarshalJSON() ([]byte, error) { return m, nil }

func TestJSONWriter_AnyMarshalerOutputIsValidCompactJSONLine(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%t", async), func(t *testing.T) {
			var sink safeBuffer
			var w *JSONWriter
			if async {
				w = NewAsyncJSONWriter(&sink, AsyncConfig{Queue: 1, OnFull: AsyncBlock})
			} else {
				w = NewJSONWriter(&sink)
			}

			entry := &Entry{
				Time:    time.Now(),
				Level:   LevelInfo,
				Message: "pretty raw JSON",
				Fields: []Field{
					Any("raw", json.RawMessage([]byte("{\n  \"nested\": true\n}"))),
					Any("custom", rawJSONMarshaler([]byte("[\n  1,\n  2\n]"))),
				},
			}
			if err := w.Write(entry); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			line := sink.String()
			if got := strings.Count(line, "\n"); got != 1 {
				t.Fatalf("output has %d physical newlines, want one JSON line: %q", got, line)
			}
			var record struct {
				Raw    json.RawMessage `json:"raw"`
				Custom json.RawMessage `json:"custom"`
			}
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			if got := string(record.Raw); got != `{"nested":true}` {
				t.Fatalf("raw field = %q, want compact JSON", got)
			}
			if got := string(record.Custom); got != `[1,2]` {
				t.Fatalf("custom field = %q, want compact JSON", got)
			}
		})
	}
}

func TestJSONWriter_AnyMalformedMarshalerOutputFallsBackToDiagnostic(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%t", async), func(t *testing.T) {
			var sink safeBuffer
			var w *JSONWriter
			if async {
				w = NewAsyncJSONWriter(&sink, AsyncConfig{Queue: 1, OnFull: AsyncBlock})
			} else {
				w = NewJSONWriter(&sink)
			}

			entry := &Entry{
				Time:    time.Now(),
				Level:   LevelInfo,
				Message: "malformed raw JSON",
				Fields: []Field{
					Any("raw", json.RawMessage([]byte(`{"unterminated":`))),
					Any("custom", rawJSONMarshaler([]byte(`[`))),
				},
			}
			if err := w.Write(entry); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			line := sink.String()
			if got := strings.Count(line, "\n"); got != 1 {
				t.Fatalf("output has %d physical newlines, want one JSON line: %q", got, line)
			}
			var record struct {
				Raw    string `json:"raw"`
				Custom string `json:"custom"`
			}
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
			if !strings.Contains(record.Raw, "<velocity: JSON marshal failed:") {
				t.Fatalf("raw field = %q, want marshal diagnostic", record.Raw)
			}
			if !strings.Contains(record.Custom, "<velocity: JSON marshal failed:") {
				t.Fatalf("custom field = %q, want marshal diagnostic", record.Custom)
			}
		})
	}
}

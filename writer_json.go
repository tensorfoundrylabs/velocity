package velocity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// JSONWriter emits structured JSON records. Its field order is hand-chosen,
// not betteralign-sorted: async sits immediately after out so the sync
// path's nil check on it shares the hot cache line. With the pointer
// elsewhere the check cost the parallel structured path a measured ~5%.
type JSONWriter struct { // betteralign:ignore
	out io.Writer
	// async, when non-nil, routes completed records through a bounded queue
	// drained by one goroutine so callers never wait on the write syscall.
	// Set once at construction and read-only afterwards. A nil check on the
	// sync path is its only cost.
	async *jsonAsync

	jsonPool  sync.Pool
	closeErr  error
	closeDone chan struct{}

	// inFlight tracks admitted write cycles so Close drains calls that are
	// still formatting (a Stringer, Error or Any marshal can block or reenter)
	// rather than rejecting their bytes mid-flight. Same pattern as
	// ConsoleWriter; see the F2 finding in the finish review.
	inFlight sync.WaitGroup

	// Family close lifecycle for direct Close callers, mirroring MultiWriter
	// and ConsoleWriter: concurrent Closes wait for the same completed drain
	// and return the same recorded closeErr (WP2 contract).
	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool
}

// admit is the admission critical section: the closed check and the in-flight
// registration happen under one mutex acquisition, so Close can never slip
// between them. Returns false when the writer is closed.
func (w *JSONWriter) admit() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	w.inFlight.Add(1)
	return true
}

func NewJSONWriter(out io.Writer) *JSONWriter {
	w := &JSONWriter{
		out: out,
	}
	w.jsonPool.New = func() any { return bytes.NewBuffer(make([]byte, 0, bufMediumSize)) }
	return w
}

// getJSONBuffer retains each JSON buffer up to 32 KiB. This keeps common
// structured records reusable without retaining exceptional input.
func (w *JSONWriter) getJSONBuffer() *bytes.Buffer {
	buf, ok := w.jsonPool.Get().(*bytes.Buffer)
	if !ok || buf == nil {
		buf = bytes.NewBuffer(make([]byte, 0, bufMediumSize))
	}
	buf.Reset()
	return buf
}

func (w *JSONWriter) putJSONBuffer(buf *bytes.Buffer) {
	if buf != nil && buf.Cap() <= bufXLargeSize {
		w.jsonPool.Put(buf)
	}
}

func appendFloat(buf *bytes.Buffer, f float64) {
	buf.Write(strconv.AppendFloat(buf.AvailableBuffer(), f, 'g', -1, 64))
}

func (w *JSONWriter) Write(e *Entry) error {
	// JSON writer is never trusted — always redact.
	return w.WriteSecure(e, false, "[REDACTED]")
}

// WriteStatus emits a status-aware JSON line. The StatusKind is serialised as
// a "status" field with a lowercase value (e.g. "ok", "fail"). All other fields
// are serialised normally via WriteSecure. The badge text is never embedded in
// the message — JSON consumers must read the "status" field.
func (w *JSONWriter) WriteStatus(e *Entry) error {
	return w.WriteStatusSecure(e, false, "[REDACTED]")
}

// WriteStatusSecure is the trust-aware JSON status write path.
func (w *JSONWriter) WriteStatusSecure(e *Entry, trusted bool, redactionMark string) error {
	if !w.admit() {
		return ErrWriterClosed
	}
	defer w.inFlight.Done()

	rawBuf := w.getJSONBuffer()
	buf := NewBytesBuffer(rawBuf)

	w.formatJSONStatusSecure(buf, e, trusted, redactionMark)

	// Append the newline inside the buffer so a single Write call emits a complete
	// line — halving the number of syscalls per entry versus a separate Write(newlineByte).
	_ = buf.WriteByte('\n')

	if w.async != nil {
		w.enqueueAsync(rawBuf, e.Level == LevelFatal)
		return nil
	}
	w.mu.Lock()
	_, err := w.out.Write(rawBuf.Bytes())
	w.mu.Unlock()
	w.putJSONBuffer(rawBuf)
	if err != nil {
		return fmt.Errorf("json write failed: %w", err)
	}
	return nil
}

func (w *JSONWriter) formatJSONStatusSecure(buf *BytesBuffer, e *Entry, trusted bool, redactionMark string) {
	_ = buf.WriteByte('{')

	w.writeJSONString(buf, "timestamp")
	_ = buf.WriteByte(':')
	w.writeJSONTime(buf, e.Time)

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "level")
	_ = buf.WriteByte(':')
	w.writeJSONString(buf, e.Level.String())

	// Emit the status field before message so consumers can filter without parsing.
	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "status")
	_ = buf.WriteByte(':')
	w.writeJSONString(buf, e.statusKind.statusJSONValue())

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "message")
	_ = buf.WriteByte(':')
	msg := e.Message
	if e.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}
	w.writeJSONString(buf, msg)

	if e.Caller != "" {
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "caller")
		_ = buf.WriteByte(':')
		w.writeJSONString(buf, e.Caller)
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "line")
		_ = buf.WriteByte(':')
		buf.WriteInt(int64(e.Line))
	}

	for _, f := range e.Fields {
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, f.Key)
		_ = buf.WriteByte(':')
		w.writeJSONFieldValueSecure(buf, f, trusted, redactionMark)
	}

	_ = buf.WriteByte('}')
}

// WriteGroup emits a JSON entry for a Logger.Group call.
// The entry message is the plain header string "msg (N)"; the structured fields
// "count" (int) and "items" (string array, markers stripped) are added.
func (w *JSONWriter) WriteGroup(e *Entry, items []GroupItem) error {
	return w.WriteGroupSecure(e, items, false, "[REDACTED]")
}

// WriteGroupSecure is the trust-aware group JSON write path.
func (w *JSONWriter) WriteGroupSecure(e *Entry, items []GroupItem, trusted bool, redactionMark string) error {
	if !w.admit() {
		return ErrWriterClosed
	}
	defer w.inFlight.Done()

	rawBuf := w.getJSONBuffer()
	buf := NewBytesBuffer(rawBuf)

	w.formatJSONGroupSecure(buf, e, items, trusted, redactionMark)
	_ = buf.WriteByte('\n')

	if w.async != nil {
		w.enqueueAsync(rawBuf, e.Level == LevelFatal)
		return nil
	}
	w.mu.Lock()
	_, err := w.out.Write(rawBuf.Bytes())
	w.mu.Unlock()
	w.putJSONBuffer(rawBuf)
	if err != nil {
		return fmt.Errorf("json write failed: %w", err)
	}
	return nil
}

func (w *JSONWriter) formatJSONGroupSecure(buf *BytesBuffer, e *Entry, items []GroupItem, trusted bool, redactionMark string) {
	_ = buf.WriteByte('{')

	w.writeJSONString(buf, "timestamp")
	_ = buf.WriteByte(':')
	w.writeJSONTime(buf, e.Time)

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "level")
	_ = buf.WriteByte(':')
	w.writeJSONString(buf, e.Level.String())

	// Emit message without the " (N)" suffix — the count field carries that.
	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "message")
	_ = buf.WriteByte(':')
	// Strip the " (N)" suffix from the composite message so the JSON message is clean.
	msg := e.Message
	if e.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}
	// groupMsgWithCount always appends " (N)"; strip it back out for the JSON message.
	if idx := strings.LastIndex(msg, " ("); idx >= 0 {
		msg = msg[:idx]
	}
	w.writeJSONString(buf, msg)

	if e.Caller != "" {
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "caller")
		_ = buf.WriteByte(':')
		w.writeJSONString(buf, e.Caller)
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "line")
		_ = buf.WriteByte(':')
		buf.WriteInt(int64(e.Line))
	}

	// Count and items array: markers are visual-only, JSON carries only text.
	_ = buf.WriteByte(',')
	w.writeJSONString(buf, groupCountKey)
	_ = buf.WriteByte(':')
	var tmp [20]byte
	n := formatInt(tmp[:], int64(len(items)))
	_, _ = buf.Write(tmp[:n])

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, groupItemsKey)
	_ = buf.WriteByte(':')
	_ = buf.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			_ = buf.WriteByte(',')
		}
		// Item text gets the same secure-tag policy as the header message.
		w.writeJSONString(buf, applySecureTags(item.Text, e.maybeSecure, trusted, redactionMark))
	}
	_ = buf.WriteByte(']')

	// Any additional Fields on the entry (base fields from With()).
	for _, f := range e.Fields {
		if f.Type == FieldTypeGroupItems {
			continue // already emitted above
		}
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, f.Key)
		_ = buf.WriteByte(':')
		w.writeJSONFieldValueSecure(buf, f, trusted, redactionMark)
	}

	_ = buf.WriteByte('}')
}

// WriteSecure implements SecureWriter. trusted controls whether Secure field
// values are emitted as plaintext or as redactionMark. JSON writers are typically
// called with trusted=false; a trusted JSON sink (e.g. an internal audit log)
// can be registered via AddWriter with WriterTrusted().
func (w *JSONWriter) WriteSecure(e *Entry, trusted bool, redactionMark string) error {
	if !w.admit() {
		return ErrWriterClosed
	}
	defer w.inFlight.Done()

	rawBuf := w.getJSONBuffer()
	buf := NewBytesBuffer(rawBuf)

	// Format entirely outside the lock; entry is immutable at this point.
	w.formatJSONSecure(buf, e, trusted, redactionMark)
	_ = buf.WriteByte('\n')

	if w.async != nil {
		w.enqueueAsync(rawBuf, e.Level == LevelFatal)
		return nil
	}
	w.mu.Lock()
	_, err := w.out.Write(rawBuf.Bytes())
	w.mu.Unlock()
	w.putJSONBuffer(rawBuf)
	if err != nil {
		return fmt.Errorf("json write failed: %w", err)
	}
	return nil
}

func (w *JSONWriter) formatJSONSecure(buf *BytesBuffer, e *Entry, trusted bool, redactionMark string) {
	_ = buf.WriteByte('{')

	w.writeJSONString(buf, "timestamp")
	_ = buf.WriteByte(':')
	w.writeJSONTime(buf, e.Time)

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "level")
	_ = buf.WriteByte(':')
	w.writeJSONString(buf, e.Level.String())

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "message")
	_ = buf.WriteByte(':')
	// Redact <secure> tags in message when untrusted; strip markers when trusted.
	msg := e.Message
	if e.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}
	w.writeJSONString(buf, msg)

	if e.Caller != "" {
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "caller")
		_ = buf.WriteByte(':')
		w.writeJSONString(buf, e.Caller)
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "line")
		_ = buf.WriteByte(':')
		buf.WriteInt(int64(e.Line))
	}

	for _, f := range e.Fields {
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, f.Key)
		_ = buf.WriteByte(':')
		w.writeJSONFieldValueSecure(buf, f, trusted, redactionMark)
	}

	_ = buf.WriteByte('}')
}

// writeJSONTime writes a quoted RFC3339Nano timestamp directly into buf.
// RFC3339Nano output is ASCII-safe, so JSON escaping is not needed.
func (*JSONWriter) writeJSONTime(buf *BytesBuffer, t time.Time) {
	_ = buf.WriteByte('"')
	buf.AppendTime(t, time.RFC3339Nano)
	_ = buf.WriteByte('"')
}

// jsonHexDigits is the hex alphabet used for JSON \uXXXX control-character escaping.
const jsonHexDigits = "0123456789abcdef"

func (*JSONWriter) writeJSONString(buf *BytesBuffer, s string) {
	_ = buf.WriteByte('"')

	for i := 0; i < len(s); {
		c := s[i]

		// Multibyte sequences are decoded so malformed UTF-8 never reaches the
		// stream raw: each undecodable byte becomes one \ufffd escape, matching
		// encoding/json, which keeps the output valid UTF-8 for strict
		// consumers. Valid runes are written through as their original bytes
		// (U+2028 stays unescaped; it is legal raw in a JSON string).
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				buf.WriteString(`\ufffd`)
				i++
			} else {
				buf.WriteString(s[i : i+size])
				i += size
			}
			continue
		}

		// Plain printable ASCII is copied as one run rather than byte-by-byte;
		// the scan costs less than the per-byte WriteByte calls it replaces.
		if c >= 0x20 && c != '"' && c != '\\' {
			j := i + 1
			for j < len(s) {
				d := s[j]
				if d < 0x20 || d >= utf8.RuneSelf || d == '"' || d == '\\' {
					break
				}
				j++
			}
			buf.WriteString(s[i:j])
			i = j
			continue
		}

		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		default:
			// Only control bytes reach the default: quote and backslash are
			// cases above, and printable ASCII took the run path.
			// Inline the \uXXXX escape using a stack buffer to avoid the fmt.Fprintf
			// allocation and the intermediate string that fmt.Sprintf would produce.
			var seq [6]byte
			seq[0] = '\\'
			seq[1] = 'u'
			seq[2] = '0'
			seq[3] = '0'
			seq[4] = jsonHexDigits[c>>4]
			seq[5] = jsonHexDigits[c&0x0f]
			_, _ = buf.Write(seq[:])
		}
		i++
	}

	_ = buf.WriteByte('"')
}

func (w *JSONWriter) writeJSONFieldValueSecure(buf *BytesBuffer, f Field, trusted bool, redactionMark string) {
	switch f.Type {
	case FieldTypeSecure, FieldTypeSecureURL:
		if trusted && f.value != nil {
			w.writeJSONString(buf, (*secureValue)(f.value).plain)
		} else {
			// Emit the field-level redacted form (not the writer-level mark) so that
			// the URL form still shows e.g. "redis://user:[REDACTED]@host/db".
			if f.value != nil {
				w.writeJSONString(buf, (*secureValue)(f.value).redacted)
			} else {
				w.writeJSONString(buf, redactionMark)
			}
		}
		return
	case FieldTypeRedacted:
		// Unconditionally redacted — trust has no effect.
		w.writeJSONString(buf, redactionMark)
		return
	case FieldTypeTruncated:
		if f.value != nil {
			w.writeJSONString(buf, *(*string)(f.value))
		} else {
			w.writeJSONString(buf, "")
		}
		return
	default:
		w.writeJSONFieldValueCore(buf, f)
	}
}

func (w *JSONWriter) writeJSONFieldValueCore(buf *BytesBuffer, f Field) {
	switch f.Type {
	case FieldTypeString:
		v := *(*string)(f.value)
		w.writeJSONString(buf, v)

	case FieldTypeInt:
		buf.WriteInt(f.num)

	case FieldTypeInt64:
		buf.WriteInt(f.num)

	case FieldTypeUint64:
		// Stack-buffer form: strconv.FormatUint allocates for values >= 100.
		var tmp [20]byte
		n := formatUint(tmp[:], uint64(f.num)) //nolint:gosec // G115: field storage is bit-pattern int64, reinterpretation is the contract
		_, _ = buf.Write(tmp[:n])

	case FieldTypeFloat64:
		floatValue := math.Float64frombits(uint64(f.num)) //nolint:gosec // G115: bit-pattern reinterpretation, not value conversion

		switch {
		case math.IsNaN(floatValue):
			buf.WriteString(`"NaN"`)
		case math.IsInf(floatValue, 1):
			buf.WriteString(`"Infinity"`)
		case math.IsInf(floatValue, -1):
			buf.WriteString(`"-Infinity"`)
		default:
			appendFloat(buf.buf, floatValue)
		}

	case FieldTypeBool:
		if f.num != 0 {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}

	case FieldTypeTime:
		t := *(*time.Time)(f.value)
		w.writeJSONTime(buf, t)

	case FieldTypeDuration:
		d := time.Duration(f.num)
		w.writeJSONString(buf, d.String())

	case FieldTypeError:
		if f.value == nil {
			buf.WriteString("null")
			break
		}
		err := *(*error)(f.value)
		if err == nil {
			buf.WriteString("null")
		} else {
			w.writeJSONString(buf, err.Error())
		}

	case FieldTypeStringer:
		if f.value == nil {
			buf.WriteString("null")
			break
		}
		s := *(*fmt.Stringer)(f.value)
		if s == nil {
			buf.WriteString("null")
		} else {
			w.writeJSONString(buf, s.String())
		}

	case FieldTypeBytes:
		b := *(*[]byte)(f.value)
		_ = buf.WriteByte('"')

		const base64Table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

		for i := 0; i < len(b); i += 3 {
			var a, b1, c byte
			a = b[i]
			if i+1 < len(b) {
				b1 = b[i+1]
			}
			if i+2 < len(b) {
				c = b[i+2]
			}

			_ = buf.WriteByte(base64Table[a>>2])
			_ = buf.WriteByte(base64Table[(a&0x03)<<4|b1>>4])

			if i+1 < len(b) {
				_ = buf.WriteByte(base64Table[(b1&0x0f)<<2|c>>6])
			} else {
				_ = buf.WriteByte('=')
			}

			if i+2 < len(b) {
				_ = buf.WriteByte(base64Table[c&0x3f])
			} else {
				_ = buf.WriteByte('=')
			}
		}

		_ = buf.WriteByte('"')

	case FieldTypeAny:
		w.writeJSONAnyValue(buf, f)

	case FieldTypeSecure, FieldTypeSecureURL, FieldTypeRedacted, FieldTypeTruncated:
		// Handled upstream by writeJSONFieldValueSecure before writeJSONFieldValueCore is called.

	case FieldTypeGroupItems, FieldTypeContinuationLines:
		// Typed slice fields are emitted by their dedicated write methods (WriteGroup,
		// WriteContinue). In the generic field path emit the element count so the JSON
		// remains valid without leaking the raw Go slice pointer.
		writeJSONSliceCount(buf, f)

	case FieldTypeUnknown:
		// Null prevents JSON parsing errors when field type cannot be determined
		buf.WriteString("null")
	}
}

// writeJSONAnyValue preserves an arbitrary value as JSON, with this
// ladder: a json.Marshaler's explicit form wins when MarshalJSON succeeds
// (a structured error carrying MarshalJSON keeps its shape); a failing
// MarshalJSON falls through. An error then renders Error() as a JSON
// string, because json.Marshal only sees exported fields and would turn
// errors.New, fmt.Errorf and every runtime.Error, including recovered
// panics, into a message-less {}; this includes errors whose MarshalJSON
// failed. Otherwise json.Marshal runs, a Stringer whose marshaled form is
// {} renders String(), and anything else passes the marshaled bytes
// through. Marshaler and error are settled by type assertion before any
// marshal call so a plain error never pays json.Marshal's reflection cost.
// Typed nils render JSON null rather than calling a method on a nil
// receiver. A marshal error still produces valid JSON with an explicit
// diagnostic rather than corrupting the log stream.
func (w *JSONWriter) writeJSONAnyValue(buf *BytesBuffer, f Field) {
	v := *(*any)(f.value)
	// Typed nils (a nil *T stored in the interface) pass the error and
	// Stringer assertions, and a pointer-receiver method dereferences nil and
	// panics inside the caller's log statement. The guard is deliberately
	// broader than the Error and Stringer field constructors' (which check
	// pointers only), and the value renders as JSON null.
	if isTypedNilAny(v) {
		buf.WriteString("null")
		return
	}
	if m, ok := v.(json.Marshaler); ok {
		if raw, err := m.MarshalJSON(); err == nil {
			_, _ = buf.Write(raw)
			return
		}
	}
	if err, ok := v.(error); ok {
		w.writeJSONString(buf, err.Error())
		return
	}
	raw, mErr := json.Marshal(v)
	if mErr != nil {
		w.writeJSONString(buf, "<velocity: JSON marshal failed: "+mErr.Error()+">")
		return
	}
	if s, ok := v.(fmt.Stringer); ok && len(raw) == 2 && raw[0] == '{' && raw[1] == '}' {
		w.writeJSONString(buf, s.String())
		return
	}
	_, _ = buf.Write(raw)
}

// isTypedNilAny reports whether an Any value is a nil interface or an
// interface holding a nil pointer, map, slice or func: shapes whose methods
// would run on a nil receiver. reflect.Interface cannot appear here because
// reflect.ValueOf unwraps the interface before reporting a kind.
func isTypedNilAny(v any) bool {
	if v == nil {
		return true
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}

// writeJSONSliceCount emits the element count for typed-slice fields (GroupItems,
// ContinuationLines) in the generic JSON field path. Dedicated write methods
// (WriteGroup, WriteContinue) emit the full structured representation; this is
// the fallback for additional writers that receive the field but don't specialise.
func writeJSONSliceCount(buf *BytesBuffer, f Field) {
	if f.value == nil {
		buf.WriteString("0")
		return
	}
	var count int
	switch f.Type { //nolint:exhaustive // only GroupItems and ContinuationLines are valid callers; default is unreachable
	case FieldTypeGroupItems:
		count = len(*(*[]GroupItem)(f.value))
	case FieldTypeContinuationLines:
		count = len(*(*[]string)(f.value))
	default:
		count = 0
	}
	var tmp [20]byte
	n := formatInt(tmp[:], int64(count))
	_, _ = buf.Write(tmp[:n])
}

// WriteContinue emits a JSON entry for a Logger.Continue call.
// The continuation lines are emitted as a "continuation" array. OSC 8 hyperlink
// escape sequences are stripped from each line — JSON consumers are aggregators
// that cannot render terminal control sequences and must not receive raw ESC bytes.
func (w *JSONWriter) WriteContinue(e *Entry, lines []string) error {
	return w.WriteContinueSecure(e, lines, false, "[REDACTED]")
}

// WriteContinueSecure is the trust-aware continuation JSON write path.
func (w *JSONWriter) WriteContinueSecure(e *Entry, lines []string, trusted bool, redactionMark string) error {
	if !w.admit() {
		return ErrWriterClosed
	}
	defer w.inFlight.Done()

	rawBuf := w.getJSONBuffer()
	buf := NewBytesBuffer(rawBuf)

	w.formatJSONContinueSecure(buf, e, lines, trusted, redactionMark)
	_ = buf.WriteByte('\n')

	if w.async != nil {
		w.enqueueAsync(rawBuf, e.Level == LevelFatal)
		return nil
	}
	w.mu.Lock()
	_, err := w.out.Write(rawBuf.Bytes())
	w.mu.Unlock()
	w.putJSONBuffer(rawBuf)
	if err != nil {
		return fmt.Errorf("json write failed: %w", err)
	}
	return nil
}

func (w *JSONWriter) formatJSONContinueSecure(buf *BytesBuffer, e *Entry, lines []string, trusted bool, redactionMark string) {
	_ = buf.WriteByte('{')

	w.writeJSONString(buf, "timestamp")
	_ = buf.WriteByte(':')
	w.writeJSONTime(buf, e.Time)

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "level")
	_ = buf.WriteByte(':')
	w.writeJSONString(buf, e.Level.String())

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "message")
	_ = buf.WriteByte(':')
	msg := e.Message
	if e.maybeSecure {
		if trusted {
			msg = stripSecureTags(msg)
		} else {
			msg = redactSecureTags(msg, redactionMark)
		}
	}
	w.writeJSONString(buf, msg)

	if e.Caller != "" {
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "caller")
		_ = buf.WriteByte(':')
		w.writeJSONString(buf, e.Caller)
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "line")
		_ = buf.WriteByte(':')
		buf.WriteInt(int64(e.Line))
	}

	// Continuation lines as a JSON array. OSC 8 sequences are stripped because
	// log aggregators cannot render terminal control sequences.
	_ = buf.WriteByte(',')
	w.writeJSONString(buf, continuationKey)
	_ = buf.WriteByte(':')
	_ = buf.WriteByte('[')
	for i, line := range lines {
		if i > 0 {
			_ = buf.WriteByte(',')
		}
		// Line text gets the same secure-tag policy as the header message;
		// OSC 8 stripping is preserved on the post-policy text.
		w.writeJSONString(buf, stripOSC8(applySecureTags(line, e.maybeSecure, trusted, redactionMark)))
	}
	_ = buf.WriteByte(']')

	// Any additional Fields on the entry (base fields from With()).
	for _, f := range e.Fields {
		if f.Type == FieldTypeContinuationLines {
			continue // already emitted above
		}
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, f.Key)
		_ = buf.WriteByte(':')
		w.writeJSONFieldValueSecure(buf, f, trusted, redactionMark)
	}

	_ = buf.WriteByte('}')
}

// Flush drains any buffered output without closing the writer.
// Only has effect when the underlying io.Writer implements Flush.
func (w *JSONWriter) Flush() error {
	if a := w.async; a != nil {
		// Drain through the queue rather than flushing underneath it: the
		// barrier acknowledges only after the drainer wrote everything ahead
		// of it, so the underlying flush sees every accepted record. The
		// inFlight registration keeps Close from retiring the drainer while
		// this barrier is still in flight; a closed writer was already
		// drained and flushed by Close itself.
		if !w.admit() {
			return nil
		}
		b := make(chan struct{})
		a.ch <- jsonAsyncItem{barrier: b}
		<-b

		// Serialise the underlying flush with the drainer's writes via the
		// async I/O mutex; w.mu is admission-only here, so a concurrent
		// caller never waits behind this flush either. The in-flight slot
		// spans the flush too: released only after the mutex section, so
		// Close's inFlight.Wait covers the whole operation, not just the
		// barrier. The ordering of Done versus a concurrent Close's final
		// writeMu acquisition has no deterministic test: whether this Flush
		// or Close wins the lock decides only which flush runs last, and
		// both orders are correct, so only the race detector's overlap
		// check (TestAsyncOutput_CloseFlushExclusiveWithConcurrentFlush)
		// observes it.
		a.writeMu.Lock()
		defer a.writeMu.Unlock()
		defer w.inFlight.Done()
	} else {
		w.mu.Lock()
		defer w.mu.Unlock()
	}

	if f, ok := w.out.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

func (w *JSONWriter) Close() error {
	w.closeOnce.Do(func() {
		// Created here (not at construction) so zero-value writers are safe;
		// Once's completion guarantee makes the field visible to every caller
		// before the receive below.
		w.closeDone = make(chan struct{})
		defer close(w.closeDone)

		w.mu.Lock()
		w.closed = true
		w.mu.Unlock()

		// Drain admitted callers outside the mutex — they need it for their
		// final writes. When Wait returns no in-flight cycle remains, so the
		// flush below is the last underlying write.
		w.inFlight.Wait()

		flushSink := func() {
			if f, ok := w.out.(interface{ Flush() error }); ok {
				if err := f.Flush(); err != nil && w.closeErr == nil {
					w.closeErr = err
				}
			}
		}

		if a := w.async; a != nil {
			// Admission is gone and every admitted caller has finished its
			// enqueue, so nothing else can enter the queue. The stop sentinel
			// rides behind the last buffered record; its ack means the queue
			// is empty, and done proves the drainer's final write finished
			// before the flush below. There is deliberately no timeout: a
			// stalled sink blocks Close, same as the synchronous contract.
			b := make(chan struct{})
			a.ch <- jsonAsyncItem{barrier: b, stop: true}
			<-b
			<-a.done

			a.writeErrMu.Lock()
			err := a.writeErr
			a.writeErrMu.Unlock()
			if err != nil && w.closeErr == nil {
				w.closeErr = err
			}

			// Hold the drainer's I/O mutex across the final flush. An async
			// Flush that admitted before close releases its in-flight slot
			// only after its own writeMu section, so inFlight.Wait covers it;
			// the mutex additionally excludes a Flush that admitted but has
			// not yet reached its section, keeping the sink flush exclusive.
			a.writeMu.Lock()
			flushSink()
			a.writeMu.Unlock()
		} else {
			flushSink()
		}
	})
	// A second concurrent Close waits for the same completed drain and
	// returns the same recorded result — never an early nil.
	<-w.closeDone
	return w.closeErr
}

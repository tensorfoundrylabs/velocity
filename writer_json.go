package velocity

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

type JSONWriter struct {
	out     io.Writer
	bufPool *BufferPool
	mu      sync.Mutex
	closed  bool
}

func NewJSONWriter(out io.Writer) *JSONWriter {
	return &JSONWriter{
		out:     out,
		bufPool: NewBufferPool(),
	}
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
	rawBuf := w.bufPool.Get(HintStructuredLog)
	buf := NewBytesBuffer(rawBuf)

	w.formatJSONStatusSecure(buf, e, trusted, redactionMark)

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		w.bufPool.Put(rawBuf)
		return ErrWriterClosed
	}
	_, err := w.out.Write(buf.Bytes())
	if err == nil {
		_, err = w.out.Write(newlineByte)
	}
	w.mu.Unlock()

	w.bufPool.Put(rawBuf)
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
	rawBuf := w.bufPool.Get(HintStructuredLog)
	buf := NewBytesBuffer(rawBuf)

	w.formatJSONGroupSecure(buf, e, items, trusted, redactionMark)

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		w.bufPool.Put(rawBuf)
		return ErrWriterClosed
	}
	_, err := w.out.Write(buf.Bytes())
	if err == nil {
		_, err = w.out.Write(newlineByte)
	}
	w.mu.Unlock()

	w.bufPool.Put(rawBuf)
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
		w.writeJSONString(buf, item.Text)
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
	rawBuf := w.bufPool.Get(HintStructuredLog)
	buf := NewBytesBuffer(rawBuf)

	// Format entirely outside the lock; entry is immutable at this point.
	w.formatJSONSecure(buf, e, trusted, redactionMark)

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		w.bufPool.Put(rawBuf)
		return ErrWriterClosed
	}
	_, err := w.out.Write(buf.Bytes())
	if err == nil {
		_, err = w.out.Write(newlineByte)
	}
	w.mu.Unlock()

	w.bufPool.Put(rawBuf)
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

	for i := range len(s) {
		c := s[i]

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
			if c < 32 {
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
			} else {
				_ = buf.WriteByte(c)
			}
		}
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
			buf.WriteString(strconv.FormatFloat(floatValue, 'g', -1, 64))
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
		// Fallback uses reflection but covers all cases
		v := *(*any)(f.value)
		w.writeJSONString(buf, fmt.Sprintf("%v", v))

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
	rawBuf := w.bufPool.Get(HintStructuredLog)
	buf := NewBytesBuffer(rawBuf)

	w.formatJSONContinueSecure(buf, e, lines, trusted, redactionMark)

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		w.bufPool.Put(rawBuf)
		return ErrWriterClosed
	}
	_, err := w.out.Write(buf.Bytes())
	if err == nil {
		_, err = w.out.Write(newlineByte)
	}
	w.mu.Unlock()

	w.bufPool.Put(rawBuf)
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
		w.writeJSONString(buf, stripOSC8(line))
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
	w.mu.Lock()
	defer w.mu.Unlock()

	if f, ok := w.out.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

func (w *JSONWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true

	if f, ok := w.out.(interface{ Flush() error }); ok {
		return f.Flush()
	}

	return nil
}

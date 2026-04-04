package velocity

import (
	"fmt"
	"io"
	"math"
	"strconv"
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
	rawBuf := w.bufPool.Get(HintStructuredLog)
	buf := NewBytesBuffer(rawBuf)

	// Format entirely outside the lock; entry is immutable at this point.
	w.formatJSON(buf, e)

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

func (w *JSONWriter) formatJSON(buf *BytesBuffer, e *Entry) {
	_ = buf.WriteByte('{')

	w.writeJSONString(buf, "timestamp")
	_ = buf.WriteByte(':')
	w.writeJSONTime(buf, e.Time, time.RFC3339Nano)

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "level")
	_ = buf.WriteByte(':')
	w.writeJSONString(buf, e.Level.String())

	_ = buf.WriteByte(',')
	w.writeJSONString(buf, "message")
	_ = buf.WriteByte(':')
	w.writeJSONString(buf, e.Message)

	if e.Caller != "" {
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, "caller")
		_ = buf.WriteByte(':')
		_ = buf.WriteByte('"')
		buf.WriteString(e.Caller)
		_ = buf.WriteByte(':')
		buf.WriteInt(int64(e.Line))
		_ = buf.WriteByte('"')
	}

	for _, f := range e.Fields {
		_ = buf.WriteByte(',')
		w.writeJSONString(buf, f.Key)
		_ = buf.WriteByte(':')
		w.writeJSONFieldValue(buf, f)
	}

	_ = buf.WriteByte('}')
}

// writeJSONTime writes a quoted timestamp directly into buf using AppendFormat.
// RFC3339/RFC3339Nano output is ASCII-safe, so JSON escaping is not needed.
func (*JSONWriter) writeJSONTime(buf *BytesBuffer, t time.Time, layout string) {
	_ = buf.WriteByte('"')
	buf.AppendTime(t, layout)
	_ = buf.WriteByte('"')
}

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
				_, _ = fmt.Fprintf(buf, `\u%04x`, c)
			} else {
				_ = buf.WriteByte(c)
			}
		}
	}

	_ = buf.WriteByte('"')
}

func (w *JSONWriter) writeJSONFieldValue(buf *BytesBuffer, f Field) {
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
			buf.WriteString(strconv.FormatFloat(floatValue, 'g', 6, 64))
		}

	case FieldTypeBool:
		if f.num != 0 {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}

	case FieldTypeTime:
		t := *(*time.Time)(f.value)
		w.writeJSONTime(buf, t, time.RFC3339Nano)

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

	case FieldTypeUnknown:
		// Null prevents JSON parsing errors when field type cannot be determined
		buf.WriteString("null")
	}
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

package velocity

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"
	"unsafe"
)

// FieldType identifies the type of a field for zero-allocation serialisation.
type FieldType byte

const (
	FieldTypeUnknown FieldType = iota
	FieldTypeString
	FieldTypeInt
	FieldTypeInt64
	FieldTypeFloat64
	FieldTypeBool
	FieldTypeTime
	FieldTypeDuration
	FieldTypeError
	FieldTypeStringer
	FieldTypeBytes
	FieldTypeAny // Avoid in hot paths
)

// Field represents a structured log field optimised for minimal allocations.
//
// Safety: Uses unsafe.Pointer to avoid interface{} allocations in hot paths.
// This is safe because:
//  1. Escape analysis ensures referenced values remain on heap
//  2. Field constructors take address of parameters, forcing heap allocation
//  3. Values are never modified after Field creation (immutable pattern)
//  4. Type discrimination via FieldType ensures correct pointer casting
type Field struct {
	// Using unsafe.Pointer to avoid interface{} allocations in hot path
	value unsafe.Pointer
	Key   string
	// For numeric types, store the value directly to avoid allocations
	num  int64
	Type FieldType
}

// F creates a field with automatic type detection.
// For performance-critical code, use typed constructors instead.
func F(key string, value any) Field {
	switch v := value.(type) {
	case string:
		return String(key, v)
	case int:
		return Int(key, v)
	case int64:
		return Int64(key, v)
	case float64:
		return Float64(key, v)
	case bool:
		return Bool(key, v)
	case time.Time:
		return Time(key, v)
	case time.Duration:
		return Duration(key, v)
	case error:
		// Typed nils satisfy the error interface but panic on .Error(); delegate to Error which handles them.
		return Error(key, v)
	case fmt.Stringer:
		// Typed nils satisfy fmt.Stringer but panic on .String(); delegate to Stringer which handles them.
		return Stringer(key, v)
	case []byte:
		return Bytes(key, v)
	default:
		return Any(key, v)
	}
}

func String(key, val string) Field {
	return Field{
		Key:   key,
		Type:  FieldTypeString,
		value: unsafe.Pointer(&val),
	}
}

func Int(key string, val int) Field {
	return Field{
		Key:  key,
		Type: FieldTypeInt,
		num:  int64(val),
	}
}

func Int64(key string, val int64) Field {
	return Field{
		Key:  key,
		Type: FieldTypeInt64,
		num:  val,
	}
}

func Float64(key string, val float64) Field {
	bits := math.Float64bits(val)
	// Safe conversion: uint64 from Float64bits won't exceed int64 range in bit pattern
	return Field{
		Key:  key,
		Type: FieldTypeFloat64,
		num:  int64(bits), // #nosec G115 - bit pattern conversion, not value conversion
	}
}

func Bool(key string, val bool) Field {
	var n int64
	if val {
		n = 1
	}
	return Field{
		Key:  key,
		Type: FieldTypeBool,
		num:  n,
	}
}

func Time(key string, val time.Time) Field {
	return Field{
		Key:   key,
		Type:  FieldTypeTime,
		value: unsafe.Pointer(&val),
	}
}

func Duration(key string, val time.Duration) Field {
	return Field{
		Key:  key,
		Type: FieldTypeDuration,
		num:  int64(val),
	}
}

// Milliseconds creates a float field with duration in milliseconds.
// Useful for showing precise timing for fast operations (e.g., "0.5ms" instead of "0s").
func Milliseconds(key string, val time.Duration) Field {
	ms := float64(val) / float64(time.Millisecond)
	return Float64(key, ms)
}

func Error(key string, val error) Field {
	if val == nil {
		return String(key, "<nil>")
	}
	// Catch typed nils where the interface holds a non-nil type with nil data.
	if rv := reflect.ValueOf(val); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return String(key, "<nil>")
	}
	return Field{
		Key:   key,
		Type:  FieldTypeError,
		value: unsafe.Pointer(&val),
	}
}

func Stringer(key string, val fmt.Stringer) Field {
	if val == nil {
		return String(key, "<nil>")
	}
	// Catch typed nils where the interface holds a non-nil type with nil data.
	if rv := reflect.ValueOf(val); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return String(key, "<nil>")
	}
	return Field{
		Key:   key,
		Type:  FieldTypeStringer,
		value: unsafe.Pointer(&val),
	}
}

func Bytes(key string, val []byte) Field {
	return Field{
		Key:   key,
		Type:  FieldTypeBytes,
		value: unsafe.Pointer(&val),
	}
}

// Any creates a field with arbitrary value.
// Avoid in hot paths as it causes allocations.
func Any(key string, val any) Field {
	return Field{
		Key:   key,
		Type:  FieldTypeAny,
		value: unsafe.Pointer(&val),
	}
}

// Value returns the field's value based on its type.
// This method allocates and should be avoided in hot paths.
func (f Field) Value() any {
	switch f.Type {
	case FieldTypeString:
		return *(*string)(f.value)
	case FieldTypeInt:
		return int(f.num)
	case FieldTypeInt64:
		return f.num
	case FieldTypeFloat64:
		// Safe conversion: int64 back to uint64 for bit pattern
		return math.Float64frombits(uint64(f.num)) // #nosec G115 - bit pattern conversion, not value conversion
	case FieldTypeBool:
		return f.num != 0
	case FieldTypeTime:
		return *(*time.Time)(f.value)
	case FieldTypeDuration:
		return time.Duration(f.num)
	case FieldTypeError:
		return *(*error)(f.value)
	case FieldTypeStringer:
		return *(*fmt.Stringer)(f.value)
	case FieldTypeBytes:
		return *(*[]byte)(f.value)
	case FieldTypeAny:
		return *(*any)(f.value)
	case FieldTypeUnknown:
		return nil
	}
	return nil
}

type Fields struct {
	fields []Field
}

func NewFields(capacity int) *Fields {
	return &Fields{
		fields: make([]Field, 0, capacity),
	}
}

func (fs *Fields) Add(key string, value any) *Fields {
	fs.fields = append(fs.fields, F(key, value))
	return fs
}

func (fs *Fields) AddField(f Field) *Fields {
	fs.fields = append(fs.fields, f)
	return fs
}

func (fs *Fields) Reset() {
	fs.fields = fs.fields[:0]
}

func (fs *Fields) Slice() []Field {
	return fs.fields
}

func (f Field) writeFormatted(buf interface {
	WriteString(string) (int, error)
	WriteRune(rune) (int, error)
	Write([]byte) (int, error)
},
) {
	switch f.Type {
	case FieldTypeString:
		val := *(*string)(f.value)
		_, _ = buf.WriteString(val)
	case FieldTypeInt, FieldTypeInt64:
		var tmp [20]byte
		n := formatInt(tmp[:], f.num)
		_, _ = buf.Write(tmp[:n])
	case FieldTypeFloat64:
		val := math.Float64frombits(uint64(f.num)) //nolint:gosec // G115: bit-pattern reinterpretation, not value conversion
		_, _ = buf.WriteString(strconv.FormatFloat(val, 'g', -1, 64))
	case FieldTypeBool:
		if f.num != 0 {
			_, _ = buf.WriteString("true")
		} else {
			_, _ = buf.WriteString("false")
		}
	case FieldTypeTime:
		val := *(*time.Time)(f.value)
		_, _ = buf.WriteString(val.Format(time.RFC3339))
	case FieldTypeDuration:
		val := time.Duration(f.num)
		_, _ = buf.WriteString(val.String())
	case FieldTypeError:
		if f.value == nil {
			_, _ = buf.WriteString("<nil>")
		} else {
			val := *(*error)(f.value)
			if val == nil {
				_, _ = buf.WriteString("<nil>")
			} else {
				_, _ = buf.WriteString(val.Error())
			}
		}
	case FieldTypeStringer:
		if f.value == nil {
			_, _ = buf.WriteString("<nil>")
		} else {
			val := *(*fmt.Stringer)(f.value)
			if val == nil {
				_, _ = buf.WriteString("<nil>")
			} else {
				_, _ = buf.WriteString(val.String())
			}
		}
	case FieldTypeBytes:
		val := *(*[]byte)(f.value)
		_, _ = buf.WriteString(string(val))
	case FieldTypeAny:
		val := *(*any)(f.value)
		_, _ = fmt.Fprintf(buf, "%v", val)
	case FieldTypeUnknown:
		// Unknown field type - write nothing
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	// MinInt64 negation overflows back to itself, so handle it directly.
	if i == math.MinInt64 {
		return "-9223372036854775808"
	}
	if i < 0 {
		return "-" + itoa(-i)
	}

	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

package velocity

import (
	"fmt"
	"math"
	"net/url"
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

	// Secure field types — hold a *secureValue in the value slot.
	// One small heap alloc at constructor call; zero extra cost on the hot path
	// for entries that contain no secure fields.
	FieldTypeSecure    // plaintext + redacted pair, writer decides which to emit
	FieldTypeSecureURL // URL with userinfo password redacted
	FieldTypeRedacted  // permanently redacted; no plaintext stored anywhere
	FieldTypeTruncated // value clipped to maxLen; may still be sensitive

	// FieldTypeGroupItems carries a []GroupItem for Logger.Group calls.
	// Stored as a typed Field so Entry layout stays unchanged — entries that never
	// call Logger.Group pay zero cost.
	FieldTypeGroupItems

	// FieldTypeContinuationLines carries a []string for Logger.Continue calls.
	// Stored as a typed Field so Entry layout stays unchanged — entries that never
	// call Logger.Continue pay zero cost.
	FieldTypeContinuationLines
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

// parseURL wraps url.Parse so it can be swapped in tests.
// Kept unexported — callers outside this file should not need it.
var parseURL = url.Parse

// userInfoRedacted builds a url.Userinfo with username preserved and the
// password sentinel "REDACTED". Brackets are intentionally omitted because
// url.String() URL-encodes '[' and ']', producing ugly %5BREDACTED%5D output.
func userInfoRedacted(username string) *url.Userinfo {
	return url.UserPassword(username, "REDACTED")
}

// secureValue pairs a redacted display string with the original plaintext.
// Stored behind an unsafe.Pointer so Field width stays unchanged.
// One alloc per Secure/SecureURL constructor call; zero per log call.
type secureValue struct {
	plain    string
	redacted string
}

// Secure creates a field that renders as redacted on untrusted writers.
// On TTY console writers (trusted by context) the plaintext is shown.
// One heap alloc at the call site; zero per log call thereafter.
func Secure(key, val string) Field {
	sv := &secureValue{plain: val, redacted: redactedMark}
	return Field{
		Key:   key,
		Type:  FieldTypeSecure,
		value: unsafe.Pointer(sv),
	}
}

// SecureURL creates a field from a URL string, redacting any userinfo password.
// The plaintext form retains the full URL; the redacted form replaces the
// password with "[REDACTED]". Falls back to treating the raw string as plaintext
// when parsing fails. One heap alloc at the call site.
func SecureURL(key, rawURL string) Field {
	plain := rawURL
	redacted := rawURL

	if u, err := parseURL(rawURL); err == nil && u.User != nil {
		if _, hasPass := u.User.Password(); hasPass {
			safe := *u
			safe.User = userInfoRedacted(u.User.Username())
			redacted = safe.String()
		}
	}

	sv := &secureValue{plain: plain, redacted: redacted}
	return Field{
		Key:   key,
		Type:  FieldTypeSecureURL,
		value: unsafe.Pointer(sv),
	}
}

// Redacted creates a field with no plaintext — the value is permanently hidden
// regardless of writer trust level. Use when the value must never appear in any
// log output, not even on trusted writers.
func Redacted(key string) Field {
	return Field{
		Key:  key,
		Type: FieldTypeRedacted,
		// No value stored. Writers emit the redaction mark unconditionally.
	}
}

// Truncated clips val to maxLen runes, appending '…' when trimmed.
// Zero-alloc when val fits within maxLen (stored as FieldTypeString).
// One alloc when trimming occurs; returns FieldTypeTruncated so writers can
// distinguish truncated fields from plain strings if needed.
func Truncated(key, val string, maxLen int) Field {
	if maxLen <= 0 {
		return Field{Key: key, Type: FieldTypeTruncated}
	}
	// Count runes to handle multi-byte sequences correctly.
	n := 0
	for i := range val {
		if n == maxLen {
			// val exceeds maxLen — clip and append ellipsis.
			clipped := val[:i] + "…"
			return Field{
				Key:   key,
				Type:  FieldTypeTruncated,
				value: unsafe.Pointer(&clipped),
			}
		}
		_ = i
		n++
	}
	// val fits; equivalent alloc profile to String() since &val forces escape.
	return Field{
		Key:   key,
		Type:  FieldTypeTruncated,
		value: unsafe.Pointer(&val),
	}
}

// redactedMark is the default redaction sentinel used across the package.
const redactedMark = "[REDACTED]"

// SecurePlain returns the plaintext value of a Secure or SecureURL field.
// Returns empty string for all other types or when no plaintext is stored.
func SecurePlain(f Field) string {
	if (f.Type == FieldTypeSecure || f.Type == FieldTypeSecureURL) && f.value != nil {
		return (*secureValue)(f.value).plain
	}
	return ""
}

// SecureRedacted returns the redacted form of a Secure or SecureURL field.
func SecureRedacted(f Field) string {
	if (f.Type == FieldTypeSecure || f.Type == FieldTypeSecureURL) && f.value != nil {
		return (*secureValue)(f.value).redacted
	}
	return redactedMark
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
	case FieldTypeSecure, FieldTypeSecureURL:
		if f.value != nil {
			return (*secureValue)(f.value).plain
		}
		return ""
	case FieldTypeRedacted:
		return redactedMark
	case FieldTypeTruncated:
		if f.value != nil {
			return *(*string)(f.value)
		}
		return ""
	case FieldTypeGroupItems:
		// Items are handled directly by group-aware writers.
		// Returning nil here prevents fmt fallback from trying to dereference the slice pointer.
		return nil
	case FieldTypeContinuationLines:
		// Lines are handled directly by continuation-aware writers.
		return nil
	case FieldTypeUnknown:
		return nil
	}
	return nil
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
	case FieldTypeSecure, FieldTypeSecureURL:
		// Default: emit redacted form. Trusted writers call writeFormattedTrusted instead.
		if f.value != nil {
			_, _ = buf.WriteString((*secureValue)(f.value).redacted)
		} else {
			_, _ = buf.WriteString(redactedMark)
		}
	case FieldTypeRedacted:
		_, _ = buf.WriteString(redactedMark)
	case FieldTypeTruncated:
		// Truncated values are not sensitive by definition; always emit as-is.
		if f.value != nil {
			_, _ = buf.WriteString(*(*string)(f.value))
		}
	case FieldTypeGroupItems:
		// Group items are rendered by the console/JSON writers directly.
		// In generic contexts (e.g. additional writers) write the item count as a hint.
		if f.value != nil {
			items := *(*[]GroupItem)(f.value)
			var tmp [20]byte
			n := formatInt(tmp[:], int64(len(items)))
			_, _ = buf.WriteString("[")
			_, _ = buf.Write(tmp[:n])
			_, _ = buf.WriteString(" items]")
		}
	case FieldTypeContinuationLines:
		// Continuation lines are rendered by WriteContinue directly.
		// In generic contexts emit the line count as a hint.
		if f.value != nil {
			lines := *(*[]string)(f.value)
			var tmp [20]byte
			n := formatInt(tmp[:], int64(len(lines)))
			_, _ = buf.WriteString("[")
			_, _ = buf.Write(tmp[:n])
			_, _ = buf.WriteString(" lines]")
		}
	case FieldTypeUnknown:
		// Unknown field type - write nothing
	}
}

// writeFormattedTrusted writes the field to buf, using plaintext for Secure/SecureURL.
// Call this only from writers that have been explicitly opted into trust.
func (f Field) writeFormattedTrusted(buf interface {
	WriteString(string) (int, error)
	WriteRune(rune) (int, error)
	Write([]byte) (int, error)
},
) {
	switch f.Type {
	case FieldTypeSecure, FieldTypeSecureURL:
		if f.value != nil {
			_, _ = buf.WriteString((*secureValue)(f.value).plain)
		}
	case FieldTypeRedacted:
		// Redacted is unconditional — trust has no effect.
		_, _ = buf.WriteString(redactedMark)
	default:
		f.writeFormatted(buf)
	}
}

// writeFormattedWithMark writes the field to buf, replacing Secure/SecureURL values
// with the given redactionMark. Use on untrusted writer paths.
func (f Field) writeFormattedWithMark(buf interface {
	WriteString(string) (int, error)
	WriteRune(rune) (int, error)
	Write([]byte) (int, error)
}, redactionMark string,
) {
	switch f.Type {
	case FieldTypeSecure, FieldTypeSecureURL:
		if f.value != nil {
			// Emit the field-level redacted form rather than the writer-level mark
			// so URL fields still show the host/path portion.
			_, _ = buf.WriteString((*secureValue)(f.value).redacted)
		} else {
			_, _ = buf.WriteString(redactionMark)
		}
	case FieldTypeRedacted:
		_, _ = buf.WriteString(redactionMark)
	default:
		f.writeFormatted(buf)
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

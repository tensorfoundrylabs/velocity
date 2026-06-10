// Package velocity provides high-performance structured logging for Go applications.
package velocity

import (
	"fmt"
	"strconv"
	"time"
	"unsafe"
)

const nilValueString = "<nil>"

// FieldValueToString converts a Field value to string representation.
// Handles all Field types without allocations where possible.
//
// This is shared logic used by both StreamingWriter and external adapters
// to avoid duplication (DRY). The function doesn't depend on any struct state,
// making it ideal as a package-level utility.
func FieldValueToString(f Field) string {
	switch f.Type {
	case FieldTypeString:
		return *(*string)(f.value)
	case FieldTypeInt:
		// Use FormatInt rather than UnsafeString over a stack-local buffer; the
		// latter creates a string header pointing at a stack array that may be
		// overwritten after the function returns (latent dangling-pointer hazard).
		return strconv.FormatInt(f.num, 10)
	case FieldTypeInt64:
		return strconv.FormatInt(f.num, 10)
	case FieldTypeFloat64:
		// Convert int64 bits back to float64
		// #nosec G115 - bit pattern conversion, not value conversion
		bits := uint64(f.num)
		val := strconv.FormatFloat(float64FromBits(bits), 'g', -1, 64)
		return val
	case FieldTypeBool:
		return strconv.FormatBool(f.num != 0)
	case FieldTypeTime:
		val := *(*time.Time)(f.value)
		return val.Format(time.RFC3339Nano)
	case FieldTypeDuration:
		val := time.Duration(f.num)
		return val.String()
	case FieldTypeError:
		// f.value is unsafe.Pointer pointing to an error interface
		// Dereference to get the actual error interface value
		if f.value == nil {
			return nilValueString
		}
		errVal := *(*error)(f.value)
		if errVal == nil {
			return nilValueString
		}
		return errVal.Error()
	case FieldTypeStringer:
		// f.value is unsafe.Pointer pointing to a Stringer interface
		// Dereference to get the actual Stringer interface value
		if f.value == nil {
			return nilValueString
		}
		stringerVal := *(*fmt.Stringer)(f.value)
		if stringerVal == nil {
			return nilValueString
		}
		return stringerVal.String()
	case FieldTypeBytes:
		val := (*[]byte)(f.value)
		if val == nil {
			return nilValueString
		}
		return string(*val)
	case FieldTypeAny:
		// f.value is unsafe.Pointer pointing to an interface{}
		// Dereference to get the actual interface{} value
		val := *(*any)(f.value)
		if val == nil {
			return nilValueString
		}
		return fmt.Sprintf("%v", val)
	case FieldTypeSecure, FieldTypeSecureURL:
		// Return redacted form; callers that need plaintext use SecurePlain().
		if f.value == nil {
			return redactedMark
		}
		return (*secureValue)(f.value).redacted
	case FieldTypeRedacted:
		return redactedMark
	case FieldTypeTruncated:
		if f.value == nil {
			return ""
		}
		return *(*string)(f.value)
	case FieldTypeGroupItems:
		// Return a human-readable hint; group-aware writers handle items directly.
		if f.value == nil {
			return "[0 items]"
		}
		items := *(*[]GroupItem)(f.value)
		return "[" + strconv.FormatInt(int64(len(items)), 10) + " items]"
	case FieldTypeContinuationLines:
		// Return a human-readable hint; continuation-aware writers handle lines directly.
		if f.value == nil {
			return "[0 lines]"
		}
		lines := *(*[]string)(f.value)
		return "[" + strconv.FormatInt(int64(len(lines)), 10) + " lines]"
	case FieldTypeUnknown:
		return ""
	}
	return ""
}

// float64FromBits reconstructs a float64 from its IEEE 754 binary representation.
// Mirrors math.Float64frombits for use with Field.num storage.
//
// This is shared logic used by FieldValueToString for float64 conversion,
// avoiding the need to import math package solely for this function.
func float64FromBits(b uint64) float64 {
	return *(*float64)(unsafe.Pointer(&b))
}

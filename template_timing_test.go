package velocity

import (
	"bytes"
	"math"
	"testing"
	"time"
)

// TestWriteSmartDuration pins the smart duration renderer byte-for-byte for
// every value the old nanosecond-conversion code rendered correctly, plus the
// overflow cases it silently wrapped: integer milliseconds above ~9.2e12 and
// both int64 duration extremes. writeSmartDuration takes the field's own unit
// (ms or ns) so integer values never pass through nanoseconds, and handles
// magnitude unsigned because -MinInt64 overflows.
func TestWriteSmartDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		f    Field
		want string
	}{
		// Duration (nanosecond) input.
		{"duration zero", Duration("d", 0), "0µs"},
		{"duration 1ns", Duration("d", 1), "0µs"},
		{"duration 999ns", Duration("d", 999), "0µs"},
		{"duration 1us", Duration("d", time.Microsecond), "1µs"},
		{"duration 850us", Duration("d", 850*time.Microsecond), "850µs"},
		{"duration 999999ns", Duration("d", 999999), "999µs"},
		{"duration 1ms", Duration("d", time.Millisecond), "1ms"},
		{"duration 294ms", Duration("d", 294*time.Millisecond), "294ms"},
		{"duration 999ms", Duration("d", 999*time.Millisecond), "999ms"},
		{"duration 1s", Duration("d", time.Second), "1.00s"},
		{"duration 2778ms", Duration("d", 2778*time.Millisecond), "2.77s"},
		{"duration 3s", Duration("d", 3*time.Second), "3.00s"},
		{"duration -850us", Duration("d", -850*time.Microsecond), "-850µs"},
		{"duration -294ms", Duration("d", -294*time.Millisecond), "-294ms"},
		{"duration -2778ms", Duration("d", -2778*time.Millisecond), "-2.77s"},
		{"duration max", Duration("d", time.Duration(math.MaxInt64)), "9223372036.85s"},
		{"duration min", Duration("d", time.Duration(math.MinInt64)), "-9223372036.85s"},

		// Integer (millisecond) input: 9223372036854 is
		// math.MaxInt64/time.Millisecond, the largest value whose old
		// ns conversion did not wrap; its neighbours did.
		{"int zero", Int("ms", 0), "0µs"},
		{"int 294", Int("ms", 294), "294ms"},
		{"int 999", Int("ms", 999), "999ms"},
		{"int 1000", Int("ms", 1000), "1.00s"},
		{"int -294", Int("ms", -294), "-294ms"},
		{"int -1000", Int("ms", -1000), "-1.00s"},
		{"int maxns-1", Int64("ms", int64(math.MaxInt64/time.Millisecond)-1), "9223372036.85s"},
		{"int maxns", Int64("ms", int64(math.MaxInt64/time.Millisecond)), "9223372036.85s"},
		{"int maxns+1", Int64("ms", int64(math.MaxInt64/time.Millisecond)+1), "9223372036.85s"},
		{"int max", Int64("ms", math.MaxInt64), "9223372036854775.80s"},
		{"int min", Int64("ms", math.MinInt64), "-9223372036854775.80s"},

		// Non-timing types fall back to the raw integer.
		{"fallback uint", Field{Key: "n", Type: FieldTypeUint64, num: 7}, "7"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			writeSmartDuration(&buf, tc.f)
			if got := buf.String(); got != tc.want {
				t.Errorf("writeSmartDuration(%v): want %q, got %q", tc.f.num, tc.want, got)
			}
		})
	}
}

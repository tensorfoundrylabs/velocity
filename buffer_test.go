package velocity

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

func TestBytesBuffer_WriteInt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		val  int64
		want string
	}{
		{"zero", 0, "0"},
		{"positive", 42, "42"},
		{"negative", -42, "-42"},
		{"large positive", 1234567890, "1234567890"},
		{"large negative", -1234567890, "-1234567890"},
		{"max int64", math.MaxInt64, "9223372036854775807"},
		{"min int64", math.MinInt64, "-9223372036854775808"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf := NewBytesBuffer(bytes.NewBuffer(nil))
			buf.WriteInt(tc.val)
			got := buf.String()
			if got != tc.want {
				t.Errorf("WriteInt(%d) = %q, want %q", tc.val, got, tc.want)
			}
		})
	}
}

func TestFormatter_WriteInt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		val  int64
		want string
	}{
		{"zero", 0, "0"},
		{"positive", 99, "99"},
		{"negative", -1, "-1"},
		{"max int64", math.MaxInt64, "9223372036854775807"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := NewFormatter(64)
			defer f.Release()
			f.WriteInt(tc.val)
			got := f.String()
			if got != tc.want {
				t.Errorf("Formatter.WriteInt(%d) = %q, want %q", tc.val, got, tc.want)
			}
		})
	}
}

func TestJSONWriter_IntFieldsSerialisedCorrectly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	entry := GetEntry()
	entry.SetMessage("test")
	entry.WithFields(
		Int("count", 42),
		Int64("size", 1024),
		Int("zero", 0),
		Int("negative", -7),
	)

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entry.Release()

	out := buf.String()

	checks := []string{
		`"count":42`,
		`"size":1024`,
		`"zero":0`,
		`"negative":-7`,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output: %s", want, out)
		}
	}
}

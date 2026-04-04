package velocity

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestItoa_MinInt64(t *testing.T) {
	got := itoa(math.MinInt64)
	want := "-9223372036854775808"
	if got != want {
		t.Errorf("itoa(math.MinInt64) = %q, want %q", got, want)
	}
}

// concreteError is a concrete type implementing error, used to create typed nils.
type concreteError struct{ msg string }

func (e *concreteError) Error() string { return e.msg }

// concreteStringer is a concrete type implementing fmt.Stringer, used to create typed nils.
type concreteStringer struct{ val string }

func (s *concreteStringer) String() string { return s.val }

func TestError_TypedNil(t *testing.T) {
	var err *concreteError

	f := Error("err", err)

	if f.Type != FieldTypeString {
		t.Errorf("expected FieldTypeString for typed nil, got %v", f.Type)
	}

	var buf strings.Builder
	f.writeFormatted(&buf)

	if got := buf.String(); got != "<nil>" {
		t.Errorf("expected <nil>, got %q", got)
	}
}

func TestStringer_TypedNil(t *testing.T) {
	var s *concreteStringer

	f := Stringer("s", s)

	if f.Type != FieldTypeString {
		t.Errorf("expected FieldTypeString for typed nil, got %v", f.Type)
	}

	var buf strings.Builder
	f.writeFormatted(&buf)

	if got := buf.String(); got != "<nil>" {
		t.Errorf("expected <nil>, got %q", got)
	}
}

func TestF_TypedNilError(t *testing.T) {
	var err *concreteError

	f := F("err", err)

	if f.Type != FieldTypeString {
		t.Errorf("expected FieldTypeString for typed nil via F(), got %v", f.Type)
	}
}

func TestF_TypedNilStringer(t *testing.T) {
	var s *concreteStringer

	f := F("s", fmt.Stringer(s))

	if f.Type != FieldTypeString {
		t.Errorf("expected FieldTypeString for typed nil stringer via F(), got %v", f.Type)
	}
}

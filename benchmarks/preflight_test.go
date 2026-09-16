package benchmarks

// Preflight assertions for the comparison benchmarks. These run outside every
// timed loop and prove the claims the comparison depends on: each library's
// enabled configuration actually serializes to the shared sink and the output
// is valid for the library's claimed format, while the disabled configurations
// write nothing. A library that fails these is misconfigured, not fast.

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// captureWriter records everything written so preflight can inspect the exact
// bytes each library's benchmark configuration produces.
type captureWriter struct {
	buf    bytes.Buffer
	writes int
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.buf.Write(p)
}

// TestPreflight_EnabledOutputDeliveredAndValid exercises the exact Setup
// configuration of BenchmarkLibraries for each library with a single enabled
// call, then checks delivery and format.
func TestPreflight_EnabledOutputDeliveredAndValid(t *testing.T) {
	for _, lib := range libraries {
		lib := lib
		t.Run(lib.Name, func(t *testing.T) {
			capture := &captureWriter{}
			logger := lib.Setup(capture)

			lib.Info(logger)

			if capture.writes == 0 {
				t.Fatalf("%s: sink Write was never invoked on the enabled path", lib.Name)
			}
			if capture.buf.Len() == 0 {
				t.Fatalf("%s: sink received zero bytes on the enabled path", lib.Name)
			}

			out := capture.buf.String()
			switch lib.Format {
			case "json":
				if !json.Valid([]byte(out)) {
					t.Errorf("%s: claimed JSON format but output does not parse; got %q", lib.Name, out)
				}
				if !strings.Contains(out, "request completed") {
					t.Errorf("%s: JSON output does not contain the message; got %q", lib.Name, out)
				}
			case "text":
				if !strings.Contains(out, "request completed") {
					t.Errorf("%s: text output does not contain the message; got %q", lib.Name, out)
				}
			default:
				t.Errorf("%s: library declares no Format for preflight validation", lib.Name)
			}
		})
	}
}

// TestPreflight_DisabledWritesNothing exercises the exact Setup configuration
// of BenchmarkDisabledLevel and proves the benchmark workload (and the stronger
// Info-at-Error-level case) produces zero output.
func TestPreflight_DisabledWritesNothing(t *testing.T) {
	for _, lib := range disabledLibraries {
		lib := lib
		t.Run(lib.Name, func(t *testing.T) {
			capture := &captureWriter{}
			logger := lib.Setup(capture)

			lib.InfoDisabled(logger)

			if capture.writes != 0 || capture.buf.Len() != 0 {
				t.Fatalf("%s: disabled path wrote to the sink: %d bytes, %d writes",
					lib.Name, capture.buf.Len(), capture.writes)
			}
		})
	}
}

// Compile-time guard that the sink satisfies io.Writer.
var _ io.Writer = (*benchSink)(nil)

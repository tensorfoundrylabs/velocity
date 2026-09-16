package velocity

// Preflight assertions for the benchmark fixtures. These run outside every
// timed loop and prove the claims the benchmarks depend on: enabled paths
// actually serialize to a sink, the output is valid for its format, and
// disabled paths write nothing. If one of these fails, the corresponding
// benchmark numbers are meaningless and the fixture is broken, not fast.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// captureSink records everything written to it so preflight can inspect the
// exact bytes the benchmark configuration produces. Benchmarks themselves use
// the allocation-free benchSink; this type is for assertion, not timing.
type captureSink struct {
	buf    bytes.Buffer
	writes int
}

func (s *captureSink) Write(p []byte) (int, error) {
	s.writes++
	return s.buf.Write(p)
}

func (s *captureSink) bytesWritten() string { return s.buf.String() }
func (s *captureSink) byteCount() int       { return s.buf.Len() }
func (s *captureSink) writeCount() int      { return s.writes }

// newCaptureLogger mirrors newSinkLogger but with capture sinks, so the
// preflight asserts the same configuration the enabled-path benchmarks run.
func newCaptureLogger() (*Logger, *captureSink, *captureSink) {
	console := &captureSink{}
	structured := &captureSink{}
	cfg := defaultConfig()
	cfg.ConsoleOutput = console
	cfg.StructuredOutput = structured
	cfg.ConsoleLevel = LevelDebug
	cfg.StructuredLevel = LevelDebug
	return newFromConfig(cfg), console, structured
}

func TestPreflight_SinkLoggerDeliversBothFormats(t *testing.T) {
	l, console, structured := newCaptureLogger()

	l.Info("preflight message", fiveFields()...)

	if got := console.byteCount(); got == 0 {
		t.Error("console sink received no bytes on the enabled path")
	}
	if got := console.writeCount(); got == 0 {
		t.Error("console sink Write was never invoked")
	}
	if !strings.Contains(console.bytesWritten(), "preflight message") {
		t.Errorf("console output does not contain the message; got %q", console.bytesWritten())
	}

	if got := structured.byteCount(); got == 0 {
		t.Fatal("structured sink received no bytes on the enabled path")
	}
	if !json.Valid([]byte(structured.bytesWritten())) {
		t.Errorf("structured output is not valid JSON; got %q", structured.bytesWritten())
	}
	if !strings.Contains(structured.bytesWritten(), "preflight message") {
		t.Errorf("structured output does not contain the message; got %q", structured.bytesWritten())
	}
}

func TestPreflight_DisabledLevelWritesNothing(t *testing.T) {
	// Exact configuration of BenchmarkInfo_Disabled.
	l, console, structured := newCaptureLogger()
	l.SetLevel(LevelInfo)

	l.Debug("this is suppressed", fiveFields()...)

	if console.byteCount() != 0 || console.writeCount() != 0 {
		t.Errorf("console sink saw output on the disabled path: %d bytes, %d writes",
			console.byteCount(), console.writeCount())
	}
	if structured.byteCount() != 0 || structured.writeCount() != 0 {
		t.Errorf("structured sink saw output on the disabled path: %d bytes, %d writes",
			structured.byteCount(), structured.writeCount())
	}
}

// TestPreflight_NoOutputLoggerHasNoWriters documents why the NoOutput
// benchmarks are labelled separately: newFromConfig maps io.Discard to "no
// output", so no writer is constructed and log calls never serialize. Those
// benchmarks measure the pipeline only, never serialization.
func TestPreflight_NoOutputLoggerHasNoWriters(t *testing.T) {
	l := newNoOutputLogger()

	if l.consoleWriter != nil {
		t.Error("io.Discard config unexpectedly constructed a console writer")
	}
	if l.jsonWriter != nil {
		t.Error("io.Discard config unexpectedly constructed a JSON writer")
	}
}

func TestPreflight_DirectWriterOutputValid(t *testing.T) {
	fields := fiveFields()
	entry := func() *Entry {
		e := GetEntry()
		e.SetLevel(LevelInfo)
		e.SetMessage("preflight message")
		e.SetTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		e.WithFields(fields...)
		return e
	}

	t.Run("JSONWriter", func(t *testing.T) {
		sink := &captureSink{}
		w := NewJSONWriter(sink)
		e := entry()
		if err := w.Write(e); err != nil {
			t.Fatalf("write: %v", err)
		}
		e.Write()
		e.Release()
		if sink.byteCount() == 0 {
			t.Fatal("JSONWriter delivered nothing")
		}
		if !json.Valid([]byte(sink.bytesWritten())) {
			t.Errorf("JSONWriter output is not valid JSON; got %q", sink.bytesWritten())
		}
	})

	t.Run("ConsoleWriter", func(t *testing.T) {
		sink := &captureSink{}
		w := NewConsoleWriter(sink, ThemeNightOwl)
		e := entry()
		if err := w.Write(e); err != nil {
			t.Fatalf("write: %v", err)
		}
		e.Write()
		e.Release()
		if sink.byteCount() == 0 {
			t.Fatal("ConsoleWriter delivered nothing")
		}
		if !strings.Contains(sink.bytesWritten(), "preflight message") {
			t.Errorf("ConsoleWriter output does not contain the message; got %q", sink.bytesWritten())
		}
	})

	t.Run("ConsoleWriter_NoTemplate", func(t *testing.T) {
		sink := &captureSink{}
		w := NewConsoleWriterWithOptions(sink, nil, time.UTC, FieldDisplayInline)
		w.SetTemplate(nil)
		e := entry()
		if err := w.Write(e); err != nil {
			t.Fatalf("write: %v", err)
		}
		e.Write()
		e.Release()
		if sink.byteCount() == 0 {
			t.Fatal("ConsoleWriter (no template) delivered nothing")
		}
		if !strings.Contains(sink.bytesWritten(), "preflight message") {
			t.Errorf("ConsoleWriter (no template) output does not contain the message; got %q", sink.bytesWritten())
		}
	})
}

// TestPreflight_DisplayVariantsDeliver covers the console-only configurations
// whose benchmarks claim display-format serialization: tree mode, the
// Detailed() child and inline indicators.
func TestPreflight_DisplayVariantsDeliver(t *testing.T) {
	cases := []struct {
		name   string
		build  func() (*Logger, *captureSink)
		fields []Field
	}{
		{
			name: "TreeMode",
			build: func() (*Logger, *captureSink) {
				console := &captureSink{}
				cfg := defaultConfig()
				cfg.ConsoleOutput = console
				cfg.StructuredOutput = nil
				cfg.ConsoleLevel = LevelDebug
				cfg.FieldDisplayMode = FieldDisplayTree
				return newFromConfig(cfg), console
			},
			fields: fiveFields(),
		},
		{
			name: "Indicators",
			build: func() (*Logger, *captureSink) {
				console := &captureSink{}
				cfg := defaultConfig()
				cfg.ConsoleOutput = console
				cfg.StructuredOutput = nil
				cfg.ConsoleLevel = LevelDebug
				cfg.Indicators = inlineIndicators{
					component:      true,
					componentField: "component",
					componentWidth: 8,
					countFields:    []string{"count"},
					timingFields:   []string{"startup_ms"},
					statePairs:     [][2]string{{"old_state", "new_state"}},
					removeFromTree: true,
					showGlyphs:     false,
					glyphsExplicit: true,
				}
				return newFromConfig(cfg), console
			},
			fields: indicatorFields(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, console := tc.build()
			l.Info("display variant", tc.fields...)
			if console.byteCount() == 0 || console.writeCount() == 0 {
				t.Fatalf("console sink saw no output: %d bytes, %d writes",
					console.byteCount(), console.writeCount())
			}
			if !strings.Contains(console.bytesWritten(), "display variant") {
				t.Errorf("output does not contain the message; got %q", console.bytesWritten())
			}
		})
	}

	// The Detailed() child serializes through the parent's writers.
	l, console, structured := newCaptureLogger()
	l.Detailed().Info("detailed variant", fiveFields()...)
	if console.byteCount() == 0 {
		t.Error("Detailed child delivered nothing to the console sink")
	}
	if structured.byteCount() == 0 || !json.Valid([]byte(structured.bytesWritten())) {
		t.Errorf("Detailed child delivered invalid structured output: %q", structured.bytesWritten())
	}
}

// TestPreflight_AsyncAccounting proves the invariant the async benchmarks
// assert after their loops: every enqueued entry is either delivered or
// counted as dropped, and delivery reaches a real sink.
func TestPreflight_AsyncAccounting(t *testing.T) {
	t.Run("Deliver", func(t *testing.T) {
		sink := &captureSink{}
		counter := &entryCountingWriter{inner: NewJSONWriter(sink)}
		l := newNoOutputLogger()
		l.SetLevel(LevelDebug)
		l.AddWriter("json", counter)
		mw := l.writers.mw

		const n = 1000
		for range n {
			l.Info("async entry")
		}
		if err := l.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		delivered := counter.entries.Load()
		dropped := mw.DroppedCount()
		// dropped counts only entries the n sends above could have dropped.
		if delivered+int64(dropped) != n { //nolint:gosec // G115: dropped <= n = 1000 on this fresh MultiWriter, conversion cannot overflow
			t.Fatalf("accounting mismatch: enqueued %d, delivered %d, dropped %d", n, delivered, dropped)
		}
		if sink.byteCount() == 0 {
			t.Error("async delivery reached the sink with zero bytes")
		}
	})

	t.Run("DropPath", func(t *testing.T) {
		gated := newGatedWriter()
		l := newNoOutputLogger()
		l.SetLevel(LevelDebug)
		l.AddWriter("gated", gated)
		mw := l.writers.mw

		const n = 1000
		for range n {
			l.Info("async entry")
		}
		// The worker is blocked inside its first Write and the channel holds at
		// most its capacity, so with n well above both, drops are guaranteed.
		close(gated.release)
		if err := l.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		delivered := gated.writes.Load()
		dropped := mw.DroppedCount()
		// dropped counts only entries the n sends above could have dropped.
		if delivered+int64(dropped) != n { //nolint:gosec // G115: dropped <= n = 1000 on this fresh MultiWriter, conversion cannot overflow
			t.Fatalf("accounting mismatch: enqueued %d, delivered %d, dropped %d", n, delivered, dropped)
		}
		if dropped == 0 {
			t.Error("gated worker with full channel produced no drops; drop accounting is broken")
		}
	})
}

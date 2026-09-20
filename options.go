package velocity

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Option is a functional option that mutates a config during logger construction.
// Options are applied in order, so later options override earlier ones.
// Preset options (WithDevelopment, WithProduction, etc.) reset the config to a
// known baseline; layering overrides after them is the intended pattern.
type Option func(*config)

// WithDevelopment resets config to development defaults: coloured console on
// stdout, debug level, local timezone, no structured output.
func WithDevelopment() Option {
	return func(c *config) {
		c.ConsoleOutput = io.Writer(nil) // reset first, then assign
		*c = config{
			ConsoleOutput:    defaultStdout(),
			ConsoleTheme:     nil,
			ConsoleLevel:     LevelDebug,
			StructuredOutput: nil,
			StructuredFormat: FormatJSON,
			StructuredLevel:  LevelOff,
			BufferSize:       1024,
			FieldPoolSize:    50,
			DisableColour:    false,
			TimeFormat:       "2006-01-02 15:04:05",
			DisplayTimezone:  time.Local,
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// WithProduction resets config to production defaults: JSON to stderr at info
// level, no console output, UTC timestamps.
//
// stderr is used rather than stdout so application-level output (piped to
// another process, written to a file, etc.) is not contaminated by log lines.
// Override with WithStructuredOutput if a different destination is required.
func WithProduction() Option {
	return func(c *config) {
		*c = config{
			ConsoleOutput:    io.Discard,
			ConsoleTheme:     nil,
			ConsoleLevel:     LevelOff,
			StructuredOutput: defaultStderr(),
			StructuredFormat: FormatJSON,
			StructuredLevel:  LevelInfo,
			BufferSize:       4096,
			FieldPoolSize:    200,
			DisableColour:    true,
			TimeFormat:       "2006-01-02T15:04:05Z07:00",
			DisplayTimezone:  time.UTC,
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// WithContainer resets config for containerised environments: JSON to stdout at
// info level, colour disabled unless stdout is a TTY.
func WithContainer() Option {
	return func(c *config) {
		*c = config{
			ConsoleOutput:    nil,
			ConsoleTheme:     nil,
			ConsoleLevel:     LevelOff,
			StructuredOutput: defaultStdout(),
			StructuredFormat: FormatJSON,
			StructuredLevel:  LevelInfo,
			BufferSize:       2048,
			FieldPoolSize:    100,
			DisableColour:    !isTerminal(defaultStdoutFile()),
			TimeFormat:       "2006-01-02T15:04:05Z07:00",
			DisplayTimezone:  time.UTC,
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// TestingT is the subset of *testing.T needed by WithTesting.
// Defined here to avoid importing the testing package in the core library.
type TestingT interface {
	Log(args ...any)
	Cleanup(func())
	Helper()
}

// WithTesting configures a logger for use in tests. Writes via t.Log, disables
// colour, sets level to Debug, and registers t.Cleanup(logger.Close).
// Notify output is also captured via the same testingWriter so tests can assert
// on ephemeral output without stderr pollution.
// The cleanup registration happens at construction time.
func WithTesting(t TestingT) Option {
	tw := &testingWriter{t: t}
	return func(c *config) {
		*c = config{
			ConsoleOutput:    tw,
			NotifyOutput:     tw,
			ConsoleTheme:     nil,
			ConsoleLevel:     LevelDebug,
			StructuredOutput: nil,
			StructuredFormat: FormatJSON,
			StructuredLevel:  LevelOff,
			BufferSize:       512,
			FieldPoolSize:    25,
			DisableColour:    true,
			TimeFormat:       "15:04:05.000",
			DisplayTimezone:  time.Local,
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// WithNop configures a logger that discards all output. Use for tests or when a no-op logger is needed.
func WithNop() Option {
	return func(c *config) {
		*c = config{
			ConsoleOutput:    io.Discard,
			ConsoleLevel:     LevelOff,
			StructuredOutput: io.Discard,
			StructuredLevel:  LevelOff,
			BufferSize:       256,
			FieldPoolSize:    0,
			TimeFormat:       "2006-01-02T15:04:05Z07:00",
			FieldDisplayMode: FieldDisplayInline,
		}
	}
}

// WithHighThroughput resets config for high-throughput scenarios: JSON to stderr,
// info level, large buffer, sampling enabled (1000 initial, 100 thereafter).
func WithHighThroughput() Option {
	return func(c *config) {
		*c = config{
			ConsoleOutput:    io.Discard,
			ConsoleTheme:     nil,
			ConsoleLevel:     LevelOff,
			StructuredOutput: defaultStderr(),
			StructuredFormat: FormatJSON,
			StructuredLevel:  LevelInfo,
			BufferSize:       8192,
			FieldPoolSize:    500,
			DisableColour:    true,
			TimeFormat:       "2006-01-02T15:04:05Z07:00",
			DisplayTimezone:  time.UTC,
			FieldDisplayMode: FieldDisplayInline,
			Sampler:          NewCountSampler(1000, 100),
		}
	}
}

// WithLevel sets the minimum level for console (pretty) output only. Structured
// (JSON) output is unaffected. Use WithLevels to set both thresholds in one call,
// or WithStructuredLevel to target the structured output independently.
func WithLevel(level Level) Option {
	return func(c *config) {
		c.ConsoleLevel = level
	}
}

// WithLevels sets the minimum level for both console and structured output in a
// single call. Reach for this when all outputs should share the same threshold
// (e.g. bumping everything to Warn in a test or in a noisy environment). Use
// WithLevel or WithStructuredLevel when you need the thresholds to differ.
func WithLevels(level Level) Option {
	return func(c *config) {
		c.ConsoleLevel = level
		c.StructuredLevel = level
	}
}

// WithConsoleOutput sets the console (pretty) output destination.
//
// To coordinate console logs with live widgets (velocity/live ProgressBar,
// Spinner, MultiProgress), pass the SAME *live.Output to this option and to
// the widget constructors: the coordinator serialises clearing live rows,
// writing whole log records and redrawing the live area. Ordinary io.Writers
// still work standalone, but writes through them and any raw writes
// bypassing a shared Output are outside that coordination guarantee.
func WithConsoleOutput(w io.Writer) Option {
	return func(c *config) {
		c.ConsoleOutput = w
	}
}

// WithNotifyOutput redirects Notify/NotifyLines/NotifyBox output to w instead of
// os.Stderr. Useful in tests where stderr is not captured by the test runner, or
// when the operator channel should go to a specific file descriptor.
func WithNotifyOutput(w io.Writer) Option {
	return func(c *config) {
		c.NotifyOutput = w
	}
}

func WithStructuredOutput(w io.Writer) Option {
	return func(c *config) {
		c.StructuredOutput = w
	}
}

// WithAsyncOutput makes the primary structured (JSON) output non-blocking.
// Callers still format each record (formatting was already outside the
// writer's mutex) but then enqueue the finished bytes onto a bounded queue
// drained by a single background goroutine that performs the write. No
// syscall — and no mutex held across one — ever runs on the logging
// goroutine, so a slow disk serialises nothing: at high request rates the
// synchronous writer's mutex-across-write otherwise parks most request
// goroutines on one lock.
//
// By default the queue holds DefaultAsyncQueue (8192) records; AsyncConfig.Queue
// overrides it (non-positive values get the default). AsyncConfig.OnFull
// selects block (lossless back-pressure, the zero value) or drop (never block
// the caller; losses are counted and reported by Logger.StructuredDroppedCount
// and JSONWriter.DroppedCount).
//
// Fatal delivery stays reliable and ordered: a Fatal record rides the queue
// behind a barrier, so it and every entry accepted before it are written
// before Logger.Fatal proceeds to the FatalHandler. Flush and Close drain
// everything already accepted — Close stops admission first, the drainer
// empties the queue, and there is no timeout, so a permanently stalled sink
// blocks Close exactly as it would synchronously. Close is also amplified by
// the queue: worst case it performs Queue sink writes (8192 of them at the
// default depth, each at the sink's full per-write cost) before returning.
// The library takes no deadline by standing rule, so a caller that needs a
// bounded shutdown must bound Close itself (its own timeout around the call,
// accepting that the drainer may still be writing afterwards). Console output
// is unaffected and stays synchronous.
//
// The option must come after any preset option (WithProduction and friends
// reset the whole config). Without this option behaviour is unchanged and
// the synchronous path gains no allocations.
func WithAsyncOutput(acfg AsyncConfig) Option {
	return func(c *config) {
		c.AsyncOutput = &acfg
	}
}

func WithFormat(format Format) Option {
	return func(c *config) {
		c.StructuredFormat = format
	}
}

func WithStructuredLevel(level Level) Option {
	return func(c *config) {
		c.StructuredLevel = level
	}
}

func WithTheme(theme *Theme) Option {
	return func(c *config) {
		c.ConsoleTheme = theme
	}
}

func WithTimeFormat(format string) Option {
	return func(c *config) {
		c.TimeFormat = format
	}
}

// WithBufferSize is retained for v2 source compatibility. Deprecated: active
// pools are shared and this option does not tune their size.
func WithBufferSize(size int) Option {
	return func(c *config) {
		c.BufferSize = size
	}
}

// WithFieldPoolSize is retained for v2 source compatibility. Deprecated: active
// pools are shared and this option does not tune their size.
func WithFieldPoolSize(size int) Option {
	return func(c *config) {
		c.FieldPoolSize = size
	}
}

// WithColour enables or disables ANSI colour in console output.
func WithColour(enabled bool) Option {
	return func(c *config) {
		c.DisableColour = !enabled
	}
}

// WithSampling enables log sampling using a CountSampler.
// initial is the number of initial messages to log before sampling begins.
// thereafter is the sampling interval (1 in thereafter messages).
func WithSampling(initial, thereafter uint32) Option {
	return func(c *config) {
		c.Sampler = NewCountSampler(uint64(initial), uint64(thereafter))
	}
}

// WithSampler sets a sampler for the logger. Pass nil to disable sampling.
func WithSampler(s Sampler) Option {
	return func(c *config) {
		c.Sampler = s
	}
}

// WithDisplayTimezone sets the timezone for displaying timestamps in console output.
// Logs are stored in UTC but displayed in this zone. Use MustLocation to parse
// an IANA name when building options at init time.
func WithDisplayTimezone(loc *time.Location) Option {
	return func(c *config) {
		if loc != nil {
			c.DisplayTimezone = loc
		}
	}
}

// WithCaller enables or disables caller information capture (file:line and function name).
func WithCaller(enabled bool) Option {
	return func(c *config) {
		c.AddCaller = enabled
	}
}

// WithCallerSkip sets the number of extra stack frames to skip when capturing
// caller information. Use this when wrapping the logger to skip wrapper frames.
func WithCallerSkip(skip int) Option {
	return func(c *config) {
		c.CallerSkip = skip
	}
}

// WithFatalHandler overrides the function called after Fatal() writes its entry.
// Useful in tests to prevent os.Exit.
func WithFatalHandler(fn FatalHandler) Option {
	return func(c *config) {
		c.FatalHandler = fn
	}
}

// WithFieldDisplayMode sets how fields are rendered in console output.
func WithFieldDisplayMode(mode FieldDisplayMode) Option {
	return func(c *config) {
		c.FieldDisplayMode = mode
	}
}

// WithSecureTags controls the per-call <secure>...</secure> message scanner.
// Defaults to true (scan when warranted by the writer mix).
// Set to false only for extreme-perf consumers that never embed sensitive data
// in message strings. The field constructors Secure/SecureURL/Redacted are
// unaffected — they rely on field type, not message scanning.
func WithSecureTags(enabled bool) Option {
	return func(c *config) {
		c.DisableSecureTags = !enabled
	}
}

// WithComponentStyling enables the compact inline-indicator feature with sensible
// defaults: component field name "component", count field "count", state-transition
// pairs old/new and prev/next, glyph auto-detection. The component column is compact
// by default (the name is followed by a single space and the bar), so bars line up
// when names share a length. Call WithComponentColumnWidth for a fixed-width column,
// and WithTimingFields to enable timing promotion (timing field names are
// application-specific).
func WithComponentStyling() Option {
	return func(c *config) {
		c.Indicators.component = true
		c.Indicators.componentField = "component"
		c.Indicators.countFields = []string{"count"}
		c.Indicators.statePairs = [][2]string{
			{"old_state", "new_state"},
			{"prev_state", "next_state"},
		}
		c.Indicators.removeFromTree = true
		// Glyph use is resolved at newFromConfig time: explicit WithInlineGlyphs wins,
		// otherwise GlyphsSupported() auto-detects the terminal.
	}
}

// WithComponentField enables the component prefix indicator and sets the field name
// to look up on each entry. The column is compact by default; call
// WithComponentColumnWidth for a fixed-width aligned column.
func WithComponentField(name string) Option {
	return func(c *config) {
		c.Indicators.component = true
		c.Indicators.componentField = name
		c.Indicators.removeFromTree = true
	}
}

// WithComponentColumnWidth pads (or truncates) the component name to a fixed width so
// the bars align into a column even when names differ in length. The default (unset,
// or zero) is compact: the name keeps its natural width. Has no effect when the
// component indicator is disabled.
func WithComponentColumnWidth(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.Indicators.componentWidth = n
		}
	}
}

// WithCountFields registers field names whose integer values are promoted to a
// "(N)" suffix after the message. The first matching entry field (in field order)
// wins; later matches remain in the tree. Pass multiple names for apps that use
// different field names across components. Promoted values are hidden from the
// console field tree by default; JSON output keeps every field.
func WithCountFields(names ...string) Option {
	return func(c *config) {
		c.Indicators.countFields = append(c.Indicators.countFields, names...)
		c.Indicators.removeFromTree = true
	}
}

// WithTimingFields registers field names whose values are promoted to a timing
// suffix after the message. Matching fields appear in entry-field order, all
// inside one bracket. Timing fields are intentionally not included in
// WithComponentStyling because their names are application-specific. Promoted
// values are hidden from the console field tree by default; JSON output keeps
// every field.
func WithTimingFields(names ...string) Option {
	return func(c *config) {
		c.Indicators.timingFields = append(c.Indicators.timingFields, names...)
		c.Indicators.removeFromTree = true
	}
}

// WithStateTransitionPairs registers pairs of field names that together represent
// a state transition. When both fields of a pair are present on an entry, they are
// collapsed into a "from → to" suffix in the header. Pairs are checked in
// configured order; the first pair with both sides present wins. Promoted values
// are hidden from the console field tree by default; JSON output keeps every field.
func WithStateTransitionPairs(pairs ...[2]string) Option {
	return func(c *config) {
		c.Indicators.statePairs = append(c.Indicators.statePairs, pairs...)
		c.Indicators.removeFromTree = true
	}
}

// WithInlineGlyphs overrides automatic glyph detection (VELOCITY_GLYPHS env var).
// When enabled is false, Unicode glyphs in timing and state indicators are replaced
// with ASCII fallbacks.
func WithInlineGlyphs(enabled bool) Option {
	return func(c *config) {
		c.Indicators.showGlyphs = enabled
		c.Indicators.glyphsExplicit = true
	}
}

// MustLocation parses an IANA timezone name and panics on failure.
// Intended for package-level variable initialisation.
func MustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("velocity: invalid timezone %q: %v", name, err))
	}
	return loc
}

// defaultStdout returns os.Stdout. Extracted so preset closures don't capture
// the global at the wrong moment.
func defaultStdout() *os.File {
	return os.Stdout
}

func defaultStdoutFile() *os.File {
	return os.Stdout
}

func defaultStderr() *os.File {
	return os.Stderr
}

// testingWriter adapts TestingT.Log to io.Writer so the console writer can
// forward formatted log lines into the test's output stream.
type testingWriter struct {
	t TestingT
}

func (w *testingWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	// Trim trailing newline — t.Log adds its own.
	s := string(p)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	w.t.Log(s)
	return len(p), nil
}

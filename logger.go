package velocity

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// writerSet is a shared container for the MultiWriter, its guard mutex, and
// the scanSecure flag. Parent and child loggers hold the same *writerSet pointer
// so that a writer added to the parent after a child is created is visible to
// all siblings. AddWriter initialises the inner MultiWriter on first use.
type writerSet struct {
	closeErr error

	mw        *MultiWriter
	closeDone chan struct{}

	// outputInFlight tracks admitted operations that write to destinations the
	// console writer does not cover (Notify/NotifyLines/NotifyBox to
	// NotifyOutput, BannerLines' no-console fallback). They share the same
	// Close contract as console admission: Close drains them instead of
	// letting a paused admitted call write after it returned. Admission
	// (check+Add) is atomic against the closing transition under mu, and the
	// family drain waits with no locks held.
	outputInFlight sync.WaitGroup
	mu             sync.RWMutex

	closeOnce sync.Once

	// scanSecure reports whether <secure> tag scanning is enabled. It mirrors
	// the WithSecureTags(false) opt-out and is derived from message content
	// only — deliberately independent of the current writer mix, so a writer
	// registered between the scan and the dispatch can never observe plaintext
	// the pre-existing topology would have hidden. Each writer applies its own
	// trust decision to the flagged entry. Immutable after construction.
	scanSecure atomic.Bool

	// Family-wide close lifecycle. Parent and children share one lifetime:
	// once any member completes Close the whole family is closed for good.
	//
	// closing flips to true when the (single) drain begins; it is the write
	// admission gate checked by every log call. closeDone is closed when the
	// drain finishes; closeErr is written before that and read after it, so
	// concurrent Closes all wait for the same completion and all return the
	// same recorded result rather than racing a flag.
	closing atomic.Bool
}

// admitOutput is the shutdown admission point for output paths that have no
// ConsoleWriter to admit through (loggers without console output, Notify's
// caller-owned destination). The closing check and the in-flight registration
// happen under one mutex acquisition, and closeFamily sets closing under the
// same mutex — so an admission either lands before the drain begins (and is
// drained by it) or is rejected here.
func (ws *writerSet) admitOutput() bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.closing.Load() {
		return false
	}
	ws.outputInFlight.Add(1)
	return true
}

// doneOutput balances a successful admitOutput. Call exactly once per granted
// admission, on every return path.
func (ws *writerSet) doneOutput() {
	ws.outputInFlight.Done()
}

// closeFamily runs drain exactly once and blocks every caller until the same
// completion, returning the recorded result. A caller that loses the once race
// must not report success merely because another caller set a flag.
func (ws *writerSet) closeFamily(drain func() error) error {
	ws.closeOnce.Do(func() {
		// Publish completion even if the drain panics (e.g. a user writer's
		// Close panicking through MultiWriter.Close): the deferred close lets
		// the panicking caller's unwind still release every concurrent family
		// Close instead of stranding them on closeDone. Matches MultiWriter
		// and ConsoleWriter.
		defer close(ws.closeDone)

		// closing transitions under mu so admitOutput's check+Add is atomic
		// against it: an admitted operation either registers before this
		// point (and is drained below) or is rejected afterwards.
		ws.mu.Lock()
		ws.closing.Store(true)
		ws.mu.Unlock()

		// Family-level admitted output (Notify family, BannerLines no-console
		// fallback) completes BEFORE the writer drain, not after: those
		// operations can share the console/JSON destinations, and drain()
		// closes and flushes those destinations — a write landing after that
		// flush stays buffered forever with no second flush to emit it (the
		// F2 order confirmation). Waiting first is safe: these operations hold
		// no drain-owned locks (they serialise on consoleWriter.mu or the
		// notify fallback mutex), and the drain holds none while waiting.
		// Console/JSON-admitted operations are drained inside drain() by each
		// writer's own inFlight wait, which still precedes that writer's
		// flush.
		ws.outputInFlight.Wait()

		ws.closeErr = drain()
	})
	<-ws.closeDone
	return ws.closeErr
}

// isClosing is the write-admission check shared by every log call. Reads a
// single atomic; log calls on a closed family cost one load and return.
func (ws *writerSet) isClosing() bool {
	return ws.closing.Load()
}

// themeState is the mutable presentation state shared by a logger and every
// child created via With / Detailed / WithComponent / WithRequest. Static
// config stays immutable after construction; runtime theme swaps land here so
// Theme(), Style() and all renderer reads share one synchronised source.
// The mutex is never held while calling into writers or renderables.
type themeState struct {
	theme *Theme
	mu    sync.RWMutex
}

// get returns the active theme, falling back to ThemeNightOwl for the
// nil-means-default convention.
func (ts *themeState) get() *Theme {
	if ts == nil {
		return ThemeNightOwl
	}
	ts.mu.RLock()
	theme := ts.theme
	ts.mu.RUnlock()
	if theme == nil {
		return ThemeNightOwl
	}
	return theme
}

// set publishes theme as the active theme for the whole family.
func (ts *themeState) set(theme *Theme) {
	ts.mu.Lock()
	ts.theme = theme
	ts.mu.Unlock()
}

type Logger struct {
	sampler Sampler

	cfg           *config
	consoleWriter *ConsoleWriter
	jsonWriter    *JSONWriter

	// writers is shared by reference between a logger and all children created
	// via With / Detailed / WithComponent / WithRequest. AddWriter on any member
	// of the family is immediately visible to all siblings.
	writers *writerSet

	// themes carries the runtime-swappable console theme. Shared by reference
	// with all children so SetTheme on any member reaches the whole family and
	// every renderer read is synchronised.
	themes *themeState

	// baseFields are prepended to every log entry on this logger.
	// Set by With() and inherited by child loggers.
	baseFields []Field

	// forceTreeDisplay makes every log call on this logger render fields as a tree,
	// regardless of FieldDisplayMode. Set via Detailed().
	forceTreeDisplay bool

	level atomic.Int32
}

// New constructs a Logger from the given options. Panics if the resolved
// configuration is invalid (e.g. BufferSize < 256, sampler with both counts
// zero). Apply preset options first, then override-specific ones:
//
//	log := velocity.New(velocity.WithDevelopment(), velocity.WithLevel(velocity.LevelWarn))
func New(opts ...Option) *Logger {
	l, err := TryNew(opts...)
	if err != nil {
		panic(fmt.Sprintf("velocity: invalid configuration: %v", err))
	}
	return l
}

// NopLogger returns a Logger that discards all output. Intended for tests and
// wiring paths where a non-nil logger is required but output is unwanted.
func NopLogger() *Logger {
	return New(WithNop())
}

// TryNew constructs a Logger from the given options, returning any validation
// error rather than panicking.
func TryNew(opts ...Option) (*Logger, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	l := newFromConfig(cfg)

	// WithTesting registers cleanup on the testing.T after the logger is built
	// so that Close() flushes the async MultiWriter before the test ends.
	for _, opt := range opts {
		if tw, ok := extractTestingOpt(opt); ok {
			tw.t.Cleanup(func() { _ = l.Close() })
			break
		}
	}

	return l, nil
}

// extractTestingOpt peeks at an option to see whether it wired a testingWriter.
// We need the TestingT so we can register t.Cleanup on the logger after build.
func extractTestingOpt(opt Option) (*testingWriter, bool) {
	if opt == nil {
		return nil, false
	}
	probe := &config{}
	opt(probe)
	if tw, ok := probe.ConsoleOutput.(*testingWriter); ok {
		return tw, true
	}
	return nil, false
}

func newFromConfig(cfg *config) *Logger {
	logger := &Logger{
		cfg:     cfg,
		sampler: cfg.Sampler,
		writers: &writerSet{closeDone: make(chan struct{})},
		themes:  &themeState{theme: cfg.ConsoleTheme},
	}
	// Tag scanning is content-driven: whenever it is enabled the scan runs
	// regardless of the writer mix, so late-added untrusted writers inherit
	// redaction for entries flagged before they registered. WithSecureTags(false)
	// is the explicit opt-out.
	logger.writers.scanSecure.Store(!cfg.DisableSecureTags)

	// Clamp levels to LevelOff when the corresponding output doesn't exist, so
	// the gate only reflects outputs that are actually wired up. Without this a
	// console-only logger at LevelWarn still processes Info entries because the
	// default StructuredLevel (LevelInfo) drags the effective gate down.
	consoleLevel := cfg.ConsoleLevel
	if cfg.ConsoleOutput == nil || cfg.ConsoleOutput == io.Discard {
		consoleLevel = LevelOff
	}
	structuredLevel := cfg.StructuredLevel
	if cfg.StructuredOutput == nil || cfg.StructuredOutput == io.Discard {
		structuredLevel = LevelOff
	}
	// Use the most permissive level of the outputs that actually exist so logs
	// aren't dropped when outputs have different thresholds.
	effectiveLevel := min(structuredLevel, consoleLevel)
	// If no fixed outputs are configured at all (e.g. MultiWriter-only or Nop),
	// fall back to the original min so dynamic AddWriter calls still work.
	if consoleLevel == LevelOff && structuredLevel == LevelOff {
		effectiveLevel = min(cfg.StructuredLevel, cfg.ConsoleLevel)
	}
	logger.level.Store(int32(effectiveLevel))

	if cfg.ConsoleOutput != nil && cfg.ConsoleOutput != io.Discard {
		// Resolve the theme before constructing the writer so it never needs to
		// know about DisableColour. When colour is disabled we pass noColourTheme
		// (all cached escapes are empty strings) instead of the user-supplied theme.
		consoleTheme := cfg.ConsoleTheme
		if cfg.DisableColour {
			consoleTheme = noColourTheme
		}
		logger.consoleWriter = NewConsoleWriterWithOptions(cfg.ConsoleOutput, consoleTheme, cfg.DisplayTimezone, cfg.FieldDisplayMode)
		// Record the explicit colour disable on the writer so it survives every
		// SetTheme — the writer itself never sees config again after this.
		logger.consoleWriter.colourExplicitlyDisabled = cfg.DisableColour
		// Recompute cached prefix widths after applying a custom TimeFormat so
		// Logger.Render's indent matches the actual rendered timestamp width.
		if cfg.TimeFormat != "" && logger.consoleWriter != nil {
			logger.consoleWriter.template.timeFormat = cfg.TimeFormat
			logger.consoleWriter.template.initCache()
		}
		// Thread inline-indicator config onto the template so render paths can
		// access it without needing a reference back to config. indicatorsActive is
		// precomputed here so the disabled hot path checks one bool, not the struct.
		if logger.consoleWriter != nil {
			logger.consoleWriter.template.indicators = cfg.Indicators
			logger.consoleWriter.template.indicatorsActive = cfg.Indicators.active()
		}
	}

	if cfg.StructuredOutput != nil && cfg.StructuredOutput != io.Discard {
		logger.jsonWriter = NewJSONWriter(cfg.StructuredOutput)
	}

	return logger
}

func validateConfig(cfg *config) error {
	var errs []error

	if cfg.BufferSize < 256 {
		errs = append(errs, fmt.Errorf("buffer size must be at least 256 bytes, got %d", cfg.BufferSize))
	}
	if cfg.BufferSize > 1024*1024 {
		errs = append(errs, fmt.Errorf("buffer size must not exceed 1MB, got %d", cfg.BufferSize))
	}
	if cfg.FieldPoolSize < 0 {
		errs = append(errs, fmt.Errorf("field pool size must not be negative, got %d", cfg.FieldPoolSize))
	}
	if cfg.FieldPoolSize > 10000 {
		errs = append(errs, fmt.Errorf("field pool size must not exceed 10000, got %d", cfg.FieldPoolSize))
	}
	if cfg.Sampler != nil {
		if cs, ok := cfg.Sampler.(*CountSampler); ok {
			if cs.Initial == 0 && cs.Thereafter == 0 {
				errs = append(errs, errors.New("sampler initial and thereafter counts must not both be zero"))
			}
		}
	}

	return errors.Join(errs...)
}

func (l *Logger) SetLevel(level Level) {
	if l == nil {
		return
	}
	l.level.Store(int32(level))
}

func (l *Logger) Level() Level {
	if l == nil {
		return LevelOff
	}
	return Level(l.level.Load())
}

// CallerEnabled reports whether this logger is configured to capture caller
// information. Used by adapters (e.g. slogbridge) that carry their own PC and
// need to know whether to resolve it into Caller/Line/Function fields.
func (l *Logger) CallerEnabled() bool {
	if l == nil || l.cfg == nil {
		return false
	}
	return l.cfg.AddCaller
}

// With returns a child logger that prepends the given fields to every log entry.
// The child shares the writer topology (writers) with the parent, so writers
// added to the parent after the child is created are immediately visible to both.
// Level is snapshotted at the time of the call; dynamic parent level changes
// do not propagate to the child after creation.
func (l *Logger) With(fields ...Field) *Logger {
	if l == nil || len(fields) == 0 {
		return l
	}
	child := &Logger{
		cfg:              l.cfg,
		consoleWriter:    l.consoleWriter,
		jsonWriter:       l.jsonWriter,
		sampler:          l.sampler,
		writers:          l.writers, // shared pointer — parent topology changes propagate
		themes:           l.themes,  // shared pointer — runtime theme swaps reach the family
		forceTreeDisplay: l.forceTreeDisplay,
	}
	child.level.Store(l.level.Load())
	// scanSecure and themes live on the shared writerSet/themeState — no copy needed.
	newBase := make([]Field, len(l.baseFields)+len(fields))
	copy(newBase, l.baseFields)
	copy(newBase[len(l.baseFields):], fields)
	child.baseFields = newBase
	return child
}

// AddWriter registers a named writer to receive log entries.
// Options control per-writer behaviour; see WriterTrusted.
// Thread-safe; writers process entries asynchronously via MultiWriter.
// Writers added to a parent logger are immediately visible to all child loggers
// created via With, Detailed, WithComponent, or WithRequest.
func (l *Logger) AddWriter(name string, w Writer, opts ...WriterOption) {
	if l == nil {
		return
	}

	l.writers.mu.Lock()
	defer l.writers.mu.Unlock()

	// Admission point for dynamic registration: once the family drain has
	// started there is no worker left to deliver, and creating a fresh
	// MultiWriter would revive output for every sibling after a completed
	// Close. The check sits under the same lock the drain uses to snapshot
	// the MultiWriter, so an AddWriter that passes here is either fully
	// registered before the drain reads the topology (and is drained with it)
	// or rejected here.
	if l.writers.isClosing() {
		return
	}

	if l.writers.mw == nil {
		l.writers.mw = NewMultiWriter()
	}
	l.writers.mw.AddWriter(name, w, opts...)

	// Propagate trust to the writer itself when it exposes the hook.
	// This keeps writer.IsTrusted() consistent with the MultiWriter worker state.
	o := applyWriterOptions(opts)
	if o.isTrusted {
		if st, ok := w.(interface{ SetTrusted(bool) }); ok {
			st.SetTrusted(true)
		}
	}
}

// RemoveWriter removes the named writer and returns it for inspection or flush.
// The MultiWriter worker drains and closes the writer asynchronously after removal —
// do not call Close on the returned value, or you risk a double-close panic on writers
// that aren't idempotent. Returns nil if no writer with that name exists.
// Thread-safe.
func (l *Logger) RemoveWriter(name string) Writer {
	if l == nil {
		return nil
	}

	l.writers.mu.Lock()
	defer l.writers.mu.Unlock()

	if l.writers.mw == nil {
		return nil
	}
	w := l.writers.mw.RemoveWriter(name)
	return w
}

// Writer returns the writer registered under name, or nil.
// Useful for inspecting writer capabilities without removing it.
// Thread-safe.
func (l *Logger) Writer(name string) Writer {
	if l == nil {
		return nil
	}

	l.writers.mu.RLock()
	defer l.writers.mu.RUnlock()

	if l.writers.mw == nil {
		return nil
	}
	return l.writers.mw.WriterByName(name)
}

// Close flushes and shuts down all writers owned by the logger.
//
// Specifically: the console writer is flushed (its output buffer drained), the
// JSON writer is flushed, and all named writers added via AddWriter are drained
// and closed. Caller-supplied io.Writers passed via WithConsoleOutput /
// WithStructuredOutput are NOT closed — the logger does not own those handles.
//
// Close state is family-wide: parent and child loggers share one lifetime, so
// closing any member closes the family for every sibling, and later Close
// calls (from any member, concurrent or not) wait for the same completed drain
// and return the same recorded error. AddWriter after Close cannot revive
// output.
//
// Writes admitted before the drain starts are drained to their sinks; log
// calls that arrive afterwards are dropped silently. A stalled writer can hold
// the drain open indefinitely — a generic io.Writer cannot be cancelled and no
// timeout is faked.
//
// Returns the first error encountered; remaining flushes still proceed.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}

	// The drain closure runs exactly once for the whole family; consoleWriter,
	// jsonWriter and the MultiWriter are shared pointers, so whichever member
	// runs it, the effect is identical for every sibling.
	return l.writers.closeFamily(func() error {
		var firstErr error
		setErr := func(e error) {
			if firstErr == nil && e != nil {
				firstErr = e
			}
		}

		// Flush the console writer if it implements io.Closer (ring-buffer path does).
		if l.consoleWriter != nil {
			if c, ok := any(l.consoleWriter).(io.Closer); ok {
				setErr(c.Close())
			}
		}

		// Flush the JSON writer if it implements io.Closer.
		if l.jsonWriter != nil {
			if c, ok := any(l.jsonWriter).(io.Closer); ok {
				setErr(c.Close())
			}
		}

		// Snapshot the MultiWriter under the lock, then drain with no locks
		// held: workers never need writers.mu, but holding it across the drain
		// would block concurrent admission checks for the drain's duration.
		l.writers.mu.Lock()
		mw := l.writers.mw
		l.writers.mu.Unlock()

		if mw != nil {
			setErr(mw.Close())
			l.writers.mu.Lock()
			if l.writers.mw == mw {
				l.writers.mw = nil
			}
			l.writers.mu.Unlock()
		}

		return firstErr
	})
}

func (l *Logger) Debug(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[!DBG] %s\n", msg)
		return
	}
	if l.writers.isClosing() || !l.isEnabled(LevelDebug) {
		return
	}
	l.log(LevelDebug, msg, fields...)
}

func (l *Logger) Info(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[INFO] %s\n", msg)
		return
	}
	if l.writers.isClosing() || !l.isEnabled(LevelInfo) {
		return
	}
	l.log(LevelInfo, msg, fields...)
}

func (l *Logger) Warn(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[WARN] %s\n", msg)
		return
	}
	if l.writers.isClosing() || !l.isEnabled(LevelWarn) {
		return
	}
	l.log(LevelWarn, msg, fields...)
}

func (l *Logger) Error(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[ERR!] %s\n", msg)
		return
	}
	if l.writers.isClosing() || !l.isEnabled(LevelError) {
		return
	}
	l.log(LevelError, msg, fields...)
}

// Status logs a message at the given level with a StatusKind badge.
//
// Console output: renders an inline indented badge ([OKAY] / [FAIL] etc.) with
// no timestamp or level label — visually subordinate to the surrounding log lines.
//
// JSON / structured output: emits a full structured record with a "status" field
// set to the lowercase kind string (ok, fail, warn, info, pending, skip), so log
// queries continue to work.
//
// All standard log-call semantics apply: level filtering, sampling, base fields.
func (l *Logger) Status(level Level, kind StatusKind, msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", kind.String(), msg)
		return
	}
	if l.writers.isClosing() || !l.isEnabled(level) {
		return
	}

	// Honour the sampler before doing any work — consistent with logInternal.
	// Fatal is exempt on every dispatch path: the level is never suppressed.
	if level != LevelFatal && l.sampler != nil && !l.sampler.Sample(level, msg) {
		return
	}

	// Merge baseFields with call-site fields so child loggers stamp their
	// context fields onto both the console badge and the structured record.
	allFields := fields
	if len(l.baseFields) > 0 {
		merged := make([]Field, len(l.baseFields)+len(fields))
		copy(merged, l.baseFields)
		copy(merged[len(l.baseFields):], fields)
		allFields = merged
	}

	// Console path: inline badge via Render, no timestamp or level label.
	// Uses the logger's active theme and routes through the console writer mutex
	// so status items cannot interleave with concurrent log lines.
	if l.consoleWriter != nil && level >= l.cfg.ConsoleLevel {
		// Apply secure-tag processing to the message before rendering to the console.
		// TTY (trusted) writers show the plaintext with delimiters stripped;
		// non-TTY (untrusted, e.g. piped to a file) writers show the redaction mark.
		consoleMsg := msg
		if l.writers.scanSecure.Load() && strings.IndexByte(msg, '<') >= 0 {
			if l.consoleWriter.isTTY {
				consoleMsg = stripSecureTags(msg)
			} else {
				consoleMsg = redactSecureTags(msg, "[REDACTED]")
			}
		}
		item := NewStatusItem(kind, consoleMsg, l.Theme(), allFields...)
		l.Render(item)
	}

	// Structured / additional-writer path: full record with statusKind set.
	// The console writer is skipped here — it already rendered inline above.
	// Pass allFields so structured output also includes baseFields.
	l.logStatusStructuredWithFields(level, kind, msg, allFields)
}

// logStatusStructuredWithFields emits a structured log entry for Status calls.
// Only JSON and additional writers receive this entry; the console writer is
// intentionally skipped because Status renders inline via Render instead.
// fields must already include baseFields — the caller is responsible for merging.
func (l *Logger) logStatusStructuredWithFields(level Level, kind StatusKind, msg string, fields []Field) {
	if l == nil {
		return
	}

	// Nothing to do when there are no structured outputs.
	// Guard the mw read with the RLock to avoid a race with concurrent AddWriter/Close
	// calls that replace or nil-out the MultiWriter under the write lock.
	l.writers.mu.RLock()
	hasMW := l.writers.mw != nil
	l.writers.mu.RUnlock()
	hasStructured := (l.jsonWriter != nil && level >= l.cfg.StructuredLevel) || hasMW
	if !hasStructured {
		return
	}

	entry := GetEntry()
	defer entry.Release()

	entry.SetLevel(level)
	entry.SetMessage(msg)
	entry.SetTime(time.Now())
	entry.forceTreeDisplay = l.forceTreeDisplay
	entry.statusKind = kind

	if l.writers.scanSecure.Load() && strings.IndexByte(msg, '<') >= 0 {
		entry.maybeSecure = true
	}

	if len(fields) > 0 {
		entry.WithFields(fields...)
	}

	// Status → logStatusStructuredWithFields → captureCaller is 3 frames, not 4.
	l.captureCaller(entry, -1)

	if l.jsonWriter != nil && level >= l.cfg.StructuredLevel {
		if err := l.jsonWriter.WriteStatus(entry); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
		}
	}

	entry.Write()

	l.writers.mu.RLock()
	if l.writers.mw != nil {
		_ = l.writers.mw.Write(entry)
	}
	l.writers.mu.RUnlock()
}

// Group logs a count-headed block with one item per line.
//
// On a TTY console the output is:
//
//	2006-01-02T15:04:05+10:00 [INFO] Registering routes (3)
//	                                   ├─ GET  /api/v1/users
//	                                   ├─ POST /api/v1/users
//	                                   └─ GET  /api/v1/users/:id
//
// The JSON writer emits a single entry with "count" and "items" fields.
// Item markers are visual-only and are stripped from JSON output.
// All standard log-call semantics apply: level filtering, sampling, base fields.
func (l *Logger) Group(level Level, msg string, items ...GroupItem) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[%s] %s (%d)\n", level.ConciseLabel(), msg, len(items))
		return
	}
	if l.writers.isClosing() || !l.isEnabled(level) {
		return
	}
	l.logGroup(level, msg, items)
}

// logGroup is the internal implementation of Group.
func (l *Logger) logGroup(level Level, msg string, items []GroupItem) {
	if l == nil {
		return
	}

	if level != LevelFatal && l.sampler != nil && !l.sampler.Sample(level, msg) {
		return
	}

	entry := GetEntry()
	defer entry.Release()

	// The composite "msg (N)" string is set as the entry message so the standard
	// template path renders the count on non-TTY paths without special-casing.
	entry.SetLevel(level)
	entry.SetMessage(groupMsgWithCount(msg, len(items)))
	entry.SetTime(time.Now())
	entry.forceTreeDisplay = l.forceTreeDisplay

	// The scan covers the header AND every item payload: writers apply the
	// entry-wide flag to both, so non-header text follows the same redaction
	// policy as the header.
	if l.writers.scanSecure.Load() && !entry.maybeSecure {
		if strings.IndexByte(msg, '<') >= 0 {
			entry.maybeSecure = true
		} else {
			for _, item := range items {
				if strings.IndexByte(item.Text, '<') >= 0 {
					entry.maybeSecure = true
					break
				}
			}
		}
	}

	if len(l.baseFields) > 0 {
		entry.WithFields(l.baseFields...)
	}

	// Group → logGroup → captureCaller is 3 frames, not 4.
	l.captureCaller(entry, -1)

	if l.cfg != nil {
		// Console and JSON writers receive items directly — their dedicated Group
		// methods handle rendering without adding a FieldTypeGroupItems field to
		// the entry, which would cause the standard template to emit "[N items]".
		if level >= l.cfg.ConsoleLevel && l.consoleWriter != nil {
			if err := l.consoleWriter.WriteGroup(entry, items); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
			}
		}

		if level >= l.cfg.StructuredLevel && l.jsonWriter != nil {
			if err := l.jsonWriter.WriteGroup(entry, items); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
			}
		}

		entry.Write()

		l.writers.mu.RLock()
		if l.writers.mw != nil {
			// Additional writers get the typed field so they can optionally
			// render the items. Writers that don't understand FieldTypeGroupItems
			// emit "[N items]" as a fallback hint (see writeFormatted).
			entry.WithFields(groupItemsField(items))
			_ = l.writers.mw.Write(entry)
		}
		l.writers.mu.RUnlock()
		return
	}

	entry.Write()
}

// Fatal logs at LevelFatal, guarantees delivery of the entry (and everything
// accepted before it) to the active sinks, flushes flushable sinks, then
// invokes the configured FatalHandler or exits with status 1.
//
// Delivery is reliable and ordered: the async fan-out takes a blocking path
// for the fatal entry — never the queue-full drop branch — and blocks until
// the workers have processed it. A stalled writer therefore stalls Fatal; a
// generic io.Writer cannot be cancelled and no timeout is faked. A custom
// FatalHandler that returns leaves the logger reusable: nothing is closed on
// this path.
//
// Only Fatal has process-control semantics. Entries routed through LogEntry or
// slogbridge at LevelFatal are never sampled but never exit the process.
func (l *Logger) Fatal(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[FATL] %s\n", msg)
		os.Exit(1)
	}
	l.logReliable(LevelFatal, msg, l.forceTreeDisplay, fields...)

	// Flush what can be flushed before the handler/exit decision. The named
	// writers' own flushables were already flushed in-line by their workers
	// (see writerWorker); these are the logger-level sinks.
	if l.consoleWriter != nil {
		_ = l.consoleWriter.Flush() //nolint:staticcheck // A flush error must not block the exit path
	}
	if l.jsonWriter != nil {
		_ = l.jsonWriter.Flush() //nolint:staticcheck // A flush error must not block the exit path
	}

	if l.cfg != nil && l.cfg.FatalHandler != nil {
		l.cfg.FatalHandler()
		return
	}
	os.Exit(1)
}

func (l *Logger) isEnabled(level Level) bool {
	return level >= Level(l.level.Load())
}

// captureCaller populates entry with caller information if configured.
// extraSkip adjusts the number of frames skipped on top of the standard 4.
// Pass -1 from 3-frame call sites (Status/Group/Continue) that don't go through
// the log→logInternal pair, so the reported frame is the user call site, not the
// internal dispatch helper.
func (l *Logger) captureCaller(entry *Entry, extraSkip int) {
	if l.cfg == nil || !l.cfg.AddCaller {
		return
	}

	// Call stack: user code → Info/Debug/etc → logInternal → captureCaller → runtime.Caller
	// Skip 4 frames to reach user code (captureCaller, logInternal, log/logDetailed, Info/Debug/etc)
	skip := 4 + l.cfg.CallerSkip + extraSkip

	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return
	}

	shortFile := file
	for i := len(file) - 1; i >= 0; i-- {
		if file[i] == '/' || file[i] == '\\' {
			shortFile = file[i+1:]
			break
		}
	}

	entry.Caller = shortFile
	entry.Line = line

	if fn := runtime.FuncForPC(pc); fn != nil {
		entry.Function = fn.Name()
	}
}

func (l *Logger) log(level Level, msg string, fields ...Field) {
	l.logInternal(level, msg, l.forceTreeDisplay, false, fields...)
}

// logReliable is the Fatal entry point: identical to log but the async fan-out
// uses MultiWriter.WriteReliable so the entry cannot be dropped.
func (l *Logger) logReliable(level Level, msg string, forceTree bool, fields ...Field) {
	l.logInternal(level, msg, forceTree, true, fields...)
}

// LogEntry dispatches a pre-populated entry to all configured writers.
// Used by slog bridge and other external adapters.
func (l *Logger) LogEntry(e *Entry) {
	if l == nil || e == nil || l.writers.isClosing() || !l.isEnabled(e.Level) {
		return
	}
	// Adapters use this path too; apply the same sampler exactly once. Fatal is
	// exempt on every dispatch path (matching logInternal), but a mapped slog
	// fatal level is only a level value: it must never invoke FatalHandler or
	// os.Exit — only Logger.Fatal has process-control semantics.
	if e.Level != LevelFatal && l.sampler != nil && !l.sampler.Sample(e.Level, e.Message) {
		return
	}
	// Prepend base fields from With() so child loggers propagate their fields.
	// Copy existing into a separate slice before zeroing e.Fields; if we simply
	// re-slice to [:0] and append baseFields, the backing array is shared and
	// the first len(baseFields) user fields get silently overwritten.
	if len(l.baseFields) > 0 {
		saved := make([]Field, len(e.Fields))
		copy(saved, e.Fields)
		e.Fields = e.Fields[:0]
		e.WithFields(l.baseFields...)
		e.WithFields(saved...)
	}
	// Apply the same <secure> tag scan as logInternal so entries routed through
	// external adapters (e.g. slogbridge) benefit from message-level redaction.
	if l.writers.scanSecure.Load() && strings.IndexByte(e.Message, '<') >= 0 {
		e.maybeSecure = true
	}
	if l.cfg != nil {
		if e.Level >= l.cfg.ConsoleLevel && l.consoleWriter != nil {
			_ = l.consoleWriter.Write(e)
		}
		if e.Level >= l.cfg.StructuredLevel && l.jsonWriter != nil {
			_ = l.jsonWriter.Write(e)
		}
		e.Write()
		l.writers.mu.RLock()
		if l.writers.mw != nil {
			_ = l.writers.mw.Write(e)
		}
		l.writers.mu.RUnlock()
		return
	}
	e.Write()
}

// logInternal is the shared implementation for log and logReliable. reliable
// selects the blocking fan-out path (used only by Fatal); the ordinary path
// stays nonblocking with observable drops.
func (l *Logger) logInternal(level Level, msg string, forceTree, reliable bool, fields ...Field) {
	if l == nil {
		return
	}

	if level != LevelFatal && l.sampler != nil && !l.sampler.Sample(level, msg) {
		return
	}

	entry := GetEntry()
	defer entry.Release()

	entry.SetLevel(level)
	entry.SetMessage(msg)
	entry.SetTime(time.Now())
	entry.forceTreeDisplay = forceTree

	// Check whether the message contains a '<' so writers can run the
	// <secure> tag pass. strings.IndexByte is SIMD-accelerated in the Go runtime
	// (~3-5ns), zero-alloc on string input. The flag is content-derived and
	// independent of the writer mix: every writer then applies its own trust
	// decision, which closes the AddWriter-between-scan-and-dispatch window —
	// a late-registered untrusted writer can never receive an unflagged entry.
	if l.writers.scanSecure.Load() && strings.IndexByte(msg, '<') >= 0 {
		entry.maybeSecure = true
	}

	if len(l.baseFields) > 0 {
		entry.WithFields(l.baseFields...)
	}
	if len(fields) > 0 {
		entry.WithFields(fields...)
	}

	l.captureCaller(entry, 0)

	if l.cfg != nil {
		if level >= l.cfg.ConsoleLevel && l.consoleWriter != nil {
			if err := l.consoleWriter.Write(entry); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
			}
		}

		if level >= l.cfg.StructuredLevel && l.jsonWriter != nil {
			if err := l.jsonWriter.Write(entry); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
			}
		}

		entry.Write()

		l.writers.mu.RLock()
		mw := l.writers.mw
		l.writers.mu.RUnlock()
		if mw != nil {
			if reliable {
				// Fatal: blocking, ordered delivery — never the queue-full drop.
				_ = mw.WriteReliable(entry)
			} else {
				_ = mw.Write(entry)
			}
		}
		return
	}

	entry.Write()
}

// Theme returns the active console theme for this logger. The value lives in
// shared runtime state (themeState), so a SetTheme on any family member is
// immediately visible here and on every child. Falls back to ThemeNightOwl
// when nil or unconfigured.
func (l *Logger) Theme() *Theme {
	if l == nil {
		return ThemeNightOwl
	}
	return l.themes.get()
}

// SetTheme updates the active theme on all writers that support it.
// The new theme is published to the family-wide runtime state, so subsequent
// With() clones and all existing children observe it.
// A nil theme resets to the default (ThemeNightOwl); it does not disable colour.
// To disable colour use WithColour(false) or the NO_COLOR environment variable.
// User-defined themes are cached automatically: if the theme's ANSI sequences are
// not yet populated they are computed in-place. Nil-safe.
//
// Registration state is snapshotted before any external writer is called: an
// external ThemedWriter.SetTheme implementation may call back into Writer,
// WriterByName, Stats or Theme and must never deadlock. A writer removed
// concurrently with the swap may miss the new theme — its worker is already
// draining for closure, so no further output observes a stale theme.
// Theme changes never affect trust or colour permission.
func (l *Logger) SetTheme(theme *Theme) {
	if l == nil {
		return
	}

	// Nil means "reset to default". Normalise here so the shared state and all
	// writers agree.
	if theme == nil {
		theme = ThemeNightOwl
	}

	// Publish to the family-wide runtime state first so Theme()/Style() and
	// child loggers observe the swap even if a writer callback misbehaves.
	l.themes.set(theme)

	if l.consoleWriter != nil {
		l.consoleWriter.SetTheme(theme)
	}

	// Snapshot registered ThemedWriters under the topology locks, then invoke
	// them with no locks held. Lock order matches the write path:
	// writers.mu -> mw.mu; external code runs outside both.
	l.writers.mu.RLock()
	var themed []ThemedWriter
	if l.writers.mw != nil {
		l.writers.mw.mu.Lock()
		for _, ws := range l.writers.mw.workers {
			if s, ok := ws.w.(ThemedWriter); ok {
				themed = append(themed, s)
			}
		}
		l.writers.mw.mu.Unlock()
	}
	l.writers.mu.RUnlock()

	for _, s := range themed {
		s.SetTheme(theme)
	}
}

// Style returns the active theme for use in manual ANSI formatting.
// Follows the same fallback logic as Theme(): an unconfigured theme falls back
// to ThemeNightOwl (matching the console writer), not to the no-colour sentinel.
// noColourTheme is only returned when colour is explicitly disabled, or when the
// logger has no console writer at all (JSON-only, nop, or production preset).
func (l *Logger) Style() *Theme {
	if l == nil {
		return noColourTheme
	}
	// Colour explicitly disabled — return a mono theme regardless of writer.
	if l.cfg != nil && l.cfg.DisableColour {
		return noColourTheme
	}
	// No console output configured — there is no styled channel to match.
	if l.consoleWriter == nil {
		return noColourTheme
	}
	// Colour resolved to off for this writer (NO_COLOR, piped, non-TTY) — return
	// mono so callers using Style().Format() don't emit ANSI into pipes or files.
	// The template is snapshotted under the writer mutex: SetTheme replaces it
	// concurrently and this read must not race.
	tmpl := l.consoleWriter.snapshotState()
	if tmpl == nil || !tmpl.useColours {
		return noColourTheme
	}
	// Console writer is active and colour-capable: return the themed palette.
	return l.Theme()
}

// BannerLines prints multiple lines of pre-formatted text to the console writer
// without log timestamps, levels, or field formatting.
// Named BannerLines to avoid collision with the Banner Renderable type.
// Nil-safe.
func (l *Logger) BannerLines(lines ...string) {
	if l == nil {
		for _, line := range lines {
			_, _ = fmt.Fprintln(os.Stdout, line)
		}
		return
	}

	// Same admission gate as Render/RenderRaw/Newline: the console branch
	// writes directly to consoleWriter.out under its mutex, bypassing
	// ConsoleWriter's closed check.
	if l.writers.isClosing() {
		return
	}

	var out io.Writer
	switch {
	case l.consoleWriter != nil && l.consoleWriter.out != nil:
		// Admission so Close drains this write cycle (see Render). The lines
		// contain no user callbacks, but the drain contract is uniform across
		// every direct console write.
		_, ok := l.consoleWriter.admit()
		if !ok {
			return
		}
		defer l.consoleWriter.inFlight.Done()
		l.consoleWriter.mu.Lock()
		defer l.consoleWriter.mu.Unlock()
		out = l.consoleWriter.out
	default:
		// No console writer: the destination (cfg.ConsoleOutput or os.Stdout)
		// has no writer to admit through, so use the family-level output
		// admission like the Notify paths — same Close-drain contract.
		if !l.writers.admitOutput() {
			return
		}
		defer l.writers.doneOutput()
		if l.cfg != nil && l.cfg.ConsoleOutput != nil {
			out = l.cfg.ConsoleOutput
		} else {
			out = os.Stdout
		}
	}

	for _, line := range lines {
		_, _ = fmt.Fprintln(out, line)
	}
}

// Detailed returns a child logger that forces every log call to render fields
// in tree format, regardless of the logger's FieldDisplayMode setting.
// The child shares writers, config, sampler, and pool with the parent.
// One alloc at the call site; zero extra cost per log call after that.
func (l *Logger) Detailed() *Logger {
	if l == nil {
		return nil
	}
	child := &Logger{
		cfg:              l.cfg,
		consoleWriter:    l.consoleWriter,
		jsonWriter:       l.jsonWriter,
		sampler:          l.sampler,
		writers:          l.writers, // shared pointer — parent topology changes propagate
		themes:           l.themes,  // shared pointer — runtime theme swaps reach the family
		forceTreeDisplay: true,
	}
	child.level.Store(l.level.Load())
	// scanSecure and themes live on the shared writerSet/themeState — no copy needed.
	if len(l.baseFields) > 0 {
		newBase := make([]Field, len(l.baseFields))
		copy(newBase, l.baseFields)
		child.baseFields = newBase
	}
	return child
}

// WithComponent returns a child logger that stamps every entry with a
// "component" string field. Sugar for l.With(String("component", name)).
func (l *Logger) WithComponent(name string) *Logger {
	return l.With(String("component", name))
}

// WithRequest returns a child logger that stamps every entry with a
// "request_id" string field. Sugar for l.With(String("request_id", id)).
func (l *Logger) WithRequest(id string) *Logger {
	return l.With(String("request_id", id))
}

// Render writes r to the console writer, indented to align with the message column.
// Each line after the first is prefixed with spaces equal to the template prefix width
// so the output sits flush with log messages in tree mode.
//
// When r implements StyledRenderable, RenderStyled is called with styling
// (resolved colour permission) and trust (actual terminal classification) as
// independent inputs. When r implements only TTYRenderable, RenderTTY is
// called with the console writer's resolved TTY state (which accounts for
// FORCE_COLOR / NO_COLOR and fd detection), so colour decisions match the rest
// of the log line. Types must implement TTYRenderable
// if they use IsTerminalWriter internally — calling it on the intermediate buffer
// passed by Render always yields false regardless of the actual output destination.
//
// JSON writers and MultiWriter silently ignore Render calls — indented rich output
// is only meaningful on a terminal-backed console writer.
//
// Render is nil-safe: a nil logger or nil renderable is a no-op.
func (l *Logger) Render(r Renderable) {
	// Admission gate: Render writes straight to consoleWriter.out under its
	// mutex, bypassing ConsoleWriter's own closed check, so the family-close
	// check here is what keeps post-close output off the sink.
	if l == nil || r == nil || l.writers.isClosing() || l.consoleWriter == nil {
		return
	}

	// Admission BEFORE any renderable code runs: the callback can block or
	// reenter, and Close must drain this whole cycle rather than let the write
	// land after it returned (F2). The template/isTTY snapshot rides the same
	// critical section as the admission — SetTheme replaces the template
	// concurrently, so the read must be under the writer mutex anyway.
	adm, ok := l.consoleWriter.admit()
	if !ok {
		return
	}
	defer l.consoleWriter.inFlight.Done()
	if adm.tmpl == nil {
		return
	}
	indent := adm.tmpl.CachedMessageIndentStr()
	// Renderables receive actual terminal status so secure values never become
	// visible merely because FORCE_COLOR requests styling.
	// Styling and trust are passed independently: styling follows the resolved
	// colour permission (useColours), trust follows the actual terminal
	// classification. Renderables that distinguish them (StatusItem) get both;
	// TTY-only renderables keep their legacy interface.
	styled := adm.tmpl.useColours
	trusted := adm.isTTY

	tmp := GetTemplateBuffer()
	defer PutTemplateBuffer(tmp)

	if sr, ok := r.(StyledRenderable); ok {
		if err := sr.RenderStyled(tmp, styled, trusted); err != nil {
			return
		}
	} else if tr, ok := r.(TTYRenderable); ok {
		if err := tr.RenderTTY(tmp, trusted); err != nil {
			return
		}
	} else {
		if err := r.Render(tmp); err != nil {
			return
		}
	}

	out := indentLines(tmp.Bytes(), indent)

	l.consoleWriter.mu.Lock()
	_, _ = l.consoleWriter.out.Write(out)
	l.consoleWriter.mu.Unlock()
}

// RenderRaw writes r flush-left to the console writer, with no indentation.
// Like Render, it is terminal-only and ignored by JSON/multi writers.
// When r implements TTYRenderable, the console writer's TTY state is passed
// rather than detecting it from the intermediate buffer. Nil-safe.
func (l *Logger) RenderRaw(r Renderable) {
	// Same admission gate as Render: the write bypasses ConsoleWriter's closed
	// check, so closing the family must stop it here.
	if l == nil || r == nil || l.writers.isClosing() || l.consoleWriter == nil {
		return
	}

	// Admission before the renderable callback — same drain contract as Render.
	adm, ok := l.consoleWriter.admit()
	if !ok {
		return
	}
	defer l.consoleWriter.inFlight.Done()
	styled := adm.tmpl != nil && adm.tmpl.useColours
	trusted := adm.isTTY

	tmp := GetTemplateBuffer()
	defer PutTemplateBuffer(tmp)

	if sr, ok := r.(StyledRenderable); ok {
		if err := sr.RenderStyled(tmp, styled, trusted); err != nil {
			return
		}
	} else if tr, ok := r.(TTYRenderable); ok {
		if err := tr.RenderTTY(tmp, trusted); err != nil {
			return
		}
	} else {
		if err := r.Render(tmp); err != nil {
			return
		}
	}

	l.consoleWriter.mu.Lock()
	_, _ = l.consoleWriter.out.Write(tmp.Bytes())
	l.consoleWriter.mu.Unlock()
}

// Newline writes a single newline to the console writer under the same mutex as log calls,
// preventing interleaving with concurrent log output.
// Nil-safe.
func (l *Logger) Newline() {
	// Same admission gate as Render/RenderRaw: direct out write under the
	// console mutex. Admission registers the in-flight cycle so Close drains
	// rather than races it.
	if l == nil || l.writers.isClosing() || l.consoleWriter == nil {
		return
	}

	_, ok := l.consoleWriter.admit()
	if !ok {
		return
	}
	defer l.consoleWriter.inFlight.Done()

	l.consoleWriter.mu.Lock()
	_, _ = l.consoleWriter.out.Write(newlineByte)
	l.consoleWriter.mu.Unlock()
}

// notifyMu is the fallback mutex for Notify calls on loggers that have no console
// writer. It prevents interleaving across loggers that share os.Stderr as their
// notify destination but have no common mutex.
var notifyMu sync.Mutex

// notifyDest returns the writer and mutex to use for Notify output.
// When a console writer is present it shares that writer's mutex so Notify and
// log lines on a shared terminal cannot interleave. Otherwise the package-level
// fallback is used with os.Stderr (or the configured override).
func (l *Logger) notifyDest() (io.Writer, *sync.Mutex) {
	if l.consoleWriter != nil {
		// Share the console writer's mutex regardless of the notify output
		// destination — this is the primary non-interleave guarantee.
		out := l.cfg.NotifyOutput
		if out == nil {
			out = os.Stderr
		}
		return out, &l.consoleWriter.mu
	}
	out := l.cfg.NotifyOutput
	if out == nil {
		out = os.Stderr
	}
	return out, &notifyMu
}

// Notify writes a formatted message directly to the notify destination (default
// os.Stderr), bypassing all writers, the level filter, the sampler, and the
// structured pipeline. Intended for ephemeral operator-visible output such as
// setup URLs and one-time bootstrap messages that must appear regardless of log
// level or writer configuration.
//
// Uses the console writer mutex when present to prevent interleaving with
// concurrent log output on shared terminals. Nil-safe.
//
//nolint:goprintffuncname // Notify is an intentional API name, not a generic printf wrapper.
func (l *Logger) Notify(format string, args ...any) {
	if l == nil {
		return
	}
	// Family-level admission: Sprintf can block in a caller's Stringer after
	// this point, and the destination (NotifyOutput) has no console writer to
	// admit through. Level/sampler bypass and destination behaviour are
	// unchanged — admission is purely the Close-drain contract.
	if !l.writers.admitOutput() {
		return
	}
	defer l.writers.doneOutput()
	out, mu := l.notifyDest()
	msg := fmt.Sprintf(format, args...)
	mu.Lock()
	_, _ = io.WriteString(out, msg)
	mu.Unlock()
}

// NotifyLines writes each line to the notify destination separated by newlines.
// Behaves identically to Notify with respect to writer bypass and mutex sharing.
// Nil-safe.
func (l *Logger) NotifyLines(lines ...string) {
	if l == nil || len(lines) == 0 {
		return
	}
	if !l.writers.admitOutput() {
		return
	}
	defer l.writers.doneOutput()
	out, mu := l.notifyDest()
	mu.Lock()
	for _, line := range lines {
		_, _ = io.WriteString(out, line)
		_, _ = io.WriteString(out, "\n")
	}
	mu.Unlock()
}

// NotifyBox renders a Box to the notify destination. Useful for visually-prominent
// operator messages — the canonical use case is an onboarding URL that must stand
// out regardless of whether structured logging is active.
// Nil-safe; a nil Box is a no-op.
func (l *Logger) NotifyBox(b *Box) {
	if l == nil || b == nil {
		return
	}
	if !l.writers.admitOutput() {
		return
	}
	defer l.writers.doneOutput()
	out, mu := l.notifyDest()
	tmp := GetTemplateBuffer()
	if err := b.Render(tmp); err != nil {
		PutTemplateBuffer(tmp)
		return
	}
	mu.Lock()
	_, _ = out.Write(tmp.Bytes())
	mu.Unlock()
	PutTemplateBuffer(tmp)
}

// Box renders a bordered box with an optional title to the console writer,
// indented to align with the message column. Uses the logger's active theme.
// Nil-safe; no-op when there is no console writer.
func (l *Logger) Box(title, body string) {
	if l == nil || l.writers.isClosing() || l.consoleWriter == nil {
		return
	}
	l.Render(NewBox(title, body, l.Style()))
}

// Table renders an aligned table with auto-sized columns to the console writer,
// indented to align with the message column. Uses the logger's active theme.
// Nil-safe; no-op when there is no console writer.
func (l *Logger) Table(headers []string, rows [][]string) {
	if l == nil || l.writers.isClosing() || l.consoleWriter == nil {
		return
	}
	l.Render(NewTable(headers, rows, l.Style()))
}

// Tree renders a hierarchical tree of TreeItem nodes to the console writer,
// indented to align with the message column. Uses the logger's active theme.
// Nil-safe; no-op when there is no console writer.
func (l *Logger) Tree(items []TreeItem) {
	if l == nil || l.writers.isClosing() || l.consoleWriter == nil {
		return
	}
	l.Render(NewTree(items, l.Style()))
}

// KeyValues renders a sequence of key-value pairs to the console writer,
// indented to align with the message column. Uses the logger's active theme.
// Nil-safe; no-op when there is no console writer or pairs is empty.
func (l *Logger) KeyValues(pairs []KeyValuePair) {
	if l == nil || l.writers.isClosing() || l.consoleWriter == nil || len(pairs) == 0 {
		return
	}
	// Console admission BEFORE any preparation: Style() below can park on the
	// theme lock (and be preempted generally) after this check, and Close must
	// drain the admitted cycle rather than let the write land after it
	// returned. The template snapshot rides the admission.
	adm, ok := l.consoleWriter.admit()
	if !ok {
		return
	}
	defer l.consoleWriter.inFlight.Done()
	if adm.tmpl == nil {
		return
	}
	// Render each pair under the same indent; they read as a continuation block.
	theme := l.Style()
	indent := adm.tmpl.CachedMessageIndentStr()
	tmp := GetTemplateBuffer()
	defer PutTemplateBuffer(tmp)
	for _, p := range pairs {
		kv := NewKeyValue(p.Key, p.Value, theme)
		if err := kv.Render(tmp); err != nil {
			return
		}
	}
	out := indentLines(tmp.Bytes(), indent)
	l.consoleWriter.mu.Lock()
	_, _ = l.consoleWriter.out.Write(out)
	l.consoleWriter.mu.Unlock()
}

// SystemInfo renders a titled block of key-value system metadata to the console
// writer, indented to align with the message column. Uses the logger's active theme.
// Nil-safe; no-op when there is no console writer or info is nil.
func (l *Logger) SystemInfo(info *SystemInfoData) {
	if l == nil || l.writers.isClosing() || l.consoleWriter == nil || info == nil {
		return
	}
	l.Render(NewSystemInfo(info, l.Style()))
}

// Bullet renders an indented bullet point at the given nesting level to the
// console writer, aligned with the message column. Uses the logger's active theme.
// Bullets cycle through •, ◦, ▪, ▫ with depth. Nil-safe; no-op without a console writer.
func (l *Logger) Bullet(level int, text string) {
	if l == nil || l.writers.isClosing() || l.consoleWriter == nil {
		return
	}
	// Console admission BEFORE Style(): the theme read can park on the theme
	// lock after the family check, and Close must drain this cycle (the
	// Terra confirmation reproduced post-Close output through exactly that
	// window). Balanced on every return path via the deferred Done.
	adm, ok := l.consoleWriter.admit()
	if !ok {
		return
	}
	defer l.consoleWriter.inFlight.Done()
	theme := l.Style()
	if level < 0 {
		level = 0 // clamp: negative nesting must not reach strings.Repeat
	}
	indent := strings.Repeat("  ", level)
	bullets := []string{"•", "◦", "▪", "▫"}
	bullet := bullets[level%len(bullets)]

	tmp := GetTemplateBuffer()
	defer PutTemplateBuffer(tmp)

	tmp.WriteString(indent)
	tmp.WriteString(theme.CachedFieldKeyFg())
	tmp.WriteString(bullet)
	tmp.WriteString(Reset)
	tmp.WriteString(" ")
	tmp.WriteString(theme.CachedMessageFg())
	tmp.WriteString(text)
	tmp.WriteString(Reset)
	tmp.WriteString("\n")

	if adm.tmpl == nil {
		return
	}
	out := indentLines(tmp.Bytes(), adm.tmpl.CachedMessageIndentStr())

	l.consoleWriter.mu.Lock()
	_, _ = l.consoleWriter.out.Write(out)
	l.consoleWriter.mu.Unlock()
}

// indentLines prefixes every non-empty line in b with indent.
func indentLines(b []byte, indent string) []byte {
	if len(b) == 0 || indent == "" {
		return b
	}

	nlCount := 0
	for _, c := range b {
		if c == '\n' {
			nlCount++
		}
	}

	out := make([]byte, 0, len(b)+(nlCount+1)*len(indent))
	out = append(out, indent...)
	start := 0

	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i+1]...)
			start = i + 1
			if start < len(b) {
				out = append(out, indent...)
			}
		}
	}

	if start < len(b) {
		out = append(out, b[start:]...)
	}

	return out
}

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

type Logger struct {
	sampler Sampler

	cfg           *config
	bufPool       *BufferPool
	consoleWriter *ConsoleWriter
	jsonWriter    *JSONWriter

	// Additional writers added post-initialisation for dynamic log routing
	additionalWriters *MultiWriter

	// baseFields are prepended to every log entry on this logger.
	// Set by With() and inherited by child loggers.
	baseFields []Field

	// forceTreeDisplay makes every log call on this logger render fields as a tree,
	// regardless of FieldDisplayMode. Set via Detailed().
	forceTreeDisplay bool

	// scanSecure is true when at least one output path would redact secure data,
	// i.e. any untrusted additional writer or a non-TTY console writer.
	// Recomputed on AddWriter/RemoveWriter. When false, the IndexByte('<') scan
	// is skipped entirely — dev sessions with only a TTY console pay zero scan cost.
	scanSecure atomic.Bool

	// secureScanEnabled is the user-facing gate. False when WithSecureTags(false) was
	// applied; in that case scanSecure stays false regardless of the writer mix.
	secureScanEnabled atomic.Bool

	writersMu sync.RWMutex
	level     atomic.Int32
	closed    atomic.Bool
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
		bufPool: NewBufferPool(),
		sampler: cfg.Sampler,
	}
	// Default: secure tag scanning is enabled unless explicitly disabled.
	logger.secureScanEnabled.Store(!cfg.DisableSecureTags)

	// Use the most permissive level so logs aren't dropped when outputs have
	// different thresholds.
	effectiveLevel := min(cfg.StructuredLevel, cfg.ConsoleLevel)
	logger.level.Store(int32(effectiveLevel))

	if cfg.ConsoleOutput != nil && cfg.ConsoleOutput != io.Discard {
		logger.consoleWriter = NewConsoleWriterWithOptions(cfg.ConsoleOutput, cfg.ConsoleTheme, cfg.DisplayTimezone, cfg.FieldDisplayMode)
		// Recompute cached prefix widths after applying a custom TimeFormat so
		// Logger.Render's indent matches the actual rendered timestamp width.
		if cfg.TimeFormat != "" && logger.consoleWriter != nil {
			logger.consoleWriter.template.timeFormat = cfg.TimeFormat
			logger.consoleWriter.template.initCache()
		}
	}

	if cfg.StructuredOutput != nil && cfg.StructuredOutput != io.Discard {
		logger.jsonWriter = NewJSONWriter(cfg.StructuredOutput)
	}

	// Compute initial scan flag based on writer mix at construction time.
	logger.recomputeScanSecure()

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

// With returns a child logger that prepends the given fields to every log entry.
// The child shares writers, config, and sampler with the parent.
// Level is snapshotted at the time of the call; dynamic parent level changes
// do not propagate to the child after creation.
func (l *Logger) With(fields ...Field) *Logger {
	if l == nil || len(fields) == 0 {
		return l
	}
	// additionalWriters is shared by reference. AddWriter on the child after
	// creation diverges from the parent because writersMu is not shared.
	child := &Logger{
		cfg:               l.cfg,
		bufPool:           l.bufPool,
		consoleWriter:     l.consoleWriter,
		jsonWriter:        l.jsonWriter,
		sampler:           l.sampler,
		additionalWriters: l.additionalWriters,
		forceTreeDisplay:  l.forceTreeDisplay,
	}
	child.level.Store(l.level.Load())
	child.secureScanEnabled.Store(l.secureScanEnabled.Load())
	child.scanSecure.Store(l.scanSecure.Load())
	newBase := make([]Field, len(l.baseFields)+len(fields))
	copy(newBase, l.baseFields)
	copy(newBase[len(l.baseFields):], fields)
	child.baseFields = newBase
	return child
}

// recomputeScanSecure recalculates whether the <secure> tag scan must run on
// every log call. Called at AddWriter/RemoveWriter time. The scan fires when:
//   - scan is globally enabled (secureScanEnabled), AND
//   - at least one output path is untrusted:
//     a) the JSON writer is always untrusted, OR
//     b) the console writer is on a non-TTY (pipe/file), OR
//     c) any additional writer registered without WriterTrusted()
//
// Must be called with writersMu held (write lock) or before the logger is shared.
func (l *Logger) recomputeScanSecure() {
	if !l.secureScanEnabled.Load() {
		l.scanSecure.Store(false)
		return
	}

	// JSON writer is always untrusted.
	if l.jsonWriter != nil {
		l.scanSecure.Store(true)
		return
	}

	// Non-TTY console writer is untrusted (writing to a pipe or file).
	if l.consoleWriter != nil && !l.consoleWriter.isTTY {
		l.scanSecure.Store(true)
		return
	}

	// Any untrusted additional writer flips the flag.
	// We hold l.writersMu (write lock) here; mw.mu is separate, so take it briefly.
	if l.additionalWriters != nil {
		l.additionalWriters.mu.Lock()
		hasUntrusted := false
		for _, ws := range l.additionalWriters.workers {
			if !ws.isTrusted {
				hasUntrusted = true
				break
			}
		}
		l.additionalWriters.mu.Unlock()
		if hasUntrusted {
			l.scanSecure.Store(true)
			return
		}
	}

	l.scanSecure.Store(false)
}

// AddWriter registers a named writer to receive log entries.
// Options control per-writer behaviour; see WriterTrusted.
// Thread-safe; writers process entries asynchronously via MultiWriter.
func (l *Logger) AddWriter(name string, w Writer, opts ...WriterOption) {
	if l == nil {
		return
	}

	l.writersMu.Lock()
	defer l.writersMu.Unlock()

	if l.additionalWriters == nil {
		l.additionalWriters = NewMultiWriter()
	}
	l.additionalWriters.AddWriter(name, w, opts...)
	l.recomputeScanSecure()
}

// RemoveWriter removes the named writer and returns it so the caller can
// flush or close it as appropriate. Returns nil if no writer with that name exists.
// Thread-safe.
func (l *Logger) RemoveWriter(name string) Writer {
	if l == nil {
		return nil
	}

	l.writersMu.Lock()
	defer l.writersMu.Unlock()

	if l.additionalWriters == nil {
		return nil
	}
	w := l.additionalWriters.RemoveWriter(name)
	l.recomputeScanSecure()
	return w
}

// Writer returns the writer registered under name, or nil.
// Useful for inspecting writer capabilities without removing it.
// Thread-safe.
func (l *Logger) Writer(name string) Writer {
	if l == nil {
		return nil
	}

	l.writersMu.RLock()
	defer l.writersMu.RUnlock()

	if l.additionalWriters == nil {
		return nil
	}
	return l.additionalWriters.WriterByName(name)
}

// Close flushes and shuts down all writers owned by the logger.
//
// Specifically: the console writer is flushed (its output buffer drained), the
// JSON writer is flushed, and all named writers added via AddWriter are drained
// and closed. Caller-supplied io.Writers passed via WithConsoleOutput /
// WithStructuredOutput are NOT closed — the logger does not own those handles.
//
// Close is idempotent: subsequent calls are no-ops. After Close returns, any
// further log calls on this logger drop silently.
//
// Returns the first error encountered; remaining flushes still proceed.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	// Already closed — nothing to do.
	if !l.closed.CompareAndSwap(false, true) {
		return nil
	}

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

	l.writersMu.Lock()
	defer l.writersMu.Unlock()

	if l.additionalWriters != nil {
		setErr(l.additionalWriters.Close())
	}

	return firstErr
}

func (l *Logger) Debug(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[!DBG] %s\n", msg)
		return
	}
	if l.closed.Load() || !l.isEnabled(LevelDebug) {
		return
	}
	l.log(LevelDebug, msg, fields...)
}

func (l *Logger) Info(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[INFO] %s\n", msg)
		return
	}
	if l.closed.Load() || !l.isEnabled(LevelInfo) {
		return
	}
	l.log(LevelInfo, msg, fields...)
}

func (l *Logger) Warn(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[WARN] %s\n", msg)
		return
	}
	if l.closed.Load() || !l.isEnabled(LevelWarn) {
		return
	}
	l.log(LevelWarn, msg, fields...)
}

func (l *Logger) Error(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[ERR!] %s\n", msg)
		return
	}
	if l.closed.Load() || !l.isEnabled(LevelError) {
		return
	}
	l.log(LevelError, msg, fields...)
}

func (l *Logger) Fatal(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[FATL] %s\n", msg)
		os.Exit(1)
	}
	l.log(LevelFatal, msg, fields...)
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
	l.logInternal(level, msg, l.forceTreeDisplay, fields...)
}

// LogEntry dispatches a pre-populated entry to all configured writers.
// Used by slog bridge and other external adapters.
func (l *Logger) LogEntry(e *Entry) {
	if l == nil {
		return
	}
	// Prepend base fields from With() so child loggers propagate their fields.
	if len(l.baseFields) > 0 {
		existing := e.Fields
		e.Fields = e.Fields[:0]
		e.WithFields(l.baseFields...)
		e.WithFields(existing...)
	}
	if l.cfg != nil {
		if e.Level >= l.cfg.ConsoleLevel && l.consoleWriter != nil {
			_ = l.consoleWriter.Write(e)
		}
		if e.Level >= l.cfg.StructuredLevel && l.jsonWriter != nil {
			_ = l.jsonWriter.Write(e)
		}
		e.Write()
		l.writersMu.RLock()
		if l.additionalWriters != nil {
			_ = l.additionalWriters.Write(e)
		}
		l.writersMu.RUnlock()
		return
	}
	e.Write()
}

// logInternal is the shared implementation for log and logDetailed.
func (l *Logger) logInternal(level Level, msg string, forceTree bool, fields ...Field) {
	if l == nil {
		return
	}

	if l.sampler != nil && !l.sampler.Sample(level, msg) {
		return
	}

	entry := GetEntry()
	defer entry.Release()

	entry.SetLevel(level)
	entry.SetMessage(msg)
	entry.SetTime(time.Now())
	entry.forceTreeDisplay = forceTree

	// When any output path is untrusted, check whether the message contains a
	// <secure> tag. strings.IndexByte is SIMD-accelerated in the Go runtime (~3-5ns),
	// zero-alloc on string input. The flag is read without a lock — worst case a
	// concurrent AddWriter races and we miss one log line; acceptable for a
	// best-effort security feature.
	if l.scanSecure.Load() && strings.IndexByte(msg, '<') >= 0 {
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

		l.writersMu.RLock()
		if l.additionalWriters != nil {
			_ = l.additionalWriters.Write(entry)
		}
		l.writersMu.RUnlock()
		return
	}

	entry.Write()
}

// Theme returns the console theme configured for this logger.
// Falls back to ThemeNightOwl when nil or unconfigured.
func (l *Logger) Theme() *Theme {
	if l == nil || l.cfg == nil || l.cfg.ConsoleTheme == nil {
		return ThemeNightOwl
	}
	return l.cfg.ConsoleTheme
}

// SetTheme updates the active theme on all writers that support it.
// Updates cfg.ConsoleTheme so subsequent With() clones inherit the new theme.
// Nil theme is treated as explicit colour-disable; writers receive nil and handle it themselves.
// User-defined themes are cached automatically: if the theme's ANSI sequences are not yet populated
// they are computed in-place, so the caller's original pointer is not mutated. Nil-safe.
func (l *Logger) SetTheme(theme *Theme) {
	if l == nil {
		return
	}

	// Themes are immutable from construction — ANSI codes already populated.

	if l.cfg != nil {
		l.cfg.ConsoleTheme = theme
	}

	if s, ok := any(l.consoleWriter).(ThemedWriter); ok && l.consoleWriter != nil {
		s.SetTheme(theme)
	}

	l.writersMu.RLock()
	defer l.writersMu.RUnlock()

	if l.additionalWriters == nil {
		return
	}

	l.additionalWriters.mu.Lock()
	defer l.additionalWriters.mu.Unlock()

	for _, ws := range l.additionalWriters.workers {
		if s, ok := ws.w.(ThemedWriter); ok {
			s.SetTheme(theme)
		}
	}
}

// Style returns the active theme for use in manual ANSI formatting.
// When the logger has no console writer (JSON-only or nop), it returns
// a no-colour theme so callers can always call Style() without a nil check.
func (l *Logger) Style() *Theme {
	if l == nil {
		return noColourTheme
	}
	if l.cfg != nil && l.cfg.ConsoleTheme != nil && !l.cfg.DisableColour {
		return l.cfg.ConsoleTheme
	}
	return noColourTheme
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

	var out io.Writer
	switch {
	case l.consoleWriter != nil && l.consoleWriter.out != nil:
		l.consoleWriter.mu.Lock()
		defer l.consoleWriter.mu.Unlock()
		out = l.consoleWriter.out
	case l.cfg != nil && l.cfg.ConsoleOutput != nil:
		out = l.cfg.ConsoleOutput
	default:
		out = os.Stdout
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
		cfg:               l.cfg,
		bufPool:           l.bufPool,
		consoleWriter:     l.consoleWriter,
		jsonWriter:        l.jsonWriter,
		sampler:           l.sampler,
		additionalWriters: l.additionalWriters,
		forceTreeDisplay:  true,
	}
	child.level.Store(l.level.Load())
	child.secureScanEnabled.Store(l.secureScanEnabled.Load())
	child.scanSecure.Store(l.scanSecure.Load())
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
// JSON writers and MultiWriter silently ignore Render calls — indented rich output
// is only meaningful on a terminal-backed console writer.
//
// Render is nil-safe: a nil logger or nil renderable is a no-op.
func (l *Logger) Render(r Renderable) {
	if l == nil || r == nil || l.consoleWriter == nil {
		return
	}

	indent := l.consoleWriter.template.CachedMessageIndentStr()

	tmp := GetTemplateBuffer()
	defer PutTemplateBuffer(tmp)

	if err := r.Render(tmp); err != nil {
		return
	}

	out := indentLines(tmp.Bytes(), indent)

	l.consoleWriter.mu.Lock()
	_, _ = l.consoleWriter.out.Write(out)
	l.consoleWriter.mu.Unlock()
}

// RenderRaw writes r flush-left to the console writer, with no indentation.
// Like Render, it is terminal-only and ignored by JSON/multi writers.
// Nil-safe.
func (l *Logger) RenderRaw(r Renderable) {
	if l == nil || r == nil || l.consoleWriter == nil {
		return
	}

	tmp := GetTemplateBuffer()
	defer PutTemplateBuffer(tmp)

	if err := r.Render(tmp); err != nil {
		return
	}

	l.consoleWriter.mu.Lock()
	_, _ = l.consoleWriter.out.Write(tmp.Bytes())
	l.consoleWriter.mu.Unlock()
}

// Newline writes a single newline to the console writer under the same mutex as log calls,
// preventing interleaving with concurrent log output.
// Nil-safe.
func (l *Logger) Newline() {
	if l == nil || l.consoleWriter == nil {
		return
	}

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
	if l == nil || l.closed.Load() {
		return
	}
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
	if l == nil || l.closed.Load() || len(lines) == 0 {
		return
	}
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
	if l == nil || l.closed.Load() || b == nil {
		return
	}
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
	if l == nil || l.closed.Load() || l.consoleWriter == nil {
		return
	}
	l.Render(NewBox(title, body, l.Style()))
}

// Table renders an aligned table with auto-sized columns to the console writer,
// indented to align with the message column. Uses the logger's active theme.
// Nil-safe; no-op when there is no console writer.
func (l *Logger) Table(headers []string, rows [][]string) {
	if l == nil || l.closed.Load() || l.consoleWriter == nil {
		return
	}
	l.Render(NewTable(headers, rows, l.Style()))
}

// Tree renders a hierarchical tree of TreeItem nodes to the console writer,
// indented to align with the message column. Uses the logger's active theme.
// Nil-safe; no-op when there is no console writer.
func (l *Logger) Tree(items []TreeItem) {
	if l == nil || l.closed.Load() || l.consoleWriter == nil {
		return
	}
	l.Render(NewTree(items, l.Style()))
}

// KeyValues renders a sequence of key-value pairs to the console writer,
// indented to align with the message column. Uses the logger's active theme.
// Nil-safe; no-op when there is no console writer or pairs is empty.
func (l *Logger) KeyValues(pairs []KeyValuePair) {
	if l == nil || l.closed.Load() || l.consoleWriter == nil || len(pairs) == 0 {
		return
	}
	// Render each pair under the same indent; they read as a continuation block.
	theme := l.Style()
	indent := l.consoleWriter.template.CachedMessageIndentStr()
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
	if l == nil || l.closed.Load() || l.consoleWriter == nil || info == nil {
		return
	}
	l.Render(NewSystemInfo(info, l.Style()))
}

// Bullet renders an indented bullet point at the given nesting level to the
// console writer, aligned with the message column. Uses the logger's active theme.
// Bullets cycle through •, ◦, ▪, ▫ with depth. Nil-safe; no-op without a console writer.
func (l *Logger) Bullet(level int, text string) {
	if l == nil || l.closed.Load() || l.consoleWriter == nil {
		return
	}
	theme := l.Style()
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

	msgIndent := l.consoleWriter.template.CachedMessageIndentStr()
	out := indentLines(tmp.Bytes(), msgIndent)

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

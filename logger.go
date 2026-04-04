package velocity

import (
	"bytes"
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

	cfg             *Config
	bufPool         *BufferPool
	consoleWriter   *ConsoleWriter
	jsonWriter      *JSONWriter
	pretty          *Pretty
	statusFormatter *StatusFormatter

	// Additional writers added post-initialisation for dynamic log routing
	additionalWriters *MultiWriter

	// baseFields are prepended to every log entry on this logger.
	// Set by With() and inherited by child loggers.
	baseFields []Field

	prettyMu  sync.RWMutex
	writersMu sync.RWMutex
	level     atomic.Int32
}

func New(w io.Writer) *Logger {
	cfg := DefaultConfig()
	cfg.ConsoleOutput = w
	return NewWithConfig(cfg)
}

func NewWithConfig(cfg *Config) *Logger {
	// Respect the config's intention - nil output means disabled
	logger := &Logger{
		cfg:     cfg,
		bufPool: NewBufferPool(),
		sampler: cfg.Sampler,
	}

	// Using the most permissive level ensures logs aren't dropped when outputs have different thresholds
	effectiveLevel := min(cfg.StructuredLevel, cfg.ConsoleLevel)
	logger.level.Store(int32(effectiveLevel))

	// Initialise console writer if configured
	if cfg.ConsoleOutput != nil && cfg.ConsoleOutput != io.Discard {
		logger.consoleWriter = NewConsoleWriterWithOptions(cfg.ConsoleOutput, cfg.ConsoleTheme, cfg.DisplayTimezone, cfg.FieldDisplayMode)
		// Apply time format if specified
		if cfg.TimeFormat != "" && logger.consoleWriter != nil {
			logger.consoleWriter.template.timeFormat = cfg.TimeFormat
		}
		// Status formatter respects terminal capability and colours
		isTTY := logger.consoleWriter != nil && logger.consoleWriter.IsTTY()
		theme := cfg.ConsoleTheme
		if cfg.DisableColour {
			theme = nil
		}
		logger.statusFormatter = NewStatusFormatter(theme, isTTY)
	}

	// Initialise JSON writer if configured
	if cfg.StructuredOutput != nil && cfg.StructuredOutput != io.Discard {
		logger.jsonWriter = NewJSONWriter(cfg.StructuredOutput)
	}

	// Ensure status formatter exists even without console writer
	if logger.statusFormatter == nil {
		logger.statusFormatter = NewStatusFormatter(nil, false)
	}

	return logger
}

func NewWithBuilder(builder *Builder) *Logger {
	cfg := builder.MustBuild()
	return NewWithConfig(cfg)
}

func NewWithOptions(opts ...Option) *Logger {
	builder := NewConfig()
	for _, opt := range opts {
		opt(builder)
	}
	return NewWithBuilder(builder)
}

// NewDevelopment creates a logger optimised for development with colourful console output.
func NewDevelopment() *Logger {
	builder := PresetDevelopment()
	cfg := builder.MustBuild()
	return NewWithConfig(cfg)
}

func NewForTesting(w io.Writer) *Logger {
	builder := PresetTesting(w)
	cfg := builder.MustBuild()
	return NewWithConfig(cfg)
}

func (l *Logger) SetLevel(level Level) {
	l.level.Store(int32(level))
}

func (l *Logger) Level() Level {
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
		pretty:            l.pretty,
		statusFormatter:   l.statusFormatter,
		sampler:           l.sampler,
		additionalWriters: l.additionalWriters,
	}
	child.level.Store(l.level.Load())
	newBase := make([]Field, len(l.baseFields)+len(fields))
	copy(newBase, l.baseFields)
	copy(newBase[len(l.baseFields):], fields)
	child.baseFields = newBase
	return child
}

// AddWriter adds a named writer to receive log entries.
// Thread-safe for concurrent calls.
// Writers process entries asynchronously via MultiWriter.
func (l *Logger) AddWriter(name string, w Writer) {
	if l == nil {
		return
	}

	l.writersMu.Lock()
	defer l.writersMu.Unlock()

	if l.additionalWriters == nil {
		l.additionalWriters = NewMultiWriter()
	}
	l.additionalWriters.AddWriter(name, w)
}

// RemoveWriter removes a named writer.
// Thread-safe for concurrent calls.
func (l *Logger) RemoveWriter(name string) {
	if l == nil {
		return
	}

	l.writersMu.Lock()
	defer l.writersMu.Unlock()

	if l.additionalWriters != nil {
		l.additionalWriters.RemoveWriter(name)
	}
}

// Close closes any additional writers that were added to the logger.
// Thread-safe and nil-safe - returns nil if logger is nil or has no additional writers.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}

	l.writersMu.Lock()
	defer l.writersMu.Unlock()

	if l.additionalWriters != nil {
		return l.additionalWriters.Close()
	}

	return nil
}

func (l *Logger) Debug(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[!DBG] %s\n", msg)
		return
	}
	if !l.isEnabled(LevelDebug) {
		return
	}
	l.log(LevelDebug, msg, fields...)
}

func (l *Logger) Info(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[INFO] %s\n", msg)
		return
	}
	if !l.isEnabled(LevelInfo) {
		return
	}
	l.log(LevelInfo, msg, fields...)
}

func (l *Logger) Warn(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[WARN] %s\n", msg)
		return
	}
	if !l.isEnabled(LevelWarn) {
		return
	}
	l.log(LevelWarn, msg, fields...)
}

func (l *Logger) Error(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[ERR!] %s\n", msg)
		return
	}
	if !l.isEnabled(LevelError) {
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

// DebugDetailed logs a debug message with fields always displayed in tree format
func (l *Logger) DebugDetailed(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[DEBU] %s\n", msg)
		return
	}
	if !l.isEnabled(LevelDebug) {
		return
	}
	l.logDetailed(LevelDebug, msg, fields...)
}

// InfoDetailed logs an info message with fields always displayed in tree format
func (l *Logger) InfoDetailed(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[INFO] %s\n", msg)
		return
	}
	if !l.isEnabled(LevelInfo) {
		return
	}
	l.logDetailed(LevelInfo, msg, fields...)
}

// WarnDetailed logs a warning message with fields always displayed in tree format
func (l *Logger) WarnDetailed(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[WARN] %s\n", msg)
		return
	}
	if !l.isEnabled(LevelWarn) {
		return
	}
	l.logDetailed(LevelWarn, msg, fields...)
}

// ErrorDetailed logs an error message with fields always displayed in tree format
func (l *Logger) ErrorDetailed(msg string, fields ...Field) {
	if l == nil {
		fmt.Fprintf(os.Stderr, "[ERR!] %s\n", msg)
		return
	}
	if !l.isEnabled(LevelError) {
		return
	}
	l.logDetailed(LevelError, msg, fields...)
}

func (l *Logger) isEnabled(level Level) bool {
	return level >= Level(l.level.Load())
}

// captureCaller populates entry with caller information if configured.
// extraSkip allows callers to account for additional frames in the call stack.
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

	// Extract just the filename from full path
	// Use bit shift to find last separator for performance
	shortFile := file
	for i := len(file) - 1; i >= 0; i-- {
		if file[i] == '/' || file[i] == '\\' {
			shortFile = file[i+1:]
			break
		}
	}

	entry.Caller = shortFile
	entry.Line = line

	// Get function name if available
	if fn := runtime.FuncForPC(pc); fn != nil {
		entry.Function = fn.Name()
	}
}

func (l *Logger) log(level Level, msg string, fields ...Field) {
	l.logInternal(level, msg, false, fields...)
}

// logDetailed logs a message with fields forced to display in tree format.
func (l *Logger) logDetailed(level Level, msg string, fields ...Field) {
	l.logInternal(level, msg, true, fields...)
}

// logEntry dispatches a pre-populated entry to all configured writers.
// Used by SlogHandler to avoid duplicating writer dispatch logic.
func (l *Logger) logEntry(e *Entry) {
	if l == nil {
		return
	}
	// Prepend base fields from With() so child loggers propagate their fields.
	if len(l.baseFields) > 0 {
		combined := make([]Field, 0, len(l.baseFields)+len(e.Fields))
		combined = append(combined, l.baseFields...)
		combined = append(combined, e.Fields...)
		e.Fields = combined
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
// forceTree controls whether the entry's forceTreeDisplay flag is set.
func (l *Logger) logInternal(level Level, msg string, forceTree bool, fields ...Field) {
	if l == nil {
		return
	}

	// Early sampling check to avoid allocation when entry will be dropped
	if l.sampler != nil && !l.sampler.Sample(level, msg) {
		return
	}

	// Pool reduces GC pressure in high-throughput scenarios by reusing Entry objects
	entry := GetEntry()
	defer entry.Release()

	entry.SetLevel(level)
	entry.SetMessage(msg)
	entry.SetTime(time.Now())
	entry.forceTreeDisplay = forceTree
	if len(l.baseFields) > 0 {
		entry.WithFields(l.baseFields...)
	}
	if len(fields) > 0 {
		entry.WithFields(fields...)
	}

	// Capture caller information if enabled (no extra skip needed beyond the 4 already counted)
	l.captureCaller(entry, 0)

	if l.cfg != nil {
		// Synchronous writers (console and JSON)
		if level >= l.cfg.ConsoleLevel && l.consoleWriter != nil {
			if err := l.consoleWriter.Write(entry); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
			}
		}

		if level >= l.cfg.StructuredLevel && l.jsonWriter != nil {
			if err := l.jsonWriter.Write(entry); err != nil { //nolint:staticcheck // Silently drop on write errors to prevent logging from blocking
			}
		}

		// Additional writers (async via MultiWriter - handles Retain internally)
		// NOTE: Must call entry.Write() BEFORE async writes to avoid race on written field
		entry.Write() // Marks entry as written (required before Release can return to pool)

		l.writersMu.RLock()
		if l.additionalWriters != nil {
			_ = l.additionalWriters.Write(entry) // Non-blocking async write
		}
		l.writersMu.RUnlock()
		return
	}

	entry.Write() // Marks entry as written (required before Release can return to pool)
}

// NewConsoleWriter creates a new console writer with the logger's theme.
func (l *Logger) NewConsoleWriter(out io.Writer) *ConsoleWriter {
	return NewConsoleWriterWithOptions(out, l.cfg.ConsoleTheme, l.cfg.DisplayTimezone, l.cfg.FieldDisplayMode)
}

func (*Logger) NewJSONWriter(out io.Writer) *JSONWriter {
	return NewJSONWriter(out)
}

func (*Logger) NewMultiWriter() *MultiWriter {
	return NewMultiWriter()
}

// Pretty returns the Pretty printer for formatted output.
// Lazy-initialised on first access to avoid allocations when not used.
func (l *Logger) Pretty() *Pretty {
	if l == nil {
		return NewPretty(os.Stdout, ThemeNightOwl)
	}

	l.prettyMu.RLock()
	if l.pretty != nil {
		l.prettyMu.RUnlock()
		return l.pretty
	}
	l.prettyMu.RUnlock()

	l.prettyMu.Lock()
	defer l.prettyMu.Unlock()

	// Double-check after acquiring write lock
	if l.pretty != nil {
		return l.pretty
	}

	var writer io.Writer = os.Stdout
	theme := ThemeNightOwl

	if l.cfg != nil {
		if l.cfg.ConsoleOutput != nil {
			writer = l.cfg.ConsoleOutput
		}
		if l.cfg.ConsoleTheme != nil {
			theme = l.cfg.ConsoleTheme
		}
	}

	l.pretty = NewPretty(writer, theme)
	return l.pretty
}

// Status returns the StatusFormatter for coloured status indicators.
// Safe to call even if logger is nil - returns a non-coloured formatter.
func (l *Logger) Status() *StatusFormatter {
	if l == nil || l.statusFormatter == nil {
		return NewStatusFormatter(nil, false)
	}
	return l.statusFormatter
}

// Raw prints a message without any formatting, timestamp, or level.
// The caller is responsible for including newlines if desired.
func (l *Logger) Raw(message string) {
	if l == nil {
		_, _ = fmt.Fprint(os.Stdout, message)
		return
	}

	switch {
	case l.consoleWriter != nil && l.consoleWriter.out != nil:
		l.consoleWriter.mu.Lock()
		_, _ = io.WriteString(l.consoleWriter.out, message)
		l.consoleWriter.mu.Unlock()
	case l.cfg != nil && l.cfg.ConsoleOutput != nil:
		_, _ = io.WriteString(l.cfg.ConsoleOutput, message)
	default:
		_, _ = fmt.Fprint(os.Stdout, message)
	}
}

// Banner prints multiple lines of text without formatting.
// Newlines are automatically added after each line.
func (l *Logger) Banner(lines ...string) {
	if l == nil {
		for _, line := range lines {
			_, _ = fmt.Fprintln(os.Stdout, line)
		}
		return
	}

	for _, line := range lines {
		l.Raw(line + "\n")
	}
}

func (l *Logger) SetTemplate(t *Template) {
	if l == nil {
		return
	}

	if l.consoleWriter != nil {
		l.consoleWriter.SetTemplate(t)
	}
}

// WithTemplate creates a child logger with a different output template.
// The consoleWriter is intentionally recreated so the new template takes effect.
func (l *Logger) WithTemplate(t *Template) *Logger {
	if l == nil {
		return nil
	}

	newLogger := &Logger{
		cfg:               l.cfg,
		bufPool:           l.bufPool,
		jsonWriter:        l.jsonWriter,
		pretty:            l.pretty,
		statusFormatter:   l.statusFormatter,
		sampler:           l.sampler,
		additionalWriters: l.additionalWriters,
	}
	newLogger.level.Store(l.level.Load())

	if len(l.baseFields) > 0 {
		newBase := make([]Field, len(l.baseFields))
		copy(newBase, l.baseFields)
		newLogger.baseFields = newBase
	}

	if l.cfg.ConsoleOutput != nil && l.cfg.ConsoleOutput != io.Discard {
		newLogger.consoleWriter = NewConsoleWriterWithOptions(l.cfg.ConsoleOutput, l.cfg.ConsoleTheme, l.cfg.DisplayTimezone, l.cfg.FieldDisplayMode)
		if t != nil {
			newLogger.consoleWriter.SetTemplate(t)
		}
	}

	return newLogger
}

// TreeItem represents a hierarchical data structure for tree display

type TreeItem struct {
	Key      string
	Value    any
	Children []TreeItem
}

// Tree displays hierarchical data in a tree structure.
// Uses box-drawing characters for visual hierarchy.
// Each line includes its own newline and no extra newlines are added.
func (l *Logger) Tree(label string, items []TreeItem) {
	// Default to enabled indentation for backward compatibility
	l.TreeWithIndent(label, items, true)
}

// TreeWithIndent displays hierarchical data with optional indentation.
// When indent is true, tree lines align with regular log message prefixes.
// Uses box-drawing characters for visual hierarchy.
// Each line includes its own newline and no extra newlines are added.
func (l *Logger) TreeWithIndent(label string, items []TreeItem, indent bool) {
	if l == nil {
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", label)

		// Calculate prefix width if indentation is enabled even for nil logger
		var prefix string
		if indent {
			// Use default template to calculate width
			template := TemplateDefault

			// Create a temporary entry to calculate prefix width
			// For nil logger, default to local timezone for backward compatibility
			entry := GetEntry()
			defer entry.Release()
			entry.SetTime(time.Now().In(time.Local))
			entry.SetLevel(LevelInfo) // Use INFO level as default for width calculation

			prefixWidth := template.CalculatePrefixWidth(entry)
			prefix = strings.Repeat(" ", prefixWidth)
		}

		for i, item := range items {
			writeTreeItemStandalone(os.Stdout, item, prefix, i == len(items)-1)
		}
		return
	}

	// Try to log the label as a proper info message with timestamp and level
	// If there's no consoleWriter (e.g., when using bytes.Buffer in tests),
	// fall back to Raw output
	if l.consoleWriter != nil {
		l.Info(label)
	} else {
		l.Raw(label + "\n")
	}

	// Calculate prefix width if indentation is enabled
	var prefix string
	if indent {
		// Try to get template from consoleWriter, fall back to default if not available
		var template *Template
		if l.consoleWriter != nil && l.consoleWriter.template != nil {
			template = l.consoleWriter.template
		} else if l.cfg != nil {
			// Create a default template if consoleWriter is nil
			template = TemplateDefault
		}

		if template != nil {
			// Create a temporary entry to calculate prefix width
			// Use display timezone to ensure consistent width calculation
			entry := GetEntry()
			defer entry.Release()

			// Determine the timezone to use for width calculation
			tz := time.Local
			if l.consoleWriter != nil && l.consoleWriter.displayTimezone != nil {
				tz = l.consoleWriter.displayTimezone
			} else if l.cfg != nil && l.cfg.DisplayTimezone != nil {
				tz = l.cfg.DisplayTimezone
			}

			entry.SetTime(time.Now().In(tz))
			entry.SetLevel(LevelInfo) // Use INFO level as default for width calculation

			prefixWidth := template.CalculatePrefixWidth(entry)
			prefix = strings.Repeat(" ", prefixWidth)
		}
	}

	for i, item := range items {
		isLast := i == len(items)-1
		l.writeTreeItem(item, prefix, isLast)
	}
}

// writeTreeItem recursively writes a tree item with proper indentation.
// Tree structure uses box-drawing characters with consistent 4-space indentation.
func (l *Logger) writeTreeItem(item TreeItem, prefix string, isLast bool) {
	connector := treeBranch
	if isLast {
		connector = treeCorner
	}

	var line string
	if item.Value != nil {
		line = fmt.Sprintf("%s%s%s: %v\n", prefix, connector, item.Key, item.Value)
	} else {
		line = fmt.Sprintf("%s%s%s\n", prefix, connector, item.Key)
	}

	l.Raw(line)

	// Indentation preserves visual hierarchy in tree structure
	childPrefix := prefix
	if isLast {
		childPrefix += treeBlank
	} else {
		childPrefix += treePipe
	}

	for i, child := range item.Children {
		childIsLast := i == len(item.Children)-1
		l.writeTreeItem(child, childPrefix, childIsLast)
	}
}

// Table displays tabular data with headers and rows.
// Uses default indentation to align with regular log messages.
func (l *Logger) Table(headers []string, rows [][]string) {
	l.TableWithIndent(headers, rows, true)
}

// TableWithIndent displays tabular data with optional indentation.
// When indent is true, table lines align with regular log message prefixes.
// Each line includes its own newline and no extra newlines are added.
func (l *Logger) TableWithIndent(headers []string, rows [][]string, indent bool) {
	if l == nil {
		// Fallback to stdout for nil logger
		pretty := NewPretty(os.Stdout, ThemeNightOwl)
		pretty.Table(headers, rows)
		return
	}

	// Calculate prefix width if indentation is enabled
	if indent {
		var prefix string
		// Try to get template from consoleWriter, fall back to default if not available
		var template *Template
		if l.consoleWriter != nil && l.consoleWriter.template != nil {
			template = l.consoleWriter.template
		} else if l.cfg != nil {
			// Create a default template if consoleWriter is nil
			template = TemplateDefault
		}

		if template != nil {
			// Create a temporary entry to calculate prefix width
			entry := GetEntry()
			defer entry.Release()
			entry.SetTime(time.Now())
			entry.SetLevel(LevelInfo) // Use INFO level as default for width calculation

			prefixWidth := template.CalculatePrefixWidth(entry) - 1
			prefix = strings.Repeat(" ", prefixWidth)

			// Capture table output and apply indentation
			buf := &bytes.Buffer{}
			pretty := NewPretty(buf, l.Pretty().theme)
			pretty.Table(headers, rows)

			// Apply indentation to each line
			output := buf.String()
			lines := strings.Split(output, "\n")
			for i, line := range lines {
				if line != "" {
					l.Raw(prefix + line)
				}
				// Add newline except for the last empty line if present
				if i < len(lines)-1 {
					l.Raw("\n")
				}
			}
			return
		}
	}

	// No indentation or couldn't calculate prefix - just use Pretty.Table directly
	l.Pretty().Table(headers, rows)
}

// Progress and Spinner methods removed - add progress.go if these features are needed

func writeTreeItemStandalone(w io.Writer, item TreeItem, prefix string, isLast bool) {
	connector := treeBranch
	if isLast {
		connector = treeCorner
	}

	if item.Value != nil {
		_, _ = fmt.Fprintf(w, "%s%s%s: %v\n", prefix, connector, item.Key, item.Value)
	} else {
		_, _ = fmt.Fprintf(w, "%s%s%s\n", prefix, connector, item.Key)
	}

	childPrefix := prefix
	if isLast {
		childPrefix += treeBlank
	} else {
		childPrefix += treePipe
	}

	for i, child := range item.Children {
		childIsLast := i == len(item.Children)-1
		writeTreeItemStandalone(w, child, childPrefix, childIsLast)
	}
}

func NopLogger() *Logger {
	cfg := DefaultConfig()
	cfg.ConsoleOutput = io.Discard
	cfg.StructuredOutput = io.Discard
	cfg.ConsoleLevel = LevelOff
	cfg.StructuredLevel = LevelOff
	return NewWithConfig(cfg)
}

func CreateBanner(title, version, url string, ascii []string) string {
	var b strings.Builder
	maxLen := 0

	for _, line := range ascii {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	if len(title)+len(version)+3 > maxLen {
		maxLen = len(title) + len(version) + 3
	}
	if len(url) > maxLen {
		maxLen = len(url)
	}

	boxWidth := maxLen + 4

	b.WriteString("╔")
	b.WriteString(strings.Repeat("═", boxWidth-2))
	b.WriteString("╗\n")

	for _, line := range ascii {
		b.WriteString("║ ")
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", maxLen-len(line)))
		b.WriteString(" ║\n")
	}

	if len(ascii) > 0 {
		b.WriteString("╠")
		b.WriteString(strings.Repeat("═", boxWidth-2))
		b.WriteString("╣\n")
	}

	titleLine := fmt.Sprintf("%s v%s", title, version)
	padding := (maxLen - len(titleLine)) / 2
	b.WriteString("║ ")
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString(titleLine)
	b.WriteString(strings.Repeat(" ", maxLen-len(titleLine)-padding))
	b.WriteString(" ║\n")

	if url != "" {
		urlPadding := (maxLen - len(url)) / 2
		b.WriteString("║ ")
		b.WriteString(strings.Repeat(" ", urlPadding))
		b.WriteString(url)
		b.WriteString(strings.Repeat(" ", maxLen-len(url)-urlPadding))
		b.WriteString(" ║\n")
	}

	b.WriteString("╚")
	b.WriteString(strings.Repeat("═", boxWidth-2))
	b.WriteString("╝\n")

	return b.String()
}

package velocity

// Permanent WP1 regression coverage: secure-tag redaction across every payload
// channel, content-driven scan publication, colour/trust separation, and
// theme-state synchronisation. Promoted from the adversarial battery in
// .verify/scratch/wp3 (trust/publication/theme rows) with the same assertions.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const wp1Secret = "TOPSECRETPASSWORD"

func wp1HasANSI(s string) bool { return strings.Contains(s, "\x1b[") }

func wp1ClearColourEnv(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
}

// Group item text and continuation lines must use the same secure-tag policy
// as headers on untrusted console output. Reported per channel so a defect in
// one does not mask the others.
func TestRedaction_PayloadTagsOnUntrustedConsole(t *testing.T) {
	wp1ClearColourEnv(t)

	cases := []struct {
		name string
		emit func(*Logger)
	}{
		{
			name: "StatusMessage",
			emit: func(l *Logger) {
				l.Status(LevelInfo, StatusOK, "status <secure>"+wp1Secret+"</secure> ok")
			},
		},
		{
			name: "GroupHeader",
			emit: func(l *Logger) {
				l.Group(LevelInfo, "group header <secure>"+wp1Secret+"</secure>", GroupItem{Text: "clean item"})
			},
		},
		{
			name: "GroupItemText",
			emit: func(l *Logger) {
				l.Group(LevelInfo, "clean header", GroupItem{Text: "group item <secure>" + wp1Secret + "</secure>"})
			},
		},
		{
			name: "ContinueHeader",
			emit: func(l *Logger) {
				l.Continue(LevelInfo, "continue header <secure>"+wp1Secret+"</secure>", "clean line")
			},
		},
		{
			name: "ContinueLine",
			emit: func(l *Logger) {
				l.Continue(LevelInfo, "clean header", "continuation line <secure>"+wp1Secret+"</secure>")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := New(WithConsoleOutput(&buf), WithLevel(LevelDebug))
			tc.emit(log)
			_ = log.Close()

			if out := buf.String(); strings.Contains(out, wp1Secret) {
				t.Errorf("secure tag plaintext leaked to untrusted console output:\n%s", out)
			}
		})
	}
}

// Same coverage for the structured channel: group items and continuation lines
// in JSON output must not carry tag plaintext either.
func TestRedaction_PayloadTagsInJSON(t *testing.T) {
	wp1ClearColourEnv(t)

	cases := []struct {
		name string
		emit func(*Logger)
	}{
		{
			name: "GroupItemText",
			emit: func(l *Logger) {
				l.Group(LevelInfo, "clean header", GroupItem{Text: "item <secure>" + wp1Secret + "</secure>"})
			},
		},
		{
			name: "ContinueLine",
			emit: func(l *Logger) {
				l.Continue(LevelInfo, "clean header", "line <secure>"+wp1Secret+"</secure>")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := New(WithConsoleOutput(ioDiscardSink{}), WithStructuredOutput(&buf), WithLevel(LevelDebug))
			tc.emit(log)
			_ = log.Close()

			if out := buf.String(); strings.Contains(out, wp1Secret) {
				t.Errorf("secure tag plaintext leaked into JSON output:\n%s", out)
			}
		})
	}
}

type ioDiscardSink struct{}

func (ioDiscardSink) Write(p []byte) (int, error) { return len(p), nil }

// OSC 8 hyperlinks in continuation lines survive redaction processing in JSON:
// the visible text is kept, the escape sequences are still stripped.
func TestRedaction_ContinueOSC8PreservedInJSON(t *testing.T) {
	wp1ClearColourEnv(t)

	var buf bytes.Buffer
	log := New(WithConsoleOutput(ioDiscardSink{}), WithStructuredOutput(&buf), WithLevel(LevelDebug))
	line := "link \x1b]8;;https://example.com\x07visible" + "<secure>" + wp1Secret + "</secure>" + "text\x1b]8;;\x07"
	log.Continue(LevelInfo, "clean header", line)
	_ = log.Close()

	out := buf.String()
	if strings.Contains(out, wp1Secret) {
		t.Errorf("secure tag plaintext leaked into JSON continuation line:\n%s", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("OSC 8 escape sequences must be stripped from JSON, got:\n%q", out)
	}
	if !strings.Contains(out, "visible") {
		t.Errorf("OSC 8 display text must be preserved, got:\n%s", out)
	}
}

// Named additional writers never receive payload text at all (the typed fields
// carry counts, not strings), and header tags are redacted per-writer trust.
func TestRedaction_NamedRingWriterHeaderTags(t *testing.T) {
	wp1ClearColourEnv(t)

	var console bytes.Buffer
	log := New(WithConsoleOutput(&console), WithLevel(LevelDebug))
	ring := NewRingBufferWriter(4)
	log.AddWriter("ring", ring)

	log.Info("parent <secure>" + wp1Secret + "</secure>")
	log.Group(LevelInfo, "group <secure>"+wp1Secret+"</secure>", GroupItem{Text: "item <secure>" + wp1Secret + "</secure>"})
	_ = log.Close()

	for i, s := range ring.Snapshot(4) {
		if strings.Contains(s.Message, wp1Secret) {
			t.Fatalf("ring snapshot %d (%q) carries tag plaintext", i, s.Message)
		}
		if strings.Contains(s.Message, "<secure>") {
			t.Fatalf("ring snapshot %d (%q) carries unprocessed markers", i, s.Message)
		}
	}
}

// FORCE_COLOR while writing to a real temporary file: colour may be emitted,
// but secure fields and <secure> tags must still be redacted — the file is not
// a terminal, so it is untrusted regardless of the colour override.
func TestRedaction_ForceColourOnFileStillRedacts(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")

	path := filepath.Join(t.TempDir(), "force-colour.log")
	f, err := os.Create(path) //nolint:gosec // G304: path is built from t.TempDir()
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}

	log := New(WithConsoleOutput(f), WithStructuredOutput(f), WithLevel(LevelDebug))
	log.Info("api key <secure>"+wp1Secret+"</secure> rejected",
		Secure("token", wp1Secret),
		SecureURL("endpoint", "https://user:"+wp1Secret+"@example.com"))
	_ = log.Close()
	_ = f.Close()

	got, err := os.ReadFile(path) //nolint:gosec // G304: path is built from t.TempDir()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	out := string(got)

	if strings.Contains(out, wp1Secret) {
		t.Errorf("plaintext secret leaked to a file under FORCE_COLOR:\n%s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected a [REDACTED] mark in redacted output, got:\n%s", out)
	}
	// FORCE_COLOR must still allow colour presentation on the file: proves the
	// test exercised the colour-enabled-but-untrusted combination rather than
	// a colour-off path that happens to look the same.
	if !wp1HasANSI(out) {
		t.Errorf("expected ANSI sequences in file output under FORCE_COLOR, got none")
	}
}

// WithSecureTags(false) disables message-tag scanning but must not disable
// Secure field redaction: the opt-out covers tag scanning, not fields.
func TestRedaction_SecureTagsDisabledFieldsStillRedacted(t *testing.T) {
	wp1ClearColourEnv(t)

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf), WithLevel(LevelDebug), WithSecureTags(false))
	log.Info("message <secure>"+wp1Secret+"</secure> body", Secure("token", wp1Secret))
	_ = log.Close()

	out := buf.String()
	if !strings.Contains(out, "token: [REDACTED]") && !strings.Contains(out, `token="[REDACTED]"`) {
		t.Errorf("Secure field must stay redacted with tag scanning off, got:\n%s", out)
	}
}

// Sequential publication: after AddWriter returns, entries logged afterwards
// are redacted for the new writer, on the parent and on children created
// before the writer was added.
func TestRedaction_AfterAddWriterSubsequentEntriesRedacted(t *testing.T) {
	wp1ClearColourEnv(t)

	var console bytes.Buffer
	log := New(WithConsoleOutput(&console), WithLevel(LevelDebug))
	child := log.With(String("svc", "auth"))

	ring := NewRingBufferWriter(4)
	log.AddWriter("late", ring)

	log.Info("parent <secure>" + wp1Secret + "</secure>")
	child.Info("child <secure>" + wp1Secret + "</secure>")
	_ = log.Close()

	for i, s := range ring.Snapshot(4) {
		if strings.Contains(s.Message, wp1Secret) {
			t.Fatalf("snapshot %d (%q) carries plaintext after AddWriter completed", i, s.Message)
		}
	}
	if n := len(ring.Snapshot(4)); n != 2 {
		t.Fatalf("expected 2 snapshots in ring, got %d", n)
	}
}

// WithColour(false) survives theme swaps: no ANSI before or after, even when
// the swapped-in theme carries colour.
func TestColour_ExplicitDisableSurvivesSwapNonTTY(t *testing.T) {
	wp1ClearColourEnv(t)

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf), WithLevel(LevelDebug), WithColour(false))
	log.Info("before swap")
	log.SetTheme(ThemeDracula)
	log.Info("after swap")
	_ = log.Close()

	if out := buf.String(); wp1HasANSI(out) {
		t.Errorf("colour re-enabled by SetTheme despite WithColour(false):\n%q", out)
	}
	// Style() must keep reporting the no-colour theme while colour is disabled.
	if name := log.Style().Name(); name != "none" && name != ThemeMono.Name() {
		t.Errorf("Style() should return the no-colour theme while colour is disabled, got %q", name)
	}
}

// Mono-to-coloured swap restores colour when permission allows it, including
// FORCE_COLOR on a plain buffer. The stable permission is never inferred from
// the previous theme's useColours value.
func TestColour_MonoToColourRestoredUnderForceColorOnBuffer(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf), WithTheme(ThemeMono), WithLevel(LevelDebug))
	log.Info("mono line")
	if wp1HasANSI(buf.String()) {
		t.Fatalf("precondition: mono theme must not emit ANSI, got:\n%q", buf.String())
	}

	log.SetTheme(ThemeDracula)
	buf.Reset()
	log.Info("colour line")
	_ = log.Close()

	if !wp1HasANSI(buf.String()) {
		t.Errorf("mono-to-coloured swap must restore colour under FORCE_COLOR on a buffer, got:\n%q", buf.String())
	}
}

// NO_COLOR outranks FORCE_COLOR for styling on a buffer.
func TestColour_NoColorTakesPrecedenceOverForceColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "1")

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf), WithLevel(LevelDebug))
	log.Info("plain line")
	log.SetTheme(ThemeDracula)
	log.Info("still plain")
	_ = log.Close()

	if out := buf.String(); wp1HasANSI(out) {
		t.Errorf("NO_COLOR must outrank FORCE_COLOR for styling, got ANSI:\n%q", out)
	}
}

// Theme changes never change trust: an untrusted buffer stays untrusted across
// swaps even when colour presentation is on (FORCE_COLOR), so secure content
// stays redacted while the theme's styling applies.
func TestColour_ThemeSwapNeverChangesTrust(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf), WithLevel(LevelDebug))
	log.Info("token <secure>" + wp1Secret + "</secure> before")
	log.SetTheme(ThemeDracula)
	log.Info("token <secure>" + wp1Secret + "</secure> after")
	_ = log.Close()

	out := buf.String()
	if strings.Contains(out, wp1Secret) {
		t.Errorf("theme swap exposed tag plaintext on an untrusted console:\n%s", out)
	}
	if !wp1HasANSI(out) {
		t.Errorf("expected ANSI on the buffer under FORCE_COLOR, got:\n%q", out)
	}
}

// Custom timestamp format survives theme swaps without corrupting the prefix
// width or the rendered timestamp.
func TestTheme_CustomTimestampSurvivesSwap(t *testing.T) {
	wp1ClearColourEnv(t)

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf), WithLevel(LevelDebug), WithTimeFormat("2006-01-02"))
	log.Info("dated line")
	log.SetTheme(ThemeNord)
	log.Info("dated line after swap")
	_ = log.Close()

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	for _, line := range lines {
		// Date-only timestamp is "YYYY-MM-DD" (10 chars) followed by a space.
		if len(line) < 11 || !wp1IsDateOnly(line[:10]) {
			t.Errorf("line does not start with the custom date-only timestamp: %q", line)
		}
	}
}

func wp1IsDateOnly(s string) bool {
	if len(s) != 10 {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			if c != '-' {
				return false
			}
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// themeCallbackWriter is an external ThemedWriter whose SetTheme calls back
// into logger registration queries — exactly the re-entry that deadlocked when
// SetTheme held the topology locks.
type themeCallbackWriter struct {
	log *Logger
	got *Theme
}

func (w *themeCallbackWriter) Write(e *Entry) error { return nil }
func (w *themeCallbackWriter) Close() error         { return nil }

func (w *themeCallbackWriter) SetTheme(t *Theme) {
	// Query registration state from inside the callback. Under the old
	// implementation this call needed the very locks SetTheme held.
	if w.log.Writer("self") == nil {
		panic("callback writer must find itself registered")
	}
	w.got = t
}

// An external ThemedWriter callback that queries registration state
// (WriterByName/Stats) must not deadlock SetTheme.
func TestSetTheme_ExternalCallbackQueriesWithoutDeadlock(t *testing.T) {
	wp1ClearColourEnv(t)

	var buf bytes.Buffer
	log := New(WithConsoleOutput(&buf), WithLevel(LevelDebug))
	cb := &themeCallbackWriter{log: log}
	log.AddWriter("self", cb)
	ring := NewRingBufferWriter(4)
	log.AddWriter("ring", ring)

	done := make(chan struct{})
	go func() {
		defer close(done)
		log.SetTheme(ThemeDracula)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("SetTheme deadlocked in an external ThemedWriter callback querying registration state")
	}

	if cb.got != ThemeDracula {
		t.Errorf("callback writer did not receive the new theme, got %v", cb.got)
	}
	if got := ring.Stats(); got.Capacity != 4 {
		t.Errorf("ring stats query broken after theme swap, got %+v", got)
	}
	_ = log.Close()
}

// wp1SyncBuffer is a mutex-protected sink shared by the concurrent theme storm.
type wp1SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *wp1SyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *wp1SyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Concurrent SetTheme versus every reader the spec names: Theme, Style, Info,
// Status, Render, KeyValues, Bullet and child-logger output. The race
// detector is the assertion; output assertions confirm rendering completed.
func TestTheme_ConcurrentSetThemeVsAllReaders(t *testing.T) {
	wp1ClearColourEnv(t)

	var buf wp1SyncBuffer
	log := New(WithConsoleOutput(&buf), WithLevel(LevelDebug))
	child := log.With(String("svc", "theme"))

	themes := []*Theme{ThemeDracula, ThemeNord, ThemeSolarized}
	const rounds = 500

	var wg sync.WaitGroup
	wg.Add(5)
	go func() { // theme churn
		defer wg.Done()
		for i := range rounds {
			log.SetTheme(themes[i%len(themes)])
		}
	}()
	go func() { // Theme/Style readers
		defer wg.Done()
		for range rounds {
			_ = log.Style().Name()
			_ = log.Theme().Name()
		}
	}()
	go func() { // Info + Status
		defer wg.Done()
		for range rounds / 2 {
			log.Info("info line")
			log.Status(LevelInfo, StatusOK, "status line")
		}
	}()
	go func() { // rich renderers
		defer wg.Done()
		for range rounds / 2 {
			log.Box("t", "body")
			log.KeyValues([]KeyValuePair{{Key: "k", Value: "v"}})
			log.Bullet(1, "bullet")
			log.Render(NewBanner("banner", log.Style()))
		}
	}()
	go func() { // child logger output
		defer wg.Done()
		for range rounds / 2 {
			child.Info("child line")
		}
	}()

	wg.Wait()
	_ = log.Close()

	out := buf.String()
	for _, want := range []string{"info line", "status line", "child line", "banner"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q after concurrent theme swaps", want)
		}
	}
}

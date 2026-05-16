package velocity

import (
	"strings"
	"testing"
	"time"
)

// TestConsoleWriterRB_SecureFieldRedactedOnNonTTY is a regression test for the bug
// where ConsoleWriterRB always passed trusted=true to the template, meaning Secure
// fields were rendered in plaintext even when the writer was not a terminal.
// The fix uses isTTY (false for bytes.Buffer) so Secure fields are redacted.
func TestConsoleWriterRB_SecureFieldRedactedOnNonTTY(t *testing.T) {
	t.Parallel()

	// Use safeBuffer (mutex-protected) because the ring buffer flusher goroutine
	// writes to it concurrently with the test's Len() poll in waitFor.
	var buf safeBuffer
	// Use a theme so the template path is exercised (not the fallback formatEntry path).
	w := NewConsoleWriterRB(&buf, ThemeNightOwl, nil, FieldDisplayInline)
	// safeBuffer is not a *os.File, so resolveColourForWriter returns false → isTTY=false → untrusted.

	entry := GetEntry()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("login")
	entry.WithFields(Secure("password", "s3cr3t"))
	entry.SetTime(time.Now())
	entry.Write()

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 5*time.Second, 5*time.Millisecond, "data should flush from ConsoleWriterRB")

	_ = w.Close()

	output := buf.String()
	if strings.Contains(output, "s3cr3t") {
		t.Errorf("Secure field plaintext leaked to non-TTY ConsoleWriterRB: %q", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in non-TTY ConsoleWriterRB output, got: %q", output)
	}
}

func TestConsoleWriterRB_Timezone(t *testing.T) {
	var buf safeBuffer

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York timezone not available:", err)
	}

	// Nil theme forces the formatEntry fallback path which must apply displayTimezone.
	w := NewConsoleWriterRB(&buf, nil, loc, FieldDisplayInline)

	entry := GetEntry()
	entry.SetLevel(LevelInfo)
	entry.SetMessage("tz test")
	// Jan 15 is EST (UTC-5). 12:00 UTC becomes 07:00 EST.
	entry.SetTime(time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC))
	entry.Write()

	if err := w.Write(entry); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	waitFor(t, func() bool {
		return buf.Len() > 0
	}, 5*time.Second, 5*time.Millisecond, "data should flush from ConsoleWriterRB")

	_ = w.Close()

	output := buf.String()
	if !strings.Contains(output, "-05:00") {
		t.Fatalf("expected -05:00 timezone offset in output, got: %s", output)
	}
}

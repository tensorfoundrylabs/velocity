package velocity

import (
	"strings"
	"testing"
	"time"
)

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

package velocity

import (
	"sync"
	"testing"
)

// resetHyperlinkDetection tears down the sync.Once so tests that mutate
// VELOCITY_HYPERLINKS can start from a clean state. Each test that uses it
// must defer a second call to restore the zero value.
func resetHyperlinkDetection() {
	hyperlinkOnce = sync.Once{}
	hyperlinkSupported = false
}

// withHyperlinkEnv sets VELOCITY_HYPERLINKS to value, calls reset so detection
// re-runs on next call, and returns a cleanup func.
func withHyperlinkEnv(t *testing.T, value string) {
	t.Helper()
	t.Setenv("VELOCITY_HYPERLINKS", value)
	resetHyperlinkDetection()
	t.Cleanup(resetHyperlinkDetection)
}

// ---- HyperlinksSupported ----------------------------------------------------

func TestHyperlinksSupported_ForceOn(t *testing.T) {
	withHyperlinkEnv(t, "1")

	if !HyperlinksSupported() {
		t.Error("expected HyperlinksSupported()=true when VELOCITY_HYPERLINKS=1")
	}
}

func TestHyperlinksSupported_ForceOff(t *testing.T) {
	withHyperlinkEnv(t, "0")

	if HyperlinksSupported() {
		t.Error("expected HyperlinksSupported()=false when VELOCITY_HYPERLINKS=0")
	}
}

// ---- Hyperlink — OSC 8 path -------------------------------------------------

func TestHyperlink_OSC8_ForceOn(t *testing.T) {
	withHyperlinkEnv(t, "1")

	const uri = "https://example.com"
	const text = "click here"
	got := Hyperlink(uri, text)
	want := "\x1b]8;;" + uri + "\x07" + text + "\x1b]8;;\x07"
	if got != want {
		t.Errorf("OSC 8 sequence wrong\nwant: %q\ngot:  %q", want, got)
	}
}

func TestHyperlink_OSC8_ContainsText(t *testing.T) {
	withHyperlinkEnv(t, "1")

	got := Hyperlink("https://example.com", "docs")
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	// The visible text must appear between the two OSC sequences.
	if got[len("\x1b]8;;https://example.com\x07"):len(got)-len("\x1b]8;;\x07")] != "docs" {
		t.Errorf("text not in expected position: %q", got)
	}
}

// ---- Hyperlink — fallback paths ---------------------------------------------

func TestHyperlink_Fallback_Parens(t *testing.T) {
	withHyperlinkEnv(t, "0")

	got := Hyperlink("https://example.com", "click here")
	want := "click here (https://example.com)"
	if got != want {
		t.Errorf("parens fallback: want %q, got %q", want, got)
	}
}

func TestHyperlink_Fallback_None(t *testing.T) {
	withHyperlinkEnv(t, "0")

	const text = "click here"
	got := Hyperlink("https://example.com", text, WithHyperlinkFallback(HyperlinkFallbackNone))
	if got != text {
		t.Errorf("none fallback: want %q, got %q", text, got)
	}
	// None fallback returns the exact input slice — no allocation.
	if got != text {
		t.Errorf("identity: want same string, got %q", got)
	}
}

func TestHyperlink_Fallback_Brackets(t *testing.T) {
	withHyperlinkEnv(t, "0")

	got := Hyperlink("https://example.com", "click here", WithHyperlinkFallback(HyperlinkFallbackBrackets))
	want := "click here [https://example.com]"
	if got != want {
		t.Errorf("brackets fallback: want %q, got %q", want, got)
	}
}

// ---- Edge cases -------------------------------------------------------------

func TestHyperlink_EmptyText(t *testing.T) {
	// Empty text returns empty regardless of hyperlink support or URI.
	for _, env := range []string{"0", "1"} {
		withHyperlinkEnv(t, env)
		got := Hyperlink("https://example.com", "")
		if got != "" {
			t.Errorf("VELOCITY_HYPERLINKS=%s: empty text must return empty, got %q", env, got)
		}
	}
}

func TestHyperlink_EmptyURI_Supported(t *testing.T) {
	withHyperlinkEnv(t, "1")

	// Empty URI with supported terminal falls through to fallback — we don't
	// emit a zero-URI OSC sequence mid-string as it would close any active link.
	got := Hyperlink("", "label", WithHyperlinkFallback(HyperlinkFallbackParens))
	// No URI to append in parens form, so just the text.
	if got != "label" {
		t.Errorf("empty URI + supported: want %q, got %q", "label", got)
	}
}

func TestHyperlink_EmptyURI_Unsupported_AllFallbacks(t *testing.T) {
	withHyperlinkEnv(t, "0")

	cases := []struct {
		mode HyperlinkFallback
		want string
	}{
		{HyperlinkFallbackParens, "label"},
		{HyperlinkFallbackNone, "label"},
		{HyperlinkFallbackBrackets, "label"},
	}
	for _, tc := range cases {
		got := Hyperlink("", "label", WithHyperlinkFallback(tc.mode))
		if got != tc.want {
			t.Errorf("mode %d, empty URI: want %q, got %q", tc.mode, tc.want, got)
		}
	}
}

func TestHyperlink_DefaultFallback_IsParens(t *testing.T) {
	withHyperlinkEnv(t, "0")

	// Confirm the zero-value of HyperlinkFallback is Parens, not None.
	var f HyperlinkFallback
	if f != HyperlinkFallbackParens {
		t.Errorf("default HyperlinkFallback should be HyperlinkFallbackParens (0), got %d", f)
	}
}

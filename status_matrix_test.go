package velocity

// Permanent coverage for the F4 finding from the independent finish review:
// Status styling must follow the logger's resolved colour permission
// (WithColour(false) / NO_COLOR / FORCE_COLOR) independently of trust (actual
// terminal classification). Trust alone decides secure plaintext visibility.
// The injected-IsTerminal writer gives a deterministic "real terminal" without
// a pty (config.go's detector hook).

import (
	"bytes"
	"strings"
	"testing"
)

// frTermBuffer classifies as a real terminal through the IsTerminal()
// detector hook while capturing output like a buffer.
type frTermBuffer struct{ bytes.Buffer }

func (*frTermBuffer) IsTerminal() bool { return true }

func frHasANSI(s string) bool { return strings.Contains(s, "\x1b[") }

// TestStatus_ColourTrustMatrix walks the complete matrix: destination
// (terminal vs buffer) x colour policy (default, WithColour(false), NO_COLOR,
// FORCE_COLOR, NO_COLOR-beats-FORCE_COLOR). Styling expectations mirror the
// ordinary log line's resolved useColours; trust expectations follow the
// destination alone.
func TestStatus_ColourTrustMatrix(t *testing.T) {
	const secret = "TOPSECRETPASSWORD"

	cases := []struct {
		name       string
		terminal   bool
		setEnv     func(t *testing.T)
		colourOff  bool
		wantANSI   bool
		wantSecret bool // secure field + tag plaintext visible
	}{
		{
			name:       "terminal default: styled and trusted",
			terminal:   true,
			setEnv:     clearBoth,
			wantANSI:   true,
			wantSecret: true,
		},
		{
			// The reviewer's probe: explicit no-colour on a terminal.
			name:       "terminal WithColour(false): plain but trusted",
			terminal:   true,
			setEnv:     clearBoth,
			colourOff:  true,
			wantANSI:   false,
			wantSecret: true,
		},
		{
			name:       "terminal NO_COLOR: plain but trusted",
			terminal:   true,
			setEnv:     setNoColour,
			wantANSI:   false,
			wantSecret: true,
		},
		{
			name:       "terminal FORCE_COLOR: styled and trusted",
			terminal:   true,
			setEnv:     setForceColour,
			wantANSI:   true,
			wantSecret: true,
		},
		{
			// Styling without trust: FORCE_COLOR permits ANSI on a
			// non-terminal, but the destination stays untrusted.
			name:       "buffer FORCE_COLOR: styled but untrusted",
			terminal:   false,
			setEnv:     setForceColour,
			wantANSI:   true,
			wantSecret: false,
		},
		{
			name:       "buffer default: plain and untrusted",
			terminal:   false,
			setEnv:     clearBoth,
			wantANSI:   false,
			wantSecret: false,
		},
		{
			name:       "buffer WithColour(false): plain and untrusted",
			terminal:   false,
			setEnv:     clearBoth,
			colourOff:  true,
			wantANSI:   false,
			wantSecret: false,
		},
		{
			name:       "buffer NO_COLOR beats FORCE_COLOR: plain and untrusted",
			terminal:   false,
			setEnv:     setBothColourVars,
			wantANSI:   false,
			wantSecret: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setEnv(t)

			var sink interface {
				Write([]byte) (int, error)
			}
			if tc.terminal {
				sink = &frTermBuffer{}
			} else {
				sink = &bytes.Buffer{}
			}

			opts := []Option{WithConsoleOutput(sink), WithLevel(LevelDebug)}
			if tc.colourOff {
				opts = append(opts, WithColour(false))
			}
			l := New(opts...)
			l.Status(LevelInfo, StatusOK,
				"api key <secure>"+secret+"</secure> accepted",
				Secure("token", secret))
			_ = l.Close()

			var out string
			switch b := sink.(type) {
			case *frTermBuffer:
				out = b.String()
			case *bytes.Buffer:
				out = b.String()
			}

			if got := frHasANSI(out); got != tc.wantANSI {
				t.Errorf("ANSI = %v, want %v; output: %q", got, tc.wantANSI, out)
			}
			if got := strings.Contains(out, secret); got != tc.wantSecret {
				t.Errorf("secure plaintext visible = %v, want %v; output: %q", got, tc.wantSecret, out)
			}
			// Styling and trust are independent: redaction always produces a mark.
			if !tc.wantSecret && !strings.Contains(out, "[REDACTED]") {
				t.Errorf("expected a [REDACTED] mark on untrusted output, got: %q", out)
			}
		})
	}
}

func clearBoth(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
}

func setNoColour(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
}

func setForceColour(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
}

func setBothColourVars(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "1")
}

// Ordinary log lines and Status must agree on styling under every policy —
// the regression was Status alone ignoring the resolved permission.
func TestStatus_StylingMatchesLogLines(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setEnv    func(t *testing.T)
		colourOff bool
		wantANSI  bool
	}{
		{"default", clearBoth, false, false},
		{"WithColour(false)", clearBoth, true, false},
		{"FORCE_COLOR", setForceColour, false, true},
		{"NO_COLOR", setNoColour, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setEnv(t)
			var buf bytes.Buffer
			opts := []Option{WithConsoleOutput(&buf), WithLevel(LevelDebug)}
			if tc.colourOff {
				opts = append(opts, WithColour(false))
			}
			l := New(opts...)
			l.Info("plain line")
			l.Status(LevelInfo, StatusOK, "status line")
			_ = l.Close()

			out := buf.String()
			infoLine := ""
			statusLine := ""
			for line := range strings.SplitSeq(out, "\n") {
				if strings.Contains(line, "plain line") {
					infoLine = line
				}
				if strings.Contains(line, "status line") {
					statusLine = line
				}
			}
			if infoLine == "" || statusLine == "" {
				t.Fatalf("missing output lines: %q", out)
			}
			if a, b := frHasANSI(infoLine), frHasANSI(statusLine); a != b {
				t.Errorf("styling mismatch: Info ANSI=%v, Status ANSI=%v\ninfo: %q\nstatus: %q", a, b, infoLine, statusLine)
			}
			if got := frHasANSI(statusLine); got != tc.wantANSI {
				t.Errorf("Status ANSI = %v, want %v (policy %s)", got, tc.wantANSI, tc.name)
			}
		})
	}
}

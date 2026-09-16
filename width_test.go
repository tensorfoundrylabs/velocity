package velocity_test

// Terminal cell-width regression battery (R11 / WP5). All measurements are
// taken on rendered output: ANSI CSI/OSC sequences are stripped by an
// independent implementation and the remainder measured in terminal cells
// with uniseg. Go string lengths are never compared. Uniform width alone
// cannot catch an over-wide measurement (columns simply widen), so the
// escape-inside-grapheme cases compare against an unstyled equivalent table.

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	velocity "github.com/tensorfoundrylabs/velocity/v2"
)

// --- Independent measurement helpers (not the production visibleLen). ---

// stripANSI removes CSI and OSC sequences the way a terminal would consume
// them. Written independently of velocity's visibleLen.
func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			if i+1 >= len(s) {
				// Lone trailing ESC initiates an escape the terminal would
				// consume; it renders nothing.
				i++
				continue
			}
			switch s[i+1] {
			case '[':
				j := i + 2
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++
				}
				i = j
				continue
			case ']':
				j := i + 2
				for j < len(s) {
					if s[j] == '\a' {
						j++
						break
					}
					if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
				continue
			default:
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// widthCells measures the terminal cell width of rendered text.
func widthCells(s string) int { return uniseg.StringWidth(stripANSI(s)) }

// requireUniformWidth asserts every rendered line occupies the same number of
// terminal cells and reports the offending lines otherwise.
func requireUniformWidth(t *testing.T, rendered, context string) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("%s: no output", context)
	}

	want := widthCells(lines[0])
	for i, line := range lines {
		got := widthCells(line)
		if got != want {
			t.Errorf("%s: line %d measures %d cells, want %d (line=%q)", context, i, got, want, stripANSI(line))
		}
	}
}

const goldenASCIIWidth = "" +
	"\x1b[38;2;126;142;166m───────┬─────────┬───────\x1b[0m\n" +
	"\x1b[38;2;126;142;166m \x1b[0m\x1b[38;2;127;211;255mName \x1b[0m\x1b[38;2;126;142;166m │ \x1b[0m\x1b[38;2;127;211;255mStatus \x1b[0m\x1b[38;2;126;142;166m │ \x1b[0m\x1b[38;2;127;211;255mCount\x1b[0m\x1b[38;2;126;142;166m \x1b[0m\n" +
	"\x1b[38;2;126;142;166m───────┼─────────┼───────\x1b[0m\n" +
	"\x1b[38;2;126;142;166m \x1b[38;2;224;224;224malpha\x1b[0m\x1b[38;2;126;142;166m │ \x1b[38;2;224;224;224mrunning\x1b[0m\x1b[38;2;126;142;166m │ \x1b[38;2;224;224;224m10   \x1b[0m\x1b[38;2;126;142;166m \x1b[0m\n" +
	"\x1b[38;2;126;142;166m \x1b[38;2;224;224;224mbeta \x1b[0m\x1b[38;2;126;142;166m │ \x1b[38;2;224;224;224mstopped\x1b[0m\x1b[38;2;126;142;166m │ \x1b[38;2;224;224;224m0    \x1b[0m\x1b[38;2;126;142;166m \x1b[0m\n" +
	"\x1b[38;2;126;142;166m───────┴─────────┴───────\x1b[0m\n" +
	"---\n" +
	"\x1b[38;2;126;142;166m───────┬───\x1b[0m\n" +
	"\x1b[38;2;126;142;166m \x1b[0m\x1b[38;2;127;211;255mA    \x1b[0m\x1b[38;2;126;142;166m │ \x1b[0m\x1b[38;2;127;211;255mB\x1b[0m\x1b[38;2;126;142;166m \x1b[0m\n" +
	"\x1b[38;2;126;142;166m───────┼───\x1b[0m\n" +
	"\x1b[38;2;126;142;166m \x1b[38;2;224;224;224mshort\x1b[0m\x1b[38;2;126;142;166m │ \x1b[38;2;224;224;224m \x1b[0m\x1b[38;2;126;142;166m \x1b[0m\n" +
	"\x1b[38;2;126;142;166m───────┴───\x1b[0m\n" +
	"---\n" +
	"\x1b[38;2;126;142;166m┌─title──────────────────────────────────┐\x1b[0m\n" +
	"\x1b[38;2;126;142;166m│ \x1b[0m\x1b[38;2;224;224;224mline one                               \x1b[0m\x1b[38;2;126;142;166m│\x1b[0m\n" +
	"\x1b[38;2;126;142;166m│ \x1b[0m\x1b[38;2;224;224;224mline two                               \x1b[0m\x1b[38;2;126;142;166m│\x1b[0m\n" +
	"\x1b[38;2;126;142;166m│ \x1b[0m\x1b[38;2;224;224;224mlonger third line                      \x1b[0m\x1b[38;2;126;142;166m│\x1b[0m\n" +
	"\x1b[38;2;126;142;166m└────────────────────────────────────────┘\x1b[0m\n" +
	"---\n" +
	"\x1b[38;2;126;142;166m┌─────────────┐\x1b[0m\n" +
	"\x1b[38;2;126;142;166m│ \x1b[0m\x1b[38;2;224;224;224mbanner text\x1b[0m\x1b[38;2;126;142;166m │\x1b[0m\n" +
	"\x1b[38;2;126;142;166m│ \x1b[0m\x1b[38;2;224;224;224msecond row \x1b[0m\x1b[38;2;126;142;166m │\x1b[0m\n" +
	"\x1b[38;2;126;142;166m└─────────────┘\x1b[0m\n" +
	"---\n" +
	"├─ \x1b[38;2;224;224;224mroot\x1b[0m\n" +
	"│   ├─ \x1b[38;2;224;224;224mleaf-a: 1\x1b[0m\n" +
	"│   └─ \x1b[38;2;224;224;224mleaf-b\x1b[0m\n" +
	"└─ \x1b[38;2;224;224;224mother: text\x1b[0m\n" +
	"---\n" +
	"\x1b[38;2;130;170;255m▓ Service v1.2.3 ▓\x1b[0m\n" +
	"\x1b[38;2;126;142;166mhost:               \x1b[0m \x1b[38;2;224;224;224ma.example\x1b[0m\n" +
	"\x1b[38;2;126;142;166mport:               \x1b[0m \x1b[38;2;224;224;224m8080\x1b[0m\n" +
	"---\n" +
	"\x1b[38;2;126;142;166mregion\x1b[0m: \x1b[38;2;211;211;211map-southeast-2\x1b[0m\n"

// --- Table geometry with wide and exotic cell content. ---

func TestWidth_TableCells_AlignAtEqualCellWidths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rows [][]string
	}{
		{"cjk", [][]string{{"日本語テキスト", "ok"}, {"ascii", "ok"}}},
		{"combining marks", [][]string{{"café nouveau", "ok"}, {"ascii", "ok"}}},
		{"flag emoji", [][]string{{"flag \U0001F1E6\U0001F1FA", "ok"}, {"ascii", "ok"}}},
		{"zwj family emoji", [][]string{{"\U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466 walks", "ok"}, {"ascii", "ok"}}},
		{"ansi styled", [][]string{{"\033[32mgreen text\033[0m", "ok"}, {"ascii", "ok"}}},
		{"osc8 hyperlink", [][]string{{"\033]8;;https://example.com\adocs link\033]8;;\a", "ok"}, {"ascii", "ok"}}},
		{"mixed wide and narrow", [][]string{{"日本", "\U0001F1E6\U0001F1FA", "é"}, {"a", "b", "c"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tbl := velocity.NewTable([]string{"Key", "Status"}, tc.rows, nil)
			requireUniformWidth(t, tbl.String(), "table/"+tc.name)
		})
	}
}

// Headers are measured with the same cell-width helper as cells; accented,
// CJK and emoji headers must not shift the column edges.
func TestWidth_TableHeaders_NonASCII(t *testing.T) {
	t.Parallel()
	for _, header := range []string{"Café", "café", "日本語", "\U0001F1E6\U0001F1FA size", "é", "\U0001F469\u200d\U0001F4BB"} {
		t.Run(header, func(t *testing.T) {
			t.Parallel()
			tbl := velocity.NewTable([]string{header, "Status"}, [][]string{{"plain", "ok"}}, nil)
			requireUniformWidth(t, tbl.String(), "header "+header)
		})
	}
}

// Rows with fewer cells than headers must be padded so the row geometry
// (borders, separators, column edges) still lines up.
func TestWidth_TableMissingCells_RowsKeepGeometry(t *testing.T) {
	t.Parallel()
	tbl := velocity.NewTable(
		[]string{"Alpha", "Beta", "Gamma"},
		[][]string{
			{"one", "two", "three"},
			{"short"},
			{"a", "b"},
			{},
		},
		nil,
	)
	requireUniformWidth(t, tbl.String(), "missing cells")
}

// Control sequences embedded between the code points of one grapheme cluster
// must not split it into separate visible characters: the styled cell's
// column is exactly as wide as the unstyled equivalent's.
func TestWidth_EscapeInsideGrapheme(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		name   string
		styled string
		clean  string
	}{
		{"sgr inside combining", "e\033[0ḿ", "é"},
		{"osc between regional pair", "\U0001F1E6\033]8;;x\a\U0001F1FA", "\U0001F1E6\U0001F1FA"},
		{"sgr inside zwj family", "\U0001F468\033[32m\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466", "\U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466"},
		{"sgr before combining", "cafe\033[1ḿ", "café"},
	}
	for _, tc := range pairs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			styledTable := velocity.NewTable([]string{"A", "B"}, [][]string{{tc.styled, "x"}, {"ascii", "x"}}, nil).String()
			cleanTable := velocity.NewTable([]string{"A", "B"}, [][]string{{tc.clean, "x"}, {"ascii", "x"}}, nil).String()

			styledLines := strings.Split(strings.TrimRight(styledTable, "\n"), "\n")
			cleanLines := strings.Split(strings.TrimRight(cleanTable, "\n"), "\n")
			for i := range cleanLines {
				if got, want := widthCells(styledLines[i]), widthCells(cleanLines[i]); got != want {
					t.Errorf("styled line %d measures %d cells, unstyled equivalent %d (styled=%q clean=%q)",
						i, got, want, tc.styled, tc.clean)
				}
			}
			if widthCells(tc.clean) == 0 {
				t.Fatalf("bad fixture: clean cell %q measures 0", tc.clean)
			}
		})
	}
}

// --- Box and banner border geometry. ---

func TestWidth_Box_CJKContent(t *testing.T) {
	t.Parallel()
	box := velocity.NewBox("results", "全部成功\nall good", nil)
	requireUniformWidth(t, box.String(), "box/cjk content")
}

func TestWidth_Box_CJKTitle(t *testing.T) {
	t.Parallel()
	box := velocity.NewBox("日本語タイトル", "plain content", nil)
	requireUniformWidth(t, box.String(), "box/cjk title")
}

func TestWidth_Box_EmojiAndCombining(t *testing.T) {
	t.Parallel()
	box := velocity.NewBox("results", "family \U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466\ncafé closed\nplain", nil)
	requireUniformWidth(t, box.String(), "box/emoji+combining")
}

func TestWidth_Banner_UnicodeText(t *testing.T) {
	t.Parallel()
	banner := velocity.NewBanner("部署完成\ndeploy finished", nil)
	requireUniformWidth(t, banner.String(), "banner/cjk")
}

func TestWidth_CreateBanner_Unicode(t *testing.T) {
	t.Parallel()
	out := velocity.CreateBanner("日本語", "1.0.0", "https://example.com/部署", []string{"plain ascii art", "プロトタイプ"})
	requireUniformWidth(t, out, "createbanner/unicode")
}

// Malformed UTF-8 anywhere in renderable content must not panic and must not
// corrupt row geometry for the well-formed rows around it.
func TestWidth_MalformedUTF8_NoPanic(t *testing.T) {
	t.Parallel()
	malformed := "\xff\xfe\x80abc\xc3"
	t.Run("table cell", func(t *testing.T) {
		t.Parallel()
		rendered := velocity.NewTable([]string{"A", "B"}, [][]string{{malformed, "ok"}, {"fine", "ok"}}, nil).String()
		requireUniformWidth(t, rendered, "table/malformed")
	})
	t.Run("box content", func(t *testing.T) {
		t.Parallel()
		rendered := velocity.NewBox("t", "bad \xff\xfe\ngood", nil).String()
		requireUniformWidth(t, rendered, "box/malformed")
	})
	t.Run("banner content", func(t *testing.T) {
		t.Parallel()
		rendered := velocity.NewBanner("bad \xff\xfe\n"+"good", nil).String()
		requireUniformWidth(t, rendered, "banner/malformed")
	})
	t.Run("truncated multibyte", func(t *testing.T) {
		t.Parallel()
		rendered := velocity.NewTable([]string{"A", "B"}, [][]string{{"prefix\xe4\xb8", "ok"}}, nil).String()
		requireUniformWidth(t, rendered, "table/truncated")
	})
	t.Run("escape stripper", func(t *testing.T) {
		t.Parallel()
		// Truncated CSI/OSC sequences and a lone trailing ESC must not loop
		// or panic, and contribute no visible cells. Raw unterminated escapes
		// land in the rendered bytes, so width is asserted on the cell string
		// itself rather than the rendered line, where a lone ESC would glue
		// onto the theme's reset sequence and confuse any stripper.
		for _, s := range []string{"\033[", "\033]8;;", "text\033", "\033]8;;url\033[32m", "\033"} {
			_ = velocity.NewTable([]string{"A"}, [][]string{{s}}, nil).String()
			stripped := stripANSI(s)
			want := map[string]string{
				"\033[":               "",
				"\033]8;;":            "",
				"text\033":            "text",
				"\033]8;;url\033[32m": "",
				"\033":                "",
			}[s]
			if stripped != want {
				t.Errorf("stripANSI(%q) = %q, want %q", s, stripped, want)
			}
		}
	})
}

// --- Negative Bullet nesting clamps instead of panicking. ---

func TestWidth_BulletNegativeNesting_Clamped(t *testing.T) {
	t.Parallel()
	t.Run("logger bullet", func(t *testing.T) {
		t.Parallel()
		buf := &strings.Builder{}
		// Preset first: a preset applied after WithConsoleOutput redirects
		// console output back to stdout.
		logger := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(buf))
		t.Cleanup(func() { _ = logger.Close() })
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Bullet(-1) panicked: %v", r)
				}
			}()
			logger.Bullet(-1, "negative nesting")
		}()
		if !strings.Contains(buf.String(), "negative nesting") {
			t.Errorf("clamped Bullet(-1) produced no output: %q", buf.String())
		}
	})

	t.Run("pretty bullet", func(t *testing.T) {
		t.Parallel()
		buf := &strings.Builder{}
		p := velocity.NewPretty(buf, nil)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Pretty.Bullet(-3) panicked: %v", r)
				}
			}()
			p.Bullet(-3, "negative nesting")
		}()
		if !strings.Contains(buf.String(), "negative nesting") {
			t.Errorf("clamped Pretty.Bullet(-3) produced no output: %q", buf.String())
		}
	})
}

// Clamped negative bullets render identically to level 0.
func TestWidth_BulletNegativeMatchesLevelZero(t *testing.T) {
	t.Parallel()
	bufA, bufB := &strings.Builder{}, &strings.Builder{}
	la := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(bufA))
	lb := velocity.New(velocity.WithDevelopment(), velocity.WithConsoleOutput(bufB))
	t.Cleanup(func() { _ = la.Close(); _ = lb.Close() })
	la.Bullet(-7, "same text")
	lb.Bullet(0, "same text")
	if bufA.String() != bufB.String() {
		t.Errorf("Bullet(-7) != Bullet(0):\n  -7: %q\n   0: %q", bufA.String(), bufB.String())
	}
}

// --- ASCII byte-identity golden fixture. ---

// renderASCIIWidthBattery renders a fixed ASCII battery of every construct
// the width helpers touch. It is byte-compared against goldenASCIIWidth,
// captured from the cab9251 baseline. The only justified divergence in the
// golden is the missing-cell table row (padded since WP5 so declared column
// geometry holds); every other byte must stay identical through future width
// work. Themes are passed explicitly so the fixture is env-independent.
func renderASCIIWidthBattery() string {
	var w strings.Builder
	w.WriteString(velocity.NewTable([]string{"Name", "Status", "Count"}, [][]string{
		{"alpha", "running", "10"},
		{"beta", "stopped", "0"},
	}, nil).String())
	w.WriteString("---\n")
	w.WriteString(velocity.NewTable([]string{"A", "B"}, [][]string{{"short"}}, nil).String())
	w.WriteString("---\n")
	w.WriteString(velocity.NewBox("title", "line one\nline two\nlonger third line", nil).String())
	w.WriteString("---\n")
	w.WriteString(velocity.NewBanner("banner text\nsecond row", nil).String())
	w.WriteString("---\n")
	w.WriteString(velocity.NewTree([]velocity.TreeItem{
		{Key: "root", Children: []velocity.TreeItem{
			{Key: "leaf-a", Value: 1},
			{Key: "leaf-b"},
		}},
		{Key: "other", Value: "text"},
	}, nil).String())
	w.WriteString("---\n")
	w.WriteString(velocity.NewSystemInfo(&velocity.SystemInfoData{
		Title: "Service", Version: "1.2.3",
		Fields: []velocity.KeyValuePair{{Key: "host", Value: "a.example"}, {Key: "port", Value: "8080"}},
	}, nil).String())
	w.WriteString("---\n")
	w.WriteString(velocity.NewKeyValue("region", "ap-southeast-2", nil).String())
	return w.String()
}

func TestWidth_ASCIIGolden(t *testing.T) {
	t.Parallel()
	got := renderASCIIWidthBattery()
	if got != goldenASCIIWidth {
		t.Errorf("ASCII battery changed; byte-identity with cab9251 is required except the justified missing-cell row.\ngot:\n%s\ndiff against golden: inspect with `go test -run TestWidth_ASCIIGolden -v`", got)
	}
}

// --- Component column: cell padding and grapheme-safe truncation. ---

// componentBarOffset returns the cell offset of the component bar │ in a
// rendered console line, or -1 when absent.
func componentBarOffset(strippedLine string) int {
	before, _, found := strings.Cut(strippedLine, "│")
	if !found {
		return -1
	}
	return widthCells(before)
}

func logComponentLine(t *testing.T, name string, width int) string {
	t.Helper()
	buf := &strings.Builder{}
	logger := velocity.New(
		velocity.WithDevelopment(),
		velocity.WithConsoleOutput(buf),
		velocity.WithComponentStyling(),
		velocity.WithComponentField("component"),
		velocity.WithComponentColumnWidth(width),
	)
	t.Cleanup(func() { _ = logger.Close() })
	logger.Info("msg", velocity.String("component", name))
	return buf.String()
}

func TestWidth_ComponentColumn_Cells(t *testing.T) {
	t.Parallel()
	// A CJK name occupies 6 cells; with an 8-cell column it, an ASCII name
	// and an accented name must all land the bar at the same cell offset
	// (the leading timestamp/level prefix is fixed, so equality is exact).
	offsets := make(map[int]string)
	for _, name := range []string{"日本語", "Scout", "éclair"} {
		offset := componentBarOffset(stripANSI(logComponentLine(t, name, 8)))
		if offset < 0 {
			t.Fatalf("no component bar for %q", name)
		}
		offsets[offset] = name
	}
	if len(offsets) != 1 {
		t.Errorf("component bar offsets differ across names: %v", offsets)
	}
}

func TestWidth_ComponentColumn_Truncation(t *testing.T) {
	t.Parallel()
	// A 12-cell CJK name in a 6-cell column truncates at whole clusters to
	// 日本… (2+2 cells + 1 ellipsis = 5) padded to the 6-cell edge that a
	// fitting name (日本語, exactly 6 cells) also lands on. Truncation must
	// never split a double-width character in half.
	line := stripANSI(logComponentLine(t, "日本語ですよ", 6))
	if !strings.Contains(line, "日本…") {
		t.Errorf("truncated CJK component should render 日本…, got %q", line)
	}
	if strings.Contains(line, "日本語") {
		t.Errorf("truncation split a grapheme cluster or missed the ellipsis: %q", line)
	}
	fits := componentBarOffset(stripANSI(logComponentLine(t, "日本語", 6)))
	truncated := componentBarOffset(line)
	if fits < 0 || truncated < 0 {
		t.Fatalf("no component bar (fits=%d truncated=%d) line=%q", fits, truncated, line)
	}
	if fits != truncated {
		t.Errorf("truncated column edge at %d cells, fitting name at %d (line=%q)", truncated, fits, line)
	}
}

// --- SystemInfo key column pads accented keys by cells. ---

func TestWidth_SystemInfo_AccentedKeyAlignment(t *testing.T) {
	t.Parallel()
	info := velocity.NewSystemInfo(&velocity.SystemInfoData{
		Title: "Service",
		Fields: []velocity.KeyValuePair{
			{Key: "host", Value: "a.example"},
			{Key: "Café", Value: "b.example"},
			{Key: "状態", Value: "c.example"},
		},
	}, nil)
	rendered := info.String()
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	offsets := make(map[int]int)
	for _, line := range lines {
		stripped := stripANSI(line)
		for _, v := range []string{"a.example", "b.example", "c.example"} {
			if before, _, found := strings.Cut(stripped, v); found {
				offsets[widthCells(before)]++
			}
		}
	}
	if len(offsets) != 1 {
		t.Errorf("value column offsets differ across accented/wide keys: %v", offsets)
	}
}

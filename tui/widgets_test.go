package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// looks are the combinations of profile and character set every drawing test runs under.
func looks(t *testing.T) map[string]Styles {
	t.Helper()
	set := exampleSet(t)
	term := &TerminalColors{Foreground: "#d0d0d0", Background: "#101010"}
	out := map[string]Styles{}
	for name, l := range map[string]struct {
		theme   string
		profile ColorProfile
		charset CharsetMode
	}{
		"dark":      {exampleDark, ProfileTrueColor, CharsetUnicode},
		"light256":  {exampleLight, ProfileANSI256, CharsetUnicode},
		"scheme":    {builtinSchemes()[2].Name, ProfileTrueColor, CharsetUnicode},
		"terminal":  {ThemeTerminal, ProfileTrueColor, CharsetUnicode},
		"plain16":   {ThemeTerminal, ProfileANSI16, CharsetUnicode},
		"mono":      {exampleDark, ProfileMono, CharsetUnicode},
		"noneascii": {exampleDark, ProfileNone, CharsetASCII},
		"asciidark": {exampleDark, ProfileTrueColor, CharsetASCII},
	} {
		st, _ := set.Resolve(l.theme, l.profile, l.charset, term)
		out[name] = st
	}
	return out
}

func noColorSequence(t *testing.T, look, out string) {
	t.Helper()
	for _, seq := range []string{"38;", "48;", "\x1b[3", "\x1b[4", "\x1b[9", "\x1b[10"} {
		if strings.Contains(out, seq) {
			t.Errorf("%s: %q holds the colour sequence %q", look, out, seq)
		}
	}
}

// Every panel line is exactly as wide as asked and the frame stays closed, at any size; the focused frame
// without colour is double, so the focus reads without its colour.
func TestPanelKeepsItsSizeAndFrame(t *testing.T) {
	lines := []string{"alpha beta gamma delta", "\x1b[1mbold\x1b[0m and plain", "✓ wide 🏠 cell"}
	for look, st := range looks(t) {
		for _, focused := range []bool{false, true} {
			for width := 0; width <= 30; width++ {
				for height := 0; height <= 6; height++ {
					out := st.Panel("Title of it", "note", lines, width, height, focused)
					if len(out) != height {
						t.Fatalf("%s %dx%d: %d lines", look, width, height, len(out))
					}
					for i, l := range out {
						if w := ansi.StringWidth(l); w != width {
							t.Errorf("%s %dx%d focused %v: line %d is %d cells: %q", look, width, height, focused, i, w, l)
						}
					}
					if width < 4 || height < 2 {
						continue
					}
					h, v, c := st.frameChars(focused)
					top, bottom := ansi.Strip(out[0]), ansi.Strip(out[height-1])
					if !strings.HasPrefix(top, c[0]+h) || !strings.HasSuffix(top, c[1]) ||
						bottom != c[2]+strings.Repeat(h, width-2)+c[3] {
						t.Errorf("%s %dx%d: open frame %q / %q", look, width, height, top, bottom)
					}
					for _, l := range out[1 : height-1] {
						if p := ansi.Strip(l); !strings.HasPrefix(p, v) || !strings.HasSuffix(p, v) {
							t.Errorf("%s %dx%d: open side %q", look, width, height, p)
						}
					}
				}
			}
		}
	}
	st := looks(t)["mono"]
	if top := ansi.Strip(st.Panel("T", "", nil, 10, 3, true)[0]); !strings.HasPrefix(top, "╔") {
		t.Errorf("focused frame without colour = %q, want a double frame", top)
	}
	if top := ansi.Strip(looks(t)["dark"].Panel("T", "", nil, 10, 3, true)[0]); !strings.HasPrefix(top, "╭") {
		t.Errorf("focused frame with colour = %q, want the rounded frame", top)
	}
	if top := ansi.Strip(looks(t)["noneascii"].Panel("T", "", nil, 10, 3, true)[0]); top != "#= T ====#" {
		t.Errorf("focused ASCII frame = %q", top)
	}
	if top := ansi.Strip(looks(t)["asciidark"].Panel("T", "", nil, 10, 3, false)[0]); top != "+- T ----+" {
		t.Errorf("quiet ASCII frame = %q", top)
	}
}

func TestPanelNoteStandsOnlyWhereItFits(t *testing.T) {
	st := looks(t)["noneascii"]
	if got := st.Panel("Title", "3 items", nil, 24, 2, false)[0]; got != "+- Title ---- 3 items -+" {
		t.Errorf("top = %q", got)
	}
	if got := st.Panel("Title", "3 items", nil, 14, 2, false)[0]; strings.Contains(got, "items") || len(got) != 14 {
		t.Errorf("a note without room stays: %q", got)
	}
}

// The panel fill stays behind styled text inside it: every reset in a line is followed by the panel colour.
func TestPanelFillSurvivesInnerResets(t *testing.T) {
	st := looks(t)["dark"]
	fill := openingOf(lipgloss.NewStyle().Background(lipgloss.Color(string(st.Palette().Panel))))
	line := st.Panel("", "", []string{st.Render(StyleOK, "ok") + " rest"}, 20, 3, false)[1]
	inner := line[strings.Index(line, fill):]
	for _, loc := range sgrSequence.FindAllStringIndex(inner, -1) {
		seq := inner[loc[0]:loc[1]]
		if (seq == "\x1b[m" || seq == "\x1b[0m") && loc[1] < len(inner) && !strings.HasPrefix(inner[loc[1]:], fill) &&
			!strings.HasPrefix(inner[loc[1]:], openingOf(st.Style(StyleBorder))) {
			t.Errorf("reset at %d is not followed by the panel colour: %q", loc[0], inner)
		}
	}
}

func TestStatusKeepsItsWordWithoutColor(t *testing.T) {
	for look, st := range looks(t) {
		for _, kind := range []StatusKind{StatusOK, StatusWarn, StatusFail, StatusBusy, "other"} {
			got := ansi.Strip(st.Status(kind, "locked"))
			want := "● locked"
			if st.Charset() == CharsetASCII {
				want = "* locked"
			}
			if got != want {
				t.Errorf("%s %s: %q, want %q", look, kind, got, want)
			}
		}
	}
	if got := looks(t)["dark"].Status(StatusFail, "x"); !strings.Contains(got, "38;2;240;82;90") {
		t.Errorf("a failed status does not take the fail colour: %q", got)
	}
}

// A message is wrapped into the width, never cut: every word stays on screen.
func TestMessageWrapsAndNeverCuts(t *testing.T) {
	text := "error: cannot write the file; run chmod 600 on the configuration file and try again"
	for look, st := range looks(t) {
		for _, kind := range []MessageKind{MessageBusy, MessageError, MessageWarning, MessageStatus, MessageResult} {
			for _, width := range []int{0, 1, 7, 20, 200} {
				out := st.Message(kind, text, width)
				for _, l := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(l); w > max(width, 1) {
						t.Errorf("%s %s width %d: line %q is %d cells", look, kind, width, l, w)
					}
				}
				if got := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(out), "\n", "")), ""); got != strings.Join(strings.Fields(text), "") {
					t.Errorf("%s %s width %d: text lost: %q", look, kind, width, ansi.Strip(out))
				}
			}
		}
	}
}

func TestTruncateMarksTheCut(t *testing.T) {
	uni, ascii := looks(t)["dark"], looks(t)["noneascii"]
	for _, tc := range []struct {
		st          Styles
		text        string
		width       int
		right, left string
	}{
		{uni, "abcdef", 6, "abcdef", "abcdef"},
		{uni, "abcdef", 4, "abc…", "…def"},
		{uni, "abcdef", 1, "…", "…"},
		{uni, "abcdef", 0, "", ""},
		{ascii, "abcdef", 4, "abc~", "~def"},
		{uni, "/home/example/projects", 12, "/home/examp…", "…le/projects"},
	} {
		if got := tc.st.Truncate(tc.text, tc.width); got != tc.right {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.right)
		}
		if got := tc.st.TruncateLeft(tc.text, tc.width); got != tc.left {
			t.Errorf("TruncateLeft(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.left)
		}
	}
	if got := uni.Truncate("ab"+uni.Render(StyleOK, "cdef"), 4); ansi.Strip(got) != "abc…" || !strings.Contains(got, "\x1b[m") {
		t.Errorf("a styled cut %q is not closed", got)
	}
}

func TestClipNeverSplitsAnEscapeSequence(t *testing.T) {
	styled := "\x1b[1;38;2;1;2;3mabc\x1b[0mdef"
	for width := 0; width <= 7; width++ {
		got := Clip(styled, width)
		if w := ansi.StringWidth(got); w != min(width, 6) {
			t.Errorf("Clip width %d: %d cells", width, w)
		}
		if strings.Count(got, "\x1b") != strings.Count(got, "m") && !strings.HasSuffix(got, "m") {
			t.Errorf("Clip width %d split a sequence: %q", width, got)
		}
		if width > 0 && width < 3 && !strings.HasSuffix(got, "\x1b[m") {
			t.Errorf("Clip width %d leaves the style open: %q", width, got)
		}
	}
	if got := Clip("plain text", 5); got != "plain" {
		t.Errorf("Clip = %q", got)
	}
	if got := Clip("a🏠b", 2); got != "a" {
		t.Errorf("Clip splits a wide cell: %q", got)
	}
}

// The selected row is marked by a character, not only by its fill: "▌" on the fill, ">" without one.
func TestRowMarksTheSelectionWithoutColor(t *testing.T) {
	for look, st := range looks(t) {
		for _, width := range []int{0, 1, 2, 3, 12} {
			for _, selected := range []bool{false, true} {
				out := st.Row(st.Render(StyleOK, "ok")+" name of the row", width, selected)
				if w := ansi.StringWidth(out); w != width {
					t.Errorf("%s width %d: %d cells", look, width, w)
				}
				if width < 2 {
					continue
				}
				plain := ansi.Strip(out)
				if selected && !strings.HasPrefix(plain, st.SelectionMark()) || !selected && !strings.HasPrefix(plain, "  ") {
					t.Errorf("%s selected %v: %q", look, selected, ansi.Strip(out))
				}
			}
		}
		if (st.Profile().Colorless() || st.Palette().Selection == "" || st.Charset() == CharsetASCII) != (st.SelectionMark() == ">") {
			t.Errorf("%s: selection mark %q", look, st.SelectionMark())
		}
	}
	dark := looks(t)["dark"]
	row := dark.Row(dark.Render(StyleOK, "ok")+" rest", 12, true)
	fill := openingOf(dark.Style(StyleActiveRow))
	if i := strings.Index(row, "ok"); !strings.Contains(row[i:], "\x1b[m"+fill) {
		t.Errorf("the selection fill does not continue after an inner reset: %q", row)
	}
}

func TestTabStripMarksTheActiveTabWithoutColor(t *testing.T) {
	words := []string{"one", "two", "|", "three"}
	for look, st := range looks(t) {
		for _, focused := range []bool{false, true} {
			line, spans := st.TabStrip(words, 1, focused)
			plain := ansi.Strip(line)
			if len(spans) != len(words) {
				t.Fatalf("%s: %d spans", look, len(spans))
			}
			for i, s := range spans {
				if s.End-s.Start != ansi.StringWidth(" "+words[i]+" ") && words[i] != "|" {
					t.Errorf("%s: tab %d takes %d cells", look, i, s.End-s.Start)
				}
			}
			if st.Profile().Colorless() && !strings.Contains(plain, "[two]") {
				t.Errorf("%s: active tab without brackets: %q", look, plain)
			}
			if !st.Profile().Colorless() && strings.Contains(plain, "[") {
				t.Errorf("%s: brackets with colour: %q", look, plain)
			}
		}
	}
}

func TestButtonShowsTheFocusByCharacters(t *testing.T) {
	for look, st := range looks(t) {
		for _, kind := range []ButtonKind{ButtonNormal, ButtonPrimary, ButtonDanger} {
			if got := ansi.Strip(st.Button(kind, "Delete", true)); got != "> Delete <" {
				t.Errorf("%s: focused button %q", look, got)
			}
			if got := ansi.Strip(st.Button(kind, "Delete", false)); got != "[ Delete ]" {
				t.Errorf("%s: button %q", look, got)
			}
		}
	}
	if got := looks(t)["noneascii"].Button(ButtonDanger, "x", true); got != "> x <" {
		t.Errorf("a button without a terminal is not plain: %q", got)
	}
}

func TestCategoryMarkIsStablePerName(t *testing.T) {
	st := looks(t)["dark"]
	if st.CategoryColor("alpha") != st.CategoryColor("alpha") || st.CategoryColor("alpha") == "" {
		t.Error("a name changes its colour")
	}
	colors := map[Color]bool{}
	for _, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"} {
		colors[st.CategoryColor(name)] = true
	}
	if len(colors) < 3 {
		t.Errorf("six names share %d colours", len(colors))
	}
	if got := looks(t)["noneascii"].CategoryMark("alpha"); got != "#" {
		t.Errorf("ASCII category mark = %q", got)
	}
}

func TestEveryGlyphHasBothForms(t *testing.T) {
	uni, ascii := Styles{charset: CharsetUnicode}, Styles{}
	for g := Glyph(0); g < glyphCount; g++ {
		if uni.Glyph(g) == "" || ascii.Glyph(g) == "" {
			t.Errorf("glyph %d lacks a form", g)
		}
		for _, r := range ascii.Glyph(g) {
			if r > 0x7e {
				t.Errorf("glyph %d: ASCII form %q is not ASCII", g, ascii.Glyph(g))
			}
		}
	}
	if uni.Glyph(glyphCount) != "" {
		t.Error("an unknown glyph has a form")
	}
}

// A painting theme gives every cell the ground colour: each line opens with it, every reset is followed by
// it, and every line is padded to the width.
func TestPaintCoversEveryCell(t *testing.T) {
	st := looks(t)["dark"]
	prefix := st.canvasPrefix()
	if prefix == "" || !st.Paints() {
		t.Fatal("the dark theme does not paint")
	}
	view := strings.Join(st.Panel("T", "", []string{st.Render(StyleOK, "ok")}, 20, 3, true), "\n") + "\n" + st.Status(StatusWarn, "w")
	lines := strings.Split(st.Paint(view, 30, 6), "\n")
	if len(lines) != 6 {
		t.Fatalf("%d lines, want 6", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 30 {
			t.Errorf("line %d is %d cells", i, w)
		}
		if !strings.HasPrefix(line, prefix) {
			t.Errorf("line %d does not open with the ground colours", i)
		}
		for _, loc := range sgrSequence.FindAllStringIndex(line, -1) {
			if seq := line[loc[0]:loc[1]]; resetsColor(seq) && loc[1] != len(line) && !strings.HasPrefix(line[loc[1]:], prefix) {
				t.Errorf("line %d: reset %q at %d is not followed by the ground colours", i, seq, loc[0])
			}
		}
	}
	for _, look := range []string{"terminal", "plain16", "mono", "noneascii"} {
		if got := looks(t)[look].Paint(view, 30, 6); got != view {
			t.Errorf("%s paints the screen", look)
		}
	}
}

func TestResetsColor(t *testing.T) {
	for seq, want := range map[string]bool{
		"\x1b[0m": true, "\x1b[m": true, "\x1b[1;49m": true, "\x1b[39m": true,
		"\x1b[1m": false, "\x1b[38;5;0m": false, "\x1b[48;2;0;0;0m": false, "\x1b[38;2;1;2;3;0m": true,
	} {
		if got := resetsColor(seq); got != want {
			t.Errorf("resetsColor(%q) = %v, want %v", seq, got, want)
		}
	}
}

// Without colour no widget names a colour; without a terminal none writes an escape sequence at all.
func TestWidgetsWithoutColor(t *testing.T) {
	for _, look := range []string{"mono", "noneascii"} {
		st := looks(t)[look]
		tabs, _ := st.TabStrip([]string{"a", "b"}, 0, true)
		out := strings.Join([]string{
			strings.Join(st.Panel("T", "n", []string{"x"}, 12, 4, true), "\n"),
			st.Status(StatusOK, "ok"), st.Message(MessageError, "error: x", 20), st.Row("r", 10, true), tabs,
			st.Button(ButtonPrimary, "b", false), st.TickBox(true), st.CategoryMark("c"), st.TitleLine("t", "n", 20),
		}, "\n")
		noColorSequence(t, look, out)
		if look == "noneascii" && strings.Contains(out, "\x1b") {
			t.Errorf("%s writes escape sequences: %q", look, out)
		}
	}
}

// The goldens fix the look of a small screen byte for byte under each look. Run with -update to rewrite them.
func TestGoldenScreens(t *testing.T) {
	for _, look := range []string{"dark", "light256", "terminal", "plain16", "mono", "noneascii"} {
		t.Run(look, func(t *testing.T) {
			st := looks(t)[look]
			tabs, _ := st.TabStrip([]string{"Overview", "Items", "|", "Settings"}, 1, true)
			lines := []string{st.TitleLine("Example", "v0.0.0", 40), tabs}
			lines = append(lines, st.Panel("Items", "2 of 3", []string{
				st.Row(st.CategoryMark("alpha")+" alpha  "+st.Status(StatusOK, "ready"), 34, true),
				st.Row(st.CategoryMark("beta")+" beta   "+st.Status(StatusFail, "stopped"), 34, false),
				st.Row(st.TickBox(true)+" "+st.Render(StyleHint, "a very long description that is cut"), 34, false),
			}, 40, 5, true)...)
			lines = append(lines, st.Button(ButtonNormal, "Cancel", false)+" "+st.Button(ButtonDanger, "Delete", true),
				st.Message(MessageWarning, "warning: two items share one name", 40))
			got := st.Paint(strings.Join(lines, "\n"), 40, 10) + "\n"
			golden := filepath.Join("testdata", "screen."+look+".golden")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("screen differs from %s:\ngot:\n%s\nwant:\n%s", golden, got, want)
			}
		})
	}
}

// Untrusted text in a widget argument never reaches the terminal as a control sequence.
func TestWidgetsEscapeControlCharacters(t *testing.T) {
	hostile := "a\x1b[2J\x1b]8;;x\x07b\rc\u009bd‮e"
	if got := Escape(hostile); strings.ContainsAny(got, "\x1b\x07\r\u009b‮") || !strings.Contains(got, `\x1b[2J`) {
		t.Errorf("Escape = %q", got)
	}
	if got := Escape("plain ✓ text 🏠"); got != "plain ✓ text 🏠" {
		t.Errorf("Escape changes printable text: %q", got)
	}
	st := looks(t)["noneascii"]
	tabs, _ := st.TabStrip([]string{hostile}, 0, false)
	for name, out := range map[string]string{
		"panel":   strings.Join(st.Panel(hostile, hostile, nil, 60, 3, false), "\n"),
		"status":  st.Status(StatusOK, hostile),
		"message": st.Message(MessageError, hostile+"\n"+hostile, 80),
		"title":   st.TitleLine(hostile, hostile, 80),
		"button":  st.Button(ButtonDanger, hostile, true),
		"tabs":    tabs,
	} {
		if strings.ContainsAny(out, "\x1b\x07\r\u009b‮") {
			t.Errorf("%s passes a control character: %q", name, out)
		}
	}
	if got := strings.Count(st.Message(MessageStatus, "one\ntwo", 80), "\n"); got != 1 {
		t.Errorf("a line break in a message gives %d breaks", got)
	}
}

// Escape masks private-use characters, so a Nerd Font glyph is drawn only as an icon that passed CheckIcon.
func TestEscapeMasksPrivateUseCharacters(t *testing.T) {
	if got := Escape("a\uf489b\U000f0219"); got != `a\uf489b\U000f0219` {
		t.Errorf("Escape: %q", got)
	}
}

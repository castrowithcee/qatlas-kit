package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func textKeys() TextViewKeys {
	return TextViewKeys{Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown", First: "first", Last: "last",
		Search: "search", Enter: "enter", Back: "back", NextMatch: ActionLogNextMatch, PrevMatch: ActionLogPrevMatch,
		Wrap: ActionLogWrap, Numbers: ActionTextNumbers, Reveal: ActionTextReveal}
}

func textKeymap(t *testing.T) *Keymap {
	t.Helper()
	actions := append(append(sampleActions(), LogViewActions()...), TextViewActions()...)
	k, err := NewKeymap(actions, CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newText(t *testing.T, spec TextViewSpec, lang i18n.Lang) *TextView {
	t.Helper()
	keys := textKeymap(t)
	spec.Keys = textKeys()
	v, err := NewTextView(spec, keys, newTranslator(t, lang, nil, keys))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (v *TextView) typeQuery(text string) {
	v.Update(typed('/'))
	for _, r := range text {
		v.Update(typed(r))
	}
}

func textRows(t *testing.T, v *TextView, look string, w, h int) []string {
	t.Helper()
	out, _ := v.View(looks(t)[look], w, h)
	return strings.Split(out, "\n")
}

func textNumbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "row %03d\n", i)
	}
	return b.String()
}

// mark is the span of the first occurrence of sub in line i of text.
func mark(text string, line int, sub string, role TextRole, masked bool) TextSpan {
	l := strings.Split(text, "\n")[line]
	at := strings.Index(l, sub)
	if at < 0 {
		panic("no " + sub + " in " + l)
	}
	return TextSpan{Start: at, End: at + len(sub), Role: role, Masked: masked}
}

func TestTextViewActionsFitATable(t *testing.T) {
	keys := textKeymap(t)
	for _, a := range TextViewActions() {
		if _, ok := keys.KeyLabel(a.ID); !ok {
			t.Errorf("%s is not in the table", a.ID)
		}
	}
	if err := CheckCatalogs(ModuleCatalog()); err != nil {
		t.Fatal(err)
	}
}

func TestNewTextViewRejectsWhatCannotWork(t *testing.T) {
	keys := textKeymap(t)
	text := newTranslator(t, i18n.English, nil, keys)
	cases := []struct {
		name string
		edit func(*TextViewSpec)
		want string
	}{
		{"negative lines", func(s *TextViewSpec) { s.MaxLines = -1 }, "cannot be negative"},
		{"lines over the limit", func(s *TextViewSpec) { s.MaxLines = TextViewMaxLines + 1 }, "the most is"},
		{"negative line length", func(s *TextViewSpec) { s.MaxLineRunes = -1 }, "cannot be negative"},
		{"line length over the limit", func(s *TextViewSpec) { s.MaxLineRunes = TextViewMaxLineRunes + 1 }, "the most is"},
		{"unknown action", func(s *TextViewSpec) { s.Keys.Wrap = "fold" }, "no action of the key table"},
		{"missing key", func(s *TextViewSpec) { s.Keys.Numbers = "" }, "numbers key is missing"},
		{"shared action", func(s *TextViewSpec) { s.Keys.Numbers = ActionLogWrap }, "also the wrap key"},
	}
	for _, tt := range cases {
		spec := TextViewSpec{Keys: textKeys()}
		tt.edit(&spec)
		if _, err := NewTextView(spec, keys, text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
	for _, edit := range []func(*TextViewSpec){
		func(s *TextViewSpec) { s.MaxLines, s.MaxLineRunes = TextViewMaxLines, TextViewMaxLineRunes },
		func(s *TextViewSpec) { s.Keys.First, s.Keys.Last, s.Keys.Reveal = "", "", "" },
	} {
		spec := TextViewSpec{Keys: textKeys()}
		edit(&spec)
		if _, err := NewTextView(spec, keys, text); err != nil {
			t.Errorf("a valid spec is refused: %v", err)
		}
	}
	if _, err := NewTextView(TextViewSpec{Keys: textKeys()}, nil, text); err == nil {
		t.Error("a missing key table is accepted")
	}
}

func TestTextViewShowsNumbersAndScrolls(t *testing.T) {
	v := newText(t, TextViewSpec{Numbers: true}, i18n.English)
	v.SetText(textNumbered(50), nil)
	rows := textRows(t, v, "noneascii", 40, 6)
	if !strings.Contains(rows[0], " 1 row 001") || !strings.Contains(rows[4], " 5 row 005") || !strings.Contains(rows[5], "lines 1-5 of 50") {
		t.Fatalf("first page:\n%s", strings.Join(rows, "\n"))
	}
	v.Update(keyOf("pgdown"))
	rows = textRows(t, v, "noneascii", 40, 6)
	if !strings.Contains(rows[0], "row 005") {
		t.Errorf("a page down keeps the last row of the page:\n%s", strings.Join(rows, "\n"))
	}
	v.Act(ActionTextNumbers)
	if rows = textRows(t, v, "noneascii", 40, 6); strings.Contains(rows[0], "5 row") || !strings.Contains(rows[0], "row 005") {
		t.Errorf("numbers do not switch off:\n%s", strings.Join(rows, "\n"))
	}
	v.Update(keyOf("end"))
	rows = textRows(t, v, "noneascii", 40, 6)
	if !strings.Contains(rows[4], "row 050") || !strings.Contains(rows[5], "lines 46-50 of 50") {
		t.Errorf("end:\n%s", strings.Join(rows, "\n"))
	}
	v.Update(keyOf("home"))
	v.Wheel(2)
	if rows = textRows(t, v, "noneascii", 40, 6); !strings.Contains(rows[0], "row 007") {
		t.Errorf("two notches of the wheel:\n%s", strings.Join(rows, "\n"))
	}
	v.Wheel(-100)
	if rows = textRows(t, v, "noneascii", 40, 6); !strings.Contains(rows[0], "row 001") {
		t.Errorf("the wheel stops at the top:\n%s", strings.Join(rows, "\n"))
	}
}

func TestTextViewNumbersGiveWayInANarrowView(t *testing.T) {
	v := newText(t, TextViewSpec{Numbers: true}, i18n.English)
	v.SetText(textNumbered(5), nil)
	rows := textRows(t, v, "noneascii", 14, 4)
	if strings.Contains(rows[0], "1 row") || !strings.Contains(rows[0], "row 001") || !v.LineNumbers() {
		t.Errorf("a narrow view keeps its numbers or loses the switch:\n%s", strings.Join(rows, "\n"))
	}
}

func TestTextViewWrapsAndClips(t *testing.T) {
	long := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma"
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText(long+"\nnext", nil)
	rows := textRows(t, v, "noneascii", 30, 8)
	if !strings.Contains(rows[0], "~") || strings.Contains(rows[1], "sigma") || !strings.HasPrefix(rows[1], "  next") {
		t.Errorf("a clipped line ends in the ellipsis:\n%s", strings.Join(rows, "\n"))
	}
	v.Act(ActionLogWrap)
	rows = textRows(t, v, "noneascii", 30, 8)
	joined := strings.Join(rows[:4], "")
	if !strings.Contains(strings.ReplaceAll(joined, " ", ""), "sigma") || !strings.Contains(strings.Join(rows, "\n"), "next") {
		t.Errorf("a wrapped line keeps all its text:\n%s", strings.Join(rows, "\n"))
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w != 30 {
			t.Errorf("row %q is %d cells wide", row, w)
		}
	}
}

// Control characters and escape sequences are escaped in every drawing and no switch, key or style leaves
// them raw.
func TestTextViewEscapingCannotBeSwitchedOff(t *testing.T) {
	hostile := "\x1b[31mred\x1b[0m \x1b]0;title\x07 nul\x00 cr\rend del\x7f c1\u009b31m bidi\u202eabc \xff\xfe bell\a\ttab"
	for _, look := range []string{"dark", "noneascii", "mono", "asciidark"} {
		for _, wrap := range []bool{false, true} {
			for _, numbers := range []bool{false, true} {
				v := newText(t, TextViewSpec{Wrap: wrap, Numbers: numbers}, i18n.English)
				v.SetText(hostile+"\n"+hostile, [][]TextSpan{{{Start: 0, End: 4, Role: TextRoleKey}}})
				for _, act := range []ActionID{"", ActionLogWrap, ActionTextNumbers, ActionTextReveal} {
					v.Act(act)
					v.SetRevealed(true)
					out, _ := v.View(looks(t)[look], 100, 10)
					// A style the look adds is a CSI sequence of SGR; nothing of the text may add another kind.
					plain := ansi.Strip(out)
					for _, r := range plain {
						if r == '\n' {
							continue
						}
						if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x202e {
							t.Fatalf("%s: raw %U in %q", look, r, plain)
						}
					}
					for _, seq := range []string{"\x1b]", "\x1b[31mred", "\x07", "\x1b[0m red"} {
						if strings.Contains(out, seq) {
							t.Fatalf("%s: the output holds %q:\n%q", look, seq, out)
						}
					}
				}
			}
		}
	}
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText("a\x1b[31mb\tc", nil)
	if got := ansi.Strip(textRows(t, v, "noneascii", 40, 3)[0]); !strings.Contains(got, `a\x1b[31mb    c`) {
		t.Errorf("escaped line = %q", got)
	}
	// The search text is escaped the same way: it finds the escaped form and nothing raw.
	v.typeQuery("\\x1b")
	if len(v.hits) != 1 {
		t.Errorf("the escaped form is not found: %d hits", len(v.hits))
	}
}

func TestTextViewDrawsRolesInTheSyntaxColors(t *testing.T) {
	text := "name = \"sample\" # a comment\nport = 8080\nif ready"
	spans := [][]TextSpan{
		{mark(text, 0, "name", TextRoleKey, false), mark(text, 0, `"sample"`, TextRoleString, false), mark(text, 0, "# a comment", TextRoleComment, false)},
		{mark(text, 1, "8080", TextRoleNumber, false)},
		{mark(text, 2, "if", TextRoleKeyword, false)},
	}
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText(text, spans)
	st := looks(t)["dark"]
	out, _ := v.View(st, 60, 6)
	for style, word := range map[TextStyle]string{StyleSyntaxKey: "name", StyleSyntaxString: `"sample"`, StyleSyntaxComment: "# a comment",
		StyleSyntaxNumber: "8080", StyleSyntaxKeyword: "if"} {
		if want := st.Render(style, word); !strings.Contains(out, want) {
			t.Errorf("%q is not drawn as %q in %q", word, want, out)
		}
	}
	// Without colour the roles leave plain text, in every profile that has none.
	for _, look := range []string{"mono", "noneascii"} {
		out, _ := v.View(looks(t)[look], 60, 6)
		noColorSequence(t, look, out)
		if rows := strings.Split(ansi.Strip(out), "\n"); !strings.Contains(rows[0], `name = "sample" # a comment`) {
			t.Errorf("%s: first row %q", look, rows[0])
		}
		if look == "noneascii" && strings.Contains(out, "\x1b") {
			t.Errorf("no profile still styles: %q", out)
		}
	}
	// An application theme without syntax roles draws them in the text colour.
	p := DarkTheme("x").Profiles[ProfileTrueColor]
	p.SyntaxKey, p.SyntaxString, p.SyntaxNumber, p.SyntaxComment, p.SyntaxKeyword = "", "", "", "", ""
	set, err := NewThemeSet([]Theme{{Name: "bare", Profiles: map[ColorProfile]Palette{ProfileTrueColor: p}}})
	if err != nil {
		t.Fatal(err)
	}
	bare, _ := set.Resolve("bare", ProfileTrueColor, CharsetUnicode, nil)
	for _, s := range []TextStyle{StyleSyntaxKey, StyleSyntaxString, StyleSyntaxNumber, StyleSyntaxComment, StyleSyntaxKeyword} {
		if bare.Render(s, "x") != bare.Render(StyleText, "x") {
			t.Errorf("style %d without a role is not the text style", s)
		}
	}
}

// A span the view cannot place exactly never panics and never shows more than the application masked.
func TestTextViewHandlesInvalidSpans(t *testing.T) {
	const line = "key=sécret-välue ok"
	at := func(s string) int { return strings.Index(line, s) }
	cases := []struct {
		name  string
		spans []TextSpan
		want  string // the line as drawn
	}{
		{"none", nil, line},
		{"empty", []TextSpan{{Start: 4, End: 4, Masked: true}}, line},
		{"reversed", []TextSpan{{Start: 10, End: 4, Masked: true}}, line},
		{"outside", []TextSpan{{Start: 100, End: 200, Masked: true}, {Start: -9, End: -1, Masked: true}}, line},
		{"clipped to the line", []TextSpan{{Start: at("ok"), End: 999, Masked: true}}, "key=sécret-välue ******"},
		{"negative start", []TextSpan{{Start: -5, End: 3, Masked: true}}, "******=sécret-välue ok"},
		{"inside a character", []TextSpan{{Start: at("é") + 1, End: at("t-"), Masked: true}}, "key=s******t-välue ok"},
		{"end inside a character", []TextSpan{{Start: at("-"), End: at("ä") + 1, Masked: true}}, "key=sécret******lue ok"},
		{"overlapping masks", []TextSpan{{Start: 4, End: 8, Masked: true}, {Start: 6, End: 12, Masked: true}}, "key=******välue ok"},
		{"a role over a mask", []TextSpan{{Start: 0, End: 20, Role: TextRoleKey}, {Start: 4, End: 8, Masked: true}}, "key=******ret-välue ok"},
		{"unknown role", []TextSpan{{Start: 0, End: 3, Role: 99}, {Start: 4, End: 8, Role: 200, Masked: true}}, "key=******ret-välue ok"},
	}
	for _, tt := range cases {
		v := newText(t, TextViewSpec{}, i18n.English)
		v.SetText(line, [][]TextSpan{tt.spans})
		got := strings.TrimRight(ansi.Strip(textRows(t, v, "asciidark", 60, 2)[0]), " ")
		got = strings.TrimSpace(got)
		if got != tt.want {
			t.Errorf("%s: drawn %q, want %q", tt.name, got, tt.want)
		}
	}
	// More spans than lines, and spans for a line that was cut away, are ignored.
	v := newText(t, TextViewSpec{MaxLines: 1}, i18n.English)
	v.SetText("a\nb", [][]TextSpan{nil, {{Start: 0, End: 1, Masked: true}}, {{Start: 0, End: 1}}})
	if v.HasMasked() {
		t.Error("a span of a line past the limit counts")
	}
}

// secrets is invented text; none of it is a real credential.
const maskText = "name = demo\nsession = hunter2-example-value\n-----BEGIN EXAMPLE-----\nQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo=\n-----END EXAMPLE-----\nplace = home"

func maskSpans() [][]TextSpan {
	return [][]TextSpan{nil,
		{mark(maskText, 1, "hunter2-example-value", TextRoleString, true)},
		nil,
		{{Start: 0, End: len("QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="), Masked: true}},
	}
}

var maskSecrets = []string{"hunter2", "example-value", "QUJDREVG", "SktMTU5P", "VVldYWVo"}

// A masked section never shows its content, in any drawing: wrapped or clipped, narrow or wide, with or
// without colour or numbers, while searching, and in the status row and the key hints.
func TestTextViewNeverShowsAMaskedSection(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		for _, look := range []string{"dark", "noneascii", "mono", "asciidark", "plain16", "light256"} {
			for _, width := range []int{12, 20, 30, 60, 120} {
				for _, wrap := range []bool{false, true} {
					v := newText(t, TextViewSpec{Wrap: wrap, Numbers: true}, lang)
					v.SetText(maskText, maskSpans())
					for _, query := range []string{"", "hunter", "example", "session", "-value", "QUJD", "BEGIN"} {
						v.Act("back")
						if query != "" {
							v.typeQuery(query)
							v.Update(keyOf("enter"))
						}
						out, targets := v.View(looks(t)[look], width, 12)
						plain := ansi.Strip(out)
						for _, secret := range maskSecrets {
							if strings.Contains(plain, secret) || strings.Contains(out, secret) {
								t.Fatalf("%s %s w%d wrap %v query %q: %q shows %q", lang, look, width, wrap, query, plain, secret)
							}
						}
						_ = targets
					}
				}
			}
		}
	}
}

func TestTextViewSearchFindsNothingInAMaskedSection(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText(maskText, maskSpans())
	for query, want := range map[string]int{
		"hunter2": 0, "example-value": 0, "hunter": 0, "session": 1, "session = ": 1, "= hunter": 0, "ue": 0,
		"QUJD": 0, "demo": 1, "BEGIN": 1, "EXAMPLE": 2,
	} {
		v.Act("back")
		v.typeQuery(query)
		if got := len(v.hits); got != want {
			t.Errorf("query %q: %d lines hit while masked, want %d", query, got, want)
		}
	}
	// The position and the match info do not give a hit away either.
	v.Act("back")
	v.typeQuery("hunter2")
	v.Update(keyOf("enter"))
	if v.hasCur || !strings.Contains(strings.Join(textRows(t, v, "noneascii", 80, 8), "\n"), "no matches") {
		t.Error("a hidden hit moves the view or is counted")
	}
	// Revealed, the same text is found; hidden again, it is not.
	v.SetRevealed(true)
	if len(v.hits) != 1 {
		t.Errorf("revealed: %d hits", len(v.hits))
	}
	v.SetRevealed(false)
	if len(v.hits) != 0 {
		t.Errorf("hidden again: %d hits", len(v.hits))
	}
}

func TestTextViewRevealsOnTheApplicationsCommand(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText(maskText, maskSpans())
	show := func() string { return ansi.Strip(strings.Join(textRows(t, v, "noneascii", 100, 10), "\n")) }
	if !v.HasMasked() || v.Revealed() || strings.Contains(show(), "hunter2") {
		t.Fatal("the text starts revealed")
	}
	if !strings.Contains(show(), "session = ******") || !strings.Contains(show(), "V reveal") {
		t.Errorf("hidden view:\n%s", show())
	}
	// The key only asks; the application decides.
	if !v.Update(typed('V')) || v.Revealed() || !v.TakeRevealRequest() || v.TakeRevealRequest() {
		t.Fatal("the key must ask once and reveal nothing")
	}
	v.SetRevealed(true)
	if out := show(); !strings.Contains(out, "session = hunter2-example-value") || !strings.Contains(out, "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo=") || !strings.Contains(out, "V hide") {
		t.Errorf("revealed view:\n%s", out)
	}
	// Hiding needs no permission.
	if !v.Act(ActionTextReveal) || v.Revealed() || strings.Contains(show(), "hunter2") {
		t.Error("the key does not hide")
	}
	// New text starts hidden, whatever the state was.
	v.SetRevealed(true)
	v.SetText(maskText, maskSpans())
	if v.Revealed() || strings.Contains(show(), "hunter2") {
		t.Error("a new text starts revealed")
	}
	// Without masked sections the key does nothing and there is no button.
	v.SetText("plain", nil)
	if v.Act(ActionTextReveal) || strings.Contains(show(), "reveal") {
		t.Error("reveal is offered without masked sections")
	}
}

func TestTextViewSearchHighlightsAndStepsThroughMatches(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "line %02d%s\n", i, map[bool]string{true: " needle", false: ""}[i%10 == 4])
	}
	v.SetText(b.String(), nil)
	v.typeQuery("NEEDLE")
	if len(v.hits) != 4 {
		t.Fatalf("%d hits, want 4 and a match without case", len(v.hits))
	}
	v.Update(keyOf("enter"))
	rows := textRows(t, v, "noneascii", 80, 8)
	if !v.hasCur || v.cur != 4 || !strings.Contains(strings.Join(rows, "\n"), "match 1/4") {
		t.Errorf("enter does not go to the first match:\n%s", strings.Join(rows, "\n"))
	}
	v.Act(ActionLogNextMatch)
	v.Act(ActionLogNextMatch)
	v.Act(ActionLogNextMatch)
	v.Act(ActionLogNextMatch)
	if v.cur != 4 {
		t.Errorf("the matches do not wrap: %d", v.cur)
	}
	v.Act(ActionLogPrevMatch)
	if v.cur != 34 {
		t.Errorf("previous wraps to %d", v.cur)
	}
	st := looks(t)["dark"]
	out, _ := v.View(st, 80, 8)
	if !strings.Contains(out, st.Render(StyleActiveRow, "needle")) {
		t.Errorf("the current match is not drawn as the selection:\n%q", out)
	}
	if !v.Update(keyOf("esc")) || v.Query() != "" || len(v.hits) != 0 {
		t.Error("back does not clear the search")
	}
	if v.Update(keyOf("esc")) {
		t.Error("back is taken without a search")
	}
}

func TestTextViewReportsItsLimits(t *testing.T) {
	v := newText(t, TextViewSpec{MaxLines: 3, MaxLineRunes: 10}, i18n.English)
	v.SetText("0123456789abc\nshort\nlast\nfourth\nfifth\n", nil)
	if !v.Truncated() || v.Len() != 3 {
		t.Fatalf("truncated %v, %d lines", v.Truncated(), v.Len())
	}
	rows := textRows(t, v, "noneascii", 100, 6)
	text := strings.Join(rows, "\n")
	for _, want := range []string{"0123456789 [cut]", "first 3 of 5 lines", "1 long lines cut"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is missing:\n%s", want, text)
		}
	}
	if strings.Contains(text, "fourth") || strings.Contains(text, "abc") {
		t.Errorf("text past a limit is shown:\n%s", text)
	}
	// Narrow, the notice shrinks to the mark but never goes before the position does.
	if got := strings.Join(textRows(t, v, "noneascii", 30, 6), "\n"); !strings.Contains(got, "[cut]") {
		t.Errorf("no sign of the cut in a narrow row:\n%s", got)
	}
	v.SetText("fits\nfits too", nil)
	if v.Truncated() || strings.Contains(strings.Join(textRows(t, v, "noneascii", 100, 6), "\n"), "[cut]") {
		t.Error("a text within the limits is reported cut")
	}
	// A masked section is cut with its line, and what stays is still masked.
	v = newText(t, TextViewSpec{MaxLineRunes: 8}, i18n.English)
	v.SetText("abcdefghijkl", [][]TextSpan{{{Start: 4, End: 12, Masked: true}}})
	if got := ansi.Strip(strings.Join(textRows(t, v, "noneascii", 60, 3), "\n")); strings.Contains(got, "efgh") || !strings.Contains(got, "abcd******") {
		t.Errorf("cut masked line:\n%s", got)
	}
}

func TestTextViewEmptyAndTinyAreas(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	if rows := textRows(t, v, "noneascii", 40, 3); !strings.Contains(rows[0], "No text.") {
		t.Errorf("an empty view says nothing: %q", rows)
	}
	v.SetText(maskText, maskSpans())
	for _, size := range [][2]int{{0, 0}, {0, 5}, {5, 0}, {-3, -3}, {1, 1}, {2, 2}, {5, 3}} {
		for _, wrap := range []bool{false, true} {
			if wrap != v.Wrapping() {
				v.Act(ActionLogWrap)
			}
			v.View(looks(t)["dark"], size[0], size[1])
		}
	}
}

// Every button of the status row works by click as its key does, and the row keeps all it shows.
func TestTextViewButtonsWorkByClick(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText(maskText, maskSpans())
	v.typeQuery("e")
	v.Update(keyOf("enter"))
	out, targets := v.View(looks(t)["noneascii"], 120, 8)
	row := strings.Split(out, "\n")[7]
	byAction := map[ActionID]Target{}
	for _, tg := range targets[1:] {
		byAction[tg.Action] = tg
		if tg.Rect.Y != 7 || tg.Zone != ZoneRows {
			t.Errorf("target %+v is not on the status row", tg)
		}
	}
	for _, id := range []ActionID{"search", ActionTextReveal, ActionLogNextMatch, ActionLogPrevMatch, ActionLogWrap, ActionTextNumbers} {
		if _, ok := byAction[id]; !ok {
			t.Fatalf("no button for %s in %q", id, row)
		}
	}
	clickButton := func(id ActionID) bool {
		t.Helper()
		_, tg := v.View(looks(t)["noneascii"], 120, 8)
		for _, c := range tg[1:] {
			if c.Action == id {
				res := NewMouseMap(tg, true).Handle(click(c.Rect.X, c.Rect.Y))
				return v.Click(res)
			}
		}
		t.Fatalf("no button for %s", id)
		return false
	}
	wrap, numbers := v.Wrapping(), v.LineNumbers()
	if !clickButton(ActionLogWrap) || v.Wrapping() == wrap {
		t.Error("the wrap button does nothing")
	}
	if !clickButton(ActionTextNumbers) || v.LineNumbers() == numbers {
		t.Error("the numbers button does nothing")
	}
	cur := v.cur
	if !clickButton(ActionLogNextMatch) || v.cur == cur {
		t.Error("the next button does nothing")
	}
	if !clickButton(ActionLogPrevMatch) || v.cur != cur {
		t.Error("the previous button does nothing")
	}
	if !clickButton(ActionTextReveal) || !v.TakeRevealRequest() || v.Revealed() {
		t.Error("the reveal button must ask and reveal nothing")
	}
	v.SetRevealed(true)
	if !clickButton(ActionTextReveal) || v.Revealed() {
		t.Error("the reveal button does not hide")
	}
	if !clickButton("search") || !v.Searching() {
		t.Error("the search button does not start the search")
	}
	if v.Click(MouseResult{}) {
		t.Error("a click on nothing is handled")
	}
}

// The status row keeps to its width and keeps its search and position as the width shrinks.
func TestTextViewStatusRowFitsEveryWidth(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		v := newText(t, TextViewSpec{MaxLines: 4}, lang)
		v.SetText(maskText, maskSpans())
		v.typeQuery("home")
		v.Update(keyOf("enter"))
		for w := 1; w <= 130; w++ {
			out, targets := v.View(looks(t)["noneascii"], w, 5)
			for _, row := range strings.Split(out, "\n") {
				if got := ansi.StringWidth(row); got != w {
					t.Fatalf("%s width %d: row %q is %d wide", lang, w, row, got)
				}
			}
			for _, tg := range targets {
				if tg.Rect.X < 0 || tg.Rect.X+tg.Rect.Width > w {
					t.Fatalf("%s width %d: target %+v leaves the row", lang, w, tg)
				}
			}
		}
		v.Update(typed('/'))
		for w := 1; w <= 60; w++ {
			out, _ := v.View(looks(t)["noneascii"], w, 4)
			for _, row := range strings.Split(out, "\n") {
				if got := ansi.StringWidth(row); got != w {
					t.Fatalf("%s searching, width %d: row %q is %d wide", lang, w, row, got)
				}
			}
		}
	}
}

func TestTextViewPasteIntoTheSearch(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText("disk full\ndisk ok", nil)
	if v.Paste("x") {
		t.Fatal("a paste is taken without a search")
	}
	v.Update(typed('/'))
	if !v.Paste("disk\nfull") || v.Query() != "disk full" || len(v.hits) != 1 {
		t.Errorf("paste: %q, %d hits", v.Query(), len(v.hits))
	}
	if v.Input() != InputText {
		t.Error("typing a search is not a text input")
	}
	v.Update(keyOf("enter"))
	if v.Input() != InputNavigation {
		t.Error("enter leaves the input")
	}
}

func TestTextViewHintsFollowTheState(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText(maskText, maskSpans())
	has := func(id ActionID) bool {
		for _, h := range v.Hints() {
			if h.Action == id {
				return true
			}
		}
		return false
	}
	if !has("search") || !has(ActionTextReveal) || has(ActionLogNextMatch) {
		t.Error("hints before a search")
	}
	v.typeQuery("home")
	if has(ActionTextReveal) || !has("enter") {
		t.Error("hints while typing")
	}
	v.Update(keyOf("enter"))
	if !has(ActionLogNextMatch) || !has("back") {
		t.Error("hints with a search")
	}
}

// Keys in the sample table that a text view reads reach it as typed keys.
func TestTextViewKeysReachTheView(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText(textNumbered(30), nil)
	textRows(t, v, "noneascii", 40, 6)
	for _, k := range []tea.KeyPressMsg{typed('w'), typed('#')} {
		if !v.Update(k) {
			t.Errorf("key %v is not taken", k)
		}
	}
	if !v.Wrapping() || !v.LineNumbers() {
		t.Error("the switches did not turn")
	}
	if v.Update(typed('z')) {
		t.Error("an unbound key is taken")
	}
}

// Goldens fix the look under each variant byte for byte. Run with -update to rewrite them.
func TestGoldenTextView(t *testing.T) {
	variants := []struct {
		name, look string
		lang       i18n.Lang
		w, h       int
	}{
		{"narrow", "noneascii", i18n.English, 40, 12},
		{"wide", "noneascii", i18n.English, 110, 12},
		{"wide.dark", "dark", i18n.English, 110, 12},
		{"wide.de", "noneascii", i18n.German, 110, 12},
	}
	const config = "# service settings\nname = \"demo\"\nport = 8080\nenabled = true\nsession = \"hunter2-example-value\"\nnotes = \"a rather long note that does not fit into a narrow view and has to be wrapped or clipped\"\ntimeout = 30\n"
	configSpans := [][]TextSpan{
		{mark(config, 0, "# service settings", TextRoleComment, false)},
		{mark(config, 1, "name", TextRoleKey, false), mark(config, 1, `"demo"`, TextRoleString, false)},
		{mark(config, 2, "port", TextRoleKey, false), mark(config, 2, "8080", TextRoleNumber, false)},
		{mark(config, 3, "enabled", TextRoleKey, false), mark(config, 3, "true", TextRoleKeyword, false)},
		{mark(config, 4, "session", TextRoleKey, false), mark(config, 4, `"hunter2-example-value"`, TextRoleString, true)},
		{mark(config, 5, "notes", TextRoleKey, false), mark(config, 5, `"a rather long note that does not fit into a narrow view and has to be wrapped or clipped"`, TextRoleString, false)},
		{mark(config, 6, "timeout", TextRoleKey, false), mark(config, 6, "30", TextRoleNumber, false)},
	}
	cases := []struct {
		name  string
		build func(*testing.T, *TextView)
	}{
		{"short", func(t *testing.T, v *TextView) { v.SetText("first line\nsecond line\n\nfourth line\n", nil) }},
		{"long.wrap", func(t *testing.T, v *TextView) {
			v.SetText(config, nil)
			v.Act(ActionLogWrap)
			v.Act(ActionTextNumbers)
		}},
		{"long.nowrap", func(t *testing.T, v *TextView) {
			v.SetText(config, nil)
			v.Act(ActionTextNumbers)
		}},
		{"search", func(t *testing.T, v *TextView) {
			v.SetText(config, nil)
			v.Act(ActionTextNumbers)
			v.typeQuery("o")
			v.Update(keyOf("enter"))
		}},
		{"roles", func(t *testing.T, v *TextView) {
			v.SetText(config, configSpans)
			v.Act(ActionTextNumbers)
			v.Act(ActionLogWrap)
		}},
		{"masked.hidden", func(t *testing.T, v *TextView) {
			v.SetText(config, configSpans)
			v.Act(ActionTextNumbers)
		}},
		{"masked.revealed", func(t *testing.T, v *TextView) {
			v.SetText(config, configSpans)
			v.Act(ActionTextNumbers)
			v.SetRevealed(true)
		}},
		{"limit", func(t *testing.T, v *TextView) {
			v.spec.MaxLines, v.spec.MaxLineRunes = 6, 40
			v.SetText(config, configSpans)
			v.Act(ActionTextNumbers)
		}},
	}
	for _, c := range cases {
		for _, vr := range variants {
			name := "textview." + c.name + "." + vr.name + ".golden"
			t.Run(name, func(t *testing.T) {
				v := newText(t, TextViewSpec{}, vr.lang)
				c.build(t, v)
				out, _ := v.View(looks(t)[vr.look], vr.w, vr.h)
				checkGolden(t, name, out+"\n")
			})
		}
	}
}

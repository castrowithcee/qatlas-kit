package tui

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// frameCatalogs are the invented texts of the frame tests. German gives the two ways of opening the same
// word, so they fold into one footer group there.
func frameCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {
			"ex.overview": "Overview", "ex.items": "Items", "ex.settings": "Settings",
			"ex.general": "General", "ex.advanced": "Advanced",
			"ex.help": "help", "ex.cmd": "cmd", "ex.back": "back", "ex.quit": "quit", "ex.save": "save",
			"ex.move": "move", "ex.open": "open", "ex.openarea": "areas", "ex.yes": "yes, remove it",
			"ex.no": "no, keep it", "ex.info": "info", "ex.type": "type to filter",
		},
		i18n.German: {
			"ex.overview": "Übersicht", "ex.items": "Einträge", "ex.settings": "Einstellungen",
			"ex.general": "Allgemein", "ex.advanced": "Erweitert",
			"ex.help": "Hilfe", "ex.cmd": "Befehle", "ex.back": "zurück", "ex.quit": "Ende", "ex.save": "speichern",
			"ex.move": "bewegen", "ex.open": "öffnen", "ex.openarea": "öffnen", "ex.yes": "ja, entfernen",
			"ex.no": "nein, behalten", "ex.info": "Info", "ex.type": "tippen zum Filtern",
		},
	}
}

func sampleAreas() []Area {
	return []Area{
		{ID: "overview", Title: "ex.overview"},
		{ID: "items", Title: "ex.items"},
		{ID: "settings", Title: "ex.settings", Tabs: []Tab{{ID: "general", Title: "ex.general"}, {ID: "advanced", Title: "ex.advanced"}}},
	}
}

// styledFrame is a frame with the styles every Render of a test draws with.
type styledFrame struct {
	*Frame
	styles Styles
}

func (f styledFrame) Render(spec FrameSpec) (string, []Target) { return f.Frame.Render(f.styles, spec) }

func sampleFrame(t *testing.T, st Styles, lang i18n.Lang) (styledFrame, *Keymap) {
	t.Helper()
	keys := sampleKeymap(t, st.Charset())
	return styledFrame{NewFrame(keys, newTranslator(t, lang, frameCatalogs(), keys)), st}, keys
}

func sampleSpec(width, height int) FrameSpec {
	return FrameSpec{
		Width: width, Height: height, Product: "example",
		Status: []StatusMark{{Kind: StatusOK, Text: "store open", Short: []string{"open"}}, {Text: "session off", Short: []string{"off"}}},
		Back:   &Hint{Action: "back", Label: "ex.back"},
		HeaderKeys: []Hint{
			{Action: "help", Label: "ex.help"}, {Action: "command", Label: "ex.cmd"},
		},
		Areas: sampleAreas(), ActiveArea: "settings", ActiveTab: "general", Focus: ZoneRows, Input: InputNavigation,
		Workspace: "first line\nsecond line",
		Footer: []Hint{
			{Action: "help", Label: "ex.help", Kind: HintAlways},
			{Action: "enter", Label: "ex.open", Priority: 3},
			{Action: "save", Label: "ex.save", Priority: 4},
			{Action: "quit", Label: "ex.quit", Priority: 6},
			{Action: "info", Label: "ex.info", Priority: 700},
			{Action: "up", Label: "ex.move", Kind: HintNavigation},
		},
		Position: "3/12",
	}
}

func plainRows(out string) []string { return strings.Split(ansi.Strip(out), "\n") }

// The frame reads from the top: header, tab line, sub-tabs where the area has them, rule, workspace, and the
// footer as the last line; every line is exactly as wide as the terminal at every size.
func TestFrameKeepsItsOrderAndSize(t *testing.T) {
	for look, st := range looks(t) {
		f, _ := sampleFrame(t, st, i18n.English)
		for width := 0; width <= 90; width += 3 {
			for height := 0; height <= 12; height++ {
				out, _ := f.Render(sampleSpec(width, height))
				if height == 0 {
					if out != "" {
						t.Fatalf("%s %dx0: %q", look, width, out)
					}
					continue
				}
				rows := strings.Split(out, "\n")
				if len(rows) != height {
					t.Fatalf("%s %dx%d: %d rows", look, width, height, len(rows))
				}
				for i, r := range rows {
					if w := ansi.StringWidth(r); w != width {
						t.Errorf("%s %dx%d: row %d is %d cells: %q", look, width, height, i, w, r)
					}
				}
				if width >= 60 && !strings.Contains(ansi.Strip(rows[height-1]), "help") {
					t.Errorf("%s %dx%d: the last row is no footer: %q", look, width, height, rows[height-1])
				}
			}
		}
	}
	st := looks(t)["noneascii"]
	f, _ := sampleFrame(t, st, i18n.English)
	rows := plainRows(first(f.Render(sampleSpec(80, 10))))
	switch {
	case !strings.HasPrefix(rows[0], "example") || !strings.Contains(rows[0], "[?] help  [:] cmd"):
		t.Errorf("header: %q", rows[0])
	case !strings.Contains(rows[1], "1 Overview") || !strings.Contains(rows[1], "[3 Settings]"):
		t.Errorf("tab line: %q", rows[1])
	case !strings.Contains(rows[2], "General") || !strings.Contains(rows[2], "Advanced"):
		t.Errorf("sub-tab line: %q", rows[2])
	case !strings.HasPrefix(rows[3], "----"):
		t.Errorf("rule: %q", rows[3])
	case !strings.HasPrefix(rows[4], "first line") || !strings.HasPrefix(rows[5], "second line"):
		t.Errorf("workspace: %q %q", rows[4], rows[5])
	}
	if r := f.Workspace(sampleSpec(80, 10)); r != (Rect{X: 0, Y: 4, Width: 80, Height: 5}) {
		t.Errorf("Workspace = %+v", r)
	}
	spec := sampleSpec(80, 10)
	spec.ActiveArea = "items"
	if r := f.Workspace(spec); r.Y != 3 || r.Height != 6 {
		t.Errorf("an area without sub-tabs has no sub-tab line: %+v", r)
	}
	if rows := plainRows(first(f.Render(spec))); !strings.HasPrefix(rows[2], "---") {
		t.Errorf("an area with one tab or none draws a sub-tab line: %q", rows[2])
	}
}

func first(s string, _ []Target) string { return s }

// footerOf is the footer rows of the frame spec draws, as the rows above and below it split them.
func footerOf(f styledFrame, spec FrameSpec) string {
	rows := plainRows(first(f.Render(spec)))
	ws := f.Workspace(spec)
	return strings.Join(rows[ws.Y+ws.Height:], "\n")
}

// A bar with the focus has "> " in front; the bar without it has blanks, so nothing moves.
func TestFocusedBarCarriesTheMarker(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := sampleSpec(80, 10)
	for zone, want := range map[FocusZone][2]string{ZoneTabs: {"> ", "  "}, ZoneSubtabs: {"  ", "> "}, ZoneRows: {"  ", "  "}} {
		spec.Focus = zone
		rows := plainRows(first(f.Render(spec)))
		if !strings.HasPrefix(rows[1], want[0]) || !strings.HasPrefix(rows[2], want[1]) {
			t.Errorf("focus %s:\n%s\n%s", zone, rows[1], rows[2])
		}
	}
}

// Where the tabs do not fit, the gaps close first, then the inactive tabs shrink to their digit; the active
// tab stays whole, and every tab keeps a target only where it is drawn.
func TestNarrowTabLineClosesGapsThenShrinksInactiveTabs(t *testing.T) {
	for look, st := range looks(t) {
		f, _ := sampleFrame(t, st, i18n.English)
		spec := sampleSpec(0, 10)
		spec.ActiveArea = "items"
		for _, c := range []struct {
			width int
			want  []string
		}{
			{80, []string{" 1 Overview ", " 3 Settings "}},
			{34, []string{"1 Overview", "3 Settings"}},
			{20, []string{" 1 ", " 3"}},
		} {
			spec.Width = c.width
			out, targets := f.Render(spec)
			line := plainRows(out)[1]
			for _, w := range c.want {
				if !strings.Contains(line, w) {
					t.Errorf("%s %d columns: %q lacks %q", look, c.width, line, w)
				}
			}
			if !strings.Contains(line, "2 Items") {
				t.Errorf("%s %d columns: the active tab is cut: %q", look, c.width, line)
			}
			digits := map[AreaID]string{"overview": "1", "items": "2", "settings": "3"}
			cells := []rune(line)
			for _, tg := range targets {
				if tg.Zone != ZoneTabs {
					continue
				}
				r := tg.Rect
				if r.Y != 1 || r.X+r.Width > c.width || !strings.Contains(string(cells[r.X:r.X+r.Width]), digits[tg.Area]) {
					t.Errorf("%s %d columns: target %+v does not cover its tab in %q", look, c.width, tg, line)
				}
			}
		}
	}
}

// The header stays one line at every width: the marks take their short words and then go before the key
// hints go, and the key hints go before the back button. A block that does not fit beside the name alone
// leaves its room to the marks.
func TestHeaderGivesWayInItsOrder(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		f, _ := sampleFrame(t, looks(t)["noneascii"], lang)
		wide := plainRows(first(f.Render(sampleSpec(200, 8))))[0]
		block := wide[strings.Index(wide, "[esc]"):]
		full, back := ansi.StringWidth(block), ansi.StringWidth(strings.SplitN(block, "  ", 2)[0])
		name := len("example  ")
		seenShort := false
		for width := 90; width >= 1; width-- {
			row := plainRows(first(f.Render(sampleSpec(width, 8))))[0]
			hasHints, hasBack := strings.Contains(row, "[?]"), strings.Contains(row, "[esc]")
			switch {
			case hasHints && !hasBack:
				t.Errorf("%s %d columns keep the key hints but lose the back button: %q", lang, width, row)
			case !hasHints && name+full <= width:
				t.Errorf("%s %d columns drop the key hints, which fit beside the name: %q", lang, width, row)
			case !hasBack && name+back <= width:
				t.Errorf("%s %d columns drop the back button, which fits beside the name: %q", lang, width, row)
			}
			if hasHints && strings.Contains(row, "store open") && name+full+len("* store open  session off  ") > width {
				t.Errorf("%s %d columns keep the full marks beside key hints that do not fit with them: %q", lang, width, row)
			}
			seenShort = seenShort || hasHints && strings.Contains(row, "* open")
			if width >= 7 && !strings.HasPrefix(row, "example") {
				t.Errorf("%s %d columns lose the name: %q", lang, width, row)
			}
		}
		if !seenShort {
			t.Errorf("%s: no width shows the short marks beside the key hints", lang)
		}
	}
}

// Forms of an invented release mark, longest first; the first holds an emoji of two cells.
var releaseForms = []string{
	"Update available v0.4.0 → v0.5.0 \U0001F389 · U update",
	"Update available v0.4.0 → v0.5.0 · U update",
	"Update available v0.5.0 · U update",
	"Update v0.5.0 · U",
}

func releaseMark() StatusMark {
	return StatusMark{Kind: StatusOK, Text: releaseForms[0], Short: releaseForms[1:], Rank: MarkBeforeKeys, NoGlyph: true}
}

// rankStatus are the marks of the ranked header tests by name.
func rankStatus(name string) []StatusMark {
	switch name {
	case "one":
		return []StatusMark{{Kind: StatusWarn, Text: "store locked", Rank: MarkAlways}}
	case "two":
		return []StatusMark{{Kind: StatusOK, Text: "store unlocked", Rank: MarkAlways}, {Kind: StatusWarn, Text: "login off", Rank: MarkAlways}}
	case "release":
		return []StatusMark{{Kind: StatusWarn, Text: "store locked", Rank: MarkAlways}, releaseMark()}
	}
	return nil
}

func rankSpec(width int, status string, back bool) FrameSpec {
	spec := sampleSpec(width, 8)
	spec.Product, spec.ProductShort = "Garden Console", "Garden"
	spec.Status = rankStatus(status)
	if !back {
		spec.Back = nil
	}
	return spec
}

// headerRow is the header line as the frame lays it out, before Render clips it to the width.
func headerRow(f styledFrame, spec FrameSpec) (string, []Target) {
	return (&frameRun{styles: f.styles, text: f.text, keys: f.keys}).header(spec, spec.Width)
}

// The ranked header lays out as the reference layout it follows: the short name where the full one, the
// marks that always stand and the widest block do not fit side by side, a mark before keys in its longest
// form that fits, and then the widest block that fits two cells apart.
func TestRankedHeaderFollowsTheReference(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	const keys, backOnly = "[esc] back  [?] help  [:] cmd", "[esc] back"
	const hints = "[?] help  [:] cmd"
	release := func(name string, form int) string { return name + "  ● store locked  " + releaseForms[form] }
	for _, c := range []struct {
		status string
		width  int
		back   bool
		left   string
		right  string
	}{
		{"one", 40, true, "Garden  ● store locked", backOnly},
		{"one", 40, false, "Garden  ● store locked", ""},
		{"one", 60, true, "Garden  ● store locked", keys},
		{"one", 60, false, "Garden Console  ● store locked", hints},
		{"one", 79, true, "Garden Console  ● store locked", keys},
		{"one", 79, false, "Garden Console  ● store locked", hints},
		{"one", 80, true, "Garden Console  ● store locked", keys},
		{"one", 80, false, "Garden Console  ● store locked", hints},
		{"one", 120, true, "Garden Console  ● store locked", keys},
		{"one", 120, false, "Garden Console  ● store locked", hints},
		{"two", 40, true, "Garden  ● store unlocked  ● login off", ""},
		{"two", 40, false, "Garden  ● store unlocked  ● login off", ""},
		{"two", 60, true, "Garden  ● store unlocked  ● login off", backOnly},
		{"two", 60, false, "Garden  ● store unlocked  ● login off", hints},
		{"two", 79, true, "Garden Console  ● store unlocked  ● login off", keys},
		{"two", 79, false, "Garden Console  ● store unlocked  ● login off", hints},
		{"two", 80, true, "Garden Console  ● store unlocked  ● login off", keys},
		{"two", 80, false, "Garden Console  ● store unlocked  ● login off", hints},
		{"two", 120, true, "Garden Console  ● store unlocked  ● login off", keys},
		{"two", 120, false, "Garden Console  ● store unlocked  ● login off", hints},
		{"release", 40, true, "Garden  ● store locked", backOnly},
		{"release", 40, false, "Garden  ● store locked", ""},
		{"release", 60, true, release("Garden", 2), ""},
		{"release", 60, false, release("Garden Console", 3), ""},
		{"release", 79, true, release("Garden Console", 0), ""},
		{"release", 79, false, release("Garden Console", 0), ""},
		{"release", 80, true, release("Garden Console", 0), ""},
		{"release", 80, false, release("Garden Console", 0), ""},
		{"release", 120, true, release("Garden Console", 0), keys},
		{"release", 120, false, release("Garden Console", 0), hints},
	} {
		row, targets := headerRow(f, rankSpec(c.width, c.status, c.back))
		want := c.left
		if c.right != "" {
			want += strings.Repeat(" ", c.width-ansi.StringWidth(c.left)-ansi.StringWidth(c.right)) + c.right
		}
		if got := ansi.Strip(row); got != want {
			t.Errorf("%s %d columns, back %v:\n got %q\nwant %q", c.status, c.width, c.back, got, want)
		}
		if c.right == "" && len(targets) > 0 {
			t.Errorf("%s %d columns, back %v: targets without a block: %+v", c.status, c.width, c.back, targets)
		}
	}
}

// At every width the ranked header is one line no wider than the width; every mark that always stands is
// there in full or cut only at the edge, and the key hints never stand without the back button.
func TestRankedHeaderKeepsMarksThatAlwaysStand(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	blocks := map[ActionID]string{"back": "[esc] back", "help": "[?] help", "command": "[:] cmd"}
	for _, status := range []string{"one", "two", "release"} {
		var words []string
		for _, m := range rankStatus(status) {
			if m.Rank == MarkAlways {
				words = append(words, "● "+m.Text)
			}
		}
		marks := strings.Join(words, "  ")
		for _, back := range []bool{true, false} {
			for width := 120; width >= 1; width-- {
				row, targets := headerRow(f, rankSpec(width, status, back))
				plain := ansi.Strip(row)
				if strings.Contains(row, "\n") || ansi.StringWidth(row) > width {
					t.Fatalf("%s %d columns: %q is not one line within the width", status, width, plain)
				}
				if !strings.Contains(plain, marks) {
					if cut := strings.TrimSuffix(plain, "…"); cut == plain || !strings.HasPrefix("Garden  "+marks, cut) {
						t.Errorf("%s %d columns lose a mark that always stands: %q", status, width, plain)
					}
				}
				if back && strings.Contains(plain, "[?]") && !strings.Contains(plain, "[esc]") {
					t.Errorf("%s %d columns keep the key hints without the back button: %q", status, width, plain)
				}
				for _, tg := range targets {
					if r := tg.Rect; ansi.Cut(plain, r.X, r.X+r.Width) != blocks[tg.Action] {
						t.Errorf("%s %d columns: target %+v does not lie on its text in %q", status, width, tg, plain)
					}
				}
				for action, text := range blocks {
					has := slices.ContainsFunc(targets, func(tg Target) bool { return tg.Action == action })
					if has != strings.Contains(plain, text) {
						t.Errorf("%s %d columns: target of %s is %v in %q", status, width, action, has, plain)
					}
				}
			}
		}
	}
}

// One column under and on each width where the name takes its short form, the key hints go and the back
// button goes, the header changes at that width and no other.
func TestRankedHeaderChangesAtItsBorders(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	full, short := ansi.StringWidth("Garden Console  ● store locked"), ansi.StringWidth("Garden  ● store locked")
	keys, back := ansi.StringWidth("[esc] back  [?] help  [:] cmd"), ansi.StringWidth("[esc] back")
	for _, c := range []struct {
		width            int
		name, hints, esc bool
	}{
		{full + 2 + keys, true, true, true},
		{full + 2 + keys - 1, false, true, true},
		{short + 2 + keys, false, true, true},
		{short + 2 + keys - 1, false, false, true},
		{short + 2 + back, false, false, true},
		{short + 2 + back - 1, false, false, false},
	} {
		plain := ansi.Strip(first(headerRow(f, rankSpec(c.width, "one", true))))
		if got := [3]bool{strings.Contains(plain, "Console"), strings.Contains(plain, "[?]"), strings.Contains(plain, "[esc]")}; got != [3]bool{c.name, c.hints, c.esc} {
			t.Errorf("%d columns: full name, hints, back = %v, want %v: %q", c.width, got, [3]bool{c.name, c.hints, c.esc}, plain)
		}
	}
	// Without a short form the name stays whole and the blocks follow the same borders.
	spec := rankSpec(full+2+keys-1, "one", true)
	spec.ProductShort = ""
	if plain := ansi.Strip(first(headerRow(f, spec))); !strings.HasPrefix(plain, "Garden Console  ● store locked") || strings.Contains(plain, "[?]") {
		t.Errorf("without a short form: %q", plain)
	}
}

// A mark before keys takes the longest of its forms that fits and goes where none fits; a mark that always
// stands takes its shorter form before the line is cut.
func TestMarkFormsTakeTheLongestThatFits(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	spec := rankSpec(0, "", false)
	spec.HeaderKeys = nil
	spec.Product = "Garden"
	spec.Status = []StatusMark{releaseMark()}
	for _, c := range []struct{ width, form int }{
		{120, 0}, {54, 0}, {53, 1}, {51, 1}, {50, 2}, {42, 2}, {41, 3}, {25, 3}, {24, -1}, {6, -1},
	} {
		spec.Width = c.width
		plain := ansi.Strip(first(headerRow(f, spec)))
		want := "Garden"
		if c.form >= 0 {
			want += "  " + releaseForms[c.form]
		}
		if plain != want {
			t.Errorf("%d columns: %q, want %q", c.width, plain, want)
		}
	}
	spec.Status = []StatusMark{{Kind: StatusWarn, Text: "store locked", Short: []string{"", "locked"}, Rank: MarkAlways}}
	for width, want := range map[int]string{22: "Garden  ● store locked", 21: "Garden  ● locked", 16: "Garden  ● locked", 15: "Garden  ● lock…", 9: "Garden  …", 8: "Garden …", 3: "Ga…"} {
		spec.Width = width
		if plain := ansi.Strip(first(headerRow(f, spec))); plain != want {
			t.Errorf("%d columns: %q, want %q", width, plain, want)
		}
	}
}

// A mark without its glyph reads as its words in every profile: no glyph without colour or in ASCII, and the
// colour of its kind where there is colour.
func TestMarkWithoutGlyph(t *testing.T) {
	spec := rankSpec(120, "", false)
	spec.HeaderKeys = nil
	spec.Status = []StatusMark{releaseMark()}
	for _, look := range []string{"mono", "noneascii", "asciidark", "dark"} {
		f, _ := sampleFrame(t, looks(t)[look], i18n.English)
		row := first(headerRow(f, spec))
		plain := ansi.Strip(row)
		if plain != "Garden Console  "+releaseForms[0] {
			t.Errorf("%s: %q", look, plain)
		}
		switch look {
		case "mono", "noneascii":
			noColorSequence(t, look, row)
		case "dark":
			if !strings.Contains(row, "\x1b[") {
				t.Errorf("%s: the mark has no colour: %q", look, row)
			}
		}
	}
}

// Untrusted text in the short name and in every form of every mark never reaches the terminal as a
// control sequence.
func TestRankedHeaderEscapesControlCharacters(t *testing.T) {
	hostile := "a\x1b[2J\x1b]8;;x\x07b\rc\u009bd‮e"
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	for width := 120; width >= 1; width-- {
		spec := rankSpec(width, "", true)
		spec.Product, spec.ProductShort = hostile+hostile, hostile
		spec.Status = []StatusMark{
			{Kind: StatusFail, Text: hostile + hostile, Short: []string{hostile}, Rank: MarkAlways},
			{Kind: StatusOK, Text: hostile + hostile, Short: []string{hostile, "\x1b"}, Rank: MarkBeforeKeys, NoGlyph: true},
			{Text: hostile + hostile, Short: []string{hostile}},
		}
		if row, _ := headerRow(f, spec); strings.ContainsAny(row, "\x1b\x07\r\u009b‮") {
			t.Fatalf("%d columns pass a control character: %q", width, row)
		}
	}
}

// A hint names a key only where it acts: on a text line the help key types text, so neither header nor
// footer names it, while esc still leads back. A key the table does not know shows as its placeholder.
func TestHintsShowOnlyKeysThatAct(t *testing.T) {
	f, keys := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := sampleSpec(100, 8)
	spec.Input = InputText
	rows := plainRows(first(f.Render(spec)))
	if strings.Contains(rows[0], "[?]") || strings.Contains(rows[7], "? help") || strings.Contains(rows[7], "q quit") {
		t.Errorf("a text line names keys that type text:\n%s\n%s", rows[0], rows[7])
	}
	if !strings.Contains(rows[0], "[esc] back") || !strings.Contains(rows[7], "F2 save") {
		t.Errorf("a text line drops keys that act:\n%s\n%s", rows[0], rows[7])
	}
	spec.Input = InputNativeTerminal
	if rows := plainRows(first(f.Render(spec))); strings.Contains(rows[7], "F2") || strings.Contains(rows[0], "[esc]") {
		t.Errorf("an embedded terminal names a key:\n%s\n%s", rows[0], rows[7])
	}
	groups := keys.Hints([]Hint{{Action: "nosuch", Label: "ex.help"}, {Label: "ex.type"}, {}}, InputNavigation, f.text)
	if len(groups) != 2 || groups[0].Text() != "{nosuch} help" || groups[1].Text() != "type to filter" {
		t.Errorf("Hints = %+v", groups)
	}
}

// Hints whose labels read the same fold into one group in that language, at the place of the first, so a
// language with longer words needs no empty text to leave one out.
func TestHintsFoldEqualLabels(t *testing.T) {
	if err := CheckCatalogs(frameCatalogs()); err != nil {
		t.Fatal(err)
	}
	hints := []Hint{
		{Action: "down", Label: "ex.open", Priority: 3},
		{Action: "area", Label: "ex.openarea", Priority: 2},
		{Action: "help", Label: "ex.help", Kind: HintAlways},
		{Action: "down", Label: "ex.openarea"},
	}
	st := looks(t)["noneascii"]
	for lang, want := range map[i18n.Lang][]string{
		i18n.English: {"down open", "1-6/down areas", "? help"},
		i18n.German:  {"down/1-6 öffnen", "? Hilfe"},
	} {
		f, keys := sampleFrame(t, st, lang)
		var got []string
		for _, g := range keys.Hints(hints, InputNavigation, f.text) {
			got = append(got, g.Text())
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", lang, got, want)
		}
	}
	f, keys := sampleFrame(t, st, i18n.German)
	if g := keys.Hints(hints, InputNavigation, f.text)[0]; g.Priority != 0 || len(g.Keys) != 2 || g.Keys[1].Action != "area" {
		t.Errorf("folded group = %+v", g)
	}
}

// When the footer runs short, navigation goes first, then the position, then the normal hints by priority,
// the later of equal ones first; the help key stays and the answers of a question are shortened, never
// dropped.
func TestFooterGivesWayByRank(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := sampleSpec(0, 12)
	order := []string{"up move", "3/12", "i info", "q quit", "F2 save", "enter open"}
	gone := 0
	for width := 100; width >= 1; width-- {
		spec.Width = width
		row := footerOf(f, spec)
		if !strings.Contains(row, "? help") && width >= 6 {
			t.Fatalf("%d columns drop the help key: %q", width, row)
		}
		for gone < len(order) && !strings.Contains(row, order[gone]) {
			gone++
		}
		for _, still := range order[gone:] {
			if !strings.Contains(row, still) {
				t.Errorf("%d columns drop %q before %q: %q", width, still, order[gone], row)
			}
		}
	}
	if gone != len(order) {
		t.Errorf("the narrowest footer keeps %q", order[gone:])
	}
	spec.Footer = []Hint{
		{Action: "help", Label: "ex.help", Kind: HintAlways},
		{Action: "yes", Label: "ex.yes", Kind: HintAnswer},
		{Action: "no", Label: "ex.no", Kind: HintAnswer},
		{Action: "back", Label: "ex.back", Priority: 1},
	}
	for width := 60; width >= 16; width-- {
		spec.Width = width
		row := footerOf(f, spec)
		if !strings.Contains(row, "? help") || !strings.Contains(row, "y ") || !strings.Contains(row, "n ") {
			t.Errorf("%d columns drop an answer or the help key: %q", width, row)
		}
	}
	spec.Width = 22
	if row := footerOf(f, spec); !strings.Contains(row, "~") {
		t.Errorf("a short answer carries no cut mark: %q", row)
	}
}

// Navigation and position stand at the right end of the footer, the hints at the left.
func TestFooterPutsNavigationRight(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	row := plainRows(first(f.Render(sampleSpec(100, 8))))[7]
	if !strings.HasPrefix(row, "? help  enter open") || !strings.HasSuffix(row, "up move  3/12") {
		t.Errorf("footer = %q", row)
	}
	spec := sampleSpec(40, 8)
	spec.Footer = nil
	if row := plainRows(first(f.Render(spec)))[7]; !strings.HasSuffix(row, "3/12") || strings.TrimSpace(row) != "3/12" {
		t.Errorf("a footer of the position alone = %q", row)
	}
}

// A rebinding shows in the header and footer of the next frame at once; two frames do not share state.
func TestFramesFollowTheirOwnKeymap(t *testing.T) {
	st := looks(t)["noneascii"]
	f, keys := sampleFrame(t, st, i18n.English)
	other, _ := sampleFrame(t, st, i18n.English)
	if err := keys.Bind(map[ActionID]string{"help": "f4"}); err != nil {
		t.Fatal(err)
	}
	rows := plainRows(first(f.Render(sampleSpec(100, 8))))
	if !strings.Contains(rows[0], "[F4] help") || !strings.Contains(rows[7], "F4 help") {
		t.Errorf("the rebinding does not show:\n%s\n%s", rows[0], rows[7])
	}
	if rows := plainRows(first(other.Render(sampleSpec(100, 8)))); !strings.Contains(rows[7], "? help") {
		t.Errorf("another frame follows the rebinding: %q", rows[7])
	}
}

// Every target covers what it names in the row it stands in: a footer key its action, a header block its
// action, a tab its area and a sub-tab its tab; nothing reaches beyond the terminal.
func TestTargetsCoverWhatTheyName(t *testing.T) {
	f, keys := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	for _, width := range []int{100, 60, 30, 12} {
		spec := sampleSpec(width, 12)
		out, targets := f.Render(spec)
		rows := plainRows(out)
		ws := f.Workspace(spec)
		footTop := ws.Y + ws.Height
		found := map[string]bool{}
		for _, tg := range targets {
			r := tg.Rect
			if r.X < 0 || r.Width <= 0 || r.X+r.Width > width || r.Height != 1 || r.Y < 0 || r.Y >= len(rows) {
				t.Fatalf("%d columns: target %+v outside the screen", width, tg)
			}
			cells := string([]rune(rows[r.Y])[r.X : r.X+r.Width])
			switch {
			case tg.Zone == ZoneTabs:
				found["tab "+string(tg.Area)] = true
			case tg.Zone == ZoneSubtabs:
				found["sub "+string(tg.Tab)] = true
				if !strings.Contains(strings.ToLower(cells), string(tg.Tab)[:min(3, len(tg.Tab))]) {
					t.Errorf("%d columns: sub-tab %q covers %q", width, tg.Tab, cells)
				}
			case r.Y == 0:
				found["head "+string(tg.Action)] = true
				if !strings.HasPrefix(cells, "["+keys.Label(tg.Action)+"]") {
					t.Errorf("%d columns: header target %s covers %q", width, tg.Action, cells)
				}
			case r.Y >= footTop:
				found["foot "+string(tg.Action)] = true
				if label := keys.Label(tg.Action); !strings.Contains(cells, label) && !strings.Contains(rows[r.Y], label+" "+cells) {
					t.Errorf("%d columns: footer target %s covers %q", width, tg.Action, cells)
				}
			default:
				t.Errorf("%d columns: target in row %d: %+v", width, r.Y, tg)
			}
		}
		if width == 100 {
			for _, want := range []string{"tab overview", "tab settings", "sub general", "sub advanced", "head back", "head help", "foot help", "foot enter", "foot up"} {
				if !found[want] {
					t.Errorf("no target %q", want)
				}
			}
		}
	}
}

// The message stands under the content of the workspace, never in the footer; it is wrapped, not cut, and
// takes its rows from the workspace.
func TestMessageStandsUnderTheContent(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := sampleSpec(30, 10)
	spec.Footer = spec.Footer[:1]
	spec.Message = &Message{Kind: MessageError, Text: "error: the sample store is gone; press esc"}
	rows := plainRows(first(f.Render(spec)))
	if got := f.Workspace(spec); got.Height != 3 {
		t.Errorf("Workspace with a two-row message = %+v", got)
	}
	if !strings.HasPrefix(rows[7], "error: the sample store is") || !strings.HasPrefix(rows[8], "gone; press esc") || strings.Contains(rows[9], "esc") {
		t.Errorf("message rows:\n%s", strings.Join(rows, "\n"))
	}
	spec.Height = 6
	if rows := plainRows(first(f.Render(spec))); !strings.Contains(rows[4], "press esc") {
		t.Errorf("a message in a low terminal loses its way out:\n%s", strings.Join(rows, "\n"))
	}
}

// Without colour the active tabs keep their brackets and their fill shows in reverse; no colour sequence
// is drawn.
func TestFrameWithoutColor(t *testing.T) {
	for _, look := range []string{"mono", "noneascii"} {
		f, _ := sampleFrame(t, looks(t)[look], i18n.English)
		out, _ := f.Render(sampleSpec(80, 8))
		noColorSequence(t, look, out)
		if !strings.Contains(plainRows(out)[1], "[3 Settings]") || !strings.Contains(plainRows(out)[2], "[General]") {
			t.Errorf("%s: active tabs without brackets:\n%s", look, out)
		}
	}
	f, _ := sampleFrame(t, looks(t)["dark"], i18n.English)
	if row := plainRows(first(f.Render(sampleSpec(80, 8))))[1]; strings.Contains(row, "[") {
		t.Errorf("brackets with colour: %q", row)
	}
}

// Untrusted text in the frame never reaches the terminal as a control sequence.
func TestFrameEscapesControlCharacters(t *testing.T) {
	hostile := "a\x1b[2J\x1b]8;;x\x07b\rc\u009bd‮e"
	st := looks(t)["noneascii"]
	keys := sampleKeymap(t, st.Charset())
	cat := frameCatalogs()
	cat[i18n.English]["ex.items"] = hostile
	f := NewFrame(keys, newTranslator(t, i18n.English, cat, keys))
	spec := sampleSpec(200, 10)
	spec.ActiveArea = "items"
	spec.Product, spec.Position = hostile, hostile
	spec.Icon, spec.Areas[1].Icon = hostile, hostile
	if CheckFrame(spec) == nil {
		t.Error("CheckFrame accepts a hostile icon")
	}
	spec.Status = []StatusMark{{Kind: StatusFail, Text: hostile}, {Text: hostile}}
	spec.Message = &Message{Kind: MessageError, Text: hostile}
	out, _ := f.Render(st, spec)
	if strings.ContainsAny(out, "\x1b\x07\r\u009b‮") {
		t.Errorf("the frame passes a control character: %q", out)
	}
}

// German texts fit the same widths, and a German footer folds the two ways of opening into one group.
func TestGermanFrame(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["noneascii"], i18n.German)
	spec := sampleSpec(100, 8)
	spec.Focus, spec.ActiveArea = ZoneTabs, "overview"
	spec.Footer = []Hint{
		{Action: "help", Label: "ex.help", Kind: HintAlways},
		{Action: "down", Label: "ex.open", Priority: 3},
		{Action: "area", Label: "ex.openarea", Priority: 3},
		{Action: "left", Label: "ex.move", Kind: HintNavigation},
	}
	rows := plainRows(first(f.Render(spec)))
	if !strings.Contains(rows[1], "1 Übersicht") || !strings.Contains(rows[0], "[?] Hilfe") || !strings.Contains(rows[len(rows)-1], "down/1-6 öffnen") {
		t.Errorf("German frame:\n%s", strings.Join(rows, "\n"))
	}
}

func TestCycleZoneFollowsTheFixedOrderAndWraps(t *testing.T) {
	zones := []FocusZone{ZoneFilters, ZoneRows, ZoneTabs, "unknown"}
	for _, c := range []struct {
		current FocusZone
		step    int
		want    FocusZone
	}{
		{ZoneTabs, 1, ZoneRows}, {ZoneRows, 1, ZoneFilters}, {ZoneFilters, 1, ZoneTabs},
		{ZoneTabs, -1, ZoneFilters}, {ZoneRows, -1, ZoneTabs}, {ZoneDetail, 1, ZoneTabs}, {ZoneRows, 0, ZoneRows},
		{"unknown", 1, ZoneTabs},
	} {
		if got := CycleZone(zones, c.current, c.step); got != c.want {
			t.Errorf("CycleZone(%s, %d) = %s, want %s", c.current, c.step, got, c.want)
		}
	}
	if got := CycleZone(nil, ZoneRows, 1); got != ZoneRows {
		t.Errorf("CycleZone without zones = %s", got)
	}
}

func TestStepAreaAndTabWrap(t *testing.T) {
	areas := sampleAreas()
	for _, c := range []struct {
		active AreaID
		step   int
		want   AreaID
	}{{"overview", -1, "settings"}, {"settings", 1, "overview"}, {"items", 1, "settings"}, {"nosuch", 1, "overview"}} {
		if got := StepArea(areas, c.active, c.step); got != c.want {
			t.Errorf("StepArea(%s, %d) = %s, want %s", c.active, c.step, got, c.want)
		}
	}
	if got := StepArea(nil, "items", 1); got != "items" {
		t.Errorf("StepArea without areas = %s", got)
	}
	if got := StepTab(areas[2], "general", -1); got != "advanced" {
		t.Errorf("StepTab back from the first = %s", got)
	}
	if got := StepTab(areas[2], "advanced", 1); got != "general" {
		t.Errorf("StepTab on from the last = %s", got)
	}
	if got := StepTab(areas[0], "", 1); got != "" {
		t.Errorf("StepTab without tabs = %q", got)
	}
}

func TestCheckAreasRejectsWhatTheTabLineCannotDraw(t *testing.T) {
	if err := CheckAreas(sampleAreas()); err != nil {
		t.Fatalf("sample areas: %v", err)
	}
	many := make([]Area, 10)
	for i := range many {
		many[i] = Area{ID: AreaID("a" + string(rune('0'+i))), Title: "ex.items"}
	}
	for name, c := range map[string]struct {
		areas []Area
		want  string
	}{
		"none":         {nil, "no area"},
		"ten":          {many, "at most 9"},
		"empty ID":     {[]Area{{Title: "ex.items"}}, "has no ID"},
		"repeated ID":  {[]Area{{ID: "a", Title: "ex.items"}, {ID: "a", Title: "ex.items"}}, "listed twice"},
		"no title":     {[]Area{{ID: "a"}}, "has no title"},
		"empty tab ID": {[]Area{{ID: "a", Title: "ex.items", Tabs: []Tab{{Title: "ex.general"}}}}, "tab 1 has no ID"},
		"repeated tab": {[]Area{{ID: "a", Title: "ex.items", Tabs: []Tab{{ID: "t", Title: "ex.general"}, {ID: "t", Title: "ex.general"}}}}, `tab "t" is listed twice`},
		"tab no title": {[]Area{{ID: "a", Title: "ex.items", Tabs: []Tab{{ID: "t"}}}}, `tab "t" has no title`},
	} {
		err := CheckAreas(c.areas)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

// Icons the tests draw: an emoji of two cells and private-use glyphs of a Nerd Font, which measure one.
const (
	iconHome  = "\U0001F3E0"
	iconNerd  = ""
	iconNerd2 = "\U000f0219"
)

func iconSpec(width int) FrameSpec {
	spec := sampleSpec(width, 8)
	spec.Icon = iconNerd
	spec.Areas = sampleAreas()
	spec.Areas[0].Icon = iconHome
	spec.Areas[1].Icon = iconNerd2
	spec.Areas[2].Icon = "✓"
	spec.ActiveArea, spec.ActiveTab = "items", ""
	return spec
}

func hostileIcons() []string { return []string{"\x1b", "❤️", "ab", "‮"} }

func TestCheckFrameAndAreasRejectHostileIcons(t *testing.T) {
	if err := CheckFrame(iconSpec(80)); err != nil {
		t.Fatalf("icon spec: %v", err)
	}
	if err := CheckAreas(iconSpec(80).Areas); err != nil {
		t.Fatalf("icon areas: %v", err)
	}
	for _, icon := range hostileIcons() {
		spec := iconSpec(80)
		spec.Icon = icon
		if err := CheckFrame(spec); err == nil || !strings.Contains(err.Error(), "frame") {
			t.Errorf("FrameSpec.Icon %q: %v", icon, err)
		}
		spec = iconSpec(80)
		spec.Areas[1].Icon = icon
		if err := CheckAreas(spec.Areas); err == nil || !strings.Contains(err.Error(), "area 2") {
			t.Errorf("CheckAreas, Area.Icon %q: %v", icon, err)
		}
		if err := CheckFrame(spec); err == nil || !strings.Contains(err.Error(), "area 2") {
			t.Errorf("CheckFrame, Area.Icon %q: %v", icon, err)
		}
		// A frame that was never checked still draws no raw glyph.
		out, _ := NewFrame(sampleKeymap(t, CharsetUnicode), nil).Render(looks(t)["mono"], spec)
		if strings.ContainsAny(out, "\x1b‮") && !strings.Contains(out, "\x1b[") {
			t.Errorf("icon %q reaches the screen: %q", icon, out)
		}
	}
}

// The icon stands beside the name; an icon of the frame or an area never replaces it, and a private-use
// character in text that is not an icon stays masked.
func TestFrameDrawsIconsBesideNames(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	rows := plainRows(first(f.Render(iconSpec(100))))
	if !strings.HasPrefix(rows[0], iconNerd+" example") {
		t.Errorf("header: %q", rows[0])
	}
	for _, want := range []string{"1 " + iconHome + " Overview", "[2 " + iconNerd2 + " Items]", "3 ✓ Settings"} {
		if !strings.Contains(rows[1], want) {
			t.Errorf("tab line %q lacks %q", rows[1], want)
		}
	}
	spec := iconSpec(100)
	spec.Icon = ""
	spec.Product = "x" + iconNerd
	spec.Status = []StatusMark{{Text: iconNerd}}
	out := ansi.Strip(first(f.Render(spec)))
	row := strings.SplitN(out, "\n", 2)[0]
	if strings.Contains(out, iconNerd) || strings.Count(row, "\\uf489") != 2 {
		t.Errorf("private-use character in the product name or status is not masked: %q", row)
	}
}

// Without UTF-8 no icon is drawn.
func TestFrameWithoutUTF8DrawsNoIcon(t *testing.T) {
	for _, look := range []string{"noneascii", "asciidark"} {
		f, _ := sampleFrame(t, looks(t)[look], i18n.English)
		out := ansi.Strip(first(f.Render(iconSpec(100))))
		for _, icon := range []string{iconNerd, iconNerd2, iconHome, "✓"} {
			if strings.Contains(out, icon) {
				t.Errorf("%s: icon %q in %q", look, icon, out)
			}
		}
	}
}

// The tab line gives up its icons before it gives up anything else, and the digit-only level holds none.
func TestNarrowTabLineDropsIconsFirst(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	for _, c := range []struct {
		width  int
		want   []string
		absent []string
	}{
		{80, []string{" 1 " + iconHome + " Overview ", "[2 " + iconNerd2 + " Items]", " 3 ✓ Settings "}, nil},
		// Without icons the tab line still fits with its gaps.
		{40, []string{" 1 Overview ", "[2 Items]", " 3 Settings "}, []string{iconHome, iconNerd2, "✓"}},
		{34, []string{"1 Overview", "[2 Items]", "3 Settings"}, []string{iconHome, iconNerd2, "✓"}},
		{20, []string{" 1 ", "[2 Items]", " 3"}, []string{iconHome, iconNerd2, "✓", "Overview"}},
	} {
		line := plainRows(first(f.Render(iconSpec(c.width))))[1]
		for _, w := range c.want {
			if !strings.Contains(line, w) {
				t.Errorf("%d columns: %q lacks %q", c.width, line, w)
			}
		}
		for _, a := range c.absent {
			if strings.Contains(line, a) {
				t.Errorf("%d columns: %q holds %q", c.width, line, a)
			}
		}
	}
}

// A click target covers the icon of its tab.
func TestTabTargetsIncludeTheIcon(t *testing.T) {
	f, _ := sampleFrame(t, looks(t)["mono"], i18n.English)
	spec := iconSpec(100)
	out, targets := f.Render(spec)
	cells := plainRows(out)[1]
	want := map[AreaID]string{"overview": iconHome + " Overview", "items": iconNerd2 + " Items", "settings": "✓ Settings"}
	n := 0
	for _, tg := range targets {
		if tg.Zone != ZoneTabs {
			continue
		}
		n++
		var covered strings.Builder
		for x, w := 0, 0; x < tg.Rect.X+tg.Rect.Width; {
			r, size := utf8.DecodeRuneInString(cells[w:])
			if r == utf8.RuneError {
				break
			}
			cw := ansi.StringWidth(string(r))
			if x >= tg.Rect.X {
				covered.WriteRune(r)
			}
			x += cw
			w += size
		}
		if !strings.Contains(covered.String(), want[tg.Area]) {
			t.Errorf("target %+v covers %q, want %q inside", tg, covered.String(), want[tg.Area])
		}
	}
	if n != 3 {
		t.Errorf("%d tab targets", n)
	}
}

func TestGoldenFrameIcons(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		width      int
	}{
		{"frame.icons.wide.golden", "mono", 100},
		{"frame.icons.narrow.golden", "mono", 40},
		{"frame.icons.wide.dark.golden", "dark", 100},
		{"frame.icons.noneascii.golden", "noneascii", 100},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := sampleFrame(t, looks(t)[tt.look], i18n.English)
			checkGolden(t, tt.name, first(f.Render(iconSpec(tt.width)))+"\n")
		})
	}
}

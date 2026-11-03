package tui

import (
	"slices"
	"strings"
	"testing"

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
		Status: []StatusMark{{Kind: StatusOK, Text: "store open", Short: "open"}, {Text: "session off", Short: "off"}},
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
	spec := sampleSpec(0, 8)
	order := []string{"up move", "3/12", "i info", "q quit", "F2 save", "enter open"}
	gone := 0
	for width := 100; width >= 1; width-- {
		spec.Width = width
		row := plainRows(first(f.Render(spec)))[7]
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
		row := plainRows(first(f.Render(spec)))[7]
		if !strings.Contains(row, "? help") || !strings.Contains(row, "y ") || !strings.Contains(row, "n ") {
			t.Errorf("%d columns drop an answer or the help key: %q", width, row)
		}
	}
	spec.Width = 22
	if row := plainRows(first(f.Render(spec)))[7]; !strings.Contains(row, "~") {
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
		spec := sampleSpec(width, 9)
		out, targets := f.Render(spec)
		rows := plainRows(out)
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
			case r.Y == len(rows)-1:
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

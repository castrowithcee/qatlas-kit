package tui

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// searchSpec is the sample pages with four more: a page whose keyword is "water", one whose text is, a long
// page that holds its match deep down and one found by a keyword that its text lacks, and the search keys.
func searchSpec(lang i18n.Lang) HelpSpec {
	pages := samplePages()
	file := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	pages["start/tips.md"] = file("---\ntitle: Tips\nkeywords: [pail, Water]\n---\nKeep a pail near.\n")
	pages["water/rain.md"] = file("---\ntitle: Rain barrel\nkeywords: [tank]\n---\nCollect water in a barrel.\n")
	long := "---\ntitle: Long page\n---\n"
	for i := 1; i <= 30; i++ {
		long += fmt.Sprintf("Filler line %d.\n\n", i)
	}
	pages["water/long.md"] = file(long + "The needle sits here deep in the page.\n\n" + strings.Repeat("Epilogue.\n\n", 20))
	spec := samplePageSpec()
	spec.Pages, spec.Lang = pages, lang
	spec.Keys.Search, spec.Keys.Enter = "search", "enter"
	return spec
}

func newSearchHelp(t *testing.T, lang i18n.Lang) *Help {
	t.Helper()
	keys := f9Keymap(t)
	h, err := NewHelp(searchSpec(lang), keys, newTranslator(t, lang, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// typeInto types text into the help one character at a time.
func helpType(h *Help, text string) {
	for _, r := range text {
		h.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// helpPress sends the named keys to the help; the names of the arrow keys are the ones press lacks.
func helpPress(h *Help, names ...string) (act ActionID, handled bool) {
	arrows := map[string]rune{"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight}
	for _, n := range names {
		if code, ok := arrows[n]; ok {
			act, handled = h.Update(tea.KeyPressMsg{Code: code})
			continue
		}
		act, handled = helpKeysTo(h, n)
	}
	return act, handled
}

// searchFor opens the search and types query.
func searchFor(t *testing.T, h *Help, query string) {
	t.Helper()
	if act, handled := helpKeysTo(h, "/"); act != "" || !handled || h.search == nil {
		t.Fatalf("the search key: %q %v, search open %v", act, handled, h.search != nil)
	}
	helpType(h, query)
}

// hitTitles are the titles of the hits of the open search in their order.
func hitTitles(h *Help) []string {
	var out []string
	for _, hit := range h.search.hits {
		out = append(out, h.pages[hit.page].page.In(h.spec.Lang).Title)
	}
	return out
}

// plainView is the help drawn without colour in w by ht cells, as plain text.
func helpPlain(t *testing.T, h *Help, w, ht int) string {
	t.Helper()
	out, _ := h.View(looks(t)["noneascii"], w, ht)
	return ansi.Strip(out)
}

// Hits come in the rank title, keywords, text, and by the order of the pages within a rank; the match ignores
// case in the keywords too.
func TestHelpSearchRanksTitleThenKeywordsThenText(t *testing.T) {
	h := newSearchHelp(t, i18n.English)
	searchFor(t, h, "WATER")
	want := []string{"Watering", "Tips", "Getting started", "Rain barrel"}
	if got := hitTitles(h); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("hits %v, want %v", got, want)
	}
	for i, rank := range []int{hitTitle, hitKeyword, hitText, hitText} {
		if got := h.search.hits[i].rank; got != rank {
			t.Errorf("hit %d has rank %d, want %d", i, got, rank)
		}
	}
	plain := helpPlain(t, h, 60, 20)
	if !inOrder(plain, "Watering", "Tips", "Getting started", "Rain barrel") {
		t.Errorf("the hits are out of order:\n%s", plain)
	}
	// Each hit names its topic: a page of the second topic stands under the title of its index page.
	if !inOrder(plain, "Watering > Rain barrel") && !strings.Contains(plain, "Watering > Rain barrel") {
		t.Errorf("no topic before the page:\n%s", plain)
	}
}

// The search reads the text without Markdown marks and the keys as drawn, and an empty input has no hits.
func TestHelpSearchReadsPlainTextWithKeysAndStaysEmptyWithoutInput(t *testing.T) {
	h := newSearchHelp(t, i18n.English)
	searchFor(t, h, "")
	if len(h.search.hits) != 0 {
		t.Errorf("an empty input has %d hits", len(h.search.hits))
	}
	helpType(h, "  ")
	if len(h.search.hits) != 0 {
		t.Error("blanks alone found pages")
	}
	h.search.query.clear()
	helpType(h, "**seed**")
	if h.research(); len(h.search.hits) != 0 {
		t.Errorf("the marks of Markdown are searched: %v", hitTitles(h))
	}
	h.search.query.clear()
	helpType(h, "press F2 to keep")
	h.research()
	if got := hitTitles(h); len(got) != 1 || got[0] != "Getting started" {
		t.Errorf("the key placeholder is not searched as drawn: %v", got)
	}
}

// A search without a hit says so in the language of the help.
func TestHelpSearchNoHitSaysSo(t *testing.T) {
	for lang, want := range map[i18n.Lang]string{i18n.English: `No page matches "zzz".`, i18n.German: `Keine Seite passt zu "zzz".`} {
		h := newSearchHelp(t, lang)
		searchFor(t, h, "zzz")
		if plain := helpPlain(t, h, 60, 10); !strings.Contains(plain, want) {
			t.Errorf("%s: no empty sentence %q:\n%s", lang, want, plain)
		}
	}
}

// The search looks in the language of the help, in English where a page has no translation.
func TestHelpSearchUsesTheLanguageWithEnglishFallback(t *testing.T) {
	h := newSearchHelp(t, i18n.German)
	searchFor(t, h, "samen")
	if got := hitTitles(h); len(got) != 1 || got[0] != "Erste Schritte" {
		t.Errorf("German hits %v", got)
	}
	h.search.query.clear()
	helpType(h, "seed")
	if h.research(); len(h.search.hits) != 0 {
		t.Errorf("the English text of a translated page is searched: %v", hitTitles(h))
	}
	h.search.query.clear()
	helpType(h, "pump")
	h.research()
	if got := hitTitles(h); len(got) != 1 || got[0] != "Pump" {
		t.Errorf("a page without translation is not found in English: %v", got)
	}
	// The topic of a hit is the title of its index page in the language.
	if plain := helpPlain(t, h, 60, 10); !strings.Contains(plain, "Watering > [Pump]") {
		t.Errorf("topic and page:\n%s", plain)
	}
	h = newSearchHelp(t, i18n.German)
	searchFor(t, h, "sommer")
	if plain := helpPlain(t, h, 70, 10); !strings.Contains(plain, "Erste Schritte > Erste Schritte") {
		t.Errorf("German topic:\n%s", plain)
	}
}

// A hit shows a line cut around the first place, with the match marked and control characters masked.
func TestHelpSearchSnippetMarksAndMasks(t *testing.T) {
	h := newSearchHelp(t, i18n.English)
	searchFor(t, h, "BELL")
	plain := helpPlain(t, h, 60, 10)
	if !strings.Contains(plain, `A [bell]\a and an escape\x1b[2J stay masked.`) {
		t.Errorf("the snippet:\n%s", plain)
	}
	if strings.ContainsAny(plain, "\x07\x1b") {
		t.Errorf("a control character reaches the terminal:\n%q", plain)
	}
	// With colour the match stands in the style of the active, and no brackets are drawn.
	dark := looks(t)["dark"]
	out, _ := h.View(dark, 60, 10)
	if strings.Contains(ansi.Strip(out), "[bell]") || !strings.Contains(out, dark.Render(StyleActive, "bell")) {
		t.Errorf("the match is not marked by style:\n%q", out)
	}
	if strings.Contains(out, "\x07") {
		t.Error("the bell reaches the terminal")
	}
	// In narrow room the line is cut around the match, which stays in view.
	h.search.query.clear()
	helpType(h, "light for the whole")
	h.research()
	narrow := helpPlain(t, h, 30, 10)
	if !strings.Contains(narrow, "[light for the whole]") && !strings.Contains(narrow, "[light for") {
		t.Errorf("the match left the narrow snippet:\n%s", narrow)
	}
	for _, l := range strings.Split(narrow, "\n") {
		if ansi.StringWidth(l) > 30 {
			t.Errorf("line wider than the room: %q", l)
		}
	}
	h.search.query.clear()
	helpType(h, "summer")
	h.research()
	if cut := helpPlain(t, h, 30, 10); !strings.Contains(cut, "[summer]") || !strings.Contains(cut, "~") {
		t.Errorf("a cut snippet shows no cut mark or lost the match:\n%s", cut)
	}
}

// Up and down choose a hit, and enter opens its page and scrolls to the first place of the match.
func TestHelpSearchEnterOpensThePageAtTheMatch(t *testing.T) {
	h := newSearchHelp(t, i18n.English)
	searchFor(t, h, "needle")
	if got := hitTitles(h); len(got) != 1 || got[0] != "Long page" {
		t.Fatalf("hits %v", got)
	}
	if act, handled := helpKeysTo(h, "enter"); act != "" || !handled || h.search != nil {
		t.Fatalf("enter: %q %v, search open %v", act, handled, h.search != nil)
	}
	if topic, page := h.Page(); topic != "water" || page != "long" {
		t.Fatalf("enter opened %s/%s", topic, page)
	}
	plain := helpPlain(t, h, 40, 8)
	if h.scroll.offset < 20 || !strings.Contains(plain, "The needle sits here") {
		t.Errorf("the page did not scroll to the match (offset %d):\n%s", h.scroll.offset, plain)
	}
	if h.find != nil {
		t.Error("the jump stays pending after the page was drawn")
	}
	// The first line of the match is the first row shown, whatever the width.
	for _, w := range []int{20, 60} {
		h.SetPage("water", "long")
		searchFor(t, h, "needle")
		helpKeysTo(h, "enter")
		plain = helpPlain(t, h, w, 8)
		if rows := strings.Split(plain, "\n"); !strings.Contains(rows[2], "needle") {
			t.Errorf("width %d: the match is not the first row:\n%s", w, plain)
		}
	}
	// A match in the title or the keywords only opens the page from its top; leaving it scrolled does not matter.
	for _, query := range []string{"Long pa", "tank"} {
		h.SetPage("water", "long")
		h.scroll.offset = 9
		searchFor(t, h, query)
		helpKeysTo(h, "enter")
		helpPlain(t, h, 40, 8)
		if h.scroll.offset != 0 {
			t.Errorf("%q opened at %d, not from the top", query, h.scroll.offset)
		}
	}
	// Up and down choose among the hits.
	h = newSearchHelp(t, i18n.English)
	searchFor(t, h, "water")
	helpPress(h, "down", "down", "up")
	helpKeysTo(h, "enter")
	if topic, page := h.Page(); topic != "start" || page != "tips" {
		t.Errorf("down, down, up, enter opened %s/%s", topic, page)
	}
	h.SetPage("start", "index")
	searchFor(t, h, "water")
	helpKeysTo(h, "up", "up")
	if h.search.cursor != 0 {
		t.Errorf("up stops at the first hit, cursor %d", h.search.cursor)
	}
}

// A click on a hit opens its page at the match as enter does; the key line is clickable.
func TestHelpSearchClicks(t *testing.T) {
	h := newSearchHelp(t, i18n.English)
	searchFor(t, h, "water")
	rows, m := framed(t, h.View, "noneascii", 60, 20)
	x, y := at(t, rows, "Rain barrel")
	if act, handled := h.Click(m.Handle(click(x, y))); act != "" || !handled || h.search != nil {
		t.Fatalf("a click on a hit: %q %v, search open %v", act, handled, h.search != nil)
	}
	if topic, page := h.Page(); topic != "water" || page != "rain" {
		t.Errorf("the click opened %s/%s", topic, page)
	}
	// The second line of a hit is part of its target.
	h.SetPage("start", "index")
	searchFor(t, h, "needle")
	rows, m = framed(t, h.View, "noneascii", 60, 20)
	x, y = at(t, rows, "[needle]")
	if _, handled := h.Click(m.Handle(click(x, y))); !handled || h.search != nil {
		t.Fatal("a click on the snippet did not open the page")
	}
	if helpPlain(t, h, 40, 8); h.scroll.offset < 20 {
		t.Errorf("the click did not scroll to the match: %d", h.scroll.offset)
	}
	h.SetPage("start", "index")
	searchFor(t, h, "water")
	view, targets, ok := h.Overlay(looks(t)["noneascii"], sampleScreen(100, 26), 100, 26, Rect{Y: 2, Width: 100, Height: 24})
	if !ok {
		t.Fatal("the help does not fit")
	}
	plain := ansi.Strip(view)
	y = lineIndex(plain, "esc back")
	x = strings.Index(strings.Split(plain, "\n")[y], "esc")
	if act, handled := h.Click(NewMouseMap(targets, true).Handle(click(x, y))); act != "" || !handled || h.search != nil {
		t.Errorf("a click on the back key: %q %v, search open %v", act, handled, h.search != nil)
	}
}

// Back closes only the search and keeps the page; the help key closes the whole help, and "?" types.
func TestHelpSearchBackAndHelpKey(t *testing.T) {
	h := newSearchHelp(t, i18n.English)
	h.SetPage("water", "pump")
	searchFor(t, h, "water")
	if h.Input() != InputText {
		t.Error("the search does not report the text input")
	}
	if act, handled := helpKeysTo(h, "esc"); act != "" || !handled || h.search != nil {
		t.Fatalf("back in the search: %q %v, search open %v", act, handled, h.search != nil)
	}
	if topic, page := h.Page(); topic != "water" || page != "pump" {
		t.Errorf("back changed the page to %s/%s", topic, page)
	}
	if h.Input() != InputNavigation {
		t.Error("the closed search still takes text")
	}
	if act, handled := helpKeysTo(h, "esc"); act != "back" || !handled {
		t.Errorf("back on the page: %q %v", act, handled)
	}
	searchFor(t, h, "")
	if act, handled := helpKeysTo(h, "?"); act != "" || !handled || h.search.query.String() != "?" {
		t.Errorf("? in the search: %q %v, typed %q", act, handled, h.search.query.String())
	}
	if act, handled := h.Update(pressF9); act != "help" || !handled {
		t.Errorf("f9 in the search: %q %v", act, handled)
	}
	// Left and right move the cursor of the input, and the other keys edit it.
	h.search.query.clear()
	helpType(h, "ab")
	helpPress(h, "left")
	helpType(h, "x")
	if got := h.search.query.String(); got != "axb" {
		t.Errorf("the input after left: %q", got)
	}
	h.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := h.search.query.String(); got != "ab" {
		t.Errorf("the input after backspace: %q", got)
	}
	// Turning the page by a command closes the search too, and the outside click acts as back.
	h.OpenScreen("home")
	if h.search != nil {
		t.Error("OpenScreen left the search open")
	}
	searchFor(t, h, "a")
	h.spec.CloseOutside = true
	if act, handled := h.Click(MouseResult{Handled: true, Zone: ZoneOutside}); act != "" || !handled || h.search != nil {
		t.Errorf("a click outside the search: %q %v, search open %v", act, handled, h.search != nil)
	}
}

// Paste fills the input of an open search and nothing else.
func TestHelpSearchPaste(t *testing.T) {
	h := newSearchHelp(t, i18n.English)
	if h.Paste("water") {
		t.Error("a closed search took a paste")
	}
	searchFor(t, h, "")
	if !h.Paste("wat\ner") || h.search.query.String() != "wat er" {
		t.Errorf("paste: %q", h.search.query.String())
	}
}

// The key line of a page names the search key, the search names enter, back, up and down, and the overlay
// draws the hits inside the frame.
func TestHelpSearchOverlayAndHints(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newSearchHelp(t, i18n.English)
	room := Rect{Y: 2, Width: 100, Height: 24}
	draw := func() (string, []Target) {
		view, targets, ok := h.Overlay(st, sampleScreen(100, 26), 100, 26, room)
		if !ok {
			t.Fatal("the help does not fit")
		}
		return ansi.Strip(view), targets
	}
	plain, _ := draw()
	if !strings.Contains(plain, "/ search") {
		t.Errorf("the key line of a page names no search key:\n%s", plain)
	}
	searchFor(t, h, "water")
	plain, targets := draw()
	if !inOrder(plain, "Help", "Watering", "Tips", "enter choose") || strings.Contains(plain, "/ search") {
		t.Fatalf("the search over the screen:\n%s", plain)
	}
	keyLine := strings.Split(plain, "\n")[lineIndex(plain, "enter choose")]
	for _, want := range []string{"esc back", "up", "down"} {
		if !strings.Contains(keyLine, want) {
			t.Errorf("the key line of the search lacks %q: %q", want, keyLine)
		}
	}
	if strings.Contains(keyLine, "? close") {
		t.Errorf("the key line offers the help key that types: %q", keyLine)
	}
	lines := strings.Split(plain, "\n")
	y := lineIndex(plain, "Rain barrel")
	m := NewMouseMap(targets, true)
	if _, handled := h.Click(m.Handle(click(strings.Index(lines[y], "Rain"), y))); !handled {
		t.Fatal("a click on a hit of the overlay was not handled")
	}
	if topic, page := h.Page(); topic != "water" || page != "rain" {
		t.Errorf("the overlay click opened %s/%s", topic, page)
	}
	// A room too small for the overlay leaves the search to View.
	h.search = nil
	searchFor(t, h, "water")
	if _, _, ok := h.Overlay(st, sampleScreen(100, 26), 100, 26, Rect{Width: 100, Height: 5}); ok {
		t.Error("the overlay drew in a room too small")
	}
}

// Over topics the key does nothing and no key line names it, and the other keys still work.
func TestHelpSearchKeyIsInertOverTopics(t *testing.T) {
	keys := f9Keymap(t)
	spec := sampleHelpSpec()
	spec.Keys.Search, spec.Keys.Enter = "search", "enter"
	h, err := NewHelp(spec, keys, newTranslator(t, i18n.English, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	if act, handled := helpKeysTo(h, "/"); act != "" || handled || h.search != nil || h.Input() != InputNavigation {
		t.Errorf("the search key over topics: %q %v", act, handled)
	}
	if act, handled := h.Act("search"); act != "" || handled {
		t.Errorf("Act(search) over topics: %q %v", act, handled)
	}
	for _, hint := range h.Hints() {
		if hint.Action == "search" {
			t.Error("the key line names the search key over topics")
		}
	}
}

func TestNewHelpChecksTheSearchKeys(t *testing.T) {
	keys := f9Keymap(t)
	text := newTranslator(t, i18n.English, helpCatalogs(), keys)
	for name, edit := range map[string]func(*HelpKeys){
		"no enter": func(k *HelpKeys) { k.Enter = "" },
		"unknown":  func(k *HelpKeys) { k.Search = "gone" },
		"shared":   func(k *HelpKeys) { k.Search = "help" },
		"enter":    func(k *HelpKeys) { k.Enter = "search" },
	} {
		spec := searchSpec(i18n.English)
		edit(&spec.Keys)
		if _, err := NewHelp(spec, keys, text); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// The overview and the search may stand together, and neither needs the other.
	spec := searchSpec(i18n.English)
	spec.Keys.First, spec.Keys.Last = "", ""
	if _, err := NewHelp(spec, keys, text); err != nil {
		t.Errorf("a search without the overview needs no first and last key: %v", err)
	}
}

func TestGoldenHelpSearch(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		lang        i18n.Lang
		w, h        int
	}{
		{"helpsearch.narrow.golden", "water", i18n.English, 30, 14},
		{"helpsearch.wide.golden", "water", i18n.English, 100, 14},
		{"helpsearch.narrow.de.golden", "sommer", i18n.German, 30, 14},
		{"helpsearch.wide.de.golden", "sommer", i18n.German, 100, 14},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newSearchHelp(t, tt.lang)
			searchFor(t, h, tt.query)
			out, _ := h.View(looks(t)["noneascii"], tt.w, tt.h)
			assertSize(t, out, tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

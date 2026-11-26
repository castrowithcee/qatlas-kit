package tui

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// samplePages are invented garden pages: two topics of two pages each, the first in English and German.
func samplePages() fstest.MapFS {
	file := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	return fstest.MapFS{
		"start/index.md": file("---\ntitle: Getting started\norder: 1\nscreens: [home]\nlink: https://garden.example/start\n---\n" +
			"Plant a **seed** first, then press {save} to keep the bed.\n\n" +
			"## Beds\n" +
			"- Choose a bed with `loam` and enough light for the whole summer.\n" +
			"  - Dig a small hole.\n" +
			"1. Water it.\n\n" +
			"Read [the guide](https://garden.example/guide) before you sow.\n\n" +
			"| table | stays |\n\n" +
			"A bell\x07 and an escape\x1b[2J stay masked.\n"),
		"start/index.de.md": file("---\ntitle: Erste Schritte\n---\n" +
			"Säe zuerst einen **Samen** und drücke {save}, um das Beet zu sichern.\n\n" +
			"## Beete\n" +
			"- Wähle ein Beet mit `Lehm` und genug Licht für den ganzen Sommer.\n" +
			"  - Grabe ein kleines Loch.\n" +
			"1. Gieße es.\n\n" +
			"Lies [die Anleitung](https://garden.example/guide) vor dem Säen.\n"),
		"start/beds.md":  file("---\ntitle: Beds\nscreens: [beds, beds.form]\n---\nA bed holds plants.\n"),
		"water/index.md": file("---\ntitle: Watering\norder: 2\nscreens: [water]\n---\nWater in the morning.\n"),
		"water/pump.md":  file("---\ntitle: Pump\n---\nThe pump runs for one minute.\n"),
	}
}

func samplePageSpec() HelpSpec {
	spec := sampleHelpSpec()
	spec.Topics, spec.Pages = nil, samplePages()
	return spec
}

func newPageHelp(t *testing.T, keys *Keymap, lang i18n.Lang) *Help {
	t.Helper()
	if keys == nil {
		keys = sampleKeymap(t, CharsetUnicode)
	}
	spec := samplePageSpec()
	spec.Lang = lang
	h, err := NewHelp(spec, keys, newTranslator(t, lang, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func pageView(h *Help, look Styles, width int) string {
	view, _ := h.View(look, width, 200)
	return view
}

func TestNewHelpRejectsPagesThatCannotWork(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, helpCatalogs(), keys)
	page := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	cases := []struct {
		name string
		edit func(*HelpSpec)
		want string
	}{
		{"topics and pages", func(s *HelpSpec) { s.Topics = sampleTopics() }, "exclude each other"},
		{"neither", func(s *HelpSpec) { s.Pages = nil }, "no topic and no pages"},
		{"unknown language", func(s *HelpSpec) { s.Lang = "fr" }, "unknown language"},
		{"unknown placeholder", func(s *HelpSpec) {
			s.Pages.(fstest.MapFS)["water/pump.md"] = page("---\ntitle: Pump\n---\nPress {dig}.\n")
		}, "dig"},
		{"missing English page", func(s *HelpSpec) {
			s.Pages.(fstest.MapFS)["water/hose.de.md"] = page("---\ntitle: Schlauch\n---\nText.\n")
		}, "translation without"},
		{"repeated screen", func(s *HelpSpec) {
			s.Pages.(fstest.MapFS)["water/pump.md"] = page("---\ntitle: Pump\nscreens: [beds]\n---\nText.\n")
		}, "also carried by"},
		{"unknown key", func(s *HelpSpec) {
			s.Pages.(fstest.MapFS)["water/pump.md"] = page("---\ntitle: Pump\ncolour: green\n---\nText.\n")
		}, "colour"},
		{"bad link", func(s *HelpSpec) {
			s.Pages.(fstest.MapFS)["water/pump.md"] = page("---\ntitle: Pump\nlink: http://garden.example\n---\nText.\n")
		}, "no https address"},
	}
	for _, tt := range cases {
		spec := samplePageSpec()
		tt.edit(&spec)
		if _, err := NewHelp(spec, keys, text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := NewHelp(samplePageSpec(), keys, text); err != nil {
		t.Errorf("the sample pages: %v", err)
	}
}

// The help opens on the page of the first screen ID that has one, else on the first page, from its top.
func TestHelpOpenScreenShowsTheFirstPageWithTheScreen(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newPageHelp(t, nil, i18n.English)
	if topic, name := h.Page(); topic != "start" || name != "index" {
		t.Fatalf("a new help shows %s/%s", topic, name)
	}
	for _, tt := range []struct {
		ids         []string
		found       bool
		topic, page string
	}{
		{[]string{"beds.form", "water"}, true, "start", "beds"},
		{[]string{"dialog.none", "water", "beds"}, true, "water", "index"},
		{[]string{"dialog.none"}, false, "start", "index"},
		{nil, false, "start", "index"},
	} {
		h.SetPage("water", "pump")
		h.View(st, 60, 8)
		h.scroll.offset = 1
		found := h.OpenScreen(tt.ids...)
		if topic, page := h.Page(); found != tt.found || topic != tt.topic || page != tt.page || h.scroll.offset != 0 {
			t.Errorf("%v: %v %s/%s at %d, want %v %s/%s at the top", tt.ids, found, topic, page, h.scroll.offset,
				tt.found, tt.topic, tt.page)
		}
	}
	topics := newSampleHelp(t, nil, i18n.English)
	topics.SetTopic("harvest")
	if topics.OpenScreen("home") || topics.Topic() != "start" {
		t.Errorf("over topics OpenScreen shows %q", topics.Topic())
	}
}

// Left and right, tab and shift+tab turn the pages topic by topic, each topic's index first, with
// wrap-around; Topic and SetTopic name the topic of the page.
func TestHelpKeysTurnThePages(t *testing.T) {
	h := newPageHelp(t, nil, i18n.English)
	for _, step := range []struct{ key, topic, page string }{
		{"right", "start", "beds"}, {"tab", "water", "index"}, {"right", "water", "pump"}, {"right", "start", "index"},
		{"left", "water", "pump"}, {"shift+tab", "water", "index"},
	} {
		act, handled := helpKeysTo(h, step.key)
		if topic, page := h.Page(); !handled || act != "" || topic != step.topic || page != step.page || h.Topic() != step.topic {
			t.Fatalf("%s: %s/%s, act %q, handled %v; want %s/%s", step.key, topic, page, act, handled, step.topic, step.page)
		}
	}
	if !h.SetTopic("start") || !h.SetPage("water", "pump") {
		t.Fatal("a known topic or page was refused")
	}
	if h.SetTopic("harvest") || h.SetPage("water", "hose") || h.SetPage("start", "") {
		t.Error("an unknown topic or page was taken")
	}
	if topic, page := h.Page(); topic != "water" || page != "pump" {
		t.Errorf("a refused page moved the help to %s/%s", topic, page)
	}
	topics := newSampleHelp(t, nil, i18n.English)
	if topic, page := topics.Page(); topic != "start" || page != "" || !topics.SetPage("harvest", "") ||
		topics.SetPage("start", "index") || topics.Topic() != "harvest" {
		t.Errorf("over topics Page is %s/%q and SetPage moved to %q", topic, page, topics.Topic())
	}
}

// A page shows in the language of the help where it has one, else in English.
func TestHelpPageShowsItsLanguage(t *testing.T) {
	st := looks(t)["noneascii"]
	de := newPageHelp(t, nil, i18n.German)
	if view := pageView(de, st, 80); !strings.Contains(view, "Erste Schritte") || !strings.Contains(view, "Mehr dazu") ||
		strings.Contains(view, "Getting started") {
		t.Errorf("German page:\n%s", view)
	}
	de.SetPage("water", "pump")
	if view := pageView(de, st, 80); !strings.Contains(view, "The pump runs") {
		t.Errorf("a page without German is not shown in English:\n%s", view)
	}
	if view := pageView(newPageHelp(t, nil, i18n.English), st, 80); !strings.Contains(view, "Getting started") ||
		!strings.Contains(view, "More") {
		t.Errorf("English page:\n%s", view)
	}
}

// A key placeholder shows the key bound now, also after a new binding.
func TestHelpPagePlaceholderFollowsTheBinding(t *testing.T) {
	st := looks(t)["noneascii"]
	keys := sampleKeymap(t, CharsetUnicode)
	h := newPageHelp(t, keys, i18n.English)
	if view := pageView(h, st, 80); !strings.Contains(view, "then press F2 to keep") {
		t.Errorf("the placeholder is not resolved:\n%s", view)
	}
	if err := keys.Bind(map[ActionID]string{"save": "f4"}); err != nil {
		t.Fatal(err)
	}
	if view := pageView(h, st, 80); !strings.Contains(view, "then press F4 to keep") || strings.Contains(view, "F2 to keep") {
		t.Errorf("the placeholder does not follow the binding:\n%s", view)
	}
}

// The page draws its subset of Markdown: headings, wrapped paragraphs, items with their marks and hanging
// indent, bold, code and links with their address; anything else stays plain and control characters are
// masked.
func TestHelpPageDrawsTheMarkdownSubset(t *testing.T) {
	all := looks(t)
	h := newPageHelp(t, nil, i18n.English)
	view := pageView(h, all["noneascii"], 40)
	lines := strings.Split(view, "\n")
	for _, want := range []string{
		"Getting started",
		"Plant a seed first, then press F2 to",
		"keep the bed.",
		"Beds",
		"- Choose a bed with loam and enough",
		"  light for the whole summer.",
		"  - Dig a small hole.",
		"1. Water it.",
		"Read the guide",
		"(https://garden.example/guide) before",
		"| table | stays |",
		`A bell\a and an escape\x1b[2J stay`,
		"More",
		"https://garden.example/start",
	} {
		if lineIndex(view, want) < 0 || !strings.HasPrefix(lines[lineIndex(view, want)], want) {
			t.Errorf("%q is not drawn at the start of a line:\n%s", want, view)
		}
	}
	if strings.ContainsAny(view, "\x1b\x07") || strings.Contains(view, "**") || strings.Contains(view, "`") {
		t.Errorf("marks or control characters reach the view:\n%q", view)
	}
	if blank := lineIndex(view, "Beds") - 1; strings.TrimSpace(lines[blank]) != "" ||
		lineIndex(view, "- Choose") != lineIndex(view, "Beds")+1 || lineIndex(view, "1. Water") != lineIndex(view, "  - Dig")+1 {
		t.Errorf("blocks are spaced wrongly:\n%s", view)
	}
	unicode := pageView(h, all["dark"], 60)
	for _, want := range []string{all["dark"].Render(StyleTitle, "seed"), all["dark"].Render(StyleSyntaxString, "loam"),
		all["dark"].Render(StyleTitle, "Beds"), "• "} {
		if !strings.Contains(unicode, want) {
			t.Errorf("%q is not drawn in its style:\n%q", want, unicode)
		}
	}
}

// An address is a hyperlink only where the profile takes it, and always shown as text.
func TestHelpPageLinksOnlyWhereTheProfileTakesThem(t *testing.T) {
	for name, st := range looks(t) {
		view := pageView(newPageHelp(t, nil, i18n.English), st, 80)
		plain := ansi.Strip(view)
		if !strings.Contains(plain, "the guide (https://garden.example/guide)") || !strings.Contains(plain, "https://garden.example/start") {
			t.Errorf("%s: an address is not shown:\n%s", name, plain)
		}
		for _, url := range []string{"https://garden.example/guide", "https://garden.example/start"} {
			if linked, want := strings.Contains(view, ansi.SetHyperlink(url)), st.profile != ProfileNone; linked != want {
				t.Errorf("%s: %s is linked = %v, want %v", name, url, linked, want)
			}
		}
		if st.profile == ProfileNone && plain != view {
			t.Errorf("%s: without colour the page draws escapes", name)
		}
	}
	// A long address breaks hard and every piece links to the whole address.
	st := looks(t)["dark"]
	view := pageView(newPageHelp(t, nil, i18n.English), st, 12)
	if n := strings.Count(view, ansi.SetHyperlink("https://garden.example/start")); n < 2 {
		t.Errorf("the address in %d linked pieces:\n%s", n, ansi.Strip(view))
	}
}

// The key lists end the context page and no other page.
func TestHelpPageKeyListsOnlyOnTheContextPage(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newPageHelp(t, nil, i18n.English)
	has := func() bool {
		v := pageView(h, st, 70)
		return strings.Contains(v, "This screen") && strings.Contains(v, "F2 save")
	}
	if !has() {
		t.Error("the first page after NewHelp has no key lists")
	}
	h.OpenScreen("beds")
	if !has() || lineIndex(pageView(h, st, 70), "This screen") < lineIndex(pageView(h, st, 70), "A bed holds") {
		t.Error("the context page does not end with the key lists")
	}
	for _, key := range []string{"right", "right", "right"} {
		helpKeysTo(h, key)
		if has() {
			t.Errorf("after %s the page %v lists the keys", key, h.pages[h.page].page.Name)
		}
	}
}

// Over pages the help has no tabs; it fits over the screen, scrolls, keeps its note, its key targets and the
// selection, which takes the text without the marks.
func TestHelpPageDrawsLikeATopic(t *testing.T) {
	st := looks(t)["noneascii"]
	spec := samplePageSpec()
	spec.Note = "h.new"
	keys := sampleKeymap(t, CharsetUnicode)
	h, err := NewHelp(spec, keys, newTranslator(t, i18n.English, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	room := Rect{Y: 2, Width: 100, Height: 17}
	view, targets, ok := h.Overlay(st, sampleScreen(100, 20), 100, 20, room)
	if !ok {
		t.Fatal("the help does not fit")
	}
	assertSize(t, view, 100, 20)
	plain := ansi.Strip(view)
	if strings.Contains(plain, "[start]") || lineIndex(plain, "new plant") >= lineIndex(plain, "Getting started") ||
		!strings.Contains(plain, "lines 1-") {
		t.Errorf("tabs, note or position wrong:\n%s", plain)
	}
	m := NewMouseMap(targets, true)
	lines := strings.Split(plain, "\n")
	y := lineIndex(plain, "pgup/pgdown page")
	if y < 0 {
		t.Fatalf("no page keys in the key line:\n%s", plain)
	}
	x := ansi.StringWidth(lines[y][:strings.Index(lines[y], "pgdown page")])
	if act, handled := h.Click(m.Handle(click(x, y))); !handled || act != "" || h.scroll.offset == 0 {
		t.Errorf("a click on page up/down: %q %v, offset %d", act, handled, h.scroll.offset)
	}
	helpKeysTo(h, "home", "down")
	if h.scroll.offset != 1 {
		t.Errorf("down scrolled to %d", h.scroll.offset)
	}
	helpKeysTo(h, "home")
	rows, mm := framed(t, h.View, "noneascii", 80, 30)
	sx, sy := at(t, rows, "Choose")
	_, ey := at(t, rows, "  - Dig")
	got, ok := drag(new(TextSelection), mm, sx, sy, 79, ey)
	if want := "Choose a bed with loam and enough light for the whole summer.\nDig a small hole."; !ok || got != want {
		t.Errorf("selection %q %v, want %q", got, ok, want)
	}
}

func TestGoldenHelpPage(t *testing.T) {
	for _, tt := range []struct {
		name string
		lang i18n.Lang
		w, h int
	}{
		{"helppage.narrow.golden", i18n.English, 36, 32},
		{"helppage.wide.golden", i18n.English, 100, 24},
		{"helppage.narrow.de.golden", i18n.German, 36, 32},
		{"helppage.wide.de.golden", i18n.German, 100, 24},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := newPageHelp(t, nil, tt.lang).View(looks(t)["noneascii"], tt.w, tt.h)
			assertSize(t, out, tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

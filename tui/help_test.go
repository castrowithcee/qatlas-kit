package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/i18n"
)

func helpCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {"sample.save": "save", "sample.help": "help", "sample.command": "commands", "h.water": "water",
			"h.new": "new plant"},
		i18n.German: {"sample.save": "Speichern", "sample.help": "Hilfe", "sample.command": "Befehle", "h.water": "gießen",
			"h.new": "neue Pflanze"},
	}
}

// sampleTopics are invented guides; the second is long enough to scroll in every size the tests draw.
func sampleTopics() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "start", Short: "Water a first plant", Text: "Plant a seed first.\n\nBeds:\n1. Choose a bed and dig a small hole for the seed."},
		{Name: "watering", Short: "When and how much", Text: strings.Repeat("Water in the morning, never at noon. ", 60) +
			"\n\nPump:\n- The pump runs for one minute and stops by itself.\nlast line of watering"},
		{Name: "harvest", Short: "Picking", Text: "Pick when ripe."},
	}
}

func sampleHelpKeys() HelpKeys {
	return HelpKeys{Left: "left", Right: "right", Next: "topic.next", Prev: "topic.prev", Up: "up", Down: "down",
		PageUp: "pageup", PageDown: "pagedown", First: "first", Last: "last", Back: "back", Help: "help"}
}

func sampleHelpSpec() HelpSpec {
	return HelpSpec{Topics: sampleTopics(), Keys: sampleHelpKeys(),
		Screen: []Hint{{Action: "new", Label: "h.new"}, {Action: "upgrade", Label: "h.water"}},
		Global: []ActionID{"save", "help", "command"}}
}

func newSampleHelp(t *testing.T, keys *Keymap, lang i18n.Lang) *Help {
	t.Helper()
	if keys == nil {
		keys = sampleKeymap(t, CharsetUnicode)
	}
	h, err := NewHelp(sampleHelpSpec(), keys, newTranslator(t, lang, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func helpKeysTo(h *Help, names ...string) (act ActionID, handled bool) {
	for _, n := range names {
		act, handled = h.Update(press(n))
	}
	return act, handled
}

func TestNewHelpRejectsWhatCannotWork(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, helpCatalogs(), keys)
	cases := []struct {
		name string
		edit func(*HelpSpec)
		want string
	}{
		{"no topic", func(s *HelpSpec) { s.Topics = nil }, "no topic"},
		{"empty name", func(s *HelpSpec) { s.Topics[1].Name = "" }, "empty or holds"},
		{"blank in name", func(s *HelpSpec) { s.Topics[1].Name = "when to water" }, "empty or holds"},
		{"control in name", func(s *HelpSpec) { s.Topics[1].Name = "water\x1b" }, "empty or holds"},
		{"repeated name", func(s *HelpSpec) { s.Topics[2].Name = "start" }, "listed twice"},
		{"no text", func(s *HelpSpec) { s.Topics[0].Text = " \n" }, "has no text"},
		{"missing key", func(s *HelpSpec) { s.Keys.PageDown = "" }, "page down key is missing"},
		{"missing close key", func(s *HelpSpec) { s.Keys.Help = "" }, "help key is missing"},
		{"unknown key", func(s *HelpSpec) { s.Keys.First = "jump" }, "no action of the key table"},
		{"shared key", func(s *HelpSpec) { s.Keys.Next = "right" }, "also the right key"},
		{"unknown screen key", func(s *HelpSpec) { s.Screen = append(s.Screen, Hint{Action: "dig", Label: "h.new"}) }, "screen key"},
		{"unknown global key", func(s *HelpSpec) { s.Global = append(s.Global, "dig") }, "global key"},
		{"repeated global key", func(s *HelpSpec) { s.Global = append(s.Global, "save") }, "listed twice"},
	}
	for _, tt := range cases {
		spec := sampleHelpSpec()
		tt.edit(&spec)
		if _, err := NewHelp(spec, keys, text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := NewHelp(sampleHelpSpec(), nil, text); err == nil {
		t.Error("a help without key table was accepted")
	}
	spec := sampleHelpSpec()
	spec.Keys.Next, spec.Keys.Prev, spec.Keys.First, spec.Keys.Last = "", "", "", ""
	spec.Screen, spec.Global = nil, nil
	if _, err := NewHelp(spec, keys, text); err != nil {
		t.Errorf("a help without the optional keys and key lists: %v", err)
	}
}

// Left and right, tab and shift+tab change the topic with wrap-around and start it at its top; the scroll
// keys stop at the ends; back and help close; every other key is left to the application.
func TestHelpKeysChangeTopicScrollAndClose(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newSampleHelp(t, nil, i18n.English)
	for _, step := range []struct {
		key  string
		want string
	}{{"right", "watering"}, {"tab", "harvest"}, {"right", "start"}, {"left", "harvest"}, {"shift+tab", "watering"}} {
		if act, handled := helpKeysTo(h, step.key); !handled || act != "" || h.Topic() != step.want {
			t.Fatalf("%s: topic %q, act %q, handled %v; want %q", step.key, h.Topic(), act, handled, step.want)
		}
	}
	h.View(st, 60, 12)
	helpKeysTo(h, "down", "down")
	if h.scroll.offset != 2 {
		t.Errorf("down twice scrolled to %d", h.scroll.offset)
	}
	helpKeysTo(h, "end")
	end := h.scroll.offset
	if end == 0 || end != h.scroll.last {
		t.Errorf("end scrolled to %d of %d", end, h.scroll.last)
	}
	view, _ := h.View(st, 60, 12)
	if !strings.Contains(view, "last line of watering") {
		t.Errorf("end does not show the last line:\n%s", view)
	}
	helpKeysTo(h, "down")
	if h.scroll.offset != end {
		t.Error("down scrolled past the end")
	}
	helpKeysTo(h, "home", "pgdown")
	if h.scroll.offset != h.scroll.page-1 {
		t.Errorf("pgdown from the top scrolled to %d, a page is %d", h.scroll.offset, h.scroll.page)
	}
	helpKeysTo(h, "up", "pgup")
	if h.scroll.offset != 0 {
		t.Errorf("pgup did not stop at the top: %d", h.scroll.offset)
	}
	helpKeysTo(h, "pgdown", "right")
	if h.scroll.offset != 0 {
		t.Error("a new topic does not start at its top")
	}
	for _, key := range []string{"esc", "?"} {
		if act, handled := helpKeysTo(h, key); !handled || (act != "back" && act != "help") {
			t.Errorf("%s: act %q, handled %v, want the close action", key, act, handled)
		}
	}
	for _, key := range []string{"f2", "n", "enter", "ctrl+c"} {
		if act, handled := helpKeysTo(h, key); handled || act != "" {
			t.Errorf("%s was taken by the help: act %q", key, act)
		}
	}
	if !h.SetTopic("start") || h.SetTopic("weeds") || h.Topic() != "start" {
		t.Error("SetTopic does not keep the topic for an unknown name")
	}
}

// The first topic ends with the keys of the screen it was opened from and the keys that act everywhere, in
// the binding in force and in the language of the help; no other topic has them.
func TestFirstTopicEndsWithTheKeysInForce(t *testing.T) {
	st := looks(t)["noneascii"]
	keys := sampleKeymap(t, CharsetUnicode)
	if err := keys.Bind(map[ActionID]string{"save": "f4"}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		lang                   i18n.Lang
		screen, all, save, new string
	}{
		{i18n.English, "This screen", "Everywhere", "F4 save", "n new plant"},
		{i18n.German, "Dieser Bildschirm", "Überall", "F4 Speichern", "n neue Pflanze"},
	} {
		h := newSampleHelp(t, keys, tt.lang)
		view, _ := h.View(st, 70, 40)
		lines := strings.Split(view, "\n")
		at := func(text string) int {
			for i, l := range lines {
				if strings.TrimSpace(l) == text {
					return i
				}
			}
			return -1
		}
		screen, all, save, plant := at(tt.screen), at(tt.all), at(tt.save), at(tt.new)
		if screen < 0 || all < 0 || save < 0 || plant < 0 || !(screen < plant && plant < all && all < save) {
			t.Errorf("%s: the key lists are missing or out of order (%d %d %d %d):\n%s", tt.lang, screen, plant, all, save, view)
		}
		if strings.Contains(view, "F2") {
			t.Errorf("%s: the key list names the default key, not the one in force:\n%s", tt.lang, view)
		}
		if at("? "+helpCatalogs()[tt.lang]["sample.help"]) < 0 || at("U "+helpCatalogs()[tt.lang]["h.water"]) < 0 {
			t.Errorf("%s: a listed key is missing:\n%s", tt.lang, view)
		}
		helpKeysTo(h, "right")
		if view, _ := h.View(st, 70, 40); strings.Contains(view, tt.all) {
			t.Errorf("%s: the second topic lists the keys:\n%s", tt.lang, view)
		}
	}
}

// The help shows the topic text as the command line wraps it, from the same topics.
func TestHelpShowsTheTopicTextOfTheCommandLine(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newSampleHelp(t, nil, i18n.English)
	helpKeysTo(h, "right")
	const width = 50
	view, _ := h.View(st, width, 200)
	want := strings.Split(helptopics.Wrap(sampleTopics()[1].Text, width), "\n")
	got := strings.Split(view, "\n")[2:]
	for i, line := range want {
		if strings.TrimRight(got[i], " ") != line {
			t.Fatalf("line %d is %q, the command line wraps %q", i, got[i], line)
		}
	}
}

// The help stands over the screen where room has space for it and fills the workspace otherwise; it keeps
// its size at every width, and a topic that scrolls shows its position.
func TestHelpFitsOverTheScreenAndInTheWorkspace(t *testing.T) {
	for name, st := range looks(t) {
		h := newSampleHelp(t, nil, i18n.English)
		helpKeysTo(h, "right")
		for _, size := range []struct{ width, height int }{{100, 30}, {60, 20}, {44, 12}, {30, 10}} {
			room := Rect{Y: 2, Width: size.width, Height: size.height - 3}
			view, targets, ok := h.Overlay(st, sampleScreen(size.width, size.height), size.width, size.height, room)
			wantOK := size.width >= dialogMinInner+8 && room.Height-8 >= helpMinText
			if ok != wantOK {
				t.Fatalf("%s %dx%d: over the screen = %v, want %v", name, size.width, size.height, ok, wantOK)
			}
			if ok {
				assertSize(t, view, size.width, size.height)
				plain := ansi.Strip(view)
				if !strings.Contains(plain, "Help") || !strings.Contains(plain, "watering") || !strings.Contains(plain, "lines 1-") {
					t.Errorf("%s %dx%d: title, topic or position missing:\n%s", name, size.width, size.height, plain)
				}
				for _, tg := range targets {
					if tg.Rect.Y < room.Y || tg.Rect.Y >= room.Y+room.Height || tg.Rect.X+tg.Rect.Width > size.width {
						t.Errorf("%s: target %+v lies outside the help", name, tg)
					}
				}
			}
			page, _ := h.View(st, size.width, size.height-3)
			assertSize(t, page, size.width, size.height-3)
			if !strings.Contains(ansi.Strip(page), "lines 1-") {
				t.Errorf("%s %dx%d: the page does not say it scrolls:\n%s", name, size.width, size.height, page)
			}
		}
		if page, targets := h.View(st, 0, 0); page != "" || targets != nil {
			t.Errorf("%s: a page without room drew %q", name, page)
		}
	}
}

// assertSize fails unless view is exactly height lines of width cells.
func assertSize(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), height, view)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != width {
			t.Fatalf("line %d is %d cells, want %d: %q", i, w, width, l)
		}
	}
}

// Every topic tab, every key of the key line and every line of the key list is a target, and a click does
// what its key does.
func TestEveryHelpControlHasATargetAndClicksAsItsKey(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newSampleHelp(t, nil, i18n.English)
	view, targets, ok := h.Overlay(st, sampleScreen(100, 40), 100, 40, Rect{Y: 2, Width: 100, Height: 37})
	if !ok {
		t.Fatal("the help does not fit")
	}
	lines := strings.Split(view, "\n")
	covered := func(x, y int) bool {
		for _, tg := range targets {
			r := tg.Rect
			if x >= r.X && x < r.X+r.Width && y == r.Y {
				return true
			}
		}
		return false
	}
	find := func(text string) (int, int) {
		for y, l := range lines {
			if i := strings.Index(l, text); i >= 0 {
				return ansi.StringWidth(l[:i]), y
			}
		}
		t.Fatalf("%q is not drawn:\n%s", text, view)
		return 0, 0
	}
	for _, text := range []string{"[start]", "watering", "harvest", "n new plant", "U water", "F2 save", "? help",
		": commands", "tab/shift+tab topic", "pgup/pgdown page", "esc/? close"} {
		x, y := find(text)
		for i := range len(text) {
			if text[i] != ' ' && text[i] != '/' && !covered(x+i, y) {
				t.Errorf("cell %d of %q has no target", i, text)
			}
		}
	}
	m := NewMouseMap(targets, true)
	x, y := find("harvest")
	if act, handled := h.Click(m.Handle(click(x, y))); !handled || act != "" || h.Topic() != "harvest" {
		t.Errorf("a click on the harvest tab: topic %q, act %q, handled %v", h.Topic(), act, handled)
	}
	h.SetTopic("start")
	x, y = find("F2 save")
	if act, handled := h.Click(m.Handle(click(x, y))); !handled || act != "save" {
		t.Errorf("a click on the save line yields %q, %v", act, handled)
	}
	for _, tg := range targets {
		if tg.Action == "" {
			continue
		}
		byKey := newSampleHelp(t, nil, i18n.English)
		byClick := newSampleHelp(t, nil, i18n.English)
		byKey.Overlay(st, sampleScreen(100, 40), 100, 40, Rect{Y: 2, Width: 100, Height: 37})
		byClick.Overlay(st, sampleScreen(100, 40), 100, 40, Rect{Y: 2, Width: 100, Height: 37})
		want, wantHandled := byKey.Act(tg.Action)
		if !wantHandled {
			want, wantHandled = tg.Action, true
		}
		got, handled := byClick.Click(m.Handle(click(tg.Rect.X, tg.Rect.Y)))
		if got != want || handled != wantHandled || byClick.Topic() != byKey.Topic() || byClick.scroll != byKey.scroll {
			t.Errorf("a click on %q gives %q %v, its key %q %v", tg.Action, got, handled, want, wantHandled)
		}
	}
	if act, handled := h.Click(MouseResult{}); handled || act != "" {
		t.Error("an empty mouse result acted")
	}
}

// The wheel scrolls the text three lines a notch and stops at both ends.
func TestHelpWheelStopsAtTheEnds(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newSampleHelp(t, nil, i18n.English)
	helpKeysTo(h, "right")
	h.View(st, 60, 12)
	h.Wheel(1)
	if h.scroll.offset != WheelTextLines {
		t.Errorf("a notch scrolled to %d", h.scroll.offset)
	}
	h.Wheel(-5)
	if h.scroll.offset != 0 {
		t.Errorf("the wheel went above the top: %d", h.scroll.offset)
	}
	h.Wheel(1000)
	if h.scroll.offset != h.scroll.last {
		t.Errorf("the wheel went past the end: %d of %d", h.scroll.offset, h.scroll.last)
	}
}

// A heading stands in bold, the keys of the key list in the key colour, and without colour the help draws no
// escape sequence and keeps every mark.
func TestHelpStylesHeadingsAndKeys(t *testing.T) {
	all := looks(t)
	h := newSampleHelp(t, nil, i18n.English)
	view, _ := h.View(all["dark"], 70, 40)
	if !strings.Contains(view, all["dark"].Render(StyleTitle, "Beds:")) {
		t.Errorf("the heading is not bold:\n%q", view)
	}
	if !strings.Contains(view, all["dark"].Render(StyleFooterKey, "F2")) {
		t.Errorf("the key is not in the key colour:\n%q", view)
	}
	plain, _ := h.View(all["noneascii"], 70, 40)
	if ansi.Strip(plain) != plain || !strings.Contains(plain, "[start]") {
		t.Errorf("without colour the help draws escapes or loses the mark of the topic:\n%q", plain)
	}
}

// Control characters of a topic never reach the terminal.
func TestHelpEscapesControlCharacters(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	spec := sampleHelpSpec()
	spec.Topics[0].Text = "Plant\x1b[2J a seed\x07 first.\r"
	h, err := NewHelp(spec, keys, newTranslator(t, i18n.English, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	for name, st := range looks(t) {
		view, _ := h.View(st, 70, 30)
		over, _, _ := h.Overlay(st, sampleScreen(90, 30), 90, 30, Rect{Y: 2, Width: 90, Height: 27})
		for _, v := range []string{view, over} {
			if strings.ContainsAny(ansi.Strip(v), "\x1b\x07\r") || !strings.Contains(v, `Plant\x1b[2J a seed\a first.\r`) {
				t.Errorf("%s: a control character reached the view:\n%q", name, v)
			}
		}
	}
}

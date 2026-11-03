package tui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/i18n"
)

// explanationKeymap is the sample table with a copy key and a key that opens a link.
func explanationKeymap(t *testing.T) *Keymap {
	t.Helper()
	actions := append(sampleActions(),
		Action{ID: "copy", Level: LevelLetter, DefaultKeys: []string{"c"}, Help: "sample.copy"},
		Action{ID: "open", Level: LevelLetter, DefaultKeys: []string{"o"}, Help: "sample.open"})
	k, err := NewKeymap(actions, CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sampleExplanation() ExplanationItem {
	return ExplanationItem{Title: "Bed", Body: "The bed the plant grows in; its soil decides how often it needs water.",
		Term: helptopics.Term{ID: "term.bed", Title: "Bed", Text: "A bed is one patch of soil. Several plants can share a bed."},
		URL:  "https://garden.example/guide/beds"}
}

// withUser is an address that carries user information.
func withUser() string {
	return (&url.URL{Scheme: "https", User: url.UserPassword("gardener", "example"), Host: "garden.example", Path: "/"}).String()
}

func sampleExplanationKeys() ExplanationKeys {
	return ExplanationKeys{Back: "back", Info: "info", Copy: "copy", Up: "up", Down: "down", Link: "open"}
}

func newSampleExplanation(t *testing.T, item ExplanationItem, lang i18n.Lang) *Explanation {
	t.Helper()
	keys := explanationKeymap(t)
	e, err := NewExplanation(ExplanationSpec{Item: item, Keys: sampleExplanationKeys()}, keys, newTranslator(t, lang, nil, keys))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestNewExplanationRejectsWhatCannotWork(t *testing.T) {
	keys := explanationKeymap(t)
	text := newTranslator(t, i18n.English, nil, keys)
	cases := []struct {
		name string
		edit func(*ExplanationSpec)
		want string
	}{
		{"missing key", func(s *ExplanationSpec) { s.Keys.Copy = "" }, "copy key is missing"},
		{"unknown key", func(s *ExplanationSpec) { s.Keys.Link = "browse" }, "no action of the key table"},
		{"shared key", func(s *ExplanationSpec) { s.Keys.Info = "back" }, "also the back key"},
		{"nothing to explain", func(s *ExplanationSpec) { s.Item = ExplanationItem{Title: "Bed", Body: " "} }, "nothing to explain"},
		{"term without ID", func(s *ExplanationSpec) { s.Item.Term.ID = "" }, "an ID"},
		{"term of four sentences", func(s *ExplanationSpec) { s.Item.Term.Text = "One. Two. Three. Four." }, "sentences"},
		{"term with control", func(s *ExplanationSpec) { s.Item.Term.Text = "Soil\x1b[2J." }, "a text"},
		{"script address", func(s *ExplanationSpec) { s.Item.URL = "javascript:alert(1)" }, "no http or https"},
		{"file address", func(s *ExplanationSpec) { s.Item.URL = "file:///etc/passwd" }, "no http or https"},
		{"relative address", func(s *ExplanationSpec) { s.Item.URL = "/guide/beds" }, "no http or https"},
		{"address without host", func(s *ExplanationSpec) { s.Item.URL = "https:///guide" }, "no host"},
		{"address with user", func(s *ExplanationSpec) { s.Item.URL = withUser() }, "user information"},
		{"address that ends the link", func(s *ExplanationSpec) { s.Item.URL = "https://garden.example/\x1b]8;;\x07" }, "control"},
		{"address with bell", func(s *ExplanationSpec) { s.Item.URL = "https://garden.example/\x07" }, "control"},
		{"address with blank", func(s *ExplanationSpec) { s.Item.URL = "https://garden.example/a b" }, "blank"},
		{"address with other letters", func(s *ExplanationSpec) { s.Item.URL = "https://gärten.example/" }, "non-ASCII"},
	}
	for _, tt := range cases {
		spec := ExplanationSpec{Item: sampleExplanation(), Keys: sampleExplanationKeys()}
		tt.edit(&spec)
		if _, err := NewExplanation(spec, keys, text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := NewExplanation(ExplanationSpec{Item: sampleExplanation(), Keys: sampleExplanationKeys()}, keys, nil); err == nil {
		t.Error("an explanation without translator was accepted")
	}
	for _, item := range []ExplanationItem{{Body: "Soil."}, {Term: sampleExplanation().Term}, {URL: "http://garden.example"}} {
		spec := ExplanationSpec{Item: item, Keys: sampleExplanationKeys()}
		spec.Keys.Link = ""
		if _, err := NewExplanation(spec, keys, text); err != nil {
			t.Errorf("%+v: %v", item, err)
		}
	}
}

func TestSetItemKeepsTheShownOneOnRefusal(t *testing.T) {
	st := looks(t)["noneascii"]
	long := sampleExplanation()
	long.Body = strings.Repeat("Water the bed in the morning. ", 40)
	e := newSampleExplanation(t, long, i18n.English)
	e.View(st, 40, 10)
	e.Wheel(1)
	if err := e.SetItem(long); err != nil || e.scroll.offset == 0 {
		t.Fatalf("the same explanation started over: %v, offset %d", err, e.scroll.offset)
	}
	bad := sampleExplanation()
	bad.URL = "javascript:alert(1)"
	if err := e.SetItem(bad); err == nil || e.Item() != long {
		t.Fatalf("a refused explanation replaced the shown one: %v", err)
	}
	if err := e.SetItem(sampleExplanation()); err != nil || e.scroll.offset != 0 || e.Item() != sampleExplanation() {
		t.Errorf("another explanation does not start at its top: %v, offset %d", err, e.scroll.offset)
	}
}

// Back and info close, the extra info key too; copy and the link key go to the application; the scroll keys
// stay inside; every other key is left to the application.
func TestExplanationKeys(t *testing.T) {
	e := newSampleExplanation(t, sampleExplanation(), i18n.English)
	if err := e.keys.Bind(map[ActionID]string{"info": "f3"}); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]ActionID{"esc": "back", "i": "info", "f3": "info", "c": "copy", "o": "open"} {
		if act, handled := e.Update(press(key)); !handled || act != want {
			t.Errorf("%s: %q %v, want %q", key, act, handled, want)
		}
	}
	for _, key := range []string{"up", "down"} {
		if act, handled := e.Update(press(key)); !handled || act != "" {
			t.Errorf("%s: %q %v, want scrolling inside", key, act, handled)
		}
	}
	for _, key := range []string{"f2", "n", "enter", "?"} {
		if act, handled := e.Update(press(key)); handled || act != "" {
			t.Errorf("%s was taken by the explanation: %q", key, act)
		}
	}
	plain := newSampleExplanation(t, ExplanationItem{Body: "Soil."}, i18n.English)
	if act, handled := plain.Update(press("o")); handled || act != "" {
		t.Errorf("the link key acts without an address: %q", act)
	}
}

// The explanation over the screen shows title, body, the term with its title in bold and the address in full
// under its label, linked where the terminal can show it; it keeps to room and falls back where room is small.
func TestExplanationOverTheScreen(t *testing.T) {
	for name, st := range looks(t) {
		e := newSampleExplanation(t, sampleExplanation(), i18n.German)
		for _, size := range []struct{ width, height int }{{100, 30}, {40, 16}, {27, 10}} {
			room := Rect{Y: 2, Width: size.width, Height: size.height - 3}
			view, targets, ok := e.Overlay(st, sampleScreen(size.width, size.height), size.width, size.height, room)
			if want := size.width-8 >= explanationMinInner; ok != want {
				t.Fatalf("%s %dx%d: over the screen = %v, want %v", name, size.width, size.height, ok, want)
			}
			if !ok {
				continue
			}
			assertSize(t, view, size.width, size.height)
			plain := strings.Join(strings.Fields(ansi.Strip(view)), " ")
			wants := []string{"Bed", "its soil decides", "Bed: A bed is one patch", "Dokumentation:", "Schließen"}
			if size.width < 100 {
				wants = []string{"Bed", "Zeilen 1-", "Scrollen", "Schließen"}
			}
			for _, want := range wants {
				if !strings.Contains(plain, want) {
					t.Errorf("%s %dx%d: %q is missing:\n%s", name, size.width, size.height, want, ansi.Strip(view))
				}
			}
			for _, tg := range targets {
				if tg.Rect.Y < room.Y || tg.Rect.Y+tg.Rect.Height > room.Y+room.Height {
					t.Errorf("%s: target %+v lies outside room", name, tg)
				}
			}
		}
		view, _, _ := e.Overlay(st, sampleScreen(120, 30), 120, 30, Rect{Y: 2, Width: 120, Height: 27})
		if !strings.Contains(ansi.Strip(view), "https://garden.example/guide/beds") {
			t.Errorf("%s: the address is not shown in full:\n%s", name, ansi.Strip(view))
		}
		linked := strings.Contains(view, ansi.SetHyperlink("https://garden.example/guide/beds"))
		if want := st.profile != ProfileNone; linked != want {
			t.Errorf("%s: the address is linked = %v, want %v", name, linked, want)
		}
		if st.profile == ProfileNone && ansi.Strip(view) != view {
			t.Errorf("%s: without colour the explanation draws escapes", name)
		}
		if name == "dark" && !strings.Contains(view, st.Render(StyleTitle, "Bed:")) {
			t.Errorf("the title of the term is not bold:\n%q", view)
		}
	}
}

// A click on a key of the key line or on the address acts as its key, and a click anywhere else on the
// explanation closes it; the screen behind takes no click.
func TestExplanationClicks(t *testing.T) {
	st := looks(t)["noneascii"]
	e := newSampleExplanation(t, sampleExplanation(), i18n.English)
	room := Rect{Y: 2, Width: 60, Height: 27}
	view, targets, ok := e.Overlay(st, sampleScreen(60, 30), 60, 30, room)
	if !ok {
		t.Fatal("the explanation does not fit")
	}
	lines := strings.Split(view, "\n")
	find := func(text string) (int, int) {
		for y, l := range lines {
			if i := strings.Index(l, text); i >= 0 {
				return ansi.StringWidth(l[:i]), y
			}
		}
		t.Fatalf("%q is not drawn:\n%s", text, view)
		return 0, 0
	}
	m := NewMouseMap(targets, true)
	for text, want := range map[string]ActionID{"c copy": "copy", "esc/i close": "back", "o open link": "open",
		"https://garden": "open", "its soil": "back", "Bed:": "back"} {
		x, y := find(text)
		for i := range len(text) {
			got, handled := e.Click(m.Handle(click(x+i, y)))
			cell := want
			if text == "esc/i close" && i == 4 {
				cell = "info"
			}
			if text[i] != ' ' && text[i] != '/' && (!handled || got != cell) {
				t.Errorf("a click on cell %d of %q gives %q %v, want %q", i, text, got, handled, cell)
			}
		}
	}
	if act, handled := e.Click(m.Handle(click(0, 0))); handled || act != "" {
		t.Errorf("a click on the screen behind gives %q", act)
	}
}

// Text for the clipboard is what the explanation shows and holds no control character.
func TestExplanationTextForTheClipboard(t *testing.T) {
	item := sampleExplanation()
	item.Title = "Bed\x1b[2J"
	e := newSampleExplanation(t, item, i18n.English)
	text := e.Text()
	for _, want := range []string{`Bed\x1b[2J`, "its soil decides", "Bed: A bed is one patch", "https://garden.example/guide/beds"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text lacks %q:\n%s", want, text)
		}
	}
	if strings.ContainsAny(text, "\x1b\x07\r") {
		t.Errorf("the text holds a control character: %q", text)
	}
}

// The pane keeps its size, scrolls with the wheel and stops at the ends, and names the detail zone so the
// wheel reaches it under the pointer.
func TestExplanationPane(t *testing.T) {
	item := sampleExplanation()
	item.Body = strings.Repeat("Water the bed in the morning. ", 20) + "last words"
	for name, st := range looks(t) {
		e := newSampleExplanation(t, item, i18n.English)
		for _, size := range []struct{ width, height int }{{40, 12}, {40, 60}, {3, 1}} {
			view, targets := e.Pane(st, size.width, size.height)
			assertSize(t, view, size.width, size.height)
			if len(targets) == 0 || targets[0].Zone != ZoneDetail || targets[0].Rect != (Rect{Width: size.width, Height: size.height}) {
				t.Errorf("%s %dx%d: the pane has no zone target: %+v", name, size.width, size.height, targets)
			}
			for _, tg := range targets {
				if tg.Rect.X+tg.Rect.Width > size.width || tg.Rect.Y >= size.height {
					t.Errorf("%s: target %+v lies outside the pane", name, tg)
				}
			}
		}
		e.Pane(st, 40, 12)
		e.Wheel(100)
		view, _ := e.Pane(st, 40, 12)
		if !strings.Contains(view, "Bed") || e.scroll.offset != e.scroll.last || e.scroll.last == 0 {
			t.Errorf("%s: the wheel does not stop at the end (%d of %d):\n%s", name, e.scroll.offset, e.scroll.last, view)
		}
		e.Wheel(-100)
		if e.scroll.offset != 0 {
			t.Errorf("%s: the wheel went above the top", name)
		}
	}
}

// The workspace page keeps its size, shows the title and names the scroll keys only while the text scrolls.
func TestExplanationPage(t *testing.T) {
	st := looks(t)["noneascii"]
	e := newSampleExplanation(t, sampleExplanation(), i18n.English)
	view, targets := e.View(st, 60, 20)
	assertSize(t, view, 60, 20)
	if !strings.HasPrefix(view, "Bed") || len(targets) == 0 || targets[len(targets)-1].Action != "open" {
		t.Errorf("the page lacks its title or the target of the address: %+v\n%s", targets, view)
	}
	if strings.Contains(strings.Join(texts(e.keys.Hints(e.Hints(), InputNavigation, e.text)), " "), "scroll") {
		t.Error("a text that fits names the scroll keys")
	}
	e.View(st, 60, 5)
	if !strings.Contains(strings.Join(texts(e.keys.Hints(e.Hints(), InputNavigation, e.text)), " "), "↑/↓ scroll") {
		t.Error("a text that scrolls does not name the scroll keys")
	}
	if page, _ := e.View(st, 0, 3); page != "\n\n" {
		t.Errorf("a page without width drew %q", page)
	}
}

func texts(groups []HintGroup) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Text()
	}
	return out
}

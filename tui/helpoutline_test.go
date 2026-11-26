package tui

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// outlineKeymap is the sample table with the overview key of the help, a letter of the help level.
func outlineKeymap(t *testing.T) *Keymap {
	t.Helper()
	k, err := NewKeymap(append(sampleActions(), Action{ID: "outline", Level: LevelHelp, DefaultKeys: []string{"o"}, Help: TextHelpOutline}), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// outlineSpec is the sample pages with two more pages in the second topic, whose order differs from their
// titles, and the overview keys.
func outlineSpec(lang i18n.Lang) HelpSpec {
	pages := samplePages()
	pages["water/zeta.md"] = &fstest.MapFile{Data: []byte("---\ntitle: Zeta\norder: 1\n---\nLast letter.\n")}
	pages["water/alpha.md"] = &fstest.MapFile{Data: []byte("---\ntitle: Alpha\norder: 2\n---\nFirst letter.\n")}
	pages["water/zeta.de.md"] = &fstest.MapFile{Data: []byte("---\ntitle: Zeta-Seite\n---\nLetzter Buchstabe.\n")}
	spec := samplePageSpec()
	spec.Pages, spec.Lang = pages, lang
	spec.Keys.Outline, spec.Keys.Enter = "outline", "enter"
	return spec
}

func newOutlineHelp(t *testing.T, lang i18n.Lang) *Help {
	t.Helper()
	keys := outlineKeymap(t)
	h, err := NewHelp(outlineSpec(lang), keys, newTranslator(t, lang, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// inOrder reports whether every text of want stands in the view, each on a later line than the one before.
func inOrder(view string, want ...string) bool {
	lines := strings.Split(view, "\n")
	at := 0
	for _, w := range want {
		i := slices.IndexFunc(lines[at:], func(l string) bool { return strings.Contains(l, w) })
		if i < 0 {
			return false
		}
		at += i + 1
	}
	return true
}

// The overview shows every topic as a group, titled as its index page, with the index and the pages in the
// order of helppages, and marks and selects the page shown.
func TestHelpOutlineListsTopicsAndPagesInOrder(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newOutlineHelp(t, i18n.English)
	h.SetPage("water", "pump")
	if act, handled := helpKeysTo(h, "o"); act != "" || !handled || h.outline == nil {
		t.Fatalf("the overview key: %q %v, outline open %v", act, handled, h.outline != nil)
	}
	plain := ansi.Strip(pageView(h, st, 50))
	// Zeta (order 1) and Alpha (order 2) come before Pump, which has no order, though Alpha sorts first by title.
	if !inOrder(plain, "Getting started", "Getting started", "Beds", "Watering", "Watering", "Zeta", "Alpha", "Pump") {
		t.Errorf("topics or pages are missing or out of order:\n%s", plain)
	}
	if id, ok := h.outline.Selected(); !ok || id != "p5" {
		t.Errorf("the page shown is not selected: %q %v", id, ok)
	}
	if strings.Contains(plain, "keep the bed") {
		t.Errorf("the page text shows beside the overview:\n%s", plain)
	}
	de := newOutlineHelp(t, i18n.German)
	helpKeysTo(de, "o")
	if plain := ansi.Strip(pageView(de, st, 50)); !inOrder(plain, "Erste Schritte", "Erste Schritte", "Beds", "Watering", "Watering", "Zeta-Seite", "Alpha") {
		t.Errorf("German overview:\n%s", plain)
	}
}

// Enter opens the marked page from its top and closes the overview; a topic row folds instead.
func TestHelpOutlineEnterOpensThePage(t *testing.T) {
	h := newOutlineHelp(t, i18n.English)
	helpKeysTo(h, "o", "down")
	h.scroll.offset = 3
	if act, handled := helpKeysTo(h, "enter"); act != "" || !handled || h.outline != nil {
		t.Fatalf("enter: %q %v, outline open %v", act, handled, h.outline != nil)
	}
	if topic, page := h.Page(); topic != "start" || page != "beds" || h.scroll.offset != 0 {
		t.Errorf("enter opened %s/%s at %d", topic, page, h.scroll.offset)
	}
	// From the first page the cursor starts on the group row of the topic.
	h.SetPage("start", "index")
	helpKeysTo(h, "o", "up")
	if _, ok := h.outline.SelectedGroup(); !ok {
		t.Fatal("up from the first page does not reach the topic row")
	}
	helpKeysTo(h, "enter")
	if h.outline == nil {
		t.Error("enter on a topic row closed the overview")
	}
}

// A click on an entry opens its page, a click on a topic folds it, and the key line is clickable.
func TestHelpOutlineClicks(t *testing.T) {
	h := newOutlineHelp(t, i18n.English)
	helpKeysTo(h, "o")
	rows, m := framed(t, h.View, "noneascii", 60, 20)
	x, y := at(t, rows, "Alpha")
	if act, handled := h.Click(m.Handle(click(x, y))); act != "" || !handled || h.outline != nil {
		t.Fatalf("a click on an entry: %q %v, outline open %v", act, handled, h.outline != nil)
	}
	if topic, page := h.Page(); topic != "water" || page != "alpha" {
		t.Errorf("the click opened %s/%s", topic, page)
	}
	helpKeysTo(h, "o")
	rows, m = framed(t, h.View, "noneascii", 60, 20)
	x, y = at(t, rows, "Watering")
	if _, handled := h.Click(m.Handle(click(x, y))); !handled || h.outline == nil {
		t.Error("a click on a topic closed the overview or was refused")
	}
	if plain := strings.Join(rows, "\n"); !strings.Contains(plain, "Zeta") {
		t.Fatalf("the topic is folded before the click:\n%s", plain)
	}
	rows, _ = framed(t, h.View, "noneascii", 60, 20)
	if plain := strings.Join(rows, "\n"); strings.Contains(plain, "Zeta") {
		t.Errorf("a click on a topic did not fold it:\n%s", plain)
	}
}

// Back closes only the overview and keeps the page; then it closes the help. The help key closes the whole
// help at once, and the outside click acts as back.
func TestHelpOutlineBackAndHelpKey(t *testing.T) {
	h := newOutlineHelp(t, i18n.English)
	h.SetPage("water", "pump")
	helpKeysTo(h, "o")
	if act, handled := helpKeysTo(h, "esc"); act != "" || !handled || h.outline != nil {
		t.Fatalf("back in the overview: %q %v, outline open %v", act, handled, h.outline != nil)
	}
	if topic, page := h.Page(); topic != "water" || page != "pump" {
		t.Errorf("back changed the page to %s/%s", topic, page)
	}
	if act, handled := helpKeysTo(h, "esc"); act != "back" || !handled {
		t.Errorf("back on the page: %q %v", act, handled)
	}
	helpKeysTo(h, "o")
	if act, handled := helpKeysTo(h, "?"); act != "help" || !handled {
		t.Errorf("the help key in the overview: %q %v", act, handled)
	}
	// The application closes the help on the help key; here the overview is still open.
	if act, handled := h.Act("outline"); act != "" || !handled || h.outline != nil {
		t.Errorf("the overview key in the overview: %q %v, outline open %v", act, handled, h.outline != nil)
	}
	helpKeysTo(h, "o")
	h.spec.CloseOutside = true
	if act, handled := h.Click(MouseResult{Handled: true, Zone: ZoneOutside}); act != "" || !handled || h.outline != nil {
		t.Errorf("a click outside the overview: %q %v, outline open %v", act, handled, h.outline != nil)
	}
	// Turning the page by a command closes the overview too.
	helpKeysTo(h, "o")
	h.OpenScreen("home")
	if h.outline != nil {
		t.Error("OpenScreen left the overview open")
	}
}

// The overview draws inside the frame of the help, with its own key line and targets.
func TestHelpOutlineOverlayAndHints(t *testing.T) {
	st := looks(t)["noneascii"]
	h := newOutlineHelp(t, i18n.English)
	room := Rect{Y: 2, Width: 100, Height: 20}
	_, _, ok := h.Overlay(st, sampleScreen(100, 22), 100, 22, room)
	if !ok {
		t.Fatal("the page does not fit")
	}
	keyLine := func(marker string) string {
		view, _, ok := h.Overlay(st, sampleScreen(100, 22), 100, 22, room)
		if !ok {
			t.Fatal("the overview does not fit")
		}
		plain := ansi.Strip(view)
		lines := strings.Split(plain, "\n")
		return lines[lineIndex(plain, marker)]
	}
	if got := keyLine("esc/? close"); !strings.Contains(got, "o overview") {
		t.Errorf("the key line of a page names no overview key: %q", got)
	}
	helpKeysTo(h, "o")
	view, targets, ok := h.Overlay(st, sampleScreen(100, 22), 100, 22, room)
	plain := ansi.Strip(view)
	if !ok || !inOrder(plain, "Help", "Getting started", "Watering", "enter choose") || strings.Contains(plain, "o overview") {
		t.Fatalf("the overview over the screen (ok %v):\n%s", ok, plain)
	}
	if got := keyLine("enter choose"); !strings.Contains(got, "esc back") || !strings.Contains(got, "? close") {
		t.Errorf("the key line of the overview: %q", got)
	}
	m := NewMouseMap(targets, true)
	lines := strings.Split(plain, "\n")
	y := lineIndex(plain, "Zeta")
	if act, handled := h.Click(m.Handle(click(strings.Index(lines[y], "Zeta"), y))); act != "" || !handled {
		t.Fatalf("a click on an entry of the overlay: %q %v", act, handled)
	}
	if topic, page := h.Page(); topic != "water" || page != "zeta" {
		t.Errorf("the overlay click opened %s/%s", topic, page)
	}
}

// Over topics the overview key does nothing and no key line names it.
func TestHelpOutlineKeyIsInertOverTopics(t *testing.T) {
	keys := outlineKeymap(t)
	spec := sampleHelpSpec()
	spec.Keys.Outline, spec.Keys.Enter = "outline", "enter"
	h, err := NewHelp(spec, keys, newTranslator(t, i18n.English, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	if act, handled := helpKeysTo(h, "o"); act != "" || handled || h.outline != nil {
		t.Errorf("the overview key over topics: %q %v", act, handled)
	}
	if act, handled := h.Act("outline"); act != "" || handled {
		t.Errorf("Act(outline) over topics: %q %v", act, handled)
	}
	for _, hint := range h.Hints() {
		if hint.Action == "outline" {
			t.Error("the key line names the overview key over topics")
		}
	}
}

func TestNewHelpChecksTheOutlineKeys(t *testing.T) {
	keys := outlineKeymap(t)
	text := newTranslator(t, i18n.English, helpCatalogs(), keys)
	for name, edit := range map[string]func(*HelpKeys){
		"no enter":     func(k *HelpKeys) { k.Enter = "" },
		"no first":     func(k *HelpKeys) { k.First = "" },
		"unknown":      func(k *HelpKeys) { k.Outline = "gone" },
		"shared":       func(k *HelpKeys) { k.Outline = "help" },
		"enter shared": func(k *HelpKeys) { k.Enter = "back" },
	} {
		spec := outlineSpec(i18n.English)
		edit(&spec.Keys)
		if _, err := NewHelp(spec, keys, text); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestGoldenHelpOutline(t *testing.T) {
	for _, tt := range []struct {
		name string
		lang i18n.Lang
		w, h int
	}{
		{"helpoutline.narrow.golden", i18n.English, 30, 14},
		{"helpoutline.wide.golden", i18n.English, 100, 14},
		{"helpoutline.narrow.de.golden", i18n.German, 30, 14},
		{"helpoutline.wide.de.golden", i18n.German, 100, 14},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newOutlineHelp(t, tt.lang)
			h.SetPage("water", "pump")
			helpKeysTo(h, "o")
			out, _ := h.View(looks(t)["noneascii"], tt.w, tt.h)
			assertSize(t, out, tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

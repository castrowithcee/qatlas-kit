package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func samplePathNavKeys() PathNavKeys {
	return PathNavKeys{Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown", First: "first", Last: "last",
		Left: "left", Right: "right", Enter: "enter", Back: "back"}
}

func samplePathNav(t *testing.T, lang i18n.Lang) *PathNav {
	t.Helper()
	keys := sampleKeymap(t, CharsetUnicode)
	p, err := NewPathNav(samplePathNavKeys(), keys, newTranslator(t, lang, frameCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func pathNavRoom(width, height int) Rect { return Rect{Y: 2, Width: width, Height: height - 2} }

func TestNewPathNavRejectsUnknownKeys(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	k := samplePathNavKeys()
	k.Back = "nonesuch"
	if _, err := NewPathNav(k, keys, newTranslator(t, i18n.English, frameCatalogs(), keys)); err == nil {
		t.Fatal("an unknown key was accepted")
	}
}

func TestGoldenPathNav(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		lang       i18n.Lang
		w          int
		overview   bool
	}{
		{"pathnav.overview.wide.golden", "noneascii", i18n.English, 100, true},
		{"pathnav.overview.narrow.golden", "noneascii", i18n.English, 46, true},
		{"pathnav.overview.wide.dark.golden", "dark", i18n.English, 100, true},
		{"pathnav.overview.wide.de.golden", "noneascii", i18n.German, 100, true},
		{"pathnav.choice.wide.golden", "noneascii", i18n.English, 100, false},
		{"pathnav.choice.narrow.golden", "noneascii", i18n.English, 46, false},
		{"pathnav.choice.wide.dark.golden", "dark", i18n.English, 100, false},
		{"pathnav.choice.wide.de.golden", "noneascii", i18n.German, 100, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := samplePathNav(t, tt.lang)
			spec := pathSpec(tt.w)
			spec.ActiveTab = "advanced"
			if tt.overview && !p.OpenOverview(spec) || !tt.overview && !p.OpenChoice(spec) {
				t.Fatal("the overlay did not open")
			}
			view, _, ok := p.Overlay(looks(t)[tt.look], sampleScreen(tt.w, 20), tt.w, 20, pathNavRoom(tt.w, 20))
			if !ok {
				t.Fatal("the overlay does not fit")
			}
			checkGolden(t, tt.name, view+"\n")
		})
	}
}

// A click on the area section opens the overview, one on a tab section the choice; enter on the focused
// section does the same, and the menu action, which the application wires, opens the overview.
func TestPathNavOpens(t *testing.T) {
	spec := pathSpec(80)
	click := func(tab TabID) MouseResult {
		return MouseResult{Handled: true, Zone: ZonePath, Area: "settings", Tab: tab}
	}
	for _, tt := range []struct {
		name string
		open func(p *PathNav) bool
		want string
	}{
		{"area click", func(p *PathNav) bool { return p.OpenClick(spec, click("")) }, "All areas"},
		{"tab click", func(p *PathNav) bool { return p.OpenClick(spec, click("general")) }, "Settings"},
		{"enter on area", func(p *PathNav) bool { s := spec; s.PathFocusArea = true; return p.OpenFocused(s) }, "All areas"},
		{"enter on tab", func(p *PathNav) bool { return p.OpenFocused(spec) }, "Settings"},
		{"menu", func(p *PathNav) bool { return p.OpenOverview(spec) }, "All areas"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := samplePathNav(t, i18n.English)
			if !tt.open(p) || !p.Open() {
				t.Fatal("not open")
			}
			view, _, _ := p.Overlay(looks(t)["noneascii"], sampleScreen(80, 20), 80, 20, pathNavRoom(80, 20))
			if !strings.Contains(view, tt.want) {
				t.Errorf("no %q in\n%s", tt.want, view)
			}
			// Only the overview lists the other areas.
			if strings.Contains(view, "Items") != (tt.want == "All areas") {
				t.Errorf("wrong overlay:\n%s", view)
			}
		})
	}
	p := samplePathNav(t, i18n.English)
	if p.OpenClick(spec, MouseResult{Handled: true, Zone: ZoneRows, Area: "settings"}) || p.Open() {
		t.Error("a click outside the path line opened it")
	}
	// An area with one place opens the overview for enter even on its last section.
	one := spec
	one.ActiveArea = "items"
	if !p.OpenFocused(one) || p.bare["items"] != (PathChoice{Area: "items"}) {
		t.Error("the area without tabs did not open the overview")
	}
	// The mode text has no place.
	mode := spec
	mode.ModeTab = "ex.setup"
	if samplePathNav(t, i18n.English).OpenOverview(mode) {
		t.Error("the mode text opened the overview")
	}
}

func TestPathNavKeysChooseAndClose(t *testing.T) {
	spec := pathSpec(80)
	p := samplePathNav(t, i18n.English)
	p.OpenOverview(spec)
	// The current tab is marked: enter right away chooses it.
	if c, h := p.Update(press("enter")); c != (PathChoice{"settings", "general"}) || !h || p.Open() {
		t.Fatalf("enter chose %+v %v, open %v", c, h, p.Open())
	}
	p.OpenOverview(spec)
	p.Update(press("down"))
	if c, _ := p.Update(press("enter")); c != (PathChoice{"settings", "advanced"}) {
		t.Errorf("down, enter chose %+v", c)
	}
	// An area without tabs is chosen on its group row; an area with tabs folds there.
	p.OpenOverview(spec)
	p.Update(keyOf("home"))
	if c, _ := p.Update(press("enter")); c != (PathChoice{Area: "overview"}) {
		t.Errorf("first row chose %+v", c)
	}
	p.OpenOverview(spec)
	for range 1 {
		p.Update(press("up"))
	}
	if c, h := p.Update(press("enter")); c != (PathChoice{}) || !h || !p.Open() {
		t.Errorf("an area with tabs chose %+v", c)
	}
	// Esc closes without a choice.
	if c, h := p.Update(press("esc")); c != (PathChoice{}) || !h || p.Open() {
		t.Errorf("esc: %+v %v open %v", c, h, p.Open())
	}
	// The choice list.
	p.OpenChoice(spec)
	p.Update(press("down"))
	if c, _ := p.Update(press("enter")); c != (PathChoice{"settings", "advanced"}) {
		t.Errorf("choice chose %+v", c)
	}
	// A closed overlay takes nothing.
	if c, h := p.Update(press("enter")); c != (PathChoice{}) || h {
		t.Errorf("closed: %+v %v", c, h)
	}
	// Typed characters do nothing: there is no search.
	p.OpenChoice(spec)
	p.Update(keyOf("x"))
	if v, _ := p.View(looks(t)["noneascii"], 40, 6); strings.Contains(v, "x") && strings.Contains(strings.ToLower(v), "search") {
		t.Errorf("a search shows:\n%s", v)
	}
}

func TestPathNavClickChooses(t *testing.T) {
	spec := pathSpec(80)
	p := samplePathNav(t, i18n.English)
	p.OpenOverview(spec)
	view, targets, ok := p.Overlay(looks(t)["noneascii"], sampleScreen(80, 20), 80, 20, pathNavRoom(80, 20))
	if !ok {
		t.Fatal("no overlay")
	}
	m := NewMouseMap(targets, true)
	rows := plainRows(view)
	find := func(word string) (int, int) {
		for y, r := range rows {
			if x := strings.Index(r, word); x >= 0 {
				return x, y
			}
		}
		t.Fatalf("no %q in %q", word, rows)
		return 0, 0
	}
	x, y := find("Advanced")
	if c, h := p.Click(clickAt(m, x, y)); c != (PathChoice{"settings", "advanced"}) || !h || p.Open() {
		t.Errorf("click on a tab chose %+v", c)
	}
	p.OpenOverview(spec)
	x, y = find("Items")
	if c, _ := p.Click(clickAt(m, x, y)); c != (PathChoice{Area: "items"}) {
		t.Errorf("click on an area without tabs chose %+v", c)
	}
	p.OpenOverview(spec)
	x, y = find("Settings")
	if c, h := p.Click(clickAt(m, x, y)); c != (PathChoice{}) || !h || !p.Open() {
		t.Errorf("click on an area with tabs: %+v", c)
	}
}

// While an overlay is open the screen behind gives no target: the targets are the overlay's alone.
func TestPathNavTargetsAreItsOwn(t *testing.T) {
	p := samplePathNav(t, i18n.English)
	p.OpenOverview(pathSpec(80))
	_, targets, _ := p.Overlay(looks(t)["noneascii"], sampleScreen(80, 20), 80, 20, pathNavRoom(80, 20))
	for _, tg := range targets {
		if tg.Zone == ZonePath || tg.Zone == ZoneTabs || tg.Zone == ZoneSubtabs {
			t.Errorf("target of the screen behind: %+v", tg)
		}
	}
	if len(targets) == 0 {
		t.Error("no target")
	}
}

// Names are escaped: a control sequence in an area or tab title never reaches the screen.
func TestPathNavMasksNames(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	cat := frameCatalogs()
	cat[i18n.English]["ex.evil"] = "\x1b[31mEvil\x1b[0m"
	p, err := NewPathNav(samplePathNavKeys(), keys, newTranslator(t, i18n.English, cat, keys))
	if err != nil {
		t.Fatal(err)
	}
	spec := pathSpec(80)
	spec.Areas = []Area{{ID: "a", Title: "ex.evil", Tabs: []Tab{{ID: "x", Title: "ex.evil"}, {ID: "y", Title: "ex.evil"}}}}
	spec.ActiveArea, spec.ActiveTab = "a", "x"
	for _, open := range []func(FrameSpec) bool{p.OpenOverview, p.OpenChoice} {
		if !open(spec) {
			t.Fatal("not open")
		}
		view, _, _ := p.Overlay(looks(t)["mono"], sampleScreen(80, 20), 80, 20, pathNavRoom(80, 20))
		if strings.Contains(view, "\x1b[31m") {
			t.Errorf("the escape sequence reached the screen:\n%q", view)
		}
		p.Close()
	}
}

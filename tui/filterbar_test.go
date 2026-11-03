package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

var sampleFilterKeys = FilterBarKeys{Left: "left", Right: "right", Next: "next", Prev: "prev", Up: "up", Down: "down",
	Enter: "enter", Back: "back"}

func sampleFields() []FilterField {
	return []FilterField{
		{ID: "group", Label: "bar.group", Options: []Choice{{ID: "kind", Label: "bar.group.kind"}, {ID: "none", Label: "bar.group.none"}}},
		{ID: "state", Label: "bar.state", Selected: "all", Options: []Choice{
			{ID: "all", Label: "bar.state.all"}, {ID: "on", Label: "bar.state.on"}, {ID: "off", Label: "bar.state.off"}}},
	}
}

func sampleBar(t *testing.T, keys *Keymap, lang i18n.Lang, list *List) *FilterBar {
	t.Helper()
	b, err := NewFilterBar(FilterBarSpec{Fields: sampleFields(), Keys: sampleFilterKeys, List: list}, keys, newTranslator(t, lang, listCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func keysTo2(b *FilterBar, names ...string) (changed FilterField, handled bool) {
	for _, n := range names {
		changed, handled = b.Update(keyOf(n))
	}
	return changed, handled
}

func TestNewFilterBarRejectsBrokenFields(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, listCatalogs(), keys)
	cases := map[string]func(f []FilterField, k *FilterBarKeys) []FilterField{
		"no field":         func([]FilterField, *FilterBarKeys) []FilterField { return nil },
		"no ID":            func(f []FilterField, _ *FilterBarKeys) []FilterField { f[0].ID = ""; return f },
		"control in ID":    func(f []FilterField, _ *FilterBarKeys) []FilterField { f[0].ID = "g\x00o"; return f },
		"field twice":      func(f []FilterField, _ *FilterBarKeys) []FilterField { f[1].ID = "group"; return f },
		"no label":         func(f []FilterField, _ *FilterBarKeys) []FilterField { f[1].Label = ""; return f },
		"one value":        func(f []FilterField, _ *FilterBarKeys) []FilterField { f[0].Options = f[0].Options[:1]; return f },
		"value twice":      func(f []FilterField, _ *FilterBarKeys) []FilterField { f[1].Options[2].ID = "all"; return f },
		"value without ID": func(f []FilterField, _ *FilterBarKeys) []FilterField { f[1].Options[2].ID = ""; return f },
		"value label":      func(f []FilterField, _ *FilterBarKeys) []FilterField { f[1].Options[0].Label = ""; return f },
		"unknown selected": func(f []FilterField, _ *FilterBarKeys) []FilterField { f[1].Selected = "gone"; return f },
		"unknown key":      func(f []FilterField, k *FilterBarKeys) []FilterField { k.Back = "gone"; return f },
		"key twice":        func(f []FilterField, k *FilterBarKeys) []FilterField { k.Back = k.Enter; return f },
	}
	for name, edit := range cases {
		nav := sampleFilterKeys
		fields := edit(sampleFields(), &nav)
		if _, err := NewFilterBar(FilterBarSpec{Fields: fields, Keys: nav}, keys, text); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewFilterBar(FilterBarSpec{Fields: sampleFields(), Keys: sampleFilterKeys}, nil, text); err == nil {
		t.Error("a bar without key table was accepted")
	}
}

// Left and right change the value with wrap-around, tab and shift+tab step through the fields and leave the
// bar at its ends to the application, enter opens the list of values, which takes a value with enter and
// closes without change on back.
func TestFilterBarKeys(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	b := sampleBar(t, keys, i18n.English, nil)
	if _, handled := keysTo2(b, "right"); handled {
		t.Fatal("a bar without focus took a key")
	}
	b.Focus(false)
	if changed, _ := keysTo2(b, "right"); changed.ID != "group" || changed.Selected != "none" || b.Value("group") != "none" {
		t.Errorf("right: %+v", changed)
	}
	if changed, _ := keysTo2(b, "right"); changed.Selected != "kind" {
		t.Errorf("right wraps round: %+v", changed)
	}
	if _, handled := keysTo2(b, "prev"); handled {
		t.Error("shift+tab before the first field is not left to the application")
	}
	keysTo2(b, "tab")
	if changed, _ := keysTo2(b, "left"); changed.ID != "state" || changed.Selected != "off" {
		t.Errorf("left on state: %+v", changed)
	}
	if _, handled := keysTo2(b, "tab"); handled {
		t.Error("tab past the last field is not left to the application")
	}
	for _, key := range []string{"up", "down", "esc"} {
		if _, handled := keysTo2(b, key); handled {
			t.Errorf("%s on a closed field is not left to the application", key)
		}
	}
	keysTo2(b, "enter")
	if !b.Open() {
		t.Fatal("enter did not open the list of values")
	}
	if _, handled := keysTo2(b, "down", "tab", "esc"); !handled || b.Open() || b.Value("state") != "off" {
		t.Errorf("back closes without change: open %v, value %q", b.Open(), b.Value("state"))
	}
	keysTo2(b, "enter", "up", "up", "up")
	if changed, _ := keysTo2(b, "enter"); changed.Selected != "all" || b.Open() {
		t.Errorf("enter takes the value under the cursor: %+v", changed)
	}
	keysTo2(b, "enter")
	if changed, handled := keysTo2(b, "enter"); changed.ID != "" || !handled {
		t.Errorf("taking the value in force changes nothing: %+v", changed)
	}
	b.Blur()
	if b.Focused() || b.Open() {
		t.Error("Blur keeps the focus")
	}
	fields := b.Fields()
	fields[0].Options[0].ID = "changed"
	if b.Fields()[0].Options[0].ID != "kind" {
		t.Error("Fields shares its options")
	}
}

// A click on a field focuses it and opens its values, a second one closes them, and a click on a value takes
// it, as enter does.
func TestFilterBarClicks(t *testing.T) {
	st := looks(t)["noneascii"]
	keys := sampleKeymap(t, st.Charset())
	b := sampleBar(t, keys, i18n.English, nil)
	_, targets := b.View(st, 80)
	m := NewMouseMap(targets, true)
	var state Target
	for _, tg := range targets {
		if tg.Item == fieldRef+"state" {
			state = tg
		}
	}
	if _, handled := b.Click(m.Handle(click(state.Rect.X+3, 0))); !handled || !b.Open() || !b.Focused() {
		t.Fatal("a click on a field does not open its values")
	}
	_, values := b.ValueList(st)
	vm := NewMouseMap(values, true)
	if changed, _ := b.Click(vm.Handle(click(values[1].Rect.X, values[1].Rect.Y))); changed.Selected != "on" || b.Open() {
		t.Errorf("a click on a value: %+v", changed)
	}
	b.Click(m.Handle(click(state.Rect.X+3, 0)))
	if _, handled := b.Click(m.Handle(click(state.Rect.X+3, 0))); !handled || b.Open() {
		t.Error("a second click on the open field does not close it")
	}
	if _, handled := b.Click(vm.Handle(click(values[1].Rect.X, values[1].Rect.Y))); handled {
		t.Error("a value of a closed list took a click")
	}
	if _, handled := b.Click(m.Handle(click(79, 0))); handled {
		t.Error("the free area of the bar is the application's, to focus the bar")
	}
}

// A narrow bar cuts its values, then shows the focused field alone, and without focus its values dimmed; the
// search field stands at the right while there is room and alone while a search is typed.
func TestFilterBarNarrows(t *testing.T) {
	st := looks(t)["noneascii"]
	keys := sampleKeymap(t, st.Charset())
	l, _ := newSampleList(t, st, i18n.English, func(s *ListSpec) { s.SearchInBar = true })
	b := sampleBar(t, keys, i18n.English, l)
	plain := func(width int) string {
		line, _ := b.View(st, width)
		if w := ansi.StringWidth(line); w != width {
			t.Errorf("width %d: the line is %d cells", width, w)
		}
		return strings.TrimRight(ansi.Strip(line), " ")
	}
	if got := plain(80); !strings.HasPrefix(got, "   Group: Kind v      State: All v") || !strings.HasSuffix(got, "/ search~") &&
		!strings.HasSuffix(got, "/ search…") {
		t.Errorf("wide bar: %q", got)
	}
	if got := plain(40); strings.Contains(got, "search") || !strings.Contains(got, "State: All v") {
		t.Errorf("an idle search gives way: %q", got)
	}
	if got := plain(27); got != "  Group: Kind - State: All" {
		t.Errorf("without focus the values: %q", got)
	}
	b.Focus(true)
	if got := plain(27); got != ">  State: All v" {
		t.Errorf("the focused field alone: %q", got)
	}
	b.Blur()
	keysTo(l, "/")
	typeList(l, "mi")
	if got := plain(24); got != "/ mi_" {
		t.Errorf("a typed search alone: %q", got)
	}
	de := sampleBar(t, keys, i18n.German, nil)
	if got, _ := de.View(st, 80); !strings.Contains(ansi.Strip(got), "Zustand: Alle") {
		t.Errorf("German bar: %q", got)
	}
}

func TestFilterBarWidthSweep(t *testing.T) {
	st := looks(t)["noneascii"]
	keys := sampleKeymap(t, st.Charset())
	tr := newTranslator(t, i18n.English, listCatalogs(), keys)
	long := []FilterField{
		{ID: "group", Label: "bar.group", Options: []Choice{{ID: "kind", Label: "bar.group.kind"}, {ID: "none", Label: "bar.group.none"}}},
		{ID: "state", Label: "bar.state", Options: []Choice{{ID: "all", Label: "bar.state.all"}, {ID: "on", Label: "bar.state.on"}}},
		{ID: "g2", Label: "bar.group", Options: []Choice{{ID: "kind", Label: "bar.state.all"}, {ID: "none", Label: "bar.group.none"}}},
		{ID: "s2", Label: "bar.state", Options: []Choice{{ID: "all", Label: "bar.group.kind"}, {ID: "on", Label: "bar.state.on"}}},
	}
	for nf := 1; nf <= 4; nf++ {
		for _, mode := range []string{"nolist", "idle", "typing"} {
			for focus := -1; focus < nf; focus++ {
				for width := 10; width <= 160; width++ {
					var list *List
					if mode != "nolist" {
						list, _ = newSampleList(t, st, i18n.English, func(s *ListSpec) { s.SearchInBar = true })
					}
					b, err := NewFilterBar(FilterBarSpec{Fields: long[:nf], Keys: sampleFilterKeys, List: list}, keys, tr)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "typing" {
						keysTo(list, "/")
						typeList(list, "mi")
					}
					if focus >= 0 {
						b.focus = focus
					}
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Errorf("fields %d %s focus %d width %d: panic %v", nf, mode, focus, width, r)
							}
						}()
						line, targets := b.View(st, width)
						for _, l := range strings.Split(line, "\n") {
							if w := lipgloss.Width(l); w > width {
								t.Errorf("fields %d %s focus %d width %d: line is %d cells", nf, mode, focus, width, w)
							}
						}
						for _, tg := range targets {
							if tg.Rect.X < 0 || tg.Rect.Width < 0 || tg.Rect.X+tg.Rect.Width > width {
								t.Errorf("fields %d %s focus %d width %d: target %+v outside", nf, mode, focus, width, tg.Rect)
							}
						}
					}()
				}
			}
		}
	}
}

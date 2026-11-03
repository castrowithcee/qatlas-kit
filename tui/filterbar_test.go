package tui

import (
	"fmt"
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

// firstField is the first changed field, the zero field for none.
func firstField(changed []FilterField) FilterField {
	if len(changed) == 0 {
		return FilterField{}
	}
	return changed[0]
}

func keysTo2(b *FilterBar, names ...string) (changed []FilterField, handled bool) {
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
	if changed, _ := keysTo2(b, "right"); len(changed) != 1 || changed[0].ID != "group" || changed[0].Selected != "none" || b.Value("group") != "none" {
		t.Errorf("right: %+v", changed)
	}
	if changed, _ := keysTo2(b, "right"); firstField(changed).Selected != "kind" {
		t.Errorf("right wraps round: %+v", changed)
	}
	if _, handled := keysTo2(b, "prev"); handled {
		t.Error("shift+tab before the first field is not left to the application")
	}
	keysTo2(b, "tab")
	if changed, _ := keysTo2(b, "left"); firstField(changed).ID != "state" || firstField(changed).Selected != "off" {
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
	if changed, _ := keysTo2(b, "enter"); firstField(changed).Selected != "all" || b.Open() {
		t.Errorf("enter takes the value under the cursor: %+v", changed)
	}
	keysTo2(b, "enter")
	if changed, handled := keysTo2(b, "enter"); changed != nil || !handled {
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
	if changed, _ := b.Click(vm.Handle(click(values[1].Rect.X, values[1].Rect.Y))); firstField(changed).Selected != "on" || b.Open() {
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

// A narrow bar cuts its values, then shows the filter button with the number of fields off their first value;
// the search field stands at the right while there is room, then only the search key, and a typed search
// keeps the room that is left.
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
	if got := plain(40); strings.Contains(got, "search") || !strings.Contains(got, "State: All v") || b.compact {
		t.Errorf("an idle search gives way: %q", got)
	}
	if got := plain(30); b.compact || !strings.Contains(got, "State: ~ v") {
		t.Errorf("cut values: %q", got)
	}
	if got := plain(28); got != "[ Filter (0) ]             /" || !b.compact {
		t.Errorf("the button and the search key: %q", got)
	}
	if got := plain(16); got != "[ Filter (0) ]" {
		t.Errorf("only the button: %q", got)
	}
	if got := plain(8); got != "[ Fil~ ]" {
		t.Errorf("a button cut to the width: %q", got)
	}
	b.Focus(false)
	if got := plain(18); got != "> Filter (0) <   /" {
		t.Errorf("the focused button: %q", got)
	}
	b.Blur()
	keysTo(l, "/")
	typeList(l, "mi")
	if got := plain(24); got != "[ Filter (0) ]  / mi_" {
		t.Errorf("a typed search beside the button: %q", got)
	}
	de := sampleBar(t, keys, i18n.German, nil)
	if got, _ := de.View(st, 80); !strings.Contains(ansi.Strip(got), "Zustand: Alle") {
		t.Errorf("German bar: %q", got)
	}
}

// compactBar is a focused bar too narrow for its fields, so the button stands for them.
func compactBar(t *testing.T, st Styles, list *List) *FilterBar {
	t.Helper()
	b := sampleBar(t, sampleKeymap(t, st.Charset()), i18n.English, list)
	b.Focus(false)
	b.View(st, 20)
	if !b.compact {
		t.Fatal("the bar is not compact")
	}
	return b
}

// The button is one stop: tab, shift+tab and the other keys are left to the application, enter opens the
// modal, which takes every key.
func TestFilterBarButtonAndModalKeys(t *testing.T) {
	st := looks(t)["noneascii"]
	b := compactBar(t, st, nil)
	for _, key := range []string{"tab", "prev", "left", "right", "up", "down", "esc"} {
		if _, handled := keysTo2(b, key); handled {
			t.Errorf("%s on the button is not left to the application", key)
		}
	}
	if changed, handled := keysTo2(b, "enter"); changed != nil || !handled || !b.Open() || !b.modal {
		t.Fatal("enter does not open the modal")
	}
	// Right on group, down to state, left twice: all and the pending values wrap round.
	for _, key := range []string{"right", "down", "left", "x", "tab"} {
		if changed, handled := keysTo2(b, key); changed != nil || !handled {
			t.Errorf("%s in the modal: changed %v handled %v", key, changed, handled)
		}
	}
	if b.Value("group") != "kind" || b.Value("state") != "all" {
		t.Error("the modal changed values before enter")
	}
	changed, handled := keysTo2(b, "enter")
	if !handled || b.Open() || len(changed) != 2 || changed[0].ID != "group" || changed[0].Selected != "none" ||
		changed[1].ID != "state" || changed[1].Selected != "off" {
		t.Fatalf("enter takes all changes at once: %+v", changed)
	}
	if b.Value("group") != "none" || b.Value("state") != "off" {
		t.Error("the values are not in force")
	}
	b.View(st, 20)
	if line, _ := b.View(st, 20); !strings.Contains(ansi.Strip(line), "Filter (2)") {
		t.Errorf("the count of the button: %q", ansi.Strip(line))
	}
	// Only one field changes; a field set back to its old value is no change.
	keysTo2(b, "enter", "right", "right", "right", "down", "right", "left")
	if changed, _ := keysTo2(b, "enter"); len(changed) != 1 || changed[0].ID != "group" || changed[0].Selected != "kind" {
		t.Errorf("only the changed field: %+v", changed)
	}
	if line, _ := b.View(st, 20); !strings.Contains(ansi.Strip(line), "Filter (1)") {
		t.Errorf("the count of the button: %q", ansi.Strip(line))
	}
	// Back drops all changes.
	keysTo2(b, "enter", "right", "down", "right")
	if changed, handled := keysTo2(b, "esc"); changed != nil || !handled || b.Open() {
		t.Error("back does not close the modal")
	}
	if b.Value("group") != "kind" || b.Value("state") != "off" {
		t.Error("back kept changes")
	}
	keysTo2(b, "enter")
	if got := b.draft; got[0] != 0 || got[1] != 2 || b.mfocus != 0 {
		t.Errorf("a reopened modal starts from the values in force: %v at %d", got, b.mfocus)
	}
	b.Blur()
	if b.Open() {
		t.Error("Blur keeps the modal open")
	}
}

// The count is the number of fields off their first value.
func TestFilterBarButtonCount(t *testing.T) {
	st := looks(t)["noneascii"]
	b := compactBar(t, st, nil)
	for want, edit := range []func(){func() {}, func() { b.fields[0].Selected = "none" }, func() { b.fields[1].Selected = "on" }} {
		edit()
		line, _ := b.View(st, 20)
		if !strings.Contains(ansi.Strip(line), fmt.Sprintf("Filter (%d)", want)) {
			t.Errorf("%d: %q", want, ansi.Strip(line))
		}
	}
}

// The button and, in the modal, a field and its value are targets; the keys of the key line act as keys.
func TestFilterBarModalClicks(t *testing.T) {
	st := looks(t)["noneascii"]
	b := compactBar(t, st, nil)
	_, targets := b.View(st, 20)
	m := NewMouseMap(targets, true)
	if _, handled := b.Click(m.Handle(click(4, 0))); !handled || !b.modal {
		t.Fatal("a click on the button does not open the modal")
	}
	screen := strings.TrimRight(strings.Repeat(strings.Repeat(" ", 80)+"\n", 24), "\n")
	_, mt, ok := b.ModalOverlay(st, screen, 80, 24, Rect{Width: 80, Height: 24})
	if !ok {
		t.Fatal("no room")
	}
	mm := NewMouseMap(mt, true)
	var group, state, stateValue Target
	for _, tg := range mt {
		switch tg.Item {
		case modalFieldRef + "state":
			state = tg
		case modalValueRef + "state":
			stateValue = tg
		case modalFieldRef + "group":
			group = tg
		}
	}
	if changed, handled := b.Click(mm.Handle(click(state.Rect.X, state.Rect.Y))); changed != nil || !handled || b.mfocus != 1 {
		t.Errorf("a click on a field focuses it: focus %d", b.mfocus)
	}
	if _, handled := b.Click(mm.Handle(click(group.Rect.X, group.Rect.Y))); !handled || b.mfocus != 0 {
		t.Errorf("a click on the first field: focus %d", b.mfocus)
	}
	if _, handled := b.Click(mm.Handle(click(stateValue.Rect.X+1, stateValue.Rect.Y))); !handled || b.mfocus != 1 || b.draft[1] != 1 {
		t.Errorf("a click on a value takes the next: focus %d draft %v", b.mfocus, b.draft)
	}
	if b.Value("state") != "all" {
		t.Error("a click on a value changed the value before enter")
	}
	var enter, back Target
	for _, tg := range mt {
		switch tg.Action {
		case "enter":
			enter = tg
		case "back":
			back = tg
		}
	}
	if enter.Rect.Width == 0 || back.Rect.Width == 0 {
		t.Fatalf("the key line has no targets: %+v", mt)
	}
	if changed, handled := b.Click(mm.Handle(click(back.Rect.X, back.Rect.Y))); changed != nil || !handled || b.modal || b.Value("state") != "all" {
		t.Error("a click on back does not drop the changes")
	}
	b.Click(m.Handle(click(4, 0)))
	b.Click(mm.Handle(click(stateValue.Rect.X+1, stateValue.Rect.Y)))
	if changed, handled := b.Click(mm.Handle(click(enter.Rect.X, enter.Rect.Y))); len(changed) != 1 || changed[0].Selected != "on" || !handled || b.modal {
		t.Errorf("a click on enter takes the changes: %+v", changed)
	}
	// A value list never opens from a button click, and a button target of an older frame does nothing wide.
	wide := sampleBar(t, sampleKeymap(t, st.Charset()), i18n.English, nil)
	if _, handled := wide.Click(MouseResult{Item: buttonRef, Handled: true}); handled {
		t.Error("the button acted without being drawn")
	}
}

// The modal is drawn over the screen where it fits and says where it does not; its fallback fills the
// workspace and keeps the field in focus in view.
func TestFilterBarModalDrawing(t *testing.T) {
	st := looks(t)["noneascii"]
	b := compactBar(t, st, nil)
	if _, _, ok := b.ModalOverlay(st, "", 80, 24, Rect{Width: 80, Height: 24}); ok {
		t.Error("a closed modal was drawn")
	}
	if got, _ := b.ModalView(st, 80, 24); got != "" || b.Hints() != nil {
		t.Error("a closed modal has a view or hints")
	}
	keysTo2(b, "enter", "right")
	screen := strings.TrimRight(strings.Repeat(strings.Repeat("x", 80)+"\n", 24), "\n")
	view, targets, ok := b.ModalOverlay(st, screen, 80, 24, Rect{Width: 80, Height: 24})
	if !ok {
		t.Fatal("no room on a large screen")
	}
	rows := strings.Split(ansi.Strip(view), "\n")
	if len(rows) != 24 {
		t.Fatalf("%d rows", len(rows))
	}
	text := strings.Join(rows, "\n")
	for _, want := range []string{"Filter", "Group: None", "State: All", "apply", "discard"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing in\n%s", want, text)
		}
	}
	for _, r := range rows {
		if w := ansi.StringWidth(r); w != 80 {
			t.Errorf("row %q is %d cells", r, w)
		}
	}
	for _, tg := range targets {
		if tg.Rect.X < 0 || tg.Rect.X+tg.Rect.Width > 80 || tg.Rect.Y < 0 || tg.Rect.Y >= 24 {
			t.Errorf("target %+v outside", tg)
		}
	}
	for _, room := range []Rect{{Width: 40, Height: 24}, {Width: 80, Height: 5}, {Width: 0, Height: 0}} {
		if _, _, ok := b.ModalOverlay(st, screen, 80, 24, room); ok {
			t.Errorf("room %+v reported as enough", room)
		}
	}
	for _, size := range [][2]int{{0, 0}, {1, 1}, {10, 1}, {30, 2}, {60, 10}} {
		view, targets := b.ModalView(st, size[0], size[1])
		if size[0] == 0 || size[1] == 0 {
			continue
		}
		for _, l := range strings.Split(view, "\n") {
			if w := ansi.StringWidth(l); w != size[0] {
				t.Errorf("%v: row %q is %d cells", size, l, w)
			}
		}
		for _, tg := range targets {
			if tg.Rect.X < 0 || tg.Rect.X+tg.Rect.Width > size[0] || tg.Rect.Y < 0 || tg.Rect.Y >= size[1] {
				t.Errorf("%v: target %+v outside", size, tg)
			}
		}
	}
	keysTo2(b, "down")
	if view, _ := b.ModalView(st, 30, 1); !strings.Contains(ansi.Strip(view), "State: All") {
		t.Errorf("the field in focus is out of view: %q", ansi.Strip(view))
	}
	for _, h := range b.Hints() {
		if h.Action == "" || h.Label == "" {
			t.Errorf("hint %+v", h)
		}
	}
	de := sampleBar(t, sampleKeymap(t, st.Charset()), i18n.German, nil)
	de.Focus(false)
	de.View(st, 20)
	keysTo2(de, "enter")
	if view, _, _ := de.ModalOverlay(st, screen, 80, 24, Rect{Width: 80, Height: 24}); !strings.Contains(ansi.Strip(view), "Zustand: Alle") {
		t.Error("German modal")
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

func TestFilterBarModalSweep(t *testing.T) {
	st := looks(t)["noneascii"]
	keys := sampleKeymap(t, st.Charset())
	tr := newTranslator(t, i18n.English, listCatalogs(), keys)
	fields := sampleFields()
	fields = append(fields, FilterField{ID: "g2", Label: "bar.group", Options: fields[1].Options}, FilterField{ID: "s2", Label: "bar.state", Options: fields[0].Options})
	for nf := 1; nf <= len(fields); nf++ {
		b, err := NewFilterBar(FilterBarSpec{Fields: fields[:nf], Keys: sampleFilterKeys}, keys, tr)
		if err != nil {
			t.Fatal(err)
		}
		b.Focus(false)
		b.View(st, 12)
		keysTo2(b, "enter", "down", "down")
		for width := 0; width <= 120; width += 3 {
			for height := 0; height <= 30; height += 2 {
				screen := strings.TrimRight(strings.Repeat(strings.Repeat("x", width)+"\n", height), "\n")
				view, targets, ok := b.ModalOverlay(st, screen, width, height, Rect{Width: width, Height: height})
				if ok {
					for _, l := range strings.Split(view, "\n") {
						if w := lipgloss.Width(l); w > width {
							t.Errorf("fields %d %dx%d: overlay row of %d cells", nf, width, height, w)
						}
					}
				}
				v, vt := b.ModalView(st, width, height)
				for _, l := range strings.Split(v, "\n") {
					if v != "" && lipgloss.Width(l) > width {
						t.Errorf("fields %d %dx%d: view row of %d cells", nf, width, height, lipgloss.Width(l))
					}
				}
				for _, tg := range append(targets, vt...) {
					if tg.Rect.X < 0 || tg.Rect.Y < 0 || tg.Rect.X+tg.Rect.Width > width || tg.Rect.Y >= max(height, 1) {
						t.Errorf("fields %d %dx%d: target %+v outside", nf, width, height, tg.Rect)
					}
				}
			}
		}
	}
}

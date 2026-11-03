package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func sampleListKeys() ListKeys {
	return ListKeys{Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown", First: "first", Last: "last",
		Left: "left", Right: "right", Enter: "enter", Search: "search", Back: "back", Toggle: "toggle"}
}

func listCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {
			"col.name": "Name", "col.kind": "Kind", "col.state": "State", "col.size": "Size", "col.note": "Note",
			"list.empty": "No plants yet; {new} adds one.", "list.nogroup": "unsorted",
			"bar.group": "Group", "bar.group.kind": "Kind", "bar.group.none": "None",
			"bar.state": "State", "bar.state.all": "All", "bar.state.on": "Active", "bar.state.off": "Inactive",
		},
		i18n.German: {
			"col.name": "Name", "col.kind": "Art", "col.state": "Zustand", "col.size": "Größe", "col.note": "Notiz",
			"list.empty": "Noch keine Pflanzen; {new} legt eine an.", "list.nogroup": "unsortiert",
			"bar.group": "Gruppe", "bar.group.kind": "Art", "bar.group.none": "Keine",
			"bar.state": "Zustand", "bar.state.all": "Alle", "bar.state.on": "Aktiv", "bar.state.off": "Inaktiv",
		},
	}
}

func sampleColumns() []Column {
	return []Column{
		{Title: "col.name"},
		{Title: "col.state", Short: map[string]string{"blooming": "bl", "resting": "re"}},
		{Title: "col.size", Optional: true, Right: true, Dimmed: true},
		{Title: "col.note", Flex: true, Dimmed: true},
	}
}

// samplePlants are invented entries in three groups: herbs, trees and none.
func samplePlants() []ListItem {
	item := func(id, group, state, size, note string) ListItem {
		return ListItem{ID: ItemID(id), Cells: []string{id, state, size, note}, Group: group}
	}
	return []ListItem{
		item("basil", "herbs", "blooming", "12", "on the sill by the kitchen window"),
		item("mint", "herbs", "resting", "30", "spreads fast"),
		item("oak", "trees", "resting", "1200", "planted long ago"),
		item("thyme", "herbs", "blooming", "8", ""),
		item("fern", "", "resting", "40", "shade"),
	}
}

func newSampleList(t *testing.T, st Styles, lang i18n.Lang, edit func(*ListSpec)) (*List, *Keymap) {
	t.Helper()
	keys := sampleKeymap(t, st.Charset())
	spec := ListSpec{Columns: sampleColumns(), Keys: sampleListKeys(), Empty: "list.empty", NoGroup: "list.nogroup"}
	if edit != nil {
		edit(&spec)
	}
	l, err := NewList(spec, keys, newTranslator(t, lang, listCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return l, keys
}

func filledList(t *testing.T, edit func(*ListSpec)) *List {
	t.Helper()
	l, _ := newSampleList(t, Styles{}, i18n.English, edit)
	if err := l.SetItems(samplePlants()); err != nil {
		t.Fatal(err)
	}
	return l
}

func keyOf(name string) tea.KeyPressMsg {
	named := map[string]rune{"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
		"left": tea.KeyLeft, "right": tea.KeyRight, "down": tea.KeyDown, "backspace": tea.KeyBackspace}
	if code, ok := named[name]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	return press(name)
}

func keysTo(l *List, names ...string) (open ItemID, handled bool) {
	for _, n := range names {
		open, handled = l.Update(keyOf(n))
	}
	return open, handled
}

func typeList(l *List, text string) {
	for _, r := range text {
		l.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// shownRows names the shown rows: entries by ID, group rows as "[key]".
func shownRows(l *List) string {
	var out []string
	for _, r := range l.rows {
		if r.all {
			out = append(out, "*")
		} else if r.item < 0 {
			out = append(out, "["+r.group+"]")
		} else {
			out = append(out, string(l.items[r.item].ID))
		}
	}
	return strings.Join(out, ",")
}

func selected(t *testing.T, l *List) ItemID {
	t.Helper()
	id, ok := l.Selected()
	if !ok {
		t.Fatalf("no entry is selected; rows %s", shownRows(l))
	}
	return id
}

func TestNewListRejectsBrokenSpecs(t *testing.T) {
	cases := []struct {
		name string
		edit func(*ListSpec)
	}{
		{"no column", func(s *ListSpec) { s.Columns = nil }},
		{"no title", func(s *ListSpec) { s.Columns[1].Title = "" }},
		{"flex first", func(s *ListSpec) { s.Columns[0].Flex = true }},
		{"optional first", func(s *ListSpec) { s.Columns[0].Optional = true }},
		{"two flex", func(s *ListSpec) { s.Columns[1].Flex = true }},
		{"no empty text", func(s *ListSpec) { s.Empty = "" }},
		{"unknown key", func(s *ListSpec) { s.Keys.PageDown = "gone" }},
		{"missing key", func(s *ListSpec) { s.Keys.Back = "" }},
		{"shared key", func(s *ListSpec) { s.Keys.Search = s.Keys.Enter }},
		{"search by key without key", func(s *ListSpec) { s.Keys.Search = "" }},
		{"unknown search mode", func(s *ListSpec) { s.Search = 9 }},
	}
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, listCatalogs(), keys)
	for _, c := range cases {
		spec := ListSpec{Columns: sampleColumns(), Keys: sampleListKeys(), Empty: "list.empty"}
		c.edit(&spec)
		if _, err := NewList(spec, keys, text); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	spec := ListSpec{Columns: sampleColumns(), Keys: sampleListKeys(), Empty: "list.empty", Search: SearchAsTyped}
	spec.Keys.Search = ""
	if _, err := NewList(spec, keys, text); err != nil {
		t.Errorf("a list that filters as typed needs no search key: %v", err)
	}
	if _, err := NewList(spec, nil, text); err == nil {
		t.Error("a list without key table was accepted")
	}
}

func TestSetItemsRejectsBrokenEntriesAndChangesNothing(t *testing.T) {
	l := filledList(t, nil)
	keysTo(l, "down")
	before := shownRows(l)
	for name, items := range map[string][]ListItem{
		"no ID":         {{Cells: []string{"a", "b", "c", "d"}}},
		"control in ID": {{ID: "a\x1b[2J", Cells: []string{"a", "b", "c", "d"}}},
		"NUL in ID":     {{ID: "\x00gherbs", Cells: []string{"a", "b", "c", "d"}}},
		"twice":         {{ID: "a", Cells: []string{"a", "b", "c", "d"}}, {ID: "a", Cells: []string{"a", "b", "c", "d"}}},
		"cells":         {{ID: "a", Cells: []string{"a"}}},
	} {
		if err := l.SetItems(items); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := shownRows(l); got != before || selected(t, l) != "mint" {
		t.Errorf("a refused fill changed the list: %s, selected %s", got, selected(t, l))
	}
}

// The selection follows the ID when the entries, their order, the filter or the grouping change; an entry
// that is gone leaves the selection where it stood.
func TestSelectionFollowsTheID(t *testing.T) {
	l := filledList(t, nil)
	if got := shownRows(l); got != "basil,mint,oak,thyme,fern" {
		t.Fatalf("flat rows = %s", got)
	}
	keysTo(l, "down", "down")
	if selected(t, l) != "oak" {
		t.Fatalf("selected %s", selected(t, l))
	}
	reversed := samplePlants()
	slices.Reverse(reversed)
	if err := l.SetItems(reversed); err != nil {
		t.Fatal(err)
	}
	if selected(t, l) != "oak" {
		t.Errorf("after a new order: %s", selected(t, l))
	}
	l.SetGrouped(true)
	if selected(t, l) != "oak" {
		t.Errorf("after grouping: %s", selected(t, l))
	}
	l.SetFilter("O")
	if selected(t, l) != "oak" {
		t.Errorf("after filtering: %s", selected(t, l))
	}
	l.SetFilter("")
	l.SetGrouped(false)
	if err := l.SetItems(samplePlants()[:2]); err != nil {
		t.Fatal(err)
	}
	if got := selected(t, l); got != "mint" {
		t.Errorf("a removed entry leaves the selection on %s, want the last one", got)
	}
	if !l.Select("basil") || selected(t, l) != "basil" || l.Select("gone") {
		t.Error("Select does not follow the ID")
	}
}

// The filter ignores case, reads the cells and the search text, keeps the order and removes nothing.
func TestFilterIgnoresCaseKeepsOrderAndRemovesNothing(t *testing.T) {
	l := filledList(t, nil)
	l.SetFilter("BLOOM")
	if got := shownRows(l); got != "basil,thyme" {
		t.Errorf("BLOOM shows %s", got)
	}
	items := samplePlants()
	items[2].SearchText = "quercus"
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	l.SetFilter("Quercus")
	if got := shownRows(l); got != "oak" {
		t.Errorf("the search text is not read: %s", got)
	}
	l.SetFilter("")
	if got := shownRows(l); got != "basil,mint,oak,thyme,fern" || len(l.items) != 5 {
		t.Errorf("clearing the filter: %s", got)
	}
}

// After the search key letters are text, enter keeps the filter, the first back clears it and only the next
// one is left to the application.
func TestSearchByKey(t *testing.T) {
	l := filledList(t, nil)
	if l.Input() != InputNavigation {
		t.Fatal("a list with letter actions reads no text before the search key")
	}
	if _, handled := keysTo(l, "d"); handled {
		t.Error("a letter acted on the list before the search")
	}
	keysTo(l, "/")
	if !l.Searching() || l.Input() != InputText {
		t.Fatal("the search key did not start the search")
	}
	typeList(l, "dn")
	keysTo(l, "backspace")
	if l.Query() != "d" {
		t.Fatalf("query %q, want d", l.Query())
	}
	typeList(l, "q")
	if l.Query() != "dq" || shownRows(l) != "" {
		t.Fatalf("letters of actions are text while searching: %q shows %s", l.Query(), shownRows(l))
	}
	keysTo(l, "backspace", "backspace")
	typeList(l, "mint")
	if open, handled := keysTo(l, "enter"); open != "" || !handled || l.Searching() || l.Query() != "mint" {
		t.Fatalf("enter: open %q, searching %v, query %q", open, l.Searching(), l.Query())
	}
	if open, _ := keysTo(l, "enter"); open != "mint" {
		t.Errorf("enter after the search opens %q", open)
	}
	if _, handled := keysTo(l, "esc"); !handled || l.Query() != "" || shownRows(l) != "basil,mint,oak,thyme,fern" {
		t.Errorf("the first back: query %q, rows %s", l.Query(), shownRows(l))
	}
	if selected(t, l) != "mint" {
		t.Errorf("clearing the filter lost the selection: %s", selected(t, l))
	}
	if _, handled := keysTo(l, "esc"); handled {
		t.Error("back without a filter was not left to the application")
	}
	keysTo(l, "/")
	typeList(l, "x")
	keysTo(l, "esc")
	if l.Searching() || l.Query() != "" {
		t.Error("back while typing does not clear and stop the search")
	}
}

// A pure choice list filters with every typed character and still opens with enter.
func TestSearchAsTyped(t *testing.T) {
	l := filledList(t, func(s *ListSpec) { s.Search, s.Keys.Search = SearchAsTyped, "" })
	if l.Input() != InputText {
		t.Fatal("a list that filters as typed reads text")
	}
	typeList(l, "thy")
	if got := shownRows(l); got != "thyme" {
		t.Errorf("typing thy shows %s", got)
	}
	if open, _ := keysTo(l, "enter"); open != "thyme" {
		t.Errorf("enter opens %q", open)
	}
	view, _ := l.View(Styles{}, 40, 6)
	if first := strings.Split(view, "\n")[0]; !strings.HasPrefix(first, "thy_") {
		t.Errorf("the search line comes first: %q", first)
	}
}

// Groups stand in alphabetical order with the group without key last; left, right and enter fold them, a
// filter opens the groups with hits without changing their fold state, and the fold state outlives a refill.
func TestGroupsFoldAndFilter(t *testing.T) {
	l := filledList(t, nil)
	l.SetGrouped(true)
	if got := shownRows(l); got != "[herbs],basil,mint,thyme,[trees],oak,[],fern" {
		t.Fatalf("grouped rows = %s", got)
	}
	l.Reset()
	if selected(t, l) != "basil" {
		t.Errorf("a reset list opens on %s, want the first entry below its group row", selected(t, l))
	}
	keysTo(l, "left")
	if got := shownRows(l); got != "[herbs],[trees],oak,[],fern" || l.mark().group != "herbs" {
		t.Fatalf("left on an entry folds its group and moves to its row: %s", got)
	}
	if _, ok := l.Selected(); ok {
		t.Error("a group row is selected as an entry")
	}
	keysTo(l, "right")
	if got := shownRows(l); got != "[herbs],basil,mint,thyme,[trees],oak,[],fern" {
		t.Fatalf("right on a group row unfolds it: %s", got)
	}
	if open, handled := keysTo(l, "enter"); open != "" || !handled || shownRows(l) != "[herbs],[trees],oak,[],fern" {
		t.Fatalf("enter on a group row folds it and opens nothing: %q %s", open, shownRows(l))
	}
	l.SetFilter("SPREADS")
	if got := shownRows(l); got != "[herbs],mint" {
		t.Errorf("the filter opens the folded group with hits: %s", got)
	}
	if _, handled := keysTo(l, "left"); handled {
		t.Error("left folds while a filter is set")
	}
	l.SetFilter("")
	if got := shownRows(l); got != "[herbs],[trees],oak,[],fern" {
		t.Errorf("clearing the filter brings the fold back: %s", got)
	}
	if err := l.SetItems(samplePlants()); err != nil {
		t.Fatal(err)
	}
	if got := shownRows(l); got != "[herbs],[trees],oak,[],fern" {
		t.Errorf("the fold is lost on a refill: %s", got)
	}
	if !l.Select("mint") || shownRows(l) != "[herbs],basil,mint,thyme,[trees],oak,[],fern" {
		t.Errorf("Select of an entry in a folded group unfolds it: %s", shownRows(l))
	}
	view, _ := l.View(Styles{}, 60, 10)
	for _, want := range []string{"v # herbs (3)", "v # unsorted (1)"} {
		if !strings.Contains(view, want) {
			t.Errorf("the group rows lack %q:\n%s", want, view)
		}
	}
	l.SetGrouped(false)
	if _, handled := keysTo(l, "left"); handled {
		t.Error("left on a flat list is not left to the application")
	}
}

// The position counts entries only; a group row counts as the entry above it.
func TestPositionCountsEntriesOnly(t *testing.T) {
	l := filledList(t, nil)
	l.SetGrouped(true)
	l.Reset()
	if got := l.Position(); got != "1/5" {
		t.Errorf("first entry: %q", got)
	}
	keysTo(l, "up")
	if got := l.Position(); got != "1/5" {
		t.Errorf("on the first group row: %q", got)
	}
	keysTo(l, "down", "down", "down", "down")
	if got := l.Position(); got != "3/5" {
		t.Errorf("on the group row after thyme: %q", got)
	}
	l.SetFilter("1200")
	if got := l.Position(); got != "1/1 (5 total)" {
		t.Errorf("filtered: %q", got)
	}
	l.SetFilter("zz")
	if got := l.Position(); got != "0/0 (5 total)" {
		t.Errorf("without match: %q", got)
	}
	de, _ := newSampleList(t, Styles{}, i18n.German, nil)
	if err := de.SetItems(samplePlants()); err != nil {
		t.Fatal(err)
	}
	de.SetFilter("o")
	if got := de.Position(); got != "1/3 (5 gesamt)" {
		t.Errorf("German: %q", got)
	}
	empty, _ := newSampleList(t, Styles{}, i18n.English, nil)
	if got := empty.Position(); got != "" {
		t.Errorf("an empty list: %q", got)
	}
}

// A list of records stops at its ends and leaves up from the first row to the application; a list to choose
// from wraps. Page keys and the wheel always stop.
func TestRecordsStopAndChoicesWrap(t *testing.T) {
	l := filledList(t, nil)
	if _, handled := keysTo(l, "up"); handled || selected(t, l) != "basil" {
		t.Error("up from the first row of a list that stops is not left to the application")
	}
	keysTo(l, "end", "down")
	if selected(t, l) != "fern" {
		t.Errorf("down past the end selected %s", selected(t, l))
	}
	keysTo(l, "home")
	if selected(t, l) != "basil" {
		t.Errorf("home selected %s", selected(t, l))
	}
	l.Wheel(-1)
	if selected(t, l) != "basil" {
		t.Error("the wheel wrapped")
	}
	l.Wheel(3)
	if selected(t, l) != "thyme" {
		t.Errorf("three notches down selected %s", selected(t, l))
	}
	c := filledList(t, func(s *ListSpec) { s.Wrap = true })
	if _, handled := keysTo(c, "up"); !handled || selected(t, c) != "fern" {
		t.Errorf("up on the first choice selected %s", selected(t, c))
	}
	keysTo(c, "down")
	if selected(t, c) != "basil" {
		t.Errorf("down on the last choice selected %s", selected(t, c))
	}
	keysTo(c, "pgup")
	if selected(t, c) != "basil" {
		t.Error("a page key wrapped")
	}
}

// The selection is always on screen, the scroll position stays while the selection is on the page, and the
// page keys move by a page.
func TestWindowKeepsTheSelectionInView(t *testing.T) {
	l, _ := newSampleList(t, Styles{}, i18n.English, nil)
	var items []ListItem
	for i := range 120 {
		id := fmt.Sprintf("item-%03d", i)
		items = append(items, ListItem{ID: ItemID(id), Cells: []string{id, "resting", "1", ""}})
	}
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	const height = 8 // the header and seven rows
	check := func(want string) {
		t.Helper()
		view, _ := l.View(Styles{}, 50, height)
		if !strings.Contains(view, "> "+want) {
			t.Fatalf("the selection %s is not on screen:\n%s", want, view)
		}
		if n := len(strings.Split(view, "\n")); n != height {
			t.Fatalf("%d lines, want %d", n, height)
		}
	}
	for i := range 120 {
		check(fmt.Sprintf("item-%03d", i))
		keysTo(l, "down")
	}
	keysTo(l, "home")
	check("item-000")
	keysTo(l, "down", "down", "down")
	check("item-003")
	if l.offset != 0 {
		t.Errorf("moving within the page scrolled to %d", l.offset)
	}
	keysTo(l, "pgdown")
	if got := selected(t, l); got != "item-009" {
		t.Errorf("pgdown selected %s, want a page of six further", got)
	}
	check("item-009")
	keysTo(l, "pgup")
	check("item-003")
	if l.offset != 3 {
		t.Errorf("the scroll position moved to %d while the selection stayed on the page", l.offset)
	}
}

// When a row does not fit, the free text gives way first, then short labels, then optional columns from the
// right, then the widest values; the first column and the titles are never cut, and the widths stay while a
// filter is typed.
func TestColumnsGiveWayInTheirOrder(t *testing.T) {
	l := filledList(t, nil)
	header := func(width int) string {
		view, _ := l.View(Styles{}, width, 3)
		return strings.TrimRight(strings.Split(view, "\n")[0], " ")
	}
	row := func(width int) string {
		view, _ := l.View(Styles{}, width, 3)
		return strings.TrimRight(strings.Split(view, "\n")[1], " ")
	}
	if got := header(80); got != "  NAME   STATE     SIZE  NOTE" {
		t.Errorf("titles are upper case: %q", got)
	}
	if got := row(80); got != "> basil  blooming    12  on the sill by the kitchen window" {
		t.Errorf("wide row = %q", got)
	}
	if got := row(43); got != "> basil  blooming    12  on the sill by th~" {
		t.Errorf("the free text gives way first: %q", got)
	}
	if got := row(37); got != "> basil  blooming    12  on the sill~" {
		t.Errorf("down to its floor: %q", got)
	}
	if got := row(34); got != "> basil  bl       12  on the sill~" {
		t.Errorf("then short labels: %q", got)
	}
	if got := row(31); got != "> basil  bl     on the sill by~" {
		t.Errorf("then the widest values: %q", got)
	}
	if got := row(25); got != "> basil  bl     on the s~" {
		t.Errorf("then the optional column: %q", got)
	}
	if got := header(21); got != "  NAME   STATE  NOTE" {
		t.Errorf("never below the titles: %q", got)
	}
	if got := row(12); !strings.HasPrefix(got, "> basil") {
		t.Errorf("the first column is cut: %q", got)
	}
	before := header(50)
	l.SetFilter("thyme")
	view, _ := l.View(Styles{}, 50, 3)
	if got := strings.TrimRight(strings.Split(view, "\n")[1], " "); got != before {
		t.Errorf("the widths change while filtering: %q, was %q", got, before)
	}
}

// An empty list says its sentence with the next step; a filter without match names the text and the key
// that clears it, in the key in force.
func TestEmptyListAndFilterWithoutMatch(t *testing.T) {
	l, _ := newSampleList(t, Styles{}, i18n.English, nil)
	view, targets := l.View(Styles{}, 60, 4)
	if !strings.Contains(view, "No plants yet; n adds one.") {
		t.Errorf("the empty list:\n%s", view)
	}
	if len(targets) != 1 || targets[0].Zone != ZoneRows || targets[0].Action != "" {
		t.Errorf("an empty list has only its free area: %+v", targets)
	}
	if err := l.SetItems(samplePlants()); err != nil {
		t.Fatal(err)
	}
	l.SetFilter("zzz")
	view, _ = l.View(Styles{}, 80, 5)
	if !strings.Contains(view, `No entry matches "zzz"; esc clears the filter.`) || strings.Contains(view, "No plants") {
		t.Errorf("a filter without match:\n%s", view)
	}
	if open, handled := keysTo(l, "enter"); open != "" || !handled {
		t.Error("enter without a selection opened something")
	}
}

// Control characters in cells, group keys and the typed filter never reach the terminal.
func TestListEscapesControlCharacters(t *testing.T) {
	for look, st := range looks(t) {
		l, _ := newSampleList(t, st, i18n.English, func(s *ListSpec) { s.SearchInBar = false })
		items := []ListItem{{ID: "a", Group: "g\x1b]0;title\x07", Cells: []string{"x\x1b[2J", "\x07bell", "1", "\x1b[31mred"}}}
		if err := l.SetItems(items); err != nil {
			t.Fatal(err)
		}
		l.SetGrouped(true)
		keysTo(l, "/")
		typeList(l, "x")
		l.query = append(l.query, '\x1b')
		view, _ := l.View(st, 80, 6)
		plain := ansi.Strip(view)
		if strings.ContainsAny(plain, "\x1b\x07") {
			t.Errorf("%s: a control character reached the screen: %q", look, plain)
		}
		for _, want := range []string{`x\x1b[2J`, `\abell`, `g\x1b]0;title\a`} {
			if !strings.Contains(plain, want) {
				t.Errorf("%s: %q is not shown escaped:\n%s", look, want, plain)
			}
		}
		for _, line := range strings.Split(view, "\n") {
			if w := ansi.StringWidth(line); w != 80 {
				t.Errorf("%s: a line is %d cells wide", look, w)
			}
		}
	}
}

// A click marks a row; only a click on the marked row while the rows have the focus opens it. A group row
// folds at the first click, and a click while a filter is typed keeps the filter and stops typing.
func TestRowClickMarksThenOpens(t *testing.T) {
	l := filledList(t, nil)
	l.SetGrouped(true)
	l.Reset()
	clickOn := func(item string, focused bool) (ItemID, bool) {
		_, targets := l.View(Styles{}, 60, 10)
		m := NewMouseMap(targets, true)
		for _, tg := range targets {
			if tg.Item == item {
				return clickFocused(l, m.Handle(click(tg.Rect.X+1, tg.Rect.Y)), focused)
			}
		}
		t.Fatalf("no target for %q", item)
		return "", false
	}
	if open, handled := clickOn("mint", true); open != "" || !handled || selected(t, l) != "mint" {
		t.Fatalf("the first click opened %q or did not mark: %s", open, selected(t, l))
	}
	if open, _ := clickOn("mint", false); open != "" {
		t.Error("a click on the marked row without focus opened it")
	}
	if open, _ := clickOn("mint", true); open != "mint" {
		t.Errorf("the click on the marked row opened %q", open)
	}
	clickOn(groupRef+"trees", true)
	if got := shownRows(l); got != "[herbs],basil,mint,thyme,[trees],[],fern" {
		t.Errorf("a click on a group row folds it: %s", got)
	}
	clickOn(groupRef+"trees", true)
	keysTo(l, "/")
	typeList(l, "a")
	if open, _ := clickOn("oak", true); open != "" || l.Searching() || l.Query() != "a" || selected(t, l) != "oak" {
		t.Errorf("a click while typing: open %q, searching %v, query %q", open, l.Searching(), l.Query())
	}
	if open, handled := clickFocused(l, MouseResult{Zone: ZoneRows, Handled: true}, true); open != "" || handled {
		t.Error("the free area is the application's, to focus the rows")
	}
	if _, handled := clickFocused(l, MouseResult{Item: "gone", Zone: ZoneRows, Action: "enter", Handled: true}, true); handled {
		t.Error("a target of an entry no longer shown acted")
	}
}

// Every control the list and its filter bar draw has a target, and a click on it does what its key does: a
// row and a group row the enter key, the search line and the search field the search key, a field and a value
// of the open list the enter key of the bar.
func TestEveryControlHasATarget(t *testing.T) {
	st := looks(t)["noneascii"]
	l, keys := newSampleList(t, st, i18n.English, nil)
	if err := l.SetItems(samplePlants()); err != nil {
		t.Fatal(err)
	}
	l.SetGrouped(true)
	keysTo(l, "/")
	typeList(l, "e")
	view, targets := l.View(st, 70, 12)
	rows := strings.Split(ansi.Strip(view), "\n")
	byRow := map[int]Target{}
	for _, tg := range targets {
		if tg.Action != "" {
			byRow[tg.Rect.Y] = tg
		}
	}
	if tg, ok := byRow[0]; !ok || tg.Action != "search" || !strings.HasPrefix(rows[0], "/ e") {
		t.Errorf("the search line has no search target: %+v, %q", tg, rows[0])
	}
	for y, line := range rows[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		tg, ok := byRow[y+2]
		if !ok || tg.Action != "enter" || tg.Item == "" {
			t.Errorf("row %q has no target", line)
		}
	}
	m := NewMouseMap(targets, true)
	for _, tg := range targets[1:] {
		if got := m.Handle(click(tg.Rect.X, tg.Rect.Y)); got.Action != tg.Action || got.Item != tg.Item {
			t.Errorf("a click at %d,%d reports %+v, want %+v", tg.Rect.X, tg.Rect.Y, got, tg)
		}
		if tg.Action != "" && !keys.Is(press(keys.Actions()[keys.index[tg.Action]].DefaultKeys[0]), tg.Action, InputNavigation) {
			t.Errorf("target action %q is no key of the table", tg.Action)
		}
	}

	bl, _ := newSampleList(t, st, i18n.English, func(s *ListSpec) { s.SearchInBar = true })
	bar := sampleBar(t, keys, i18n.English, bl)
	line, barTargets := bar.View(st, 80)
	fields := 0
	for _, tg := range barTargets {
		cells := ansi.Cut(ansi.Strip(line), tg.Rect.X, tg.Rect.X+tg.Rect.Width)
		switch {
		case strings.HasPrefix(tg.Item, fieldRef):
			fields++
			if tg.Action != "enter" || !strings.Contains(cells, ":") {
				t.Errorf("field target %+v covers %q", tg, cells)
			}
		case tg.Action == "search":
			if !strings.Contains(cells, "/ search") {
				t.Errorf("search target covers %q", cells)
			}
		case tg.Action != "" || tg.Item != "":
			t.Errorf("unexpected bar target %+v", tg)
		}
	}
	if fields != 2 {
		t.Errorf("%d field targets for two fields:\n%s", fields, line)
	}
	if open, handled := clickFocused(bl, NewMouseMap(barTargets, true).Handle(click(75, 0)), false); open != "" || !handled || !bl.Searching() {
		t.Error("a click on the search field does not start the search")
	}
	bar.Focus(false)
	keysTo2(bar, "enter")
	list, listTargets := bar.ValueList(st)
	if len(listTargets) != 2 || !strings.Contains(list, "Kind") || !strings.Contains(list, "None") {
		t.Errorf("the value list has %d targets:\n%s", len(listTargets), list)
	}
	for _, tg := range listTargets {
		if tg.Action != "enter" || !strings.HasPrefix(tg.Item, optionRef) {
			t.Errorf("value target %+v", tg)
		}
	}
}

// clickFocused hands the list a click after it says whether the rows had the focus before it.
func clickFocused(l *List, r MouseResult, focused bool) (ItemID, bool) {
	l.SetFocused(focused)
	return l.Click(r)
}

func tickList(t *testing.T, edit func(*ListSpec)) *List {
	t.Helper()
	l := filledList(t, func(s *ListSpec) {
		s.Ticks = true
		if edit != nil {
			edit(s)
		}
	})
	l.SetGrouped(true)
	l.Reset()
	return l
}

func ticks(l *List) string {
	ids := l.Ticked()
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return strings.Join(out, ",")
}

func TestNewListTicksNeedAToggleAction(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, listCatalogs(), keys)
	cases := map[string]func(*ListSpec){
		"no toggle key":      func(s *ListSpec) { s.Keys.Toggle = "" },
		"unknown toggle key": func(s *ListSpec) { s.Keys.Toggle = "gone" },
		"shared toggle key":  func(s *ListSpec) { s.Keys.Toggle = s.Keys.Enter },
	}
	for name, edit := range cases {
		spec := ListSpec{Columns: sampleColumns(), Keys: sampleListKeys(), Empty: "list.empty", Ticks: true}
		edit(&spec)
		if _, err := NewList(spec, keys, text); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	spec := ListSpec{Columns: sampleColumns(), Keys: sampleListKeys(), Empty: "list.empty", AllRow: true}
	if _, err := NewList(spec, keys, text); err == nil {
		t.Error("the row of all without tick boxes was accepted")
	}
	spec = ListSpec{Columns: sampleColumns(), Keys: sampleListKeys(), Empty: "list.empty"}
	spec.Keys.Toggle = ""
	if _, err := NewList(spec, keys, text); err != nil {
		t.Errorf("a list without tick boxes needs no toggle key: %v", err)
	}
}

func TestTicksOfEntries(t *testing.T) {
	l := tickList(t, nil)
	keysTo(l, "space", "down", "down", "down", "down", "space")
	if got := ticks(l); got != "basil,oak" {
		t.Fatalf("ticked = %q, want basil,oak", got)
	}
	if err := l.SetTicked([]ItemID{"mint", "nope"}); err == nil || ticks(l) != "basil,oak" {
		t.Errorf("an unknown ID was accepted or changed the ticks: %v, %q", err, ticks(l))
	}
	if err := l.SetTicked([]ItemID{"fern", "basil"}); err != nil || ticks(l) != "basil,fern" {
		t.Errorf("SetTicked: %v, %q (the order is the one of the entries)", err, ticks(l))
	}
	items := samplePlants()[:4]
	items[0].Cells[3] = "changed"
	if err := l.SetItems(items); err != nil || ticks(l) != "basil" {
		t.Errorf("SetItems keeps known ticks and drops vanished ones: %v, %q", err, ticks(l))
	}
	l.Select("basil")
	if got, ok := l.Current(); !ok || got.ID != "basil" || got.Cells[3] != "changed" {
		t.Errorf("Current = %+v, %v", got, ok)
	}
	keysTo(l, "up")
	if _, ok := l.Current(); ok {
		t.Error("a group row is the current entry")
	}
	plain := filledList(t, nil)
	if _, handled := keysTo(plain, "space"); handled || ticks(plain) != "" {
		t.Error("the toggle key acted in a list without tick boxes")
	}
}

func TestGroupToggleSequenceReturnsToThePartialSelection(t *testing.T) {
	l := tickList(t, nil)
	l.SetTicked([]ItemID{"basil"})
	keysTo(l, "up")
	if l.mark().group != "herbs" {
		t.Fatalf("not on the group row: %+v", l.mark())
	}
	want := []string{"basil,mint,thyme", "", "basil", "basil,mint,thyme", "", "basil"}
	for i, w := range want {
		keysTo(l, "space")
		if got := ticks(l); got != w {
			t.Fatalf("toggle %d: ticked %q, want %q", i+1, got, w)
		}
	}
	// Without a partial selection the group alternates between all and none.
	l = tickList(t, nil)
	keysTo(l, "up")
	for i, w := range []string{"basil,mint,thyme", "", "basil,mint,thyme", ""} {
		keysTo(l, "space")
		if got := ticks(l); got != w {
			t.Fatalf("plain toggle %d: ticked %q, want %q", i+1, got, w)
		}
	}
	// A single toggle in the group forgets the partial selection: the basil that is back in the memory
	// would otherwise return after none.
	l = tickList(t, nil)
	l.SetTicked([]ItemID{"basil"})
	keysTo(l, "up", "space", "space", "down", "space", "space", "up", "space")
	if got := ticks(l); got != "basil,mint,thyme" {
		t.Errorf("after a single toggle the next step from none is all: %q", got)
	}
	// SetTicked and SetItems forget it too.
	for _, forget := range []func(){func() { l.SetTicked([]ItemID{}) }, func() { l.SetItems(samplePlants()) }} {
		l = tickList(t, nil)
		l.SetTicked([]ItemID{"basil"})
		keysTo(l, "up", "space", "space")
		forget()
		keysTo(l, "space")
		if got := ticks(l); got != "basil,mint,thyme" {
			t.Errorf("the memory outlived a change of the ticks: %q", got)
		}
	}
}

func TestGroupToggleActsOnShownEntriesOnly(t *testing.T) {
	l := tickList(t, nil)
	l.SetTicked([]ItemID{"mint"})
	l.SetFilter("blooming") // basil and thyme of herbs
	if got := shownRows(l); got != "[herbs],basil,thyme" {
		t.Fatalf("rows = %s", got)
	}
	st := looks(t)["noneascii"]
	if view, _ := l.View(st, 60, 8); strings.Contains(ansi.Strip(view), "[-]") {
		t.Errorf("the box counts the shown entries, none of which is ticked:\n%s", ansi.Strip(view))
	}
	keysTo(l, "up", "space")
	if got := ticks(l); got != "basil,mint,thyme" {
		t.Errorf("filtered toggle ticked %q", got)
	}
	keysTo(l, "space")
	if got := ticks(l); got != "mint" {
		t.Errorf("filtered toggle back left %q; the hidden mint stays", got)
	}
}

func TestTickBoxesShowThreeStatesWithoutColour(t *testing.T) {
	for _, name := range []string{"noneascii", "mono"} {
		st := looks(t)[name]
		l := tickList(t, nil)
		row := func() string {
			view, _ := l.View(st, 60, 12)
			return strings.Split(ansi.Strip(view), "\n")[1]
		}
		if got := row(); !strings.Contains(got, "[ ] ") {
			t.Errorf("%s: none: %q", name, got)
		}
		l.SetTicked([]ItemID{"basil"})
		if got := row(); !strings.Contains(got, "[-] ") {
			t.Errorf("%s: some: %q", name, got)
		}
		l.SetTicked([]ItemID{"basil", "mint", "thyme"})
		if got := row(); !strings.Contains(got, "[x] ") {
			t.Errorf("%s: all: %q", name, got)
		}
		view := ansi.Strip(func() string { v, _ := l.View(st, 60, 12); return v }())
		if !strings.Contains(view, "[x] basil") || !strings.Contains(view, "[ ] oak") {
			t.Errorf("%s: entries:\n%s", name, view)
		}
	}
}

func TestClickOnTitleFoldsAndClickOnBoxTicks(t *testing.T) {
	l := tickList(t, nil)
	l.SetTicked([]ItemID{"basil"})
	at := func(item string) (Target, *MouseMap) {
		_, targets := l.View(Styles{}, 60, 12)
		for i := len(targets) - 1; i >= 0; i-- {
			if targets[i].Item == item {
				return targets[i], NewMouseMap(targets, true)
			}
		}
		t.Fatalf("no target for %q", item)
		return Target{}, nil
	}
	tg, m := at(groupRef + "herbs")
	if _, handled := l.Click(m.Handle(click(tg.Rect.X+30, tg.Rect.Y))); !handled || shownRows(l) != "[herbs],[trees],oak,[],fern" || ticks(l) != "basil" {
		t.Fatalf("a click on the title: rows %s, ticks %q", shownRows(l), ticks(l))
	}
	tg, m = at(tickRef + groupRef + "herbs")
	if _, handled := l.Click(m.Handle(click(tg.Rect.X+1, tg.Rect.Y))); !handled {
		t.Fatal("the click on the group box was not handled")
	}
	if ticks(l) != "basil,mint,thyme" || shownRows(l) != "[herbs],[trees],oak,[],fern" {
		t.Errorf("a click on the group box: ticks %q, rows %s (it ticks the folded entries too and folds nothing)", ticks(l), shownRows(l))
	}
	if _, handled := l.Click(m.Handle(click(tg.Rect.X+2, tg.Rect.Y))); !handled || ticks(l) != "" {
		t.Errorf("the second click on the box: ticks %q", ticks(l))
	}
	l.SetFocused(true)
	tg, m = at(tickRef + "oak")
	if open, _ := l.Click(m.Handle(click(tg.Rect.X, tg.Rect.Y))); open != "" || ticks(l) != "oak" {
		t.Errorf("a click on the box of an entry: open %q, ticks %q", open, ticks(l))
	}
	if open, _ := l.Click(m.Handle(click(tg.Rect.X+2, tg.Rect.Y))); open != "" || ticks(l) != "" {
		t.Errorf("a click on the box of the marked entry opened it or did not toggle: open %q, ticks %q", open, ticks(l))
	}
}

func TestRowOfAllActsAcrossGroups(t *testing.T) {
	for _, lang := range []struct {
		lang i18n.Lang
		word string
	}{{i18n.English, "all (5)"}, {i18n.German, "alle (5)"}} {
		l, _ := newSampleList(t, Styles{}, lang.lang, func(s *ListSpec) { s.Ticks, s.AllRow = true, true })
		l.SetItems(samplePlants())
		l.SetGrouped(true)
		l.Reset()
		view, _ := l.View(Styles{}, 60, 12)
		if !strings.Contains(view, "[ ] "+lang.word) {
			t.Errorf("%s: no row of all:\n%s", lang.lang, view)
		}
		if got := shownRows(l); got != "*,[herbs],basil,mint,thyme,[trees],oak,[],fern" {
			t.Fatalf("rows = %s", got)
		}
	}
	l := tickList(t, func(s *ListSpec) { s.AllRow = true })
	l.SetTicked([]ItemID{"oak"})
	keysTo(l, "up", "up")
	if !l.mark().all {
		t.Fatalf("not on the row of all: %+v", l.mark())
	}
	for i, w := range []string{"basil,mint,oak,thyme,fern", "", "oak", "basil,mint,oak,thyme,fern"} {
		keysTo(l, "space")
		if got := ticks(l); got != w {
			t.Fatalf("toggle %d: %q, want %q", i+1, got, w)
		}
	}
	keysTo(l, "left", "enter")
	if shownRows(l) != "*,[herbs],basil,mint,thyme,[trees],oak,[],fern" {
		t.Errorf("left or enter folded something on the row of all: %s", shownRows(l))
	}
	if _, ok := l.Selected(); ok {
		t.Error("the row of all is an entry")
	}
	l.SetFilter("zzz")
	if shownRows(l) != "" {
		t.Errorf("rows %s with no match", shownRows(l))
	}
}

func TestNarrowTerminalKeepsOnlyTheID(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	spec := ListSpec{Keys: sampleListKeys(), Empty: "list.empty", Ticks: true, Columns: []Column{
		{Title: "col.name"}, {Title: "col.note", Flex: true, Optional: true},
	}}
	l, err := NewList(spec, keys, newTranslator(t, i18n.English, listCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	l.SetItems([]ListItem{{ID: "installation-checker", Cells: []string{"installation-checker", "checks the installation"}}})
	row := func(width int) string {
		view, _ := l.View(Styles{}, width, 3)
		return strings.TrimRight(strings.Split(view, "\n")[1], " ")
	}
	if got := row(60); got != "> [ ] installation-checker  checks the installation" {
		t.Errorf("wide: %q", got)
	}
	if got := row(26); got != "> [ ] installation-checker" {
		t.Errorf("narrow keeps the ID whole and drops the description: %q", got)
	}
	view, _ := l.View(Styles{}, 26, 3)
	if strings.Contains(view, "NOTE") {
		t.Errorf("the title of the dropped column stays:\n%s", view)
	}
}

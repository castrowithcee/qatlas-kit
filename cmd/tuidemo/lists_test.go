package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

func TestListsFilterByKeyAndOpenByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "6", "types a filter")
	act(t, term, "\t\t", "> Basil") // tab twice: the sub-tab line, then the rows
	act(t, term, "/sa", "/ sa")
	expect(t, term, "Sage")
	gone(t, term, "Basil")
	act(t, term, "\x1b", "Basil") // the first back clears the filter
	expect(t, term, "Fennel")
	clickOn(t, term, "Mint", "> Mint")
	clickOn(t, term, "Mint", "Opened Mint")
	act(t, term, "\x1b", "[Filter]") // the next back leaves the rows
}

func TestListsLettersTypeIntoTheFilterNotIntoTheSwitches(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "6", "types a filter")
	send(t, term, "\t\t/")
	act(t, term, "tl", "/ tl")
	expect(t, term, "No entry matches")
	expect(t, term, "theme: terminal")
	expect(t, term, "language: english")
}

func TestListsFilterBarByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "6", "Group: none")
	act(t, term, "f", ">  Group: none")
	act(t, term, "\x1b[C", "Group: by place")
	expect(t, term, "East bed (2)")
	clickOn(t, term, "Watered: any time", "recently")
	clickOn(t, term, "recently", "Watered: recently")
	gone(t, term, "Fennel")
}

func TestListsGroupsFoldByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	clickOn(t, term, "6 Lists", "types a filter")
	clickOn(t, term, "Groups", "East bed (2)")
	expect(t, term, "Basil")
	clickOn(t, term, "East bed (2)", "▸")
	gone(t, term, "Basil")
	clickOn(t, term, "East bed (2)", "Basil")
	act(t, term, "\x1b[B", ">     Basil")
	act(t, term, "\x1b[D", "▸ ■ East bed") // left folds the group of the marked entry
	gone(t, term, "Basil")
}

func TestListsTicksByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "6", "types a filter")
	clickOn(t, term, "Ticks", "Ticked: 0")
	clickOn(t, term, "[ ] Chive", "Ticked: 1")
	act(t, term, " ", "Ticked: 0") // the toggle key ticks while the filter takes the typing
	act(t, term, "ba", "Basil")
	act(t, term, "\x1b[B\x1b[B", "> ")
	act(t, term, " ", "Ticked: 1")
}

func TestListsTakeTheFocusByTabAndKeepLettersAsSwitchesOutsideTyping(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "6")
	if got, want := m.zones(), []tui.FocusZone{tui.ZoneTabs, tui.ZoneSubtabs, tui.ZoneRows, tui.ZoneFilters}; !equalZones(got, want) {
		t.Fatalf("zones %v, want %v", got, want)
	}
	press := func(msg tea.KeyPressMsg) { m.Update(msg) }
	press(tea.KeyPressMsg{Code: tea.KeyTab})
	press(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.zone != tui.ZoneRows {
		t.Fatalf("zone %q after two tabs, want rows", m.zone)
	}
	// Outside the filter a letter is still a switch of the workbench.
	press(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.prefs.Theme == tui.ThemeTerminal {
		t.Fatal("t did not switch the theme while the rows only navigate")
	}
	theme := m.prefs.Theme
	// While the filter types, letters go to it.
	press(tea.KeyPressMsg{Code: '/', Text: "/"})
	if m.input() != tui.InputText {
		t.Fatal("the search key did not start the filter")
	}
	for _, r := range "tlIM" {
		press(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.prefs.Theme != theme || m.prefs.Language != uiprefs.LanguageEnglish || m.prefs.Icons != uiprefs.IconsNone || !m.prefs.MouseEnabled {
		t.Fatalf("a letter typed into the filter switched something: %+v", m.prefs)
	}
	// Back clears the filter, the next back leaves the rows, and ctrl+c always quits.
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.zone != tui.ZoneSubtabs {
		t.Fatalf("zone %q after back, want the sub-tab line", m.zone)
	}
	press(tea.KeyPressMsg{Code: tea.KeyTab})
	press(tea.KeyPressMsg{Code: '/', Text: "/"})
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+c did not quit while the filter types")
	}
}

func equalZones(a, b []tui.FocusZone) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

const keyF2 = "\x1bOQ"

func TestFormFieldsFillByKeyAndSaveWithF2(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "7", "A bed to add")
	act(t, term, "\t\t", "> Name") // tab twice: the sub-tab line, then the form
	act(t, term, keyF2, "The name is empty.")
	act(t, term, "Fern bed", "Fern bed")
	gone(t, term, "The name is empty.")
	act(t, term, "\t", "> Note")
	act(t, term, "Shade note", "Shade note")
	act(t, term, "\t\t", "> Extras") // place, then the extras
	act(t, term, " ", "[x] Mulch")   // the toggle key ticks
	act(t, term, "\t", "> Shed code")
	act(t, term, "abc", "Shed code")
	gone(t, term, "abc") // a masked field never shows what is typed
	act(t, term, keyF2, "Added bed Fern bed with 1 extras")
}

func TestFormDatesPickInCalendarByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "7", "A bed to add")
	clickOn(t, term, "Dates", "Planting day")
	act(t, term, "\t", "> Planting day") // the click on the tab took the sub-tab line
	act(t, term, keyF2, "The planting day is empty.")
	act(t, term, "\r", "May 2031")
	act(t, term, "\x1b[C\r", "2031-05-07")
	gone(t, term, "May 2031")
	act(t, term, keyF2, "Planting on 2031-05-07 saved")
	column, row := find(t, term, "▦")
	act(t, term, click(column, row), "May 2031") // the calendar symbol, one cell wide, opens it
	act(t, term, "\x1b", "2031-05-07")
	gone(t, term, "May 2031")
	act(t, term, "\t", "> Harvest")
	act(t, term, "2031-05-08 09:30", "2031-05-08 09:30")
	act(t, term, keyF2, "Planting on 2031-05-07 saved")
}

func TestFormButtonsClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "7", "A bed to add")
	clickOn(t, term, "Save", "Not saved")
	act(t, term, "Fern", "Fern") // the refused save put the focus on the name
	clickOn(t, term, "Reset", "Form cleared")
	gone(t, term, "Fern")
	clickOn(t, term, "Name", "> Name")
	act(t, term, "Rose", "Rose")
	clickOn(t, term, "Save", "Added bed Rose")
}

func TestFormTakesTheFocusByTabAndTypesLettersNotSwitches(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "7")
	if got, want := m.zones(), []tui.FocusZone{tui.ZoneTabs, tui.ZoneSubtabs, tui.ZoneForm}; !equalZones(got, want) {
		t.Fatalf("zones %v, want %v", got, want)
	}
	press := func(msg tea.KeyPressMsg) { m.Update(msg) }
	press(tea.KeyPressMsg{Code: tea.KeyTab})
	press(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.zone != tui.ZoneForm || m.input() != tui.InputText {
		t.Fatalf("zone %q, input %q after two tabs, want the form typing", m.zone, m.input())
	}
	theme := m.prefs.Theme
	for _, r := range "tlIMq+" {
		press(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.prefs.Theme != theme || m.prefs.Language != uiprefs.LanguageEnglish || m.prefs.Icons != uiprefs.IconsNone || !m.prefs.MouseEnabled {
		t.Fatalf("a letter typed into the form switched something: %+v", m.prefs)
	}
	if got := m.live().(*formBlock).form.Values()[fieldName].Text; got != "tlIMq+" {
		t.Fatalf("name %q, want the typed letters", got)
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.zone != tui.ZoneSubtabs {
		t.Fatalf("zone %q after back, want the sub-tab line", m.zone)
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+c did not quit")
	}
}

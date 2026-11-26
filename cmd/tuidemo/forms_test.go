package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestDateFieldsOpenOnTheClockAndBoundHarvest(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	clock := time.Date(2040, 3, 15, 12, 0, 0, 0, time.Local)
	m.now = func() time.Time { return clock }
	blocks, err := formBlocks(m)
	if err != nil {
		t.Fatal(err)
	}
	form := blocks["dates"].(*formBlock).form
	at := func(y, mo, d int) tui.FormValue {
		return tui.FormValue{Time: time.Date(y, time.Month(mo), d, 12, 0, 0, 0, time.Local)}
	}
	for _, c := range []struct {
		id   tui.FieldID
		v    tui.FormValue
		fail bool
	}{
		{fieldPlanted, at(2031, 1, 1), false}, {fieldPlanted, at(2060, 1, 1), false},
		{fieldHarvest, at(2039, 3, 15), false}, {fieldHarvest, at(2041, 3, 15), false},
		{fieldHarvest, at(2039, 3, 14), true}, {fieldHarvest, at(2041, 3, 16), true},
	} {
		if err := form.SetValue(c.id, c.v); (err != nil) != c.fail {
			t.Errorf("%s %v: err %v, want failure %v", c.id, c.v.Time, err, c.fail)
		}
	}
	for _, id := range []tui.FieldID{fieldPlanted, fieldHarvest} {
		form, err = blocks["dates"].(*formBlock).build() // empty again
		if err != nil {
			t.Fatal(err)
		}
		form.Focus(id)
		form.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		form.SetFocused(true)
		view, _ := form.View(m.styles, 100, 40)
		if !strings.Contains(view, "March 2040") {
			t.Fatalf("the calendar of %s did not open on the clock month:\n%s", id, view)
		}
	}
}

// pathsForm opens the path form of the demo with the focus on its first field.
func pathsForm(t *testing.T) (*model, *tui.Form) {
	t.Helper()
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "7")
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyTab}, {Code: tea.KeyRight}, {Code: tea.KeyRight}, {Code: tea.KeyTab}} {
		m.Update(k)
	}
	b, ok := m.live().(*formBlock)
	if !ok || m.zone != tui.ZoneForm {
		t.Fatalf("live %T, zone %q, want the form of the paths tab with the focus", m.live(), m.zone)
	}
	return m, b.form
}

// ask sets the text of a path field as the application would and returns the answer, not yet delivered.
func ask(t *testing.T, m *model, form *tui.Form, id tui.FieldID, text string) tea.Cmd {
	t.Helper()
	form.Focus(id)
	if err := form.SetValue(id, tui.FormValue{Text: text}); err != nil {
		t.Fatal(err)
	}
	cmd := m.pathsLoad()
	if cmd == nil {
		t.Fatal("the change asked for no entries")
	}
	return cmd
}

// deliver runs the answer of cmd and hands it to the model, as the program would.
func deliver(m *model, cmd tea.Cmd) {
	m.Update(cmd())
}

func formText(m *model, form *tui.Form) string {
	view, _ := form.View(m.styles, 200, 40)
	return view
}

func TestPathFieldShowsTheEntriesOfALevel(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"alpha-bed", "alpha-pond", "beta"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m, form := pathsForm(t)
	deliver(m, ask(t, m, form, fieldFolder, filepath.Join(root, "al")))
	view := formText(m, form)
	if !strings.Contains(view, "alpha-bed") || !strings.Contains(view, "alpha-pond") || strings.Contains(view, "beta") {
		t.Errorf("folder level:\n%s", view)
	}
	if strings.Contains(view, "alpha.txt") {
		t.Errorf("the folder field offers a file:\n%s", view)
	}
	deliver(m, ask(t, m, form, fieldFile, filepath.Join(root, "al")))
	if view = formText(m, form); !strings.Contains(view, "alpha.txt") || !strings.Contains(view, "alpha-bed") {
		t.Errorf("file level:\n%s", view)
	}
	// A level that cannot be read answers without entries.
	deliver(m, ask(t, m, form, fieldFile, filepath.Join(root, "missing", "x")))
}

func TestPathFieldShowsTheMatchesOfASearch(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "one", "two", "needle-dir")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one", "needle.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m, form := pathsForm(t)
	deliver(m, ask(t, m, form, fieldFolder, root+"*needle"))
	view := formText(m, form)
	if !strings.Contains(view, "needle-dir") || strings.Contains(view, "needle.txt") {
		t.Errorf("folder search:\n%s", view)
	}
	deliver(m, ask(t, m, form, fieldFile, root+"*needle"))
	if view = formText(m, form); !strings.Contains(view, "needle-dir") || !strings.Contains(view, "needle.txt") {
		t.Errorf("file search:\n%s", view)
	}
	// A start that cannot be read, and a search without text, answer without entries and do not fail.
	deliver(m, ask(t, m, form, fieldFile, filepath.Join(root, "missing")+"*needle"))
	deliver(m, ask(t, m, form, fieldFile, root+"*"))
}

func TestPathFieldDropsAStaleAnswerAndCancelsTheSearch(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"old-one", "new-one"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	m, form := pathsForm(t)
	first := ask(t, m, form, fieldFolder, filepath.Join(root, "old"))
	second := ask(t, m, form, fieldFolder, filepath.Join(root, "new"))
	deliver(m, second)
	deliver(m, first) // the answer of the earlier text arrives last
	view := formText(m, form)
	if !strings.Contains(view, "new-one") || strings.Contains(view, "old-one") {
		t.Errorf("a stale answer changed the entries:\n%s", view)
	}
}

func TestPathSearchEndsCanceledWithItsContext(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "needle"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := answerPath(ctx, tui.PathLoad{Typed: root + "*needle", Search: true, Kind: tui.PathDirs, Token: 7})
	if res.Token != 7 || res.End != tui.PathEndCanceled {
		t.Errorf("answer %+v, want token 7 and a canceled end", res)
	}
}

func TestPathFormCancelsTheRunningSearchOnANewRequest(t *testing.T) {
	m, form := pathsForm(t)
	root := t.TempDir()
	ask(t, m, form, fieldFolder, root+"*a")
	b := m.live().(*formBlock)
	if b.cancel == nil {
		t.Fatal("no cancel function kept for the search")
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	ask(t, m, form, fieldFolder, root+"*ab")
	if ctx.Err() == nil {
		t.Error("the new request did not cancel the running one")
	}
}

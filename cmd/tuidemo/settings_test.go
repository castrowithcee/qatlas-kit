package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

func TestSettingsFormSwitchesThemeByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "3", lookBody)
	act(t, term, "\t\t", "> theme") // the sub-tab line, then the form
	act(t, term, "\x1b[C", "theme: demo-dark")
	act(t, term, "\x1b[C", "theme: demo-light")
	act(t, term, "\x1b[D", "theme: demo-dark") // left steps the theme back
	act(t, term, "t", "theme: demo-light")     // a global switch shows in the form
	expect(t, term, "demo-light ")
	act(t, term, "I", "icons: unicode")
	act(t, term, "\t", "> icons")
	act(t, term, "\x1b[C", "icons: nerdfont")
	clickOn(t, term, "│ demo-light", "demo-dark") // a click on the field opens its values
	clickOn(t, term, "demo-dark", "theme: demo-dark")
}

func TestSettingsFormKeepsItsFocusOverALanguageSwitch(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "3", lookBody)
	clickOn(t, term, "Input", "Language and mouse change at once")
	act(t, term, "\t", "> language")
	act(t, term, "\x1b[C", "Sprache: deutsch")
	expect(t, term, "> Sprache") // the focus stays on the field
	act(t, term, "\t", "> Maus")
	act(t, term, "l", "language: english")
}

func TestSettingsFormSwitchesMouseOffAndTheKeyOn(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.openArea("settings")
	m.tabs["settings"] = "input"
	m.zone = tui.ZoneForm
	key := func(code rune) { m.Update(tea.KeyPressMsg{Code: code}) }
	key(tea.KeyDown) // the mouse field
	key(tea.KeyRight)
	if m.prefs.MouseEnabled {
		t.Fatal("the mouse stays on after the form switched it off")
	}
	if clickWhere(m, func(r tui.MouseResult) bool { return r.Handled }) {
		t.Fatal("a click acts while the mouse is off")
	}
	m.Update(tea.KeyPressMsg{Code: 'M', Text: "M", Mod: tea.ModShift})
	if !m.prefs.MouseEnabled || m.blocks[blockKey{"settings", "input"}].(*formBlock).form.Values()[fieldMouse].Choice != "on" {
		t.Fatal("M did not switch the mouse on and show it in the form")
	}
}

func TestEveryThemeChoiceHasALabel(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	for _, name := range m.themes.Names() {
		id := tui.TextID("demo.theme." + name)
		if got := m.text.Lookup(id); got == string(id) || got == "" {
			t.Errorf("theme %q shows %q in the settings form", name, got)
		}
	}
}

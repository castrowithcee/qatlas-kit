package main

import (
	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// Field IDs of the settings forms.
const (
	fieldTheme    tui.FieldID = "theme"
	fieldIcons    tui.FieldID = "icons"
	fieldLanguage tui.FieldID = "language"
	fieldMouse    tui.FieldID = "mouse"
)

// choices lists the values of a choice field; the label of each is the text prefix+value.
func choices(prefix string, values ...string) []tui.Choice {
	out := make([]tui.Choice, len(values))
	for i, v := range values {
		out[i] = tui.Choice{ID: tui.ChoiceID(v), Label: tui.TextID(prefix + v)}
	}
	return out
}

// settingValues are the values the switches hold now, as the choice IDs of the form.
func settingValues(m *model) map[tui.FieldID]string {
	mouse := "off"
	if m.prefs.MouseEnabled {
		mouse = "on"
	}
	return map[tui.FieldID]string{fieldTheme: m.prefs.Theme, fieldIcons: m.prefs.Icons, fieldLanguage: m.prefs.Language, fieldMouse: mouse}
}

// settingsBlocks makes each settings tab a form whose choices take effect at once. The theme field steps
// back with left and forward with right, so the key and command that step the theme back keep their place
// in the form; the global switches stay on, and the form shows the value they set at its next drawing.
func settingsBlocks(m *model) (map[tui.TabID]block, error) {
	themes := append([]string{}, m.themes.Names()...)
	specs := map[tui.TabID][]tui.FormField{
		"look": {
			{ID: fieldTheme, Kind: tui.FieldChoice, Label: tui.TextSettingTheme, Choices: choices("demo.theme.", themes...)},
			{ID: fieldIcons, Kind: tui.FieldChoice, Label: tui.TextSettingIcons,
				Choices: choices("demo.icons.", uiprefs.IconsUnicode, uiprefs.IconsNerdFont, uiprefs.IconsNone)},
		},
		"input": {
			{ID: fieldLanguage, Kind: tui.FieldChoice, Label: tui.TextSettingLanguage,
				Choices: choices("demo.language.", uiprefs.LanguageEnglish, uiprefs.LanguageGerman, uiprefs.LanguageSystem)},
			{ID: fieldMouse, Kind: tui.FieldChoice, Label: tui.TextSettingMouse,
				Choices: []tui.Choice{{ID: "on", Label: tui.TextValueOn}, {ID: "off", Label: tui.TextValueOff}}},
		},
	}
	notes := map[tui.TabID]tui.TextID{"look": textSettingsLookBody, "input": textSettingsInputBody}
	out := map[tui.TabID]block{}
	for tab, fields := range specs {
		form, err := tui.NewForm(tui.FormSpec{Keys: formKeys(), Fields: fields}, m.keys, m.text)
		if err != nil {
			return nil, err
		}
		b := &formBlock{form: form, note: notes[tab], before: syncSettings, after: applySettings(tab)}
		syncSettings(m, b)
		out[tab] = b
	}
	return out, nil
}

// syncSettings shows the values the switches hold; it runs at every drawing, so a key, a command or a click
// elsewhere shows in the form.
func syncSettings(m *model, b *formBlock) {
	for id, want := range settingValues(m) {
		if _, ok := b.form.Values()[id]; ok && b.form.Values()[id].Choice != tui.ChoiceID(want) {
			_ = b.form.SetValue(id, tui.FormValue{Choice: tui.ChoiceID(want)})
		}
	}
}

// applySettings takes the values the form holds into the switches. A new language builds the blocks again,
// so the form that now stands gets the focus the old one had.
func applySettings(tab tui.TabID) func(m *model, b *formBlock) {
	return func(m *model, b *formBlock) {
		focused := b.form.Focused()
		v := b.form.Values()
		cur := settingValues(m)
		for id, got := range v {
			if string(got.Choice) == cur[id] {
				continue
			}
			switch id {
			case fieldTheme:
				m.setTheme(string(got.Choice))
			case fieldIcons:
				m.setIcons(string(got.Choice))
			case fieldLanguage:
				m.setLanguage(string(got.Choice))
			case fieldMouse:
				m.setMouse(got.Choice == "on")
			}
		}
		if nb, ok := m.blocks[blockKey{"settings", tab}].(*formBlock); ok && nb != b {
			nb.form.Focus(focused)
		}
	}
}

func (m *model) onOff(on bool) string {
	if on {
		return m.text.Lookup(tui.TextValueOn)
	}
	return m.text.Lookup(tui.TextValueOff)
}

package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// pickModal puts a List over the screen as a modal. The list is no tui.Modal itself: its enter and its
// clicks answer with the opened entry, which this turns into the answers the workbench reads.
type pickModal struct{ list *tui.List }

func (p pickModal) Update(msg tea.KeyPressMsg) (tui.ActionID, bool) {
	open, handled := p.list.Update(msg)
	if open != "" {
		return actEnter, true
	}
	return "", handled
}

// Click answers enter for an opened row or the choose button, and back for the back key of the key line,
// which the list leaves to the application.
func (p pickModal) Click(r tui.MouseResult) (tui.ActionID, bool) {
	open, handled := p.list.Click(r)
	switch {
	case open != "":
		return actEnter, true
	case !handled && r.Action == actBack:
		return actBack, true
	}
	return "", handled
}

func (p pickModal) Wheel(steps int) { p.list.Wheel(steps) }
func (p pickModal) Overlay(s tui.Styles, screen string, w, h int, room tui.Rect) (string, []tui.Target, bool) {
	return p.list.Overlay(s, screen, w, h, room)
}
func (p pickModal) View(s tui.Styles, w, h int) (string, []tui.Target) { return p.list.View(s, w, h) }
func (p pickModal) Hints() []tui.Hint                                  { return p.list.Hints() }

// bedItems are the beds of the sample plants, each once.
func bedItems() []tui.ListItem {
	var items []tui.ListItem
	seen := map[string]bool{}
	for _, r := range sampleRows() {
		if !seen[r.Bed] {
			seen[r.Bed] = true
			items = append(items, tui.ListItem{ID: tui.ItemID(r.Bed), Cells: []string{r.Bed}})
		}
	}
	return items
}

// openPicker opens the list of beds as a modal, on the bed chosen before.
func (m *model) openPicker() {
	keys := tui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst, Last: actLast,
		Left: actLeft, Right: actRight, Enter: actEnter, Back: actBack}
	list, err := tui.NewList(tui.ListSpec{Columns: []tui.Column{{Title: textSampleBed}}, Keys: keys,
		Search: tui.SearchAsTyped, Wrap: true, Empty: textListEmpty, Title: textPickTitle,
		Buttons: []tui.FormButton{{Action: actEnter, Label: tui.TextKeyChoose, Primary: true}}}, m.keys, m.text)
	if err == nil {
		err = list.SetItems(bedItems())
	}
	if err != nil {
		m.show(nil, err, 0, 0)
		return
	}
	list.Select(tui.ItemID(m.bed))
	m.picker = list
	m.show(pickModal{list}, nil, kindPick, tui.InputText)
}

// suggestPaths are the paths of the sample folder that continue what was typed: the entries of the folder the
// typed text names, a folder with its slash, so enter on one keeps the dialog open at its content.
func suggestPaths(typed string) []tui.Suggestion {
	dir := typed[:strings.LastIndex(typed, "/")+1]
	var out []tui.Suggestion
	seen := map[string]bool{}
	for _, f := range sampleFiles() {
		rest, ok := strings.CutPrefix(f.Path, dir)
		if !ok {
			continue
		}
		name, _, isDir := strings.Cut(rest, "/")
		cand := dir + name
		if isDir {
			cand += "/"
		}
		if strings.HasPrefix(cand, typed) && cand != typed && !seen[cand] {
			seen[cand] = true
			out = append(out, tui.Suggestion{Text: cand, Continues: isDir})
		}
	}
	return out
}

// openSuggest opens an input whose dialog offers the sample paths while it is typed in.
func (m *model) openSuggest() {
	keys := dialogKeys(false)
	keys.Complete = actZoneNext
	d, err := tui.NewDialog(tui.DialogSpec{
		Title:    textSuggestTitle,
		Question: m.text.Lookup(textSuggestQuestion),
		Input:    &tui.DialogInput{Value: m.path, Suggest: suggestPaths},
		Answers:  []tui.Answer{{Action: actEnter, Label: textOK}, {Action: actBack, Label: tui.TextKeyBack}},
		Default:  actEnter,
		Keys:     keys,
	}, m.keys, m.text)
	if err == nil {
		m.dialog = d
		m.show(d, nil, kindSuggest, d.Input())
		return
	}
	m.show(nil, err, 0, 0)
}

// picked takes the answer of the open picker or suggestion dialog; the other answers close it as cancelled.
func (m *model) picked(act tui.ActionID) {
	defer m.closeModal()
	if act != actEnter {
		m.cancelled()
		return
	}
	if m.kind == kindPick {
		if it, ok := m.picker.Current(); ok {
			m.bed = it.Cells[0]
			m.message = &tui.Message{Kind: tui.MessageResult, Text: m.text.Lookup(textPickDone, tui.Escape(m.bed))}
		}
		return
	}
	m.path = strings.TrimSpace(m.dialog.Value())
	m.message = &tui.Message{Kind: tui.MessageResult, Text: m.text.Lookup(textSuggestDone, tui.Escape(m.path))}
}

// pickOpener is what a tab of the Picker area without a block shows: its text, the action that opens its
// part and the value it holds now.
type pickOpener struct {
	note, label, value tui.TextID
	action             tui.ActionID
	current            func(m *model) string
}

func pickOpeners() map[tui.TabID]pickOpener {
	return map[tui.TabID]pickOpener{
		"choose":  {textNotePickChoose, textKeyPick, textPickBed, actPick, func(m *model) string { return m.bed }},
		"suggest": {textNotePickSuggest, textKeySuggest, textSuggestPath, actSuggest, func(m *model) string { return m.path }},
	}
}

func drawPicker(m *model, tab tui.TabID, room tui.Rect) ([]string, []tui.Target) {
	if b := m.live(); b != nil {
		return b.draw(m, room)
	}
	o := pickOpeners()[tab]
	lines := m.paragraph(o.note, room.Width)
	label := tui.Escape(m.text.Lookup(o.label))
	target := tui.Target{Rect: tui.Rect{Y: len(lines) + 1, Width: len([]rune(label)) + 4, Height: 1}, Action: o.action}
	value := m.text.Lookup(textNone)
	if v := o.current(m); v != "" {
		value = tui.Escape(v)
	}
	return append(lines, "", m.styles.Button(tui.ButtonPrimary, label, false), "", m.text.Lookup(o.value, value)), []tui.Target{target}
}

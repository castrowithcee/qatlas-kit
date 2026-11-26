package main

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// Field IDs of the forms. A form hands the application stable IDs, never the labels shown.
const (
	fieldName    tui.FieldID = "name"
	fieldNote    tui.FieldID = "note"
	fieldPlace   tui.FieldID = "place"
	fieldExtras  tui.FieldID = "extras"
	fieldCode    tui.FieldID = "code"
	fieldPlanted tui.FieldID = "planted"
	fieldHarvest tui.FieldID = "harvest"
)

// formBlock is a Form with its buttons. The form checks nothing: the workbench validates the values when
// the save key or the save button asks and hands the problems back.
type formBlock struct {
	form  *tui.Form
	build func() (*tui.Form, error) // builds the form again for the reset button
	check func(m *model, v map[tui.FieldID]tui.FormValue) (problems []tui.FormProblem, saved string)
	note  tui.TextID
	// before runs at every drawing and after at every key, click and action, for a form whose values the
	// workbench keeps in step with its own state; see settingsBlocks.
	before, after func(m *model, b *formBlock)
	// cancel ends the path search the form last started, see takeLoad.
	cancel context.CancelFunc
}

// sampleToday is the invented day of the sample data of the log and the chat.
func sampleToday() time.Time { return time.Date(2031, 5, 6, 12, 0, 0, 0, time.Local) }

func formKeys() tui.FormKeys {
	return tui.FormKeys{Up: actUp, Down: actDown, Next: actZoneNext, Prev: actZonePrev, PageUp: actPageUp, PageDown: actPageDown,
		Left: actLeft, Right: actRight, First: actFirst, Last: actLast, Enter: actEnter, Toggle: actToggle,
		Save: tui.ActionSave, Back: actBack}
}

func formButtons() []tui.FormButton {
	return []tui.FormButton{{Action: tui.ActionSave, Label: textFormSave, Primary: true}, {Action: actReset, Label: textFormReset}}
}

func formBlocks(m *model) (map[tui.TabID]block, error) {
	fields := &formBlock{note: textNoteFormFields, check: checkBed, build: func() (*tui.Form, error) {
		return tui.NewForm(tui.FormSpec{Title: textFormBedTitle, Keys: formKeys(), Buttons: formButtons(), Fields: []tui.FormField{
			{ID: fieldName, Kind: tui.FieldText, Label: textFormName, Hint: textFormNameHint},
			{ID: fieldNote, Kind: tui.FieldText, Label: textFormNote},
			{ID: fieldPlace, Kind: tui.FieldChoice, Label: textFormPlace, Value: tui.FormValue{Choice: "east"}, Choices: []tui.Choice{
				{ID: "east", Label: textFormPlaceEast}, {ID: "south", Label: textFormPlaceSouth}, {ID: "north", Label: textFormPlaceNorth}}},
			{ID: fieldExtras, Kind: tui.FieldMultiple, Label: textFormExtras, Choices: []tui.Choice{
				{ID: "mulch", Label: textFormExtraMulch}, {ID: "net", Label: textFormExtraNet}, {ID: "frame", Label: textFormExtraFrame}}},
			{ID: fieldCode, Kind: tui.FieldMasked, Label: textFormCode, Hint: textFormCodeHint},
		}}, m.keys, m.text)
	}}
	dates := &formBlock{note: textNoteFormDates, check: checkDates, build: func() (*tui.Form, error) {
		today := m.now()
		min, max := today.AddDate(-1, 0, 0), today.AddDate(1, 0, 0)
		return tui.NewForm(tui.FormSpec{Title: textFormDatesTitle, Keys: formKeys(), Buttons: formButtons(), Fields: []tui.FormField{
			{ID: fieldPlanted, Kind: tui.FieldDate, Label: textFormPlanted, Hint: textFormPlantedHint,
				Date: tui.DateField{Now: m.now}}, // no bounds, so the calendar pages across years
			{ID: fieldHarvest, Kind: tui.FieldDate, Label: textFormHarvest, Hint: textFormHarvestHint,
				Date: tui.DateField{Time: true, Min: min, Max: max, Now: m.now}}, // bounded to a year either side of today
		}}, m.keys, m.text)
	}}
	paths := &formBlock{note: textNoteFormPaths, check: checkPaths, build: func() (*tui.Form, error) {
		return tui.NewForm(tui.FormSpec{Title: textFormPathsTitle, Keys: formKeys(), Buttons: formButtons(), Fields: []tui.FormField{
			{ID: fieldFolder, Kind: tui.FieldPath, Label: textFormFolder, Hint: textFormFolderHint,
				Path: tui.PathSpec{Kind: tui.PathDirs, Priority: pathPriority()}},
			{ID: fieldFile, Kind: tui.FieldPath, Label: textFormFile, Hint: textFormFileHint, Path: tui.PathSpec{Kind: tui.PathFiles}},
		}}, m.keys, m.text)
	}}
	for _, b := range []*formBlock{fields, dates, paths} {
		var err error
		if b.form, err = b.build(); err != nil {
			return nil, err
		}
	}
	return map[tui.TabID]block{"fields": fields, "dates": dates, "paths": paths}, nil
}

func problem(m *model, field tui.FieldID, cause, next tui.TextID) tui.FormProblem {
	return tui.FormProblem{Field: field, Cause: m.text.Lookup(cause), NextStep: m.text.Lookup(next)}
}

// checkBed needs a name; it returns the problems, or the text of the success message.
func checkBed(m *model, v map[tui.FieldID]tui.FormValue) ([]tui.FormProblem, string) {
	name := strings.TrimSpace(v[fieldName].Text)
	if name == "" {
		return []tui.FormProblem{problem(m, fieldName, textFormNeedName, textFormNeedNameNext)}, ""
	}
	return nil, m.text.Lookup(textFormBedSaved, tui.Escape(name), len(v[fieldExtras].Selected))
}

// checkDates needs the planting day, and a harvest, where set, after it.
func checkDates(m *model, v map[tui.FieldID]tui.FormValue) ([]tui.FormProblem, string) {
	planted, harvest := v[fieldPlanted].Time, v[fieldHarvest].Time
	switch {
	case planted.IsZero():
		return []tui.FormProblem{problem(m, fieldPlanted, textFormNeedDay, textFormNeedDayNext)}, ""
	case !harvest.IsZero() && harvest.Before(planted):
		return []tui.FormProblem{problem(m, fieldHarvest, textFormHarvestEarly, textFormHarvestEarlyNext)}, ""
	}
	return nil, m.text.Lookup(textFormDatesSaved, planted.Format("2006-01-02"))
}

// done runs what the form reported: the check on the save action, a fresh form on reset.
func (b *formBlock) done(m *model, act tui.ActionID) {
	if b.after != nil {
		defer b.after(m, b)
	}
	if b.check == nil {
		return
	}
	switch act {
	case tui.ActionSave:
		problems, saved := b.check(m, b.form.Values())
		b.form.SetProblems(problems)
		if len(problems) == 0 {
			m.message = &tui.Message{Kind: tui.MessageResult, Text: saved}
		} else {
			m.message = &tui.Message{Kind: tui.MessageWarning, Text: m.text.Lookup(textFormRefused)}
		}
	case actReset:
		if form, err := b.build(); err == nil {
			b.form = form
			m.message = &tui.Message{Kind: tui.MessageStatus, Text: m.text.Lookup(textFormCleared)}
		}
	}
}

func (b *formBlock) zones() []tui.FocusZone             { return []tui.FocusZone{tui.ZoneForm} }
func (b *formBlock) focus(tui.FocusZone)                {}
func (b *formBlock) input(tui.FocusZone) tui.InputFocus { return b.form.Input() }
func (b *formBlock) wheel(res tui.MouseResult)          { b.form.Wheel(res.WheelSteps) }
func (b *formBlock) paste(text string)                  { b.form.Paste(text) }
func (b *formBlock) hints(tui.FocusZone) []tui.Hint     { return b.form.Hints() }

func (b *formBlock) key(m *model, msg tea.KeyPressMsg) bool {
	act, handled := b.form.Update(msg)
	b.done(m, act)
	return handled
}

func (b *formBlock) click(m *model, res tui.MouseResult) {
	act, _ := b.form.Click(res)
	b.done(m, act)
}

// act takes the save and reset keys from anywhere, as a footer hint or a key left over names them, and the
// moves only while the focus lies in the form.
func (b *formBlock) act(m *model, action tui.ActionID) bool {
	if action != tui.ActionSave && action != actReset && m.zone != tui.ZoneForm {
		return false
	}
	act, handled := b.form.Act(action)
	b.done(m, act)
	return handled
}

func (b *formBlock) draw(m *model, room tui.Rect) ([]string, []tui.Target) {
	if b.before != nil {
		b.before(m, b)
	}
	var lines []string
	if room.Height >= 16 {
		lines = append(m.paragraph(b.note, room.Width), "")
	}
	view, ts := b.form.View(m.styles, room.Width, max(room.Height-len(lines), 0))
	return append(lines, strings.Split(view, "\n")...), tui.OffsetTargets(ts, 0, len(lines))
}

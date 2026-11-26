package main

import (
	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// logBlock is a LogView of invented lines over time with a period filter. The view loads nothing: the block
// keeps the lines and supplies those of the chosen period again when the view reports a change.
type logBlock struct {
	view  *tui.LogView
	lines []tui.LogLine
}

func newLogBlock(m *model) (*logBlock, error) {
	view, err := tui.NewLogView(tui.LogViewSpec{
		Keys: tui.LogViewKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst, Last: actLast,
			Search: actSearch, Enter: actEnter, Back: actBack, Follow: tui.ActionLogFollow, NextMatch: tui.ActionLogNextMatch,
			PrevMatch: tui.ActionLogPrevMatch, Wrap: tui.ActionLogWrap, Timestamps: tui.ActionLogTimestamps,
			Filter: tui.ActionLogFilter, FilterForm: formKeys()},
		Timestamps: true, Now: sampleToday}, m.keys, m.text)
	if err != nil {
		return nil, err
	}
	b := &logBlock{view: view, lines: sampleLog()}
	view.Append(b.lines...)
	return b, nil
}

// reload supplies the lines of the period the user chose, if the view reports one.
func (b *logBlock) reload(m *model) {
	if _, changed := b.view.TakeFilterChange(); changed {
		b.view.Clear()
		b.view.Append(b.lines...) // the view keeps the lines inside its period only
		m.message = &tui.Message{Kind: tui.MessageResult, Text: m.text.Lookup(textLogReloaded, b.view.Len(), len(b.lines))}
	}
}

// sync opens the choice of the period as a modal where the view opened it.
func (b *logBlock) sync(m *model) {
	if b.view.FilterOpen() && m.modal == nil {
		m.show(logModal{b, m}, nil, kindLogFilter, b.view.Input())
	}
}

func (b *logBlock) zones() []tui.FocusZone             { return []tui.FocusZone{tui.ZoneRows} }
func (b *logBlock) focus(tui.FocusZone)                {}
func (b *logBlock) input(tui.FocusZone) tui.InputFocus { return b.view.Input() }
func (b *logBlock) wheel(res tui.MouseResult)          { b.view.Wheel(res.WheelSteps) }
func (b *logBlock) paste(text string)                  { b.view.Paste(text) }
func (b *logBlock) hints(tui.FocusZone) []tui.Hint     { return b.view.Hints() }

func (b *logBlock) key(m *model, msg tea.KeyPressMsg) bool {
	handled := b.view.Update(msg)
	b.sync(m)
	return handled
}

func (b *logBlock) click(m *model, res tui.MouseResult) {
	b.view.Click(res)
	b.sync(m)
}

func (b *logBlock) act(m *model, action tui.ActionID) bool {
	switch {
	case action == actSearch:
		m.zone = tui.ZoneRows
	case m.zone != tui.ZoneRows:
		return false
	}
	handled := b.view.Act(action)
	b.sync(m)
	return handled
}

func (b *logBlock) draw(m *model, room tui.Rect) ([]string, []tui.Target) {
	lines := m.note(textNoteViewLog, room)
	view, ts := b.view.View(m.styles, room.Width, max(room.Height-len(lines), 0))
	return underNote(lines, view, ts)
}

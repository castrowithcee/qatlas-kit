package main

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/highlight"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// textBlock is a TextView of the sample configuration, highlighted by the highlight package. The secret of
// the configuration is a masked section, which the workbench uncovers after a question.
type textBlock struct {
	view *tui.TextView
}

func newTextBlock(m *model) (*textBlock, error) {
	view, err := tui.NewTextView(tui.TextViewSpec{Keys: tui.TextViewKeys{
		Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst, Last: actLast,
		Search: actSearch, Enter: actEnter, Back: actBack, NextMatch: tui.ActionLogNextMatch, PrevMatch: tui.ActionLogPrevMatch,
		Wrap: tui.ActionLogWrap, Numbers: tui.ActionTextNumbers, Reveal: tui.ActionTextReveal}}, m.keys, m.text)
	if err != nil {
		return nil, err
	}
	view.SetText(sampleConfig, configSpans())
	return &textBlock{view: view}, nil
}

// configSpans are the sections of the sample configuration: the syntax roles of the highlight package and the
// secret as a masked section, which the package leaves to the application.
func configSpans() [][]tui.TextSpan {
	spans := highlight.Highlight("sample.yaml", sampleConfig)
	for i, line := range strings.Split(sampleConfig, "\n") {
		at := strings.Index(line, sampleSecret)
		if at < 0 {
			continue
		}
		for len(spans) <= i {
			spans = append(spans, nil)
		}
		spans[i] = append(spans[i], tui.TextSpan{Start: at, End: at + len(sampleSecret), Role: tui.TextRoleString, Masked: true})
		slices.SortStableFunc(spans[i], func(a, b tui.TextSpan) int { return a.Start - b.Start })
	}
	return spans
}

// asked opens the question after a request to uncover; a hidden section needs none.
func (b *textBlock) asked(m *model) {
	if b.view.TakeRevealRequest() {
		m.openReveal()
	}
}

func (b *textBlock) zones() []tui.FocusZone             { return []tui.FocusZone{tui.ZoneRows} }
func (b *textBlock) focus(tui.FocusZone)                {}
func (b *textBlock) input(tui.FocusZone) tui.InputFocus { return b.view.Input() }
func (b *textBlock) wheel(res tui.MouseResult)          { b.view.Wheel(res.WheelSteps) }
func (b *textBlock) paste(text string)                  { b.view.Paste(text) }
func (b *textBlock) hints(tui.FocusZone) []tui.Hint     { return b.view.Hints() }

func (b *textBlock) key(m *model, msg tea.KeyPressMsg) bool {
	handled := b.view.Update(msg)
	b.asked(m)
	return handled
}

func (b *textBlock) click(m *model, res tui.MouseResult) {
	b.view.Click(res)
	b.asked(m)
}

func (b *textBlock) act(m *model, action tui.ActionID) bool {
	switch {
	case action == actSearch:
		m.zone = tui.ZoneRows
	case m.zone != tui.ZoneRows:
		return false
	}
	handled := b.view.Act(action)
	b.asked(m)
	return handled
}

func (b *textBlock) draw(m *model, room tui.Rect) ([]string, []tui.Target) {
	lines := m.note(textNoteViewText, room)
	view, ts := b.view.View(m.styles, room.Width, max(room.Height-len(lines), 0))
	return underNote(lines, view, ts)
}

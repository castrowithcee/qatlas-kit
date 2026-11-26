package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
)

func viewBlocks(m *model) (map[tui.TabID]block, error) {
	text, err := newTextBlock(m)
	if err != nil {
		return nil, err
	}
	log, err := newLogBlock(m)
	if err != nil {
		return nil, err
	}
	chat, err := newChatBlock(m)
	if err != nil {
		return nil, err
	}
	return map[tui.TabID]block{"text": text, "log": log, "chat": chat}, nil
}

// note draws the explanation above a block where the room allows it, and returns the lines.
func (m *model) note(id tui.TextID, room tui.Rect) []string {
	if room.Height < 16 {
		return nil
	}
	return append(m.paragraph(id, room.Width), "")
}

// underNote adds the view drawn under the lines already there.
func underNote(lines []string, view string, ts []tui.Target) ([]string, []tui.Target) {
	return append(lines, strings.Split(view, "\n")...), tui.OffsetTargets(ts, 0, len(lines))
}

// reveal asks before it uncovers a masked section. The kit only asks; the workbench decides and chooses to
// ask, since what is uncovered stays on the screen and in what a bystander sees, and hiding needs no question.
func (m *model) openReveal() {
	d, err := tui.NewDialog(tui.DialogSpec{
		Title:        textRevealTitle,
		Question:     m.text.Lookup(textRevealQuestion),
		Consequence:  m.text.Lookup(textRevealConsequence),
		Answers:      []tui.Answer{{Action: actYes, Label: tui.TextKeyYes}, {Action: actNo, Label: tui.TextKeyNo}},
		Default:      actNo,
		Keys:         dialogKeys(false),
		CloseOutside: actNo,
	}, m.keys, m.text)
	if err == nil {
		m.show(d, nil, kindReveal, d.Input())
		return
	}
	m.show(nil, err, 0, 0)
}

// revealed takes the answer of the question: only yes uncovers.
func (m *model) revealed(act tui.ActionID) {
	defer m.closeModal()
	tb, ok := m.live().(*textBlock)
	if !ok || act != actYes {
		m.cancelled()
		return
	}
	tb.view.SetRevealed(true)
	m.message = &tui.Message{Kind: tui.MessageStatus, Text: m.text.Lookup(textRevealDone)}
}

// logModal stands for the open choice of the period of a log view as the modal of the workbench: the view
// takes every key, click and notch of the wheel while it is open, and it answers back once it is closed.
type logModal struct {
	b *logBlock
	m *model
}

func (p logModal) done() (tui.ActionID, bool) {
	p.m.focus = p.b.view.Input()
	if p.b.view.FilterOpen() {
		return "", true
	}
	return actBack, true
}

func (p logModal) Update(msg tea.KeyPressMsg) (tui.ActionID, bool) {
	p.b.view.Update(msg)
	return p.done()
}

func (p logModal) Click(r tui.MouseResult) (tui.ActionID, bool) {
	p.b.view.Click(r)
	return p.done()
}

func (p logModal) Wheel(steps int) { p.b.view.Wheel(steps) }
func (p logModal) Overlay(s tui.Styles, screen string, w, h int, room tui.Rect) (string, []tui.Target, bool) {
	return p.b.view.ModalOverlay(s, screen, w, h, room)
}
func (p logModal) View(s tui.Styles, w, h int) (string, []tui.Target) {
	return p.b.view.ModalView(s, w, h)
}
func (p logModal) Hints() []tui.Hint { return p.b.view.Hints() }

// filtered runs once the period is closed: the lines of the chosen period are supplied again.
func (m *model) filtered() {
	defer m.closeModal()
	if lb, ok := m.live().(*logBlock); ok {
		lb.reload(m)
	}
}

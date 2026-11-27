package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// modalKind says which part is open; the zero value is none.
type modalKind uint8

const (
	kindQuestion modalKind = iota + 1
	kindInput
	kindNotice
	kindHelp
	kindExplain
	kindPick
	kindSuggest
	kindReveal
	kindLogFilter
)

// explanationURL is an invented address, shown in full and linked where the terminal takes escapes.
const explanationURL = "https://example.org/garden-journal"

// journalName is the name the sample table shows; an answered rename replaces it.
func (m *model) journalName() string {
	if m.journal != "" {
		return m.journal
	}
	return m.text.Lookup(textSampleTitle)
}

func dialogKeys(ends bool) tui.DialogKeys {
	k := tui.DialogKeys{Enter: actEnter, Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown}
	if ends {
		k.First, k.Last = actFirst, actLast
	}
	return k
}

// show opens part, or reports why it cannot be drawn.
func (m *model) show(part tui.Modal, err error, kind modalKind, focus tui.InputFocus) {
	if err != nil {
		m.message = &tui.Message{Kind: tui.MessageError, Text: err.Error()}
		return
	}
	m.modal, m.kind, m.focus = part, kind, focus
}

func (m *model) openQuestion() {
	d, err := tui.NewDialog(tui.DialogSpec{
		Title:       textAskTitle,
		Question:    m.text.Lookup(textAskQuestion),
		Consequence: m.text.Lookup(textAskConsequence),
		Details:     []tui.DialogLine{{Text: m.text.Lookup(textAskDetail, m.count)}},
		Answers:     []tui.Answer{{Action: actYes, Label: tui.TextKeyYes}, {Action: actNo, Label: tui.TextKeyNo}},
		Default:     actNo,
		Danger:      actYes,
		Keys:        dialogKeys(true),
	}, m.keys, m.text)
	if err == nil {
		m.dialog = d
		m.show(d, nil, kindQuestion, d.Input())
		return
	}
	m.show(nil, err, 0, 0)
}

func (m *model) openInput() {
	d, err := tui.NewDialog(tui.DialogSpec{
		Title:    textRenameTitle,
		Question: m.text.Lookup(textRenameQuestion),
		Input:    &tui.DialogInput{Value: m.journalName()},
		Answers:  []tui.Answer{{Action: actEnter, Label: textOK}, {Action: actBack, Label: tui.TextKeyBack}},
		Default:  actEnter,
		Keys:     dialogKeys(false),
		// A click beside the input answers back, as esc does.
		CloseOutside: actBack,
	}, m.keys, m.text)
	if err == nil {
		m.dialog = d
		m.show(d, nil, kindInput, d.Input())
		return
	}
	m.show(nil, err, 0, 0)
}

func (m *model) openNotice() {
	d, err := tui.NewDialog(tui.DialogSpec{
		Title:    textNoticeTitle,
		Question: m.text.Lookup(textNoticeLineOne),
		Notice:   true,
		Details:  []tui.DialogLine{{Text: m.text.Lookup(textNoticeLineTwo)}},
		Answers:  []tui.Answer{{Action: actEnter, Label: textOK}},
		Default:  actEnter,
		Keys:     dialogKeys(true),
		// A click beside the notice closes it. The question of the Dialogs tab sets none: its answer destroys.
		CloseOutside: actEnter,
	}, m.keys, m.text)
	if err == nil {
		m.dialog = d
		m.show(d, nil, kindNotice, d.Input())
		return
	}
	m.show(nil, err, 0, 0)
}

func (m *model) openHelp() {
	h, err := tui.NewHelp(tui.HelpSpec{
		Pages: helpPages(), Lang: uiprefs.Language(m.prefs.Language),
		Screen: m.footerHints(), Global: []tui.ActionID{actQuit, actHelp, actCommands, actInfo},
		Keys: tui.HelpKeys{Left: actLeft, Right: actRight, Next: actZoneNext, Prev: actZonePrev, Up: actUp, Down: actDown,
			PageUp: actPageUp, PageDown: actPageDown, First: actFirst, Last: actLast, Back: actBack, Help: actHelp,
			Outline: actOutline, Enter: actEnter, Search: actSearch},
		Note: textHelpNote, CloseOutside: true,
	}, m.keys, m.text)
	if err == nil {
		// The tab is the most specific screen, then its area.
		h.OpenScreen(string(m.area)+"/"+string(m.tab()), string(m.area))
		m.show(h, nil, kindHelp, tui.InputNavigation)
		return
	}
	m.show(nil, err, 0, 0)
}

func (m *model) openExplanation() {
	e, err := tui.NewExplanation(tui.ExplanationSpec{
		Item: tui.ExplanationItem{
			Title: m.text.Lookup(textExplainTitle), Body: m.text.Lookup(textExplainBody), URL: explanationURL,
			Term: helptopics.Term{ID: "raised-bed", Title: m.text.Lookup(textTermTitle), Text: m.text.Lookup(textTermText)},
		},
		Keys: tui.ExplanationKeys{Back: actBack, Info: actInfo, Copy: actCopy, Up: actUp, Down: actDown,
			PageUp: actPageUp, PageDown: actPageDown, First: actFirst, Last: actLast},
		CloseOutside: true,
	}, m.keys, m.text)
	if err == nil {
		m.explanation = e
		m.show(e, nil, kindExplain, tui.InputNavigation)
		return
	}
	m.show(nil, err, 0, 0)
}

func (m *model) closeModal() {
	m.modal, m.kind, m.dialog, m.explanation, m.picker = nil, 0, nil, nil, nil
}

func (m *model) cancelled() {
	m.message = &tui.Message{Kind: tui.MessageStatus, Text: m.text.Lookup(tui.TextCancelled)}
}

// modalKey gives a key to the open part. Keys it does not handle never reach the screen behind it; back
// closes a part that has no answer of its own for it.
func (m *model) modalKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	act, ok := m.modal.Update(msg)
	m.takeHelpFocus()
	if ok {
		return m.finish(act)
	}
	if m.keys.Is(msg, actBack, m.focus) {
		m.closeModal()
		m.cancelled()
	}
	return nil
}

// takeHelpFocus follows the input focus of the help, which takes text while its search is open.
func (m *model) takeHelpFocus() {
	if h, ok := m.modal.(*tui.Help); ok {
		m.focus = h.Input()
	}
}

// finish runs what the open part answered, for a key and for a click alike.
func (m *model) finish(act tui.ActionID) tea.Cmd {
	if act == "" {
		return nil
	}
	switch m.kind {
	case kindHelp, kindExplain:
		switch act {
		case actBack, actHelp, actInfo:
			m.closeModal()
		case actCopy:
			m.message = &tui.Message{Kind: tui.MessageStatus, Text: m.text.Lookup(tui.TextCopyHandedOver)}
			return tea.SetClipboard(m.explanation.Text())
		default:
			// An action of the key list of the help runs as its key does on the screen.
			m.closeModal()
			return m.run(act, "")
		}
	case kindQuestion:
		if act == actYes {
			m.count = 0
			m.message = &tui.Message{Kind: tui.MessageResult, Text: m.text.Lookup(textAskDone)}
		} else {
			m.cancelled()
		}
		m.closeModal()
	case kindInput:
		if act != actEnter {
			m.cancelled()
			m.closeModal()
			break
		}
		name := strings.TrimSpace(m.dialog.Value())
		if name == "" {
			m.dialog.SetMessage(&tui.Message{Kind: tui.MessageError, Text: m.text.Lookup(textRenameEmpty)})
			break
		}
		m.journal = name
		m.message = &tui.Message{Kind: tui.MessageResult, Text: m.text.Lookup(textRenameDone, name)}
		m.closeModal()
	case kindNotice:
		m.closeModal()
	case kindPick, kindSuggest:
		if m.kind == kindSuggest && act == actEnter && strings.TrimSpace(m.dialog.Value()) == "" {
			m.dialog.SetMessage(&tui.Message{Kind: tui.MessageError, Text: m.text.Lookup(textSuggestEmpty)})
			break
		}
		m.picked(act)
	case kindReveal:
		m.revealed(act)
	case kindLogFilter:
		m.filtered()
	}
	return nil
}

// modalClick gives a click or a notch of the wheel to the open part, which holds the only targets.
func (m *model) modalClick(msg tea.MouseMsg) tea.Cmd {
	res := m.mouse.Handle(msg)
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		// A dialog scrolls only under a notch over its details; help and explanation scroll under any notch.
		if m.kind == kindHelp || m.kind == kindExplain || m.kind == kindPick || m.kind == kindLogFilter || res.Zone == tui.ZoneDetail {
			m.modal.Wheel(res.WheelSteps)
		}
		return nil
	}
	act, _ := m.modal.Click(res)
	m.takeHelpFocus()
	return m.finish(act)
}

// commands are what the command line runs: every tab, and the actions that have a name of their own. A
// command names the action a key triggers, so the line is no second way to operate.
func commands(groups []group, text tui.Translator) []tui.Command {
	var cmds []tui.Command
	for _, g := range groups {
		cmds = append(cmds, tui.Command{Name: "go " + string(g.area.ID), Description: textCmdOpen,
			DescriptionArgs: []any{text.Lookup(g.area.Title)}, Area: g.area.ID, Available: tui.Always})
	}
	for _, c := range []struct {
		name   string
		action tui.ActionID
		text   tui.TextID
	}{
		{"theme", actTheme, textKeyTheme}, {"theme-back", actThemeBack, textKeyThemeBack}, {"language", actLanguage, textKeyLanguage}, {"icons", actIcons, textKeyIcons},
		{"mouse", actMouse, textKeyMouse}, {"navigation", actNavigate, textKeyNavigation}, {"ask", actAsk, textKeyAsk}, {"rename", actRename, tui.TextKeyRename},
		{"notice", actNotice, textKeyNotice}, {"pick", actPick, textKeyPick}, {"suggest", actSuggest, textKeySuggest}, {"help", actHelp, tui.TextKeyHelp}, {"explain", actInfo, tui.TextKeyInfo},
		{"quit", actQuit, tui.TextKeyQuit},
	} {
		cmds = append(cmds, tui.Command{Name: c.name, Description: c.text, Action: c.action, Available: tui.Always})
	}
	return cmds
}

func (m *model) openCommandLine() {
	m.cmdline.Reset()
	m.cmdline.SetContext(tui.Context{Area: m.area, Tab: m.tab(), Zone: m.zone, Focus: tui.InputNavigation})
	m.cmdOpen = true
}

// commandKey gives a key to the open command line.
func (m *model) commandKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	cmd, done := m.cmdline.Update(msg)
	return m.commandDone(cmd, done)
}

// commandClick gives a click to the open command line, which runs a match, the enter key or closes on the
// back key itself.
func (m *model) commandClick(msg tea.MouseMsg) tea.Cmd {
	if _, ok := msg.(tea.MouseClickMsg); !ok {
		return nil
	}
	return m.commandDone(m.cmdline.Click(m.mouse.Handle(msg)))
}

// commandDone closes the line and runs the command it chose, as its key or its tab click would.
func (m *model) commandDone(cmd tui.Command, done bool) tea.Cmd {
	if !done {
		return nil
	}
	m.cmdOpen = false
	switch {
	case cmd.Area != "":
		m.openArea(cmd.Area)
		if cmd.Tab != "" {
			m.tabs[cmd.Area] = cmd.Tab
		}
	case cmd.Action != "":
		return m.run(cmd.Action, "")
	}
	return nil
}

// pathKey gives a key to the open overlay of the path; keys it does not handle never reach the screen behind.
func (m *model) pathKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}
	choice, _ := m.pathnav.Update(msg)
	m.choose(choice)
	return nil
}

// pathClick gives a click or a notch of the wheel to the open overlay, which holds the only targets.
func (m *model) pathClick(msg tea.MouseMsg) tea.Cmd {
	res := m.mouse.Handle(msg)
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		m.pathnav.Wheel(res.WheelSteps)
		return nil
	}
	choice, _ := m.pathnav.Click(res)
	m.choose(choice)
	return nil
}

// choose runs a choice of the overlay as the click on that tab; the zero value is no choice.
func (m *model) choose(c tui.PathChoice) {
	if c.Area == "" {
		return
	}
	m.openArea(c.Area)
	if c.Tab != "" {
		m.tabs[c.Area] = c.Tab
	}
	m.zone, m.pathArea = tui.ZonePath, false
	if m.choiceForm() {
		m.zone = tui.ZoneTabs
		if c.Tab != "" {
			m.zone = tui.ZoneSubtabs
		}
	}
}

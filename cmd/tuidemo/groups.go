package main

import (
	"strings"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// group is one tab of the workbench and the group of building blocks it shows. groups lists them in the
// order of the tab line; a new group is one more entry, the frame and the key table follow.
type group struct {
	area tui.Area
	// icons are the glyphs of the tab for the unicode and the nerdfont icon set.
	icons [2]string
	// draw returns the lines and the click targets of the open tab, relative to the workspace.
	draw func(m *model, tab tui.TabID, room tui.Rect) ([]string, []tui.Target)
	// actions are the buttons of a tab in the order of the focus, as the actions their clicks run.
	actions func(area tui.Area, tab tui.TabID) []tui.ActionID
	// hints are the footer hints of a tab, behind the common ones: each tab shows the keys of its own
	// actions only.
	hints map[tui.TabID][]tui.Hint
	// bar is the action bar of a tab: its most important actions as buttons above the work area. A tab that
	// shows a block takes the bar of the block instead, see barer.
	bar map[tui.TabID][]tui.BarAction
	// blocks returns the live block of each tab that shows one, see block; nil for a group without any. A
	// tab shows either a block or buttons.
	blocks func(m *model) (map[tui.TabID]block, error)
}

func groups() []group {
	return []group{
		{
			area: tui.Area{ID: "overview", Key: "o", Title: textAreaOverview,
				Tabs: []tui.Tab{{ID: "about", Title: textTabAbout}, {ID: "sample", Title: textTabSample}}},
			icons: [2]string{"⌂", "\uf015"},
			draw:  drawOverview,
		},
		{
			area: tui.Area{ID: "frame", Key: "f", Title: textAreaFrame,
				Tabs: []tui.Tab{{ID: "tabs", Title: textTabTabs}, {ID: "mouse", Title: textTabMouse}}},
			icons: [2]string{"▣", "\uf2d0"},
			draw:  drawFrame,
			actions: func(_ tui.Area, tab tui.TabID) []tui.ActionID {
				if tab == "mouse" {
					return []tui.ActionID{actCount}
				}
				return nil
			},
			hints: map[tui.TabID][]tui.Hint{"mouse": {{Action: actCount, Label: textKeyCount, Priority: 1}}},
		},
		{
			area: tui.Area{ID: "settings", Key: "s", Title: textAreaSettings,
				Tabs: []tui.Tab{{ID: "look", Title: textTabLook, Key: "a"}, {ID: "input", Title: textTabInput, Key: "i"}}},
			icons:  [2]string{"✱", "\uf013"},
			draw:   drawLive,
			blocks: settingsBlocks,
		},
		{
			area: tui.Area{ID: "dialogs", Key: "d", Title: textAreaDialogs,
				Tabs: []tui.Tab{{ID: "question", Title: textTabQuestion}, {ID: "input", Title: textTabInput}, {ID: "notice", Title: textTabNotice}}},
			icons:   [2]string{"▤", "\uf27a"},
			draw:    drawDialogs,
			actions: openerAction,
			hints: map[tui.TabID][]tui.Hint{
				"question": {{Action: actAsk, Label: textKeyAsk, Priority: 1}},
				"input":    {{Action: actRename, Label: tui.TextKeyRename, Priority: 1}},
				"notice":   {{Action: actNotice, Label: textKeyNotice, Priority: 1}},
			},
			bar: map[tui.TabID][]tui.BarAction{
				"question": {{Action: actAsk, Label: textKeyAsk, Kind: tui.ButtonPrimary}},
				"input":    {{Action: actRename, Label: tui.TextKeyRename, Kind: tui.ButtonPrimary}},
				"notice":   {{Action: actNotice, Label: textKeyNotice, Kind: tui.ButtonPrimary}},
			},
		},
		{
			area: tui.Area{ID: "help", Key: "h", Title: textAreaHelp,
				Tabs: []tui.Tab{{ID: "help", Title: textTabHelp}, {ID: "explain", Title: textTabExplain}, {ID: "commands", Title: textTabCommands}}},
			icons:   [2]string{"◈", "\uf059"},
			draw:    drawHelp,
			actions: openerAction,
		},
		{
			area: tui.Area{ID: "lists", Key: "l", Title: textAreaLists,
				Tabs: []tui.Tab{{ID: "filter", Title: textTabListFilter}, {ID: "groups", Title: textTabListGroups}, {ID: "ticks", Title: textTabListTicks}}},
			icons:  [2]string{"☰", "\uf0ca"},
			draw:   drawLive,
			blocks: listBlocks,
		},
		{
			area: tui.Area{ID: "forms", Key: "r", Title: textAreaForms,
				Tabs: []tui.Tab{{ID: "fields", Title: textTabFormFields}, {ID: "dates", Title: textTabFormDates}}},
			icons:  [2]string{"✎", "\uf044"},
			draw:   drawLive,
			blocks: formBlocks,
		},
		{
			area: tui.Area{ID: "pickers", Key: "p", Title: textAreaPickers,
				Tabs: []tui.Tab{{ID: "choose", Title: textTabPickChoose}, {ID: "files", Title: textTabPickFiles}, {ID: "suggest", Title: textTabPickSuggest}}},
			icons:   [2]string{"◉", "\uf07b"},
			draw:    drawPicker,
			actions: pickAction,
			hints: map[tui.TabID][]tui.Hint{
				"choose":  {{Action: actPick, Label: textKeyPick, Priority: 1}},
				"suggest": {{Action: actSuggest, Label: textKeySuggest, Priority: 1}},
			},
			blocks: pickBlocks,
		},
		{
			area: tui.Area{ID: "views", Key: "v", Title: textAreaViews,
				Tabs: []tui.Tab{{ID: "text", Title: textTabViewText}, {ID: "log", Title: textTabViewLog}, {ID: "chat", Title: textTabViewChat}}},
			icons:  [2]string{"≣", "\uf0e5"},
			draw:   drawLive,
			blocks: viewBlocks,
		},
	}
}

// paragraph wraps the text id to width cells.
func (m *model) paragraph(id tui.TextID, width int) []string {
	return tui.Wrap(m.text.Lookup(id), width)
}

func (m *model) title(id tui.TextID) string {
	return m.styles.Render(tui.StyleTitle, tui.Escape(m.text.Lookup(id)))
}

func drawOverview(m *model, tab tui.TabID, room tui.Rect) ([]string, []tui.Target) {
	if tab == "sample" {
		return m.drawSample(room), nil
	}
	lines := []string{m.title(textAboutHeading), ""}
	lines = append(lines, m.paragraph(textAboutBody, room.Width)...)
	lines = append(lines, "")
	lines = append(lines, m.paragraph(textAboutKeys, room.Width)...)
	return lines, nil
}

// drawSample draws the invented rows as a table in a panel.
func (m *model) drawSample(room tui.Rect) []string {
	head := []string{m.text.Lookup(textSampleName), m.text.Lookup(textSampleBed), m.text.Lookup(textSampleDate)}
	rows := sampleRows()
	widths := make([]int, len(head))
	for i, h := range head {
		widths[i] = len([]rune(h))
	}
	for _, r := range rows {
		for i, v := range []string{r.Name, r.Bed, r.Watered} {
			widths[i] = max(widths[i], len([]rune(v)))
		}
	}
	row := func(cells ...string) string {
		out := make([]string, len(cells))
		for i, c := range cells {
			out[i] = tui.Pad(tui.Escape(c), widths[i])
		}
		return strings.Join(out, "  ")
	}
	lines := []string{m.styles.Render(tui.StyleHint, strings.ToUpper(row(head...)))}
	for _, r := range rows {
		lines = append(lines, row(r.Name, r.Bed, r.Watered))
	}
	return m.styles.Panel(m.text.Lookup(textSampleTitle), m.text.Lookup(textSampleNote), lines, room.Width, min(len(lines)+2, room.Height), false)
}

func drawFrame(m *model, tab tui.TabID, room tui.Rect) ([]string, []tui.Target) {
	if tab == "mouse" {
		return m.drawMouse(room)
	}
	lines := []string{m.title(textTabsHeading), ""}
	lines = append(lines, m.paragraph(textTabsBody, room.Width)...)
	return append(lines, "", m.text.Lookup(textTabsFocus, m.zone)), nil
}

func (m *model) drawMouse(room tui.Rect) ([]string, []tui.Target) {
	lines := []string{m.title(textMouseHeading), ""}
	lines = append(lines, m.paragraph(textMouseBody, room.Width)...)
	last := m.text.Lookup(textMouseNone)
	if m.hit != "" {
		last = tui.Escape(m.hit)
	}
	lines = append(lines, "", m.text.Lookup(textMouseLast, m.clickX+1, m.clickY+1, last),
		m.text.Lookup(textMouseWheel, m.wheel), m.text.Lookup(textMouseCount, m.count), "")
	label := m.text.Lookup(textMouseButton)
	button := m.styles.Button(tui.ButtonPrimary, label, m.focused(0))
	target := tui.Target{Rect: tui.Rect{X: 0, Y: len(lines), Width: len([]rune(label)) + 4, Height: 1}, Action: actCount}
	return append(lines, button), []tui.Target{target}
}

// openers are what a tab of the dialogs and help groups shows: its text and the action that opens the part.
type opener struct {
	body   tui.TextID
	action tui.ActionID
	label  tui.TextID
}

func openers() map[tui.TabID]opener {
	return map[tui.TabID]opener{
		"question": {textBodyQuestion, actAsk, textKeyAsk},
		"input":    {textBodyInput, actRename, tui.TextKeyRename},
		"notice":   {textBodyNotice, actNotice, textKeyNotice},
		"help":     {textBodyHelp, actHelp, tui.TextKeyHelp},
		"explain":  {textBodyExplain, actInfo, tui.TextKeyInfo},
		"commands": {textBodyCommands, actCommands, tui.TextKeyCommands},
	}
}

func openerAction(_ tui.Area, tab tui.TabID) []tui.ActionID {
	return []tui.ActionID{openers()[tab].action}
}

func drawDialogs(m *model, tab tui.TabID, room tui.Rect) ([]string, []tui.Target) {
	return m.drawOpener(tab, room)
}

func drawHelp(m *model, tab tui.TabID, room tui.Rect) ([]string, []tui.Target) {
	return m.drawOpener(tab, room)
}

// drawOpener draws the text of a tab and the button that opens its part, a click target for the action of
// its key.
func (m *model) drawOpener(tab tui.TabID, room tui.Rect) ([]string, []tui.Target) {
	o := openers()[tab]
	lines := m.paragraph(o.body, room.Width)
	label := tui.Escape(m.text.Lookup(o.label))
	target := tui.Target{Rect: tui.Rect{Y: len(lines) + 1, Width: len([]rune(label)) + 4, Height: 1}, Action: o.action}
	return append(lines, "", m.styles.Button(tui.ButtonPrimary, label, m.focused(0))), []tui.Target{target}
}

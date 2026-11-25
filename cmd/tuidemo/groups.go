package main

import (
	"fmt"
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
	// hints are the footer hints of the group, behind the common ones.
	hints []tui.Hint
}

func groups() []group {
	return []group{
		{
			area: tui.Area{ID: "overview", Title: textAreaOverview,
				Tabs: []tui.Tab{{ID: "about", Title: textTabAbout}, {ID: "sample", Title: textTabSample}}},
			icons: [2]string{"⌂", "\uf015"},
			draw:  drawOverview,
		},
		{
			area: tui.Area{ID: "frame", Title: textAreaFrame,
				Tabs: []tui.Tab{{ID: "tabs", Title: textTabTabs}, {ID: "mouse", Title: textTabMouse}}},
			icons: [2]string{"▣", "\uf2d0"},
			draw:  drawFrame,
			actions: func(_ tui.Area, tab tui.TabID) []tui.ActionID {
				if tab == "mouse" {
					return []tui.ActionID{actCount}
				}
				return nil
			},
			hints: []tui.Hint{{Action: actCount, Label: textKeyCount, Priority: 1}},
		},
		{
			area: tui.Area{ID: "settings", Title: textAreaSettings,
				Tabs: []tui.Tab{{ID: "look", Title: textTabLook}, {ID: "input", Title: textTabInput}}},
			icons: [2]string{"✱", "\uf013"},
			draw:  drawSettings,
			actions: func(_ tui.Area, tab tui.TabID) []tui.ActionID {
				var out []tui.ActionID
				for _, s := range settingRows()[tab] {
					out = append(out, s.action)
				}
				return out
			},
		},
		{
			area: tui.Area{ID: "dialogs", Title: textAreaDialogs,
				Tabs: []tui.Tab{{ID: "question", Title: textTabQuestion}, {ID: "input", Title: textTabInput}, {ID: "notice", Title: textTabNotice}}},
			icons:   [2]string{"▤", "\uf27a"},
			draw:    drawDialogs,
			actions: openerAction,
			hints: []tui.Hint{{Action: actAsk, Label: textKeyAsk, Priority: 1}, {Action: actRename, Label: tui.TextKeyRename, Priority: 1},
				{Action: actNotice, Label: textKeyNotice, Priority: 1}},
		},
		{
			area: tui.Area{ID: "help", Title: textAreaHelp,
				Tabs: []tui.Tab{{ID: "help", Title: textTabHelp}, {ID: "explain", Title: textTabExplain}, {ID: "commands", Title: textTabCommands}}},
			icons:   [2]string{"◈", "\uf059"},
			draw:    drawHelp,
			actions: openerAction,
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

// setting is one row of the settings tabs: a switch, its text and the value it shows now.
type setting struct {
	action tui.ActionID
	label  tui.TextID
	value  func(m *model) string
}

func settingRows() map[tui.TabID][]setting {
	return map[tui.TabID][]setting{
		"look": {
			{actTheme, tui.TextSettingTheme, func(m *model) string { return m.prefs.Theme }},
			{actThemeBack, textSettingThemeBack, func(m *model) string { return m.themes.Step(m.prefs.Theme, -1) }},
			{actIcons, tui.TextSettingIcons, func(m *model) string { return m.prefs.Icons }},
		},
		"input": {
			{actLanguage, tui.TextSettingLanguage, func(m *model) string { return m.prefs.Language }},
			{actMouse, tui.TextSettingMouse, func(m *model) string { return m.onOff(m.prefs.MouseEnabled) }},
		},
	}
}

func (m *model) onOff(on bool) string {
	if on {
		return m.text.Lookup(tui.TextValueOn)
	}
	return m.text.Lookup(tui.TextValueOff)
}

func drawSettings(m *model, tab tui.TabID, room tui.Rect) ([]string, []tui.Target) {
	body := textSettingsLookBody
	if tab == "input" {
		body = textSettingsInputBody
	}
	lines := m.paragraph(body, room.Width)
	lines = append(lines, "")
	var targets []tui.Target
	for i, s := range settingRows()[tab] {
		key := tui.Escape(m.keys.Label(s.action))
		line := fmt.Sprintf("[%s] %s  %s", key, tui.Pad(tui.Escape(m.text.Lookup(s.label)), 12),
			m.styles.Button(tui.ButtonNormal, tui.Escape(s.value(m)), m.focused(i)))
		targets = append(targets, tui.Target{Rect: tui.Rect{Y: len(lines), Width: room.Width, Height: 1}, Action: s.action})
		lines = append(lines, line)
	}
	return lines, targets
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

package main

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// group is the group of the open area.
func (m *model) group() group {
	for _, g := range m.groups {
		if g.area.ID == m.area {
			return g
		}
	}
	return m.groups[0]
}

// tab is the open sub-tab of the open area; an area opens on its first.
func (m *model) tab() tui.TabID {
	g := m.group()
	if t, ok := m.tabs[g.area.ID]; ok {
		return t
	}
	return g.area.Tabs[0].ID
}

// zones are the focus zones the open area has: the sub-tab line only where it shows.
func (m *model) zones() []tui.FocusZone {
	if len(m.group().area.Tabs) > 1 {
		return []tui.FocusZone{tui.ZoneTabs, tui.ZoneSubtabs}
	}
	return []tui.FocusZone{tui.ZoneTabs}
}

func (m *model) openArea(id tui.AreaID) {
	m.area = id
	if !slices.Contains(m.zones(), m.zone) {
		m.zone = tui.ZoneTabs
	}
}

// step moves the open tab of the focused line by one, with wrap-around.
func (m *model) step(by int) {
	if m.zone == tui.ZoneSubtabs {
		m.tabs[m.area] = tui.StepTab(m.group().area, m.tab(), by)
		return
	}
	areas := make([]tui.Area, len(m.groups))
	for i, g := range m.groups {
		areas[i] = g.area
	}
	m.openArea(tui.StepArea(areas, m.area, by))
}

// icon is the glyph of the icon set in force; empty for none. The frame draws none in an ASCII terminal.
func (m *model) icon(unicode, nerdfont string) string {
	switch m.prefs.Icons {
	case uiprefs.IconsUnicode:
		return unicode
	case uiprefs.IconsNerdFont:
		return nerdfont
	}
	return ""
}

func (m *model) areas() []tui.Area {
	areas := make([]tui.Area, len(m.groups))
	for i, g := range m.groups {
		areas[i] = g.area
		areas[i].Icon = m.icon(g.icons[0], g.icons[1])
	}
	return areas
}

// footerHints are the key hints of the screen: the group's own behind the common ones, quit always.
func (m *model) footerHints() []tui.Hint {
	hints := []tui.Hint{
		{Action: actLeft, Label: textHintStep, Kind: tui.HintNavigation},
		{Action: actRight, Label: textHintStep, Kind: tui.HintNavigation},
		{Action: actZoneNext, Label: textHintZone, Kind: tui.HintNavigation},
		{Action: actTheme, Label: textKeyTheme, Priority: 2},
		{Action: actThemeBack, Label: textKeyThemeBack, Priority: 3},
		{Action: actLanguage, Label: textKeyLanguage, Priority: 3},
		{Action: actIcons, Label: textKeyIcons, Priority: 4},
		{Action: actMouse, Label: textKeyMouse, Priority: 5},
	}
	hints = append(hints, m.group().hints...)
	return append(hints, tui.Hint{Action: actQuit, Label: tui.TextKeyQuit, Kind: tui.HintAlways})
}

func (m *model) frameSpec() tui.FrameSpec {
	status := func(id tui.TextID, value string) tui.StatusMark {
		return tui.StatusMark{Text: m.text.Lookup(id, value), Short: []string{value}}
	}
	return tui.FrameSpec{
		Width: m.width, Height: m.height,
		Product: m.text.Lookup(textProduct), Icon: m.icon("◆", "\uf121"),
		Status: []tui.StatusMark{
			status(textStatusTheme, m.prefs.Theme),
			status(textStatusLanguage, m.prefs.Language),
			status(textStatusIcons, m.prefs.Icons),
			status(textStatusMouse, m.onOff(m.prefs.MouseEnabled)),
		},
		HeaderKeys: []tui.Hint{{Action: actHelp, Label: tui.TextKeyHelp}, {Action: actCommands, Label: tui.TextKeyCommands},
			{Action: actInfo, Label: tui.TextKeyInfo}, {Action: actQuit, Label: tui.TextKeyQuit}},
		Areas: m.areas(), ActiveArea: m.area, ActiveTab: m.tab(),
		Focus: m.zone, Input: tui.InputNavigation,
		Message: m.message, Footer: m.footerHints(),
	}
}

// workspace draws the open tab into the room spec leaves and puts it into spec.
func (m *model) workspace(spec *tui.FrameSpec) (tui.Rect, []tui.Target) {
	room := m.frame.Workspace(*spec)
	lines, targets := m.group().draw(m, m.tab(), room)
	spec.Workspace = strings.Join(lines, "\n")
	return room, tui.OffsetTargets(targets, room.X, room.Y)
}

// refresh draws the screen once and keeps its click targets. The mouse map is off where the mouse is
// switched off and where the terminal is too small. Under a part that stands over the screen it holds that
// part's targets alone, so the background takes no click.
func (m *model) refresh() {
	if m.width < minWidth || m.height < minHeight {
		m.view = m.text.Lookup(textTooSmall)
		m.mouse = tui.NewMouseMap(nil, false)
		return
	}
	spec := m.frameSpec()
	var view string
	var targets []tui.Target
	switch {
	case m.cmdOpen:
		view, targets = m.drawCommandLine(spec)
	case m.modal != nil:
		view, targets = m.drawModal(spec)
	default:
		_, body := m.workspace(&spec)
		view, targets = m.frame.Render(m.styles, spec)
		targets = append(targets, body...)
	}
	m.view = m.styles.Paint(view, m.width, m.height)
	m.mouse = tui.NewMouseMap(targets, m.prefs.MouseEnabled)
}

// belowWorkspace keeps the targets under the workspace, the footer's: while a part fills the workspace, the
// footer holds its keys and the header and tabs take no click.
func belowWorkspace(targets []tui.Target, room tui.Rect) []tui.Target {
	var kept []tui.Target
	for _, t := range targets {
		if t.Rect.Y >= room.Y+room.Height {
			kept = append(kept, t)
		}
	}
	return kept
}

// drawModal draws the open part over the screen.
func (m *model) drawModal(spec tui.FrameSpec) (string, []tui.Target) {
	return m.drawOver(spec, m.modal, m.focus)
}

// drawCommandLine draws the command line over the screen.
func (m *model) drawCommandLine(spec tui.FrameSpec) (string, []tui.Target) {
	return m.drawOver(spec, m.cmdline, tui.InputText)
}

// drawOver draws part over the dimmed screen where it fits, else in the workspace with its keys in the
// footer. Its targets are the only ones, bar the footer's in the second case.
func (m *model) drawOver(spec tui.FrameSpec, part interface {
	Overlay(tui.Styles, string, int, int, tui.Rect) (string, []tui.Target, bool)
	View(tui.Styles, int, int) (string, []tui.Target)
	Hints() []tui.Hint
}, focus tui.InputFocus) (string, []tui.Target) {
	m.workspace(&spec)
	screen, _ := m.frame.Render(m.styles, spec)
	if view, targets, ok := part.Overlay(m.styles, screen, m.width, m.height, tui.Rect{Width: m.width, Height: m.height}); ok {
		return view, targets
	}
	spec.Footer, spec.Input, spec.HeaderKeys = part.Hints(), focus, nil
	room := m.frame.Workspace(spec)
	body, bt := part.View(m.styles, room.Width, room.Height)
	spec.Workspace = body
	view, targets := m.frame.Render(m.styles, spec)
	return view, append(belowWorkspace(targets, room), tui.OffsetTargets(bt, room.X, room.Y)...)
}

func (m *model) View() tea.View {
	v := tea.NewView(m.view)
	v.AltScreen = true
	if m.prefs.MouseEnabled {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

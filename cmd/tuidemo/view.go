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

// pathForm reports whether the navigation is a path, in the line or in the header.
func (m *model) pathForm() bool {
	return m.prefs.Navigation == uiprefs.NavigationPathLine || m.prefs.Navigation == uiprefs.NavigationPathHeader
}

// choiceForm reports whether the areas are a choice field in the header, above the sub-tab line.
func (m *model) choiceForm() bool { return m.prefs.Navigation == uiprefs.NavigationHeaderChoice }

// onChoice reports whether the focus lies on the choice field of the header.
func (m *model) onChoice() bool { return m.choiceForm() && m.zone == tui.ZoneTabs }

// navForm is the frame's form of the navigation in force.
func (m *model) navForm() tui.NavForm {
	switch m.prefs.Navigation {
	case uiprefs.NavigationPathLine:
		return tui.NavPathLine
	case uiprefs.NavigationPathHeader:
		return tui.NavPathHeader
	case uiprefs.NavigationHeaderChoice:
		return tui.NavHeaderChoice
	}
	return tui.NavTabs
}

// zones are the focus zones the open tab has: the path where the navigation is one, else the tab line and
// the sub-tab line only where it shows, then the work area: the zones of its block, or rows where the tab
// has buttons.
func (m *model) zones() []tui.FocusZone {
	zones := []tui.FocusZone{tui.ZonePath}
	if !m.pathForm() {
		zones[0] = tui.ZoneTabs
		if len(m.group().area.Tabs) > 1 {
			zones = append(zones, tui.ZoneSubtabs)
		}
	}
	if b := m.live(); b != nil {
		zones = append(zones, b.zones()...)
	} else if len(m.actions()) > 0 {
		zones = append(zones, tui.ZoneRows)
	}
	return zones
}

// top is the zone of the topmost line, the tab line or the path.
func (m *model) top() tui.FocusZone { return m.zones()[0] }

// onNavLine reports whether the focus lies on a line of the navigation, not in the work area.
func (m *model) onNavLine() bool {
	return m.zone == tui.ZoneTabs || m.zone == tui.ZoneSubtabs || m.zone == tui.ZonePath
}

func (m *model) openArea(id tui.AreaID) {
	m.area = id
	if !slices.Contains(m.zones(), m.zone) {
		m.zone = m.top()
	}
}

// step moves the open tab of the focused line by one, with wrap-around; on the path it moves the focus
// between the area and the tab section.
func (m *model) step(by int) {
	if m.zone == tui.ZonePath {
		m.pathArea = by < 0
		return
	}
	if !m.onNavLine() {
		return
	}
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

// footerHints are the key hints of the screen: the open tab's own behind the common ones, quit always. On
// a path form left and right act only on a path of two sections and tab only where there is a work area.
func (m *model) footerHints() []tui.Hint {
	var hints []tui.Hint
	if !m.pathForm() || m.zone == tui.ZonePath && len(m.group().area.Tabs) > 1 {
		hints = append(hints, tui.Hint{Action: actLeft, Label: textHintStep, Kind: tui.HintNavigation},
			tui.Hint{Action: actRight, Label: textHintStep, Kind: tui.HintNavigation})
	}
	if !m.pathForm() || len(m.zones()) > 1 {
		hints = append(hints, tui.Hint{Action: actZoneNext, Label: textHintZone, Kind: tui.HintNavigation})
	}
	hints = append(hints,
		tui.Hint{Action: actTheme, Label: textKeyTheme, Priority: 2},
		tui.Hint{Action: actThemeBack, Label: textKeyThemeBack, Priority: 3},
		tui.Hint{Action: actLanguage, Label: textKeyLanguage, Priority: 3},
		tui.Hint{Action: actIcons, Label: textKeyIcons, Priority: 4},
		tui.Hint{Action: actMouse, Label: textKeyMouse, Priority: 5},
		tui.Hint{Action: actNavigate, Label: textKeyNavigation, Priority: 5},
	)
	if m.pathForm() || m.choiceForm() {
		hints = append(hints, tui.Hint{Action: actMenu, Label: tui.TextKeyMenu, Priority: 1})
	}
	hints = append(hints, m.navHints()...)
	hints = append(hints, m.group().hints[m.tab()]...)
	if b := m.inBlock(m.zone); b != nil {
		hints = append(hints, b.hints(m.zone)...)
	}
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
		Nav: m.navForm(), PathFocusArea: m.pathArea,
		Focus: m.zone, Input: m.input(),
		Message: m.message, Footer: m.footerHints(), Bar: m.barActions(),
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
	if !slices.Contains(m.zones(), m.zone) {
		m.zone = m.top()
	}
	if b := m.live(); b != nil {
		b.focus(m.zone)
	}
	spec := m.frameSpec()
	var view string
	var targets []tui.Target
	switch {
	case m.cmdOpen:
		view, targets = m.drawCommandLine(spec)
	case m.modal != nil:
		view, targets = m.drawModal(spec)
	case m.pathnav.Open():
		view, targets = m.drawOver(spec, m.pathnav, tui.InputNavigation)
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
	spec.Footer, spec.Input, spec.HeaderKeys, spec.Bar = part.Hints(), focus, nil, nil
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

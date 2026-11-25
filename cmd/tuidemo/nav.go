package main

import (
	"slices"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// actions are the buttons of the open tab in the order the focus visits them; each is the action its click
// runs. A tab with buttons has the work zone rows; a tab with a block has the zones of the block instead.
func (m *model) actions() []tui.ActionID {
	if g := m.group(); g.actions != nil {
		return g.actions(g.area, m.tab())
	}
	return nil
}

// onButtons reports whether the focus lies on the buttons of the open tab.
func (m *model) onButtons() bool {
	return m.zone == tui.ZoneRows && m.live() == nil
}

// focused reports whether the button at index i of the open tab has the focus.
func (m *model) focused(i int) bool {
	return m.onButtons() && m.item == i
}

// workZone is the zone of the work area that down and enter lead to from a tab line, empty for a tab
// without one.
func (m *model) workZone() tui.FocusZone {
	z := tui.StepZoneLevel(m.zones(), tui.ZoneTabs, 2)
	if z == tui.ZoneTabs || z == tui.ZoneSubtabs {
		return ""
	}
	return z
}

// down steps the focus one level down, or to the next button inside the work area. A block takes down
// itself; what it leaves over stays in it.
func (m *model) down() {
	if m.onButtons() {
		m.item = min(m.item+1, len(m.actions())-1)
		return
	}
	m.zone = tui.StepZoneLevel(m.zones(), m.zone, 1)
}

// up steps the focus to the previous button; from the first one, and from a block that leaves up over at
// its top, it leaves the work area.
func (m *model) up() {
	if m.onButtons() && m.item > 0 {
		m.item--
		return
	}
	m.zone = tui.StepZoneLevel(m.zones(), m.zone, -1)
}

// enter leads from a tab line into the work area, and triggers the focused button there.
func (m *model) enter() {
	switch {
	case m.onButtons():
		if m.item < len(m.actions()) {
			m.run(m.actions()[m.item], "")
		}
	case m.zone == tui.ZoneTabs || m.zone == tui.ZoneSubtabs:
		if z := m.workZone(); z != "" {
			m.zone = z
		}
	}
}

// settle keeps the focus valid after the open tab changed: the first button of the new tab, and a zone it has.
func (m *model) settle(was string) {
	if now := string(m.area) + "/" + string(m.tab()); now != was {
		m.item = 0
	}
	if !slices.Contains(m.zones(), m.zone) {
		m.zone = tui.ZoneTabs
	}
}

// navHints are the hints of the keys that move the focus down, up and into the work area, where each acts.
// Inside a block they are none: its own hints name its keys.
func (m *model) navHints() []tui.Hint {
	if m.inBlock(m.zone) != nil {
		return nil
	}
	var hints []tui.Hint
	canDown := tui.StepZoneLevel(m.zones(), m.zone, 1) != m.zone
	if m.onButtons() {
		canDown = m.item < len(m.actions())-1
	}
	if canDown {
		hints = append(hints, tui.Hint{Action: actDown, Label: tui.TextKeyDown, Kind: tui.HintNavigation})
	}
	if m.zone != tui.ZoneTabs {
		hints = append(hints, tui.Hint{Action: actUp, Label: tui.TextKeyUp, Kind: tui.HintNavigation})
	}
	if m.onButtons() || m.workZone() != "" {
		hints = append(hints, tui.Hint{Action: actEnter, Label: tui.TextKeyEnter, Kind: tui.HintNavigation})
	}
	return hints
}

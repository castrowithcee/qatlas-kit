package main

import (
	"slices"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// actions are the buttons of the open tab in the order the focus visits them; each is the action its click
// runs. A tab without buttons has no work zone.
func (m *model) actions() []tui.ActionID {
	if g := m.group(); g.actions != nil {
		return g.actions(g.area, m.tab())
	}
	return nil
}

// focused reports whether the button at index i of the open tab has the focus.
func (m *model) focused(i int) bool {
	return m.zone == tui.ZoneRows && m.item == i
}

// down steps the focus one level down, or to the next button inside the work area.
func (m *model) down() {
	if m.zone == tui.ZoneRows {
		m.item = min(m.item+1, len(m.actions())-1)
		return
	}
	m.zone = tui.StepZoneLevel(m.zones(), m.zone, 1)
}

// up steps the focus to the previous button; from the first one it leaves the work area.
func (m *model) up() {
	if m.zone == tui.ZoneRows && m.item > 0 {
		m.item--
		return
	}
	m.zone = tui.StepZoneLevel(m.zones(), m.zone, -1)
}

// enter leads from a tab line into the work area, and triggers the focused button there.
func (m *model) enter() {
	switch {
	case m.zone == tui.ZoneRows:
		if m.item < len(m.actions()) {
			m.run(m.actions()[m.item], "")
		}
	case slices.Contains(m.zones(), tui.ZoneRows):
		m.zone = tui.ZoneRows
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
func (m *model) navHints() []tui.Hint {
	var hints []tui.Hint
	canDown := tui.StepZoneLevel(m.zones(), m.zone, 1) != m.zone
	if m.zone == tui.ZoneRows {
		canDown = m.item < len(m.actions())-1
	}
	if canDown {
		hints = append(hints, tui.Hint{Action: actDown, Label: tui.TextKeyDown, Kind: tui.HintNavigation})
	}
	if m.zone != tui.ZoneTabs {
		hints = append(hints, tui.Hint{Action: actUp, Label: tui.TextKeyUp, Kind: tui.HintNavigation})
	}
	if m.zone == tui.ZoneRows || slices.Contains(m.zones(), tui.ZoneRows) {
		hints = append(hints, tui.Hint{Action: actEnter, Label: tui.TextKeyEnter, Kind: tui.HintNavigation})
	}
	return hints
}

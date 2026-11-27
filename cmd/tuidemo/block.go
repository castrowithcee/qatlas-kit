package main

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// block is a live kit block in the workspace of a tab, with its own state. While the focus lies in one of
// its zones it reads the keys before the switches of the workbench do, and it gets the clicks and the wheel
// of its targets. A tab shows one by an entry of group.blocks; the frame, the key table and the focus follow.
type block interface {
	// zones are the focus zones the block adds behind the tab lines, so tab and shift+tab reach it.
	zones() []tui.FocusZone
	// focus tells the block, at every drawing, which zone has the focus.
	focus(zone tui.FocusZone)
	// input is the input focus of the block in zone: text while it types, so letters are not switches.
	input(zone tui.FocusZone) tui.InputFocus
	draw(m *model, room tui.Rect) ([]string, []tui.Target)
	// key reports whether the block took the key; the rest goes to the key table of the workbench.
	key(m *model, msg tea.KeyPressMsg) bool
	// click handles a click on a target of one of its zones; m.zone already is that zone.
	click(m *model, res tui.MouseResult)
	wheel(res tui.MouseResult)
	// act runs an action of the key table that a footer hint or a key the block left over names.
	act(m *model, action tui.ActionID) bool
	paste(text string)
	hints(zone tui.FocusZone) []tui.Hint
}

// barer is a block that brings its own actions to the action bar of the frame.
type barer interface{ bar() []tui.BarAction }

// barActions are the actions of the action bar of the open tab: the block's, else those of the group.
func (m *model) barActions() []tui.BarAction {
	if b, ok := m.live().(barer); ok {
		return b.bar()
	}
	return m.group().bar[m.tab()]
}

// blockKey names the tab a block belongs to.
type blockKey struct {
	area tui.AreaID
	tab  tui.TabID
}

// rebuildBlocks builds the blocks of every tab again, with the key table and the language in force. The
// blocks start from their first state, so a switch of the language resets them.
func (m *model) rebuildBlocks() error {
	m.blocks = map[blockKey]block{}
	for _, g := range m.groups {
		if g.blocks == nil {
			continue
		}
		bs, err := g.blocks(m)
		if err != nil {
			return err
		}
		for tab, b := range bs {
			m.blocks[blockKey{g.area.ID, tab}] = b
		}
	}
	return nil
}

// live is the block of the open tab, nil where the tab shows none.
func (m *model) live() block { return m.blocks[blockKey{m.area, m.tab()}] }

// inBlock returns the live block if zone is one of its zones.
func (m *model) inBlock(zone tui.FocusZone) block {
	if b := m.live(); b != nil && slices.Contains(b.zones(), zone) {
		return b
	}
	return nil
}

// input is the input focus of the screen: the block's while the focus lies in it, else navigation.
func (m *model) input() tui.InputFocus {
	if b := m.inBlock(m.zone); b != nil {
		return b.input(m.zone)
	}
	return tui.InputNavigation
}

// drawLive is the draw of a group whose tabs all show a block.
func drawLive(m *model, _ tui.TabID, room tui.Rect) ([]string, []tui.Target) {
	return m.live().draw(m, room)
}

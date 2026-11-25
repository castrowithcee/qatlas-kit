package tui

import (
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// PathNavKeys are the actions of the key table the overlays of the path line read. Every key is needed; the
// application usually shares them with its lists. Back closes the overlay without a choice and Enter chooses.
type PathNavKeys struct {
	Up, Down, PageUp, PageDown, First, Last ActionID
	// Left and Right fold and unfold an area of the overview.
	Left, Right ActionID
	Enter, Back ActionID
}

// PathChoice is what a choice in an overlay of the path line names: the area and, where the area has tabs,
// the tab. The application runs it as the click on that tab. The zero value is no choice.
type PathChoice struct {
	Area AreaID
	Tab  TabID
}

// PathNav is the pair of overlays the path line opens: the overview of all areas with their tabs as a tree,
// and the choice among the tabs of the current area. Both are a List in a modal, centred over the screen, with
// the current place marked and no search. It has the shape of Modal, but answers with a PathChoice instead of
// an ActionID. While one is open the application gives it every key and click and the screen behind it takes
// none, as with a Dialog.
type PathNav struct {
	keys PathNavKeys
	km   *Keymap
	text Translator

	list *List
	// places maps the item of a tab to its choice; bare maps the group key of an area without tabs, whose
	// group row is the choice itself.
	places map[ItemID]PathChoice
	bare   map[string]PathChoice
}

// NewPathNav checks keys against the key table and returns a closed PathNav.
func NewPathNav(keys PathNavKeys, km *Keymap, text Translator) (*PathNav, error) {
	p := &PathNav{keys: keys, km: km, text: text}
	if _, err := p.newList(TextPathNavOverview); err != nil {
		return nil, errors.New("path navigation: " + err.Error())
	}
	return p, nil
}

func (p *PathNav) newList(title TextID) (*List, error) {
	k := p.keys
	return NewList(ListSpec{
		Columns: []Column{{Title: TextPathNavColumn}},
		Keys: ListKeys{Up: k.Up, Down: k.Down, PageUp: k.PageUp, PageDown: k.PageDown, First: k.First,
			Last: k.Last, Left: k.Left, Right: k.Right, Enter: k.Enter, Back: k.Back},
		Search: SearchNone, Wrap: true, Outline: true, Empty: TextPathNavEmpty, Title: title,
	}, p.km, p.text)
}

// Open reports whether an overlay is open.
func (p *PathNav) Open() bool { return p.list != nil }

// Close closes the overlay without a choice.
func (p *PathNav) Close() { p.list, p.places, p.bare = nil, nil, nil }

// OpenOverview opens the overview of every area of spec with its tabs and marks the current place. It reports
// false, and opens nothing, where spec has no area or shows the mode text, which has no place.
func (p *PathNav) OpenOverview(spec FrameSpec) bool {
	if len(spec.Areas) == 0 || spec.ModeTab != "" {
		return false
	}
	l, err := p.newList(TextPathNavOverview)
	if err != nil {
		return false
	}
	places, bare := map[ItemID]PathChoice{}, map[string]PathChoice{}
	var items []ListItem
	var groups []ListGroup
	var current listMark
	for i, a := range spec.Areas {
		key := string(a.ID)
		groups = append(groups, ListGroup{Key: key, Title: lookup(p.text, a.Title), Keep: len(a.Tabs) == 0})
		if len(a.Tabs) == 0 {
			bare[key] = PathChoice{Area: a.ID}
			if a.ID == spec.ActiveArea {
				current = listMark{group: key, isGroup: true, ok: true}
			}
		}
		for j, t := range a.Tabs {
			id := ItemID("t" + strconv.Itoa(i) + "." + strconv.Itoa(j))
			places[id] = PathChoice{Area: a.ID, Tab: t.ID}
			items = append(items, ListItem{ID: id, Cells: []string{lookup(p.text, t.Title)}, Group: key})
			if a.ID == spec.ActiveArea && t.ID == spec.ActiveTab {
				current = listMark{id: id, ok: true}
			}
		}
	}
	l.SetGrouped(true)
	if l.SetGroups(groups) != nil || l.SetItems(items) != nil {
		return false
	}
	if current.isGroup {
		l.reselect(current)
	} else if current.ok {
		l.Select(current.id)
	}
	p.list, p.places, p.bare = l, places, bare
	return true
}

// OpenChoice opens the choice among the tabs of the active area and marks the active tab. It reports false
// where the area has fewer than two tabs, which the path line does not offer.
func (p *PathNav) OpenChoice(spec FrameSpec) bool {
	if spec.ModeTab != "" {
		return false
	}
	for _, a := range spec.Areas {
		if a.ID != spec.ActiveArea || len(a.Tabs) < 2 {
			continue
		}
		l, err := p.newList(a.Title)
		if err != nil {
			return false
		}
		places := map[ItemID]PathChoice{}
		var items []ListItem
		var current ItemID
		for j, t := range a.Tabs {
			id := ItemID("t" + strconv.Itoa(j))
			places[id] = PathChoice{Area: a.ID, Tab: t.ID}
			items = append(items, ListItem{ID: id, Cells: []string{lookup(p.text, t.Title)}})
			if t.ID == spec.ActiveTab {
				current = id
			}
		}
		if l.SetItems(items) != nil {
			return false
		}
		l.Select(current)
		p.list, p.places, p.bare = l, places, map[string]PathChoice{}
		return true
	}
	return false
}

// OpenClick opens the overlay for a click on a section of the path line, as the MouseResult of its target
// reports it: the overview for the area section, the choice for a tab section. It reports false for any
// other click.
func (p *PathNav) OpenClick(spec FrameSpec, r MouseResult) bool {
	if !r.Handled || r.Zone != ZonePath || r.Area == "" {
		return false
	}
	if r.Tab != "" {
		return p.OpenChoice(spec)
	}
	return p.OpenOverview(spec)
}

// OpenFocused opens the overlay for the enter key on the focused section of the path line: the overview for
// the area section, the choice for the tab section.
func (p *PathNav) OpenFocused(spec FrameSpec) bool {
	if spec.PathFocusArea {
		return p.OpenOverview(spec)
	}
	if p.OpenChoice(spec) {
		return true
	}
	return p.OpenOverview(spec)
}

// Update handles a key while an overlay is open. choice is set where the key chose a place, which closes the
// overlay; a key for back closes it with no choice. handled is false for a key the overlay leaves to the
// application.
func (p *PathNav) Update(msg tea.KeyPressMsg) (choice PathChoice, handled bool) {
	if p.list == nil {
		return PathChoice{}, false
	}
	for _, id := range []ActionID{p.keys.Back, p.keys.Enter} {
		if p.km.Is(msg, id, InputNavigation) {
			return p.act(id)
		}
	}
	open, handled := p.list.Update(msg)
	if open != "" {
		return p.pick(open)
	}
	return PathChoice{}, handled
}

// Click handles a click on a target the overlay drew. A click on a place chooses it at once and one on an
// area with tabs folds it; a click on a key of the key line acts as the key does.
func (p *PathNav) Click(r MouseResult) (choice PathChoice, handled bool) {
	if p.list == nil || !r.Handled {
		return PathChoice{}, false
	}
	if r.Item == "" && r.Action != "" {
		return p.act(r.Action)
	}
	if c, ok := p.places[ItemID(r.Item)]; ok {
		p.Close()
		return c, true
	}
	if g, ok := strings.CutPrefix(r.Item, groupRef); ok {
		if c, ok := p.bare[g]; ok {
			p.Close()
			return c, true
		}
	}
	_, handled = p.list.Click(r)
	return PathChoice{}, handled
}

// act runs one key of the overlay.
func (p *PathNav) act(id ActionID) (PathChoice, bool) {
	switch {
	case id == "":
		return PathChoice{}, false
	case id == p.keys.Back:
		p.Close()
		return PathChoice{}, true
	case id == p.keys.Enter:
		// The group row of an area without tabs is the place itself.
		if g, ok := p.list.SelectedGroup(); ok {
			if c, ok := p.bare[g]; ok {
				p.Close()
				return c, true
			}
		}
	}
	open, handled := p.list.Act(id)
	if open != "" {
		return p.pick(open)
	}
	return PathChoice{}, handled
}

func (p *PathNav) pick(id ItemID) (PathChoice, bool) {
	c, ok := p.places[id]
	if ok {
		p.Close()
	}
	return c, ok
}

// Wheel moves the marked row by the notches of the wheel.
func (p *PathNav) Wheel(steps int) {
	if p.list != nil {
		p.list.Wheel(steps)
	}
}

// Overlay draws the open overlay over screen as List.Overlay does; ok is false where none is open or the room
// is too small, and the application then draws View in the workspace and Hints in the footer.
func (p *PathNav) Overlay(styles Styles, screen string, width, height int, room Rect) (view string, targets []Target, ok bool) {
	if p.list == nil {
		return "", nil, false
	}
	return p.list.Overlay(styles, screen, width, height, room)
}

// View draws the open overlay as a list in width by height cells.
func (p *PathNav) View(styles Styles, width, height int) (string, []Target) {
	if p.list == nil {
		return "", nil
	}
	return p.list.View(styles, width, height)
}

// Hints are the keys of the open overlay for the footer.
func (p *PathNav) Hints() []Hint {
	if p.list == nil {
		return nil
	}
	return p.list.Hints()
}

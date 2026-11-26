package tui

import (
	"strconv"
	"strings"
)

// outlineRef starts the ID of a page in the outline; the rest is the index into Help.pages.
const outlineRef = "p"

// newOutline is the list of the overview: topics as groups in the order of the pages, an outline without
// filter, and a click that opens a page at once.
func (h *Help) newOutline() (*List, error) {
	k := h.spec.Keys
	return NewList(ListSpec{
		Columns: []Column{{Title: TextHelpOutlineCol}},
		Keys: ListKeys{Up: k.Up, Down: k.Down, PageUp: k.PageUp, PageDown: k.PageDown, First: k.First, Last: k.Last,
			Left: k.Left, Right: k.Right, Enter: k.Enter, Back: k.Back},
		Search: SearchNone, Wrap: true, Outline: true, ClickOpens: true, Empty: TextHelpOutlineNil,
	}, h.keys, h.text)
}

// openOutline opens the overview with the page shown marked: each topic, titled as its index page, holds its
// index page and its other pages in the order helppages gives them.
func (h *Help) openOutline() {
	l, err := h.newOutline()
	if err != nil {
		return
	}
	var groups []ListGroup
	var items []ListItem
	for i, p := range h.pages {
		title := p.page.In(h.spec.Lang).Title
		if p.page == p.topic.Index {
			groups = append(groups, ListGroup{Key: p.topic.ID, Title: title})
		}
		items = append(items, ListItem{ID: ItemID(outlineRef + strconv.Itoa(i)), Cells: []string{title}, Group: p.topic.ID})
	}
	l.SetGrouped(true)
	if l.SetGroups(groups) != nil || l.SetItems(items) != nil {
		return
	}
	l.Select(ItemID(outlineRef + strconv.Itoa(h.page)))
	h.outline = l
}

// outlineAct runs a help key while the overview is open: back and the outline key close it, the help key
// closes the whole help, and the other keys move in the list, where enter opens the page marked.
func (h *Help) outlineAct(id ActionID) (ActionID, bool) {
	k := h.spec.Keys
	switch id {
	case k.Help:
		return id, true
	case k.Back, k.Outline:
		h.outline = nil
		return "", true
	}
	open, handled := h.outline.Act(id)
	return "", h.openOutlined(open) || handled
}

// outlineClick handles a click while the overview is open: an entry or topic acts as the list does, a key of
// the key line as that key.
func (h *Help) outlineClick(r MouseResult) (ActionID, bool) {
	if r.Item != "" {
		open, handled := h.outline.Click(r)
		return "", h.openOutlined(open) || handled
	}
	if r.Action != "" && h.outline != nil {
		for _, id := range h.navKeys() {
			if id == r.Action {
				return h.Act(id)
			}
		}
	}
	return "", false
}

// openOutlined shows the page of an entry the list opened, from its top, and closes the overview.
func (h *Help) openOutlined(id ItemID) bool {
	i, err := strconv.Atoi(strings.TrimPrefix(string(id), outlineRef))
	if id == "" || err != nil || i < 0 || i >= len(h.pages) {
		return false
	}
	h.showPage(i)
	return true
}

// placeOutline draws the overview in width by room cells below lines, as tall as its rows ask for.
func (h *Help) placeOutline(lines []string, targets []Target, width, room int, styles Styles) ([]string, []Target) {
	// wanted counts the search line, which an outline never has.
	body, ts := h.outline.View(styles, width, min(h.outline.wanted()-1, room))
	targets = append(targets, OffsetTargets(ts, 0, len(lines))...)
	return append(lines, strings.Split(body, "\n")...), targets
}

package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// recentFrom is the day from which a watering counts as recent in the filter bar of the Filter tab.
const recentFrom = "2031-05-04"

// listBlock is a List of the sample plants, with a FilterBar above it where bar is set.
type listBlock struct {
	list  *tui.List
	bar   *tui.FilterBar
	items []tui.ListItem // every entry; the bar narrows what the list gets
	note  tui.TextID
	// search is true where a key starts the filter; a list that filters as typed has none.
	search bool
	ticks  bool
}

// placeOf is the group key of a bed: its name without a trailing number, so the pots stand together.
func placeOf(bed string) string { return strings.TrimRight(bed, " 0123456789") }

// plantItems are the entries of the sample plants; withBed adds the bed as a column.
func plantItems(withBed bool) []tui.ListItem {
	var items []tui.ListItem
	for _, r := range sampleRows() {
		cells := []string{r.Name, r.Watered}
		if withBed {
			cells = []string{r.Name, r.Bed, r.Watered}
		}
		items = append(items, tui.ListItem{ID: tui.ItemID(strings.ToLower(r.Name)), Cells: cells, Group: placeOf(r.Bed)})
	}
	return items
}

// plantGroups titles each place and shows its latest watering in the date column.
func plantGroups(withBed bool) []tui.ListGroup {
	latest := map[string]string{}
	var order []string
	for _, r := range sampleRows() {
		p := placeOf(r.Bed)
		if _, ok := latest[p]; !ok {
			order = append(order, p)
		}
		latest[p] = max(latest[p], r.Watered)
	}
	var groups []tui.ListGroup
	for _, p := range order {
		cells := []string{"", latest[p]}
		if withBed {
			cells = []string{"", "", latest[p]}
		}
		groups = append(groups, tui.ListGroup{Key: p, Title: p, Cells: cells})
	}
	return groups
}

// plantTickRows tick the plants by a trait across the groups; the pot row leaves Thyme to its own box.
func plantTickRows(m *model) []tui.ListTickRow {
	return []tui.ListTickRow{
		{Key: "recent", Title: m.text.Lookup(textListTickRecent),
			Match: func(it tui.ListItem) bool { return it.Cells[len(it.Cells)-1] >= recentFrom }},
		{Key: "pot", Title: m.text.Lookup(textListTickPot),
			Match:  func(it tui.ListItem) bool { return it.Group == "Pot" },
			Exempt: func(it tui.ListItem) bool { return it.ID == "thyme" }},
	}
}

func listBlocks(m *model) (map[tui.TabID]block, error) {
	filter, err := newListBlock(m, textNoteListFilter, true, false, false)
	if err != nil {
		return nil, err
	}
	groups, err := newListBlock(m, textNoteListGroups, false, false, true)
	if err != nil {
		return nil, err
	}
	ticks, err := newListBlock(m, textNoteListTicks, false, true, true)
	if err != nil {
		return nil, err
	}
	return map[tui.TabID]block{"filter": filter, "groups": groups, "ticks": ticks}, nil
}

// newListBlock builds a list with the bed column and a filter bar (bar), with tick boxes and filtering as
// typed (ticks), and grouped or flat (grouped).
func newListBlock(m *model, note tui.TextID, bar, ticks, grouped bool) (*listBlock, error) {
	cols := []tui.Column{{Title: textSampleName}, {Title: textSampleDate, Dimmed: true}}
	if bar {
		cols = []tui.Column{{Title: textSampleName}, {Title: textSampleBed, Flex: true}, {Title: textSampleDate, Dimmed: true}}
	}
	keys := tui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst, Last: actLast,
		Left: actLeft, Right: actRight, Enter: actEnter, Back: actBack, Search: actSearch}
	spec := tui.ListSpec{Columns: cols, Keys: keys, Empty: textListEmpty, SearchInBar: bar, ClickOpens: grouped && !ticks}
	if ticks {
		spec.Keys.Search, spec.Keys.Toggle = "", actToggle
		spec.Search, spec.Ticks, spec.AllRow, spec.TickWhileTyping = tui.SearchAsTyped, true, true, true
	}
	list, err := tui.NewList(spec, m.keys, m.text)
	if err != nil {
		return nil, err
	}
	b := &listBlock{list: list, items: plantItems(bar), note: note, search: !ticks, ticks: ticks}
	if err := list.SetItems(b.items); err != nil {
		return nil, err
	}
	if err := list.SetGroups(plantGroups(bar)); err != nil {
		return nil, err
	}
	list.SetGrouped(grouped)
	if ticks {
		if err := list.SetTickRows(plantTickRows(m)); err != nil {
			return nil, err
		}
	}
	if bar {
		b.bar, err = tui.NewFilterBar(tui.FilterBarSpec{List: list, Keys: tui.FilterBarKeys{Left: actLeft, Right: actRight,
			Next: actZoneNext, Prev: actZonePrev, Up: actUp, Down: actDown, Enter: actEnter, Back: actBack,
			PageUp: actPageUp, PageDown: actPageDown}, Fields: []tui.FilterField{
			{ID: "group", Label: textListBarGroup, Options: []tui.Choice{{ID: "none", Label: textListBarNone}, {ID: "place", Label: textListBarPlace}}},
			{ID: "water", Label: textListBarWater, Options: []tui.Choice{{ID: "any", Label: textListBarAny}, {ID: "recent", Label: textListBarRecent}}},
		}}, m.keys, m.text)
	}
	return b, err
}

// apply hands the list what the changed fields of the bar ask for.
func (b *listBlock) apply(changed []tui.FilterField) {
	for _, f := range changed {
		switch f.ID {
		case "group":
			b.list.SetGrouped(f.Selected == "place")
		case "water":
			items := b.items
			if f.Selected == "recent" {
				items = nil
				for _, it := range b.items {
					if it.Cells[len(it.Cells)-1] >= recentFrom {
						items = append(items, it)
					}
				}
			}
			_ = b.list.SetItems(items) // the entries are the checked ones, narrowed
		}
	}
}

// opened says which entry was opened.
func (b *listBlock) opened(m *model) {
	if it, ok := b.list.Current(); ok {
		m.message = &tui.Message{Kind: tui.MessageResult, Text: m.text.Lookup(textListOpened, it.Cells[0])}
	}
}

func (b *listBlock) zones() []tui.FocusZone {
	if b.bar != nil {
		return []tui.FocusZone{tui.ZoneRows, tui.ZoneFilters}
	}
	return []tui.FocusZone{tui.ZoneRows}
}

func (b *listBlock) focus(zone tui.FocusZone) {
	b.list.SetFocused(zone == tui.ZoneRows)
	if zone != tui.ZoneRows && b.list.Searching() {
		b.list.Act(actEnter) // leaving the rows keeps the typed filter and stops typing
	}
	if b.bar == nil {
		return
	}
	switch in := zone == tui.ZoneFilters; {
	case in && !b.bar.Focused():
		b.bar.Focus(false)
	case !in && b.bar.Focused():
		b.bar.Blur()
	}
}

func (b *listBlock) input(zone tui.FocusZone) tui.InputFocus {
	switch {
	case zone == tui.ZoneRows:
		return b.list.Input()
	case b.bar != nil && b.bar.Open():
		return tui.InputText // the open values have a search line
	}
	return tui.InputNavigation
}

func (b *listBlock) key(m *model, msg tea.KeyPressMsg) bool {
	if m.zone == tui.ZoneFilters {
		changed, handled := b.bar.Update(msg)
		b.apply(changed)
		return handled
	}
	open, handled := b.list.Update(msg)
	if open != "" {
		b.opened(m)
	}
	return handled
}

func (b *listBlock) click(m *model, res tui.MouseResult) {
	if m.zone == tui.ZoneFilters {
		changed, handled := b.bar.Click(res)
		b.apply(changed)
		if handled {
			return
		}
	}
	// The search field stands in the bar and belongs to the list, which then has the focus.
	open, _ := b.list.Click(res)
	if open != "" {
		b.opened(m)
	}
	if b.list.Searching() {
		m.zone = tui.ZoneRows
	}
}

func (b *listBlock) wheel(res tui.MouseResult) {
	if res.Zone == tui.ZoneRows {
		b.list.Wheel(res.WheelSteps)
	}
}

func (b *listBlock) act(m *model, action tui.ActionID) bool {
	switch {
	case action == actSearch && b.search:
		m.zone = tui.ZoneRows
		b.list.Act(action)
	case action == actFilter && b.bar != nil:
		m.zone = tui.ZoneFilters
	case b.bar != nil && b.bar.Open():
		changed, handled := b.bar.Click(tui.MouseResult{Action: action, Handled: true})
		b.apply(changed)
		return handled
	case m.zone == tui.ZoneRows:
		open, handled := b.list.Act(action)
		if open != "" {
			b.opened(m)
		}
		return handled
	default:
		return false
	}
	return true
}

func (b *listBlock) paste(text string) { b.list.Paste(text) }

func (b *listBlock) hints(zone tui.FocusZone) []tui.Hint {
	if zone == tui.ZoneFilters {
		return b.bar.Hints()
	}
	return b.list.Hints()
}

func (b *listBlock) draw(m *model, room tui.Rect) ([]string, []tui.Target) {
	var lines []string
	if room.Height >= 16 {
		lines = append(m.paragraph(b.note, room.Width), "")
	}
	y := len(lines)
	foot := 0
	if b.ticks {
		foot = 1
	}
	var targets []tui.Target
	add := func(view string, ts []tui.Target) {
		if view == "" {
			return
		}
		targets = append(targets, tui.OffsetTargets(ts, 0, len(lines))...)
		lines = append(lines, strings.Split(view, "\n")...)
	}
	if b.bar != nil && len(b.bar.Hints()) > 0 {
		// The modal of a narrow bar fills the workspace; its keys stand in the footer.
		add(b.bar.ModalView(m.styles, room.Width, room.Height-y))
		return lines, targets
	}
	rest := room.Height - y - foot
	if b.bar != nil {
		add(b.bar.View(m.styles, room.Width))
		rest--
		if view, ts := b.bar.ValueList(m.styles, room.Width, rest); view != "" {
			add(view, ts)
			return lines, targets
		}
	}
	add(b.list.View(m.styles, room.Width, max(rest, 0)))
	if b.ticks {
		lines = append(lines, m.styles.Render(tui.StyleHint, m.text.Lookup(textListTicked, len(b.list.Ticked()))))
	}
	return lines, targets
}

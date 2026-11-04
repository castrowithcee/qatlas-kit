package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ItemID is the stable, untranslated ID of an entry of a list.
type ItemID string

// Column is one column of a list table. The first column names the entry and is never cut.
type Column struct {
	Title TextID
	// Optional lets a terminal too narrow for the column leave it out.
	Optional bool
	// Flex marks the free text of the table, which gives way first when a row does not fit.
	Flex bool
	// Right aligns the cells and the title at the right edge of the column.
	Right bool
	// Dimmed draws a secondary column in the hint style.
	Dimmed bool
	// Short maps values to the shorter labels the column shows when its values do not fit.
	Short map[string]string
	// Tone picks the style of a cell from its value, such as the colour of a state word; nil keeps the style
	// of the column. The cell text carries the meaning, the style only adds to it.
	Tone func(cell string) TextStyle
}

// ListItem is one entry of a list. Cells are plain text, one per column; Group is the key of the group the
// entry belongs to in a grouped list, empty for none. SearchText holds further words the filter reads beside
// the cells, such as the name of the group.
type ListItem struct {
	ID         ItemID
	Cells      []string
	Group      string
	SearchText string
}

// ListGroup describes the group whose key is Key: the Title its row shows instead of the key and the Cells
// it carries in the columns of the list, one per column at most. The first column of a group row holds the
// group head, so the first cell is not shown; a missing cell is empty.
type ListGroup struct {
	Key   string
	Title string
	Cells []string
}

// ListKeys are the actions of the key table a list reads.
type ListKeys struct {
	Up, Down, PageUp, PageDown, First, Last ActionID
	// Left and Right fold and unfold the groups.
	Left, Right ActionID
	// Enter opens the marked entry, folds a group row and keeps a typed filter.
	Enter ActionID
	// Search starts typing a filter; a list that filters as typed needs none.
	Search ActionID
	// Back clears the filter; without one the list leaves it to the application.
	Back ActionID
	// Toggle ticks or unticks the marked entry, or the whole group on a group row and every entry on the
	// row of all; a list with ListSpec.Ticks needs it.
	Toggle ActionID
}

// ListSearch says how a list takes its filter.
type ListSearch uint8

const (
	// SearchByKey starts the filter with the search key, for a list whose letters are actions.
	SearchByKey ListSearch = iota
	// SearchAsTyped filters with every typed character, for a pure choice list without letter actions.
	SearchAsTyped
)

// ListSpec is what a list shows and how it reacts. The application supplies every text.
type ListSpec struct {
	Columns []Column
	Keys    ListKeys
	Search  ListSearch
	// Wrap makes up and down wrap round at the ends, for a list to choose a value from; a list of records
	// stops there.
	Wrap bool
	// Empty is the dimmed sentence of an empty list, with the next step.
	Empty TextID
	// NoGroup titles the group of entries without a group key; empty takes TextListNoGroup.
	NoGroup TextID
	// SearchInBar draws the search field in the FilterBar of the list instead of a line of its own.
	SearchInBar bool
	// Ticks puts a tick box before every entry and a three-state box before every group, so the list picks
	// any number of entries. Toggle must name an action of the key table.
	Ticks bool
	// AllRow adds a row above all groups that ticks and unticks every shown entry; it needs Ticks and takes
	// its text from TextListAll.
	AllRow bool
	// TickWhileTyping makes the toggle key tick in the text focus of a list that filters as typed: every key
	// bound to Toggle, a text key such as space too, acts before the filter reads it, and the footer and the
	// key line of the modal name it. The price is that the key no longer types its character into the filter.
	// It needs Ticks and SearchAsTyped.
	TickWhileTyping bool
	// Title is the text in the frame of the modal drawn by Overlay; without one the frame has no title.
	Title TextID
	// Buttons stand below the list in the modal, each an action of the key table and a label drawn behind
	// the key in force. A click on one acts as its key does.
	Buttons []FormButton
}

// flexFloor is the narrowest a free text column is cut to before anything else of the row gives way.
const flexFloor = 12

// columnGap is the room between two columns.
const columnGap = "  "

// groupRef starts the target item of a group row; item IDs hold no control character, so it never names one.
const groupRef = "\x00g"

// allRef starts the target item of the row of all entries and tickRef that of a tick box, followed by the
// item of the row the box belongs to.
const (
	allRef  = "\x00a"
	tickRef = "\x00t"
	// listButtonRef starts the target item of a button, followed by its index.
	listButtonRef = "\x00B"
)

// groupIndent is the indent of the entries below their group row.
const groupIndent = "  "

// tickWidth is the room of a tick box and the space after it.
const tickWidth = 4

// listRow is one shown row: an entry by its index, the row of group with item -1, or the row of all.
type listRow struct {
	item  int
	group string
	all   bool
}

// tickScope names what a tick toggle acts on: every shown entry or those of one group.
type tickScope struct {
	all   bool
	group string
}

// tickMemory is the partial selection of a scope from before it was first toggled, for the shown entries
// of one filter.
type tickMemory struct {
	ids   []ItemID
	query string
}

// listMark is the selected row by what it shows, so the selection follows an entry, not a position.
type listMark struct {
	id      ItemID
	group   string
	isGroup bool
	all     bool
	ok      bool
}

// List is a list of entries with selection, filter, groups and scroll position. The filter decides only
// which entries show: it never removes one and never changes their order. The selection follows the ID of an
// entry when the entries, the filter or the grouping change.
type List struct {
	spec   ListSpec
	keys   *Keymap
	text   Translator
	titles []string

	items   []ListItem
	grouped bool
	groups  map[string]ListGroup
	folded  map[string]bool
	ticked  map[ItemID]bool
	memory  map[tickScope]tickMemory

	rows      []listRow
	cursor    int
	offset    int
	page      int
	query     []rune
	searching bool
	focused   bool
}

// NewList checks spec and returns an empty list. It rejects a list without columns, a column without title,
// a first column that is free text or optional, more than one free text column, a list without the text of
// its empty state, the row of all without tick boxes, and a key that names no action of keys or that two list
// keys share; only a list that filters as typed may go without the search key, and only a list with tick
// boxes needs the toggle key. A button needs a label and an action of the key table that no other button has.
func NewList(spec ListSpec, keys *Keymap, text Translator) (*List, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the list needs a key table and a translator")
	}
	var errs []error
	if len(spec.Columns) == 0 {
		errs = append(errs, errors.New("the list has no column"))
	}
	flex := 0
	for i, c := range spec.Columns {
		switch {
		case c.Title == "":
			errs = append(errs, fmt.Errorf("column %d has no title", i+1))
		case i == 0 && (c.Flex || c.Optional):
			errs = append(errs, errors.New("the first column names the entry and is never cut or left out"))
		}
		if c.Flex {
			flex++
		}
	}
	if flex > 1 {
		errs = append(errs, fmt.Errorf("%d free text columns; a table has at most one", flex))
	}
	if spec.Empty == "" {
		errs = append(errs, errors.New("the list has no text for its empty state"))
	}
	if spec.Search != SearchByKey && spec.Search != SearchAsTyped {
		errs = append(errs, fmt.Errorf("unknown search mode %d", spec.Search))
	}
	if spec.AllRow && !spec.Ticks {
		errs = append(errs, errors.New("the row of all needs tick boxes"))
	}
	if spec.TickWhileTyping && (!spec.Ticks || spec.Search != SearchAsTyped) {
		errs = append(errs, errors.New("ticking while typing needs tick boxes and a list that filters as typed"))
	}
	k := spec.Keys
	named := []struct {
		name string
		id   ActionID
	}{{"up", k.Up}, {"down", k.Down}, {"page up", k.PageUp}, {"page down", k.PageDown}, {"first", k.First},
		{"last", k.Last}, {"left", k.Left}, {"right", k.Right}, {"enter", k.Enter}, {"search", k.Search}, {"back", k.Back}}
	if spec.Ticks || k.Toggle != "" {
		named = append(named, struct {
			name string
			id   ActionID
		}{"toggle", k.Toggle})
	}
	used := map[ActionID]string{}
	for _, n := range named {
		if n.id == "" && n.name == "search" && spec.Search == SearchAsTyped {
			continue
		}
		if _, ok := keys.index[n.id]; !ok {
			errs = append(errs, fmt.Errorf("list key %s %q is no action of the key table", n.name, n.id))
		}
		if other, dup := used[n.id]; dup && n.id != "" {
			errs = append(errs, fmt.Errorf("list keys %s and %s are both %q", other, n.name, n.id))
		}
		used[n.id] = n.name
	}
	pressed := map[ActionID]bool{}
	for i, b := range spec.Buttons {
		switch {
		case !keys.known(b.Action):
			errs = append(errs, fmt.Errorf("button %d: %q is no action of the key table", i, b.Action))
		case pressed[b.Action]:
			errs = append(errs, fmt.Errorf("button %d: action %q is on another button", i, b.Action))
		}
		pressed[b.Action] = true
		if b.Label == "" {
			errs = append(errs, fmt.Errorf("button %d has no label", i))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	spec.Columns = slices.Clone(spec.Columns)
	spec.Buttons = slices.Clone(spec.Buttons)
	l := &List{spec: spec, keys: keys, text: text, folded: map[string]bool{},
		ticked: map[ItemID]bool{}, memory: map[tickScope]tickMemory{}, page: 1}
	for _, c := range spec.Columns {
		l.titles = append(l.titles, strings.ToUpper(Escape(text.Lookup(c.Title))))
	}
	return l, nil
}

// SetItems replaces the entries and keeps the filter, the fold state, the ticks of the IDs that remain and,
// where it still shows, the selected entry. A partial selection a group remembers is forgotten. It rejects an entry without ID or with a control character in it, an ID listed twice and an entry
// whose cells do not match the columns, and then changes nothing.
func (l *List) SetItems(items []ListItem) error {
	var errs []error
	seen := map[ItemID]bool{}
	for i, it := range items {
		switch {
		case it.ID == "":
			errs = append(errs, fmt.Errorf("entry %d has no ID", i+1))
		case strings.IndexFunc(string(it.ID), unicode.IsControl) >= 0:
			errs = append(errs, fmt.Errorf("entry %q holds a control character in its ID", it.ID))
		case seen[it.ID]:
			errs = append(errs, fmt.Errorf("entry %q is listed twice", it.ID))
		}
		seen[it.ID] = true
		if len(it.Cells) != len(l.spec.Columns) {
			errs = append(errs, fmt.Errorf("entry %q has %d cells for %d columns", it.ID, len(it.Cells), len(l.spec.Columns)))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	prev := l.mark()
	l.items = make([]ListItem, len(items))
	for i, it := range items {
		it.Cells = slices.Clone(it.Cells)
		l.items[i] = it
	}
	for id := range l.ticked {
		if !seen[id] {
			delete(l.ticked, id)
		}
	}
	clear(l.memory)
	l.rebuild(prev)
	if !prev.ok {
		l.cursor = l.firstEntry()
	}
	return nil
}

// SetGroups replaces the descriptions of the groups: the title and the cells a group row shows. The fold
// state, the partial selection of the ticks and the marked row follow the key, so a new title or new cells
// lose none of them; the filter and the scroll position stay. A group without description shows its key and
// no cells, and a description of a key without entries draws nothing. It rejects an empty key, a key with a
// control character, a key listed twice and a group with more cells than the list has columns, and then
// changes nothing.
func (l *List) SetGroups(groups []ListGroup) error {
	var errs []error
	seen := map[string]bool{}
	for i, g := range groups {
		switch {
		case g.Key == "":
			errs = append(errs, fmt.Errorf("group %d has no key", i+1))
		case strings.IndexFunc(g.Key, unicode.IsControl) >= 0:
			errs = append(errs, fmt.Errorf("group %q holds a control character in its key", Escape(g.Key)))
		case seen[g.Key]:
			errs = append(errs, fmt.Errorf("group %q is listed twice", g.Key))
		}
		seen[g.Key] = true
		if len(g.Cells) > len(l.spec.Columns) {
			errs = append(errs, fmt.Errorf("group %q has %d cells for %d columns", Escape(g.Key), len(g.Cells), len(l.spec.Columns)))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	prev := l.mark()
	l.groups = make(map[string]ListGroup, len(groups))
	for _, g := range groups {
		g.Cells = slices.Clone(g.Cells)
		l.groups[g.Key] = g
	}
	l.rebuild(prev)
	return nil
}

// Reset forgets the filter, the selection and the scroll position and marks the first entry; the fold state
// and the ticks stay.
func (l *List) Reset() {
	l.query, l.searching, l.offset = nil, false, 0
	l.rebuild(listMark{})
	l.cursor = l.firstEntry()
}

// SetFocused says whether the rows have the focus, which decides what a click on the marked row does. The
// application sets it when the focus enters or leaves the rows.
func (l *List) SetFocused(on bool) { l.focused = on }

// SetGrouped shows the entries under their groups or as one flat list.
func (l *List) SetGrouped(on bool) {
	prev := l.mark()
	l.grouped = on
	l.rebuild(prev)
}

// SetFilter sets the filter text without typing it.
func (l *List) SetFilter(query string) {
	prev := l.mark()
	l.query = []rune(query)
	l.rebuild(prev)
}

// Query is the filter text.
func (l *List) Query() string { return string(l.query) }

// Searching reports whether a filter is being typed after the search key.
func (l *List) Searching() bool { return l.searching }

// Input is the input focus the list reads keys in: text while a filter is typed or the list filters as
// typed, navigation otherwise.
func (l *List) Input() InputFocus {
	if l.searching || l.spec.Search == SearchAsTyped {
		return InputText
	}
	return InputNavigation
}

// Selected is the marked entry; a group row or an empty list mark none.
func (l *List) Selected() (ItemID, bool) {
	m := l.mark()
	return m.id, m.ok && !m.isGroup && !m.all
}

// SelectedGroup is the key of the marked group row, empty for the group without key; ok is false on an entry,
// on the row of all and in an empty list.
func (l *List) SelectedGroup() (key string, ok bool) {
	m := l.mark()
	if !m.ok || !m.isGroup {
		return "", false
	}
	return m.group, true
}

// Current is the marked entry with its cells, for a panel that explains it; a group row, the row of all and
// an empty list mark none.
func (l *List) Current() (ListItem, bool) {
	id, ok := l.Selected()
	if !ok {
		return ListItem{}, false
	}
	for _, it := range l.items {
		if it.ID == id {
			it.Cells = slices.Clone(it.Cells)
			return it, true
		}
	}
	return ListItem{}, false
}

// Ticked is the IDs of the ticked entries in the order of the entries, shown or not.
func (l *List) Ticked() []ItemID {
	var ids []ItemID
	for _, it := range l.items {
		if l.ticked[it.ID] {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// SetTicked replaces the ticks. It rejects an ID that names no entry and then changes nothing. A partial
// selection a group remembers is forgotten.
func (l *List) SetTicked(ids []ItemID) error {
	known := make(map[ItemID]bool, len(l.items))
	for _, it := range l.items {
		known[it.ID] = true
	}
	var errs []error
	for _, id := range ids {
		if !known[id] {
			errs = append(errs, fmt.Errorf("entry %q is unknown", Escape(string(id))))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	clear(l.ticked)
	for _, id := range ids {
		l.ticked[id] = true
	}
	clear(l.memory)
	return nil
}

// Select marks the entry id, unfolding its group where it is folded, and reports whether it shows.
func (l *List) Select(id ItemID) bool {
	for _, it := range l.items {
		if it.ID == id && l.grouped && l.folded[it.Group] && len(l.query) == 0 {
			delete(l.folded, it.Group)
			l.rebuild(l.mark())
		}
	}
	return l.reselect(listMark{id: id, ok: true})
}

// Position says where the selection stands among the entries, group rows not counted: "pos/total", or with
// a filter "pos/shown (N total)"; empty for an empty list without filter.
func (l *List) Position() string {
	shown, pos := 0, 0
	for i, r := range l.rows {
		if r.item < 0 {
			continue
		}
		shown++
		if i <= l.cursor {
			pos++
		}
	}
	// A group row counts as the entry above it, at least the first.
	pos = max(pos, min(shown, 1))
	switch {
	case l.searching || len(l.query) > 0:
		return l.text.Lookup(TextListFiltered, pos, shown, len(l.items))
	case len(l.items) > 0:
		return fmt.Sprintf("%d/%d", pos, len(l.items))
	}
	return ""
}

// Update handles a key. open is the entry whose enter key was pressed, which the application opens as it
// opens any entry; handled is false for a key the list leaves to the application, such as up from the first
// row of a list that stops, which may lead to the zone above, and back without a filter.
func (l *List) Update(msg tea.KeyPressMsg) (open ItemID, handled bool) {
	focus := l.Input()
	k := l.spec.Keys
	for _, id := range []ActionID{k.Back, k.Enter, k.Search, k.Up, k.Down, k.PageUp, k.PageDown, k.First, k.Last, k.Left, k.Right, k.Toggle} {
		at := focus
		if id == k.Toggle && l.spec.TickWhileTyping {
			at = InputNavigation
		}
		if id != "" && l.keys.Is(msg, id, at) {
			return l.Act(id)
		}
	}
	if focus != InputText {
		return "", false
	}
	before := len(l.query)
	switch {
	case msg.Code == tea.KeyBackspace && msg.Mod == 0:
		if len(l.query) > 0 {
			l.query = l.query[:len(l.query)-1]
		}
	case msg.Text != "" && msg.Mod&^tea.ModShift == 0:
		for _, r := range msg.Text {
			if unicode.IsPrint(r) {
				l.query = append(l.query, r)
			}
		}
	default:
		return "", false
	}
	if len(l.query) != before {
		l.rebuild(l.mark())
	}
	return "", true
}

// Act runs one of the list keys as its key does, for a click or a command that stands for the key.
func (l *List) Act(id ActionID) (open ItemID, handled bool) {
	k := l.spec.Keys
	n := len(l.rows)
	if id == "" {
		return "", false
	}
	switch id {
	case k.Back:
		if !l.searching && len(l.query) == 0 {
			return "", false
		}
		l.searching = false
		l.SetFilter("")
	case k.Enter:
		if l.searching {
			l.searching = false
			return "", true
		}
		if l.cursor >= n {
			return "", true
		}
		r := l.rows[l.cursor]
		if r.item >= 0 {
			return l.items[r.item].ID, true
		}
		if l.folds() && !r.all {
			l.setFolded(r.group, !l.folded[r.group])
		}
	case k.Search:
		if l.spec.Search == SearchByKey {
			l.searching = true
		}
	case k.Up:
		switch {
		case l.cursor > 0:
			l.cursor--
		case l.spec.Wrap && n > 0:
			l.cursor = n - 1
		case !l.searching:
			return "", false
		}
	case k.Down:
		switch {
		case l.cursor < n-1:
			l.cursor++
		case l.spec.Wrap:
			l.cursor = 0
		}
	case k.PageUp:
		l.jump(-max(l.page-1, 1))
	case k.PageDown:
		l.jump(max(l.page-1, 1))
	case k.First:
		l.cursor = 0
	case k.Last:
		l.cursor = max(n-1, 0)
	case k.Left, k.Right:
		if !l.folds() {
			return "", false
		}
		if l.cursor >= n {
			return "", true
		}
		r := l.rows[l.cursor]
		if r.all {
			return "", true
		}
		group := r.group
		if r.item >= 0 {
			group = l.items[r.item].Group
		}
		switch {
		case id == k.Left && !l.folded[group]:
			l.setFolded(group, true)
		case id == k.Right && r.item < 0 && l.folded[group]:
			l.setFolded(group, false)
		}
	case k.Toggle:
		if !l.spec.Ticks {
			return "", false
		}
		l.toggleRow()
	default:
		return "", false
	}
	return "", true
}

// Click handles a click on a target the list drew, as the MouseMap of its targets reports it. A click marks a
// row; only a click on the marked row while the rows have the focus, as SetFocused said before the click,
// opens it, as enter does, so a click meant to scroll or to take the focus opens nothing. A click on a group
// row folds or unfolds it, and one on the search line starts the search. A filter being typed is kept and
// stops. A click on a tick box ticks or unticks its entry, group or the row of all and opens and folds
// nothing; a click on the title of a group only folds it. A click on a button acts as its key does through
// Act, so an action that is no list key comes back with handled false, and the application runs the Action of
// the MouseResult as it runs the key.
func (l *List) Click(r MouseResult) (open ItemID, handled bool) {
	if !r.Handled {
		return "", false
	}
	if j, ok := strings.CutPrefix(r.Item, listButtonRef); ok {
		n, err := strconv.Atoi(j)
		if err != nil || n < 0 || n >= len(l.spec.Buttons) {
			return "", false
		}
		return l.Act(l.spec.Buttons[n].Action)
	}
	if r.Item == "" {
		if r.Action != "" && r.Action == l.spec.Keys.Search {
			return l.Act(r.Action)
		}
		return "", false
	}
	item, tick := strings.CutPrefix(r.Item, tickRef)
	if tick && !l.spec.Ticks {
		return "", false
	}
	target := listMark{id: ItemID(item), ok: true}
	if group, ok := strings.CutPrefix(item, groupRef); ok {
		target = listMark{group: group, isGroup: true, ok: true}
	} else if item == allRef {
		target = listMark{all: true, ok: true}
	}
	marked := l.mark()
	if !l.reselect(target) {
		return "", false
	}
	switch {
	case l.searching:
		l.searching = false
		if tick {
			l.toggleRow()
		}
	case tick:
		l.toggleRow()
	case target.isGroup && l.folds():
		l.setFolded(target.group, !l.folded[target.group])
	case !target.isGroup && !target.all && l.focused && marked == target:
		return target.id, true
	}
	return "", true
}

// Wheel moves the selection by the notches of the wheel and stops at the ends.
func (l *List) Wheel(steps int) { l.cursor = WheelStep(l.cursor, len(l.rows), steps) }

// View draws the list with styles in width by height cells: the search line where the list has one, the
// dimmed column titles, and the rows that fit, the selection always among them; an empty list shows its
// sentence, a filter without match names the text and the key that clears it. The scroll position stays while
// the selection is on the page. It returns a target for the free area of the rows, one per drawn row and one
// for the search line, counted from the top left of the list.
func (l *List) View(styles Styles, width, height int) (string, []Target) {
	return l.view(styles, width, height, false)
}

// view is View; modal keeps the search line of a list that filters as typed also where the search field
// stands in a bar, which the modal does not draw.
func (l *List) view(styles Styles, width, height int, modal bool) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if height == 0 {
		return "", nil
	}
	targets := []Target{{Rect: Rect{Width: width, Height: height}, Zone: ZoneRows}}
	var lines []string
	if l.searchLine() || modal && l.spec.Search == SearchAsTyped {
		if l.spec.Keys.Search != "" {
			targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 1}, Zone: ZoneRows, Action: l.spec.Keys.Search})
		}
		lines = append(lines, l.searchField(width, styles))
	}
	indent := ""
	if l.grouped {
		indent = groupIndent
	}
	tick := 0
	if l.spec.Ticks {
		tick = tickWidth
	}
	room := max(width-2-len(indent)-tick, 0)
	layout := l.layout(room, styles)
	switch {
	case len(l.items) == 0:
		lines = append(lines, l.note(l.text.Lookup(l.spec.Empty), width, styles)...)
	default:
		lines = append(lines, "  "+indent+strings.Repeat(" ", tick)+styles.Render(StyleHint, l.header(layout, styles)))
		if len(l.rows) == 0 {
			lines = append(lines, l.note(l.text.Lookup(TextListNoMatch, string(l.query), l.keys.Label(l.spec.Keys.Back)), width, styles)...)
		}
	}
	start, end := l.window(height - len(lines))
	l.page = max(end-start, 1)
	for i := start; i < end; i++ {
		r := l.rows[i]
		ref, text, boxAt := "", "", 0
		switch {
		case r.all:
			box := l.box(tickScope{all: true}, styles)
			ref, text, boxAt = allRef, indent+box+" "+l.allRow(styles), len(indent)
		case r.item < 0:
			ref, text, boxAt = groupRef+r.group, l.groupLine(r.group, layout, styles), lipgloss.Width(styles.Glyph(GlyphExpanded))+1
		default:
			it := l.items[r.item]
			ref, text, boxAt = string(it.ID), indent, len(indent)
			if l.spec.Ticks {
				text += styles.TickBox(l.ticked[it.ID]) + " "
			}
			text += l.line(it, layout, styles)
		}
		targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 1}, Zone: ZoneRows,
			Action: l.spec.Keys.Enter, Item: ref})
		if l.spec.Ticks {
			// The box is drawn after the row, so it lies on top of it and a click on it never folds or opens.
			targets = append(targets, Target{Rect: Rect{X: 2 + boxAt, Y: len(lines), Width: 3, Height: 1}, Zone: ZoneRows,
				Action: l.spec.Keys.Toggle, Item: tickRef + ref})
		}
		lines = append(lines, styles.Row(text, width, i == l.cursor))
	}
	out := make([]string, height)
	for i := range out {
		if i < len(lines) {
			out[i] = Pad(Clip(lines[i], width), width)
		} else {
			out[i] = strings.Repeat(" ", width)
		}
	}
	kept := targets[:0]
	for _, t := range targets {
		if t.Rect.Y < height {
			kept = append(kept, t)
		}
	}
	return strings.Join(out, "\n"), kept
}

func (l *List) searchLine() bool {
	return !l.spec.SearchInBar && (l.spec.Search == SearchAsTyped || l.searching || len(l.query) > 0)
}

// searchField draws the filter in width cells: after the search key while it is typed or kept, with a
// cursor while it is typed, and the dimmed placeholder while it is empty.
func (l *List) searchField(width int, styles Styles) string {
	typing := l.searching || l.spec.Search == SearchAsTyped
	lead := ""
	if l.spec.Search == SearchByKey {
		lead = Escape(l.keys.Label(l.spec.Keys.Search)) + " "
	}
	if len(l.query) == 0 && !l.searching {
		hint := l.text.Lookup(TextSearchPrompt)
		if l.spec.Search == SearchByKey {
			hint = l.text.Lookup(TextSearchKey, l.keys.Label(l.spec.Keys.Search))
		}
		return Pad(styles.Render(StyleHint, styles.Truncate(Escape(hint), width)), width)
	}
	cursor := ""
	if typing {
		cursor = "_"
		if !styles.profile.Colorless() || styles.profile == ProfileMono {
			cursor = styles.Render(StyleActiveRow, " ")
		}
	}
	room := max(width-lipgloss.Width(lead)-lipgloss.Width(cursor), 0)
	return Pad(Clip(lead+styles.TruncateLeft(Escape(string(l.query)), room)+cursor, width), width)
}

// note is a dimmed sentence wrapped into width cells.
func (l *List) note(text string, width int, styles Styles) []string {
	lines := Wrap(Escape(text), max(width-2, 1))
	for i, line := range lines {
		lines[i] = "  " + styles.Render(StyleHint, line)
	}
	return lines
}

// allRow is how the row of all reads: the title in the accent colour and, dimmed, the number of shown entries.
func (l *List) allRow(styles Styles) string {
	n := len(l.members(tickScope{all: true}))
	return styles.Render(StyleActive, Escape(l.text.Lookup(TextListAll))) + styles.Render(StyleHint, fmt.Sprintf(" (%d)", n))
}

// box is the box of a scope: all shown entries ticked, some, or none. The characters alone tell the three
// apart.
func (l *List) box(s tickScope, styles Styles) string {
	idx := l.members(s)
	on := 0
	for _, i := range idx {
		if l.ticked[l.items[i].ID] {
			on++
		}
	}
	switch {
	case on == 0:
		return "[ ]"
	case on == len(idx):
		return styles.TickBox(true)
	}
	return styles.Render(StyleActive, "[-]")
}

// groupRow is how a group row reads: the fold glyph, the tick box where the list has them, the category mark,
// the title in the accent colour and, dimmed, the number of its entries.
func (l *List) groupRow(group string, styles Styles) string {
	glyph := GlyphExpanded
	if l.folded[group] && len(l.query) == 0 {
		glyph = GlyphCollapsed
	}
	title := l.groupTitle(group)
	count := 0
	for _, it := range l.items {
		if it.Group == group {
			count++
		}
	}
	box := ""
	if l.spec.Ticks {
		box = l.box(tickScope{group: group}, styles) + " "
	}
	return styles.Render(StyleHint, styles.Glyph(glyph)) + " " + box + styles.CategoryMark(group) + " " +
		styles.Render(StyleActive, Escape(title)) + styles.Render(StyleHint, fmt.Sprintf(" (%d)", count))
}

// groupTitle is what a group row shows as its title: the described title, else the key, and for the group
// without key the text of the list.
func (l *List) groupTitle(group string) string {
	if group == "" {
		id := l.spec.NoGroup
		if id == "" {
			id = TextListNoGroup
		}
		return l.text.Lookup(id)
	}
	if g, ok := l.groups[group]; ok && g.Title != "" {
		return g.Title
	}
	return group
}

func (l *List) folds() bool { return l.grouped && len(l.query) == 0 }

func (l *List) setFolded(group string, folded bool) {
	if folded {
		l.folded[group] = true
	} else {
		delete(l.folded, group)
	}
	l.rebuild(listMark{group: group, isGroup: true, ok: true})
}

func (l *List) jump(by int) { l.cursor = min(max(l.cursor+by, 0), max(len(l.rows)-1, 0)) }

func (l *List) mark() listMark {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return listMark{}
	}
	r := l.rows[l.cursor]
	switch {
	case r.all:
		return listMark{all: true, ok: true}
	case r.item < 0:
		return listMark{group: r.group, isGroup: true, ok: true}
	}
	return listMark{id: l.items[r.item].ID, ok: true}
}

// reselect marks the row of m and reports whether it shows.
func (l *List) reselect(m listMark) bool {
	if !m.ok {
		return false
	}
	for i, r := range l.rows {
		if (m.all && r.all) || (m.isGroup && r.item < 0 && !r.all && r.group == m.group) ||
			(!m.isGroup && !m.all && r.item >= 0 && l.items[r.item].ID == m.id) {
			l.cursor = i
			return true
		}
	}
	return false
}

func (l *List) firstEntry() int {
	for i, r := range l.rows {
		if r.item >= 0 {
			return i
		}
	}
	return 0
}

// rebuild recomputes the shown rows and marks prev where it still shows. Without a filter a grouped list
// shows every group row and the entries of the open groups; with one, every entry that matches under its
// group, folded or not, and the group rows of those entries. The fold state itself stays.
func (l *List) rebuild(prev listMark) {
	query := l.queryKey()
	matches := l.matcher()
	l.rows = l.rows[:0]
	if l.spec.Ticks && l.spec.AllRow && len(l.members(tickScope{all: true})) > 0 {
		l.rows = append(l.rows, listRow{item: -1, all: true})
	}
	if !l.grouped {
		for i, it := range l.items {
			if matches(it) {
				l.rows = append(l.rows, listRow{item: i})
			}
		}
	} else {
		members := map[string][]int{}
		var groups []string
		for i, it := range l.items {
			if _, ok := members[it.Group]; !ok {
				groups = append(groups, it.Group)
			}
			members[it.Group] = append(members[it.Group], i)
		}
		// Groups go in alphabetical order of their shown title, the group without key last.
		slices.SortFunc(groups, func(a, b string) int {
			if (a == "") != (b == "") {
				if a == "" {
					return 1
				}
				return -1
			}
			if c := strings.Compare(l.groupTitle(a), l.groupTitle(b)); c != 0 {
				return c
			}
			return strings.Compare(a, b)
		})
		for _, g := range groups {
			var hits []listRow
			for _, i := range members[g] {
				if (query == "" && !l.folded[g]) || (query != "" && matches(l.items[i])) {
					hits = append(hits, listRow{item: i})
				}
			}
			if query == "" || len(hits) > 0 {
				l.rows = append(l.rows, listRow{item: -1, group: g})
				l.rows = append(l.rows, hits...)
			}
		}
	}
	if !l.reselect(prev) {
		l.cursor = min(max(l.cursor, 0), max(len(l.rows)-1, 0))
	}
}

// queryKey is the filter as the entries are matched against it.
func (l *List) queryKey() string { return strings.ToLower(strings.TrimSpace(string(l.query))) }

// matcher reports whether an entry passes the filter.
func (l *List) matcher() func(ListItem) bool {
	query := l.queryKey()
	return func(it ListItem) bool {
		if query == "" {
			return true
		}
		text := strings.ToLower(strings.Join(it.Cells, " ") + " " + it.SearchText)
		return strings.Contains(text, query)
	}
}

// members are the indexes of the entries a scope covers: those that pass the filter, whether their group is
// folded or not, so ticking acts on what the filter shows and never on what it hides.
func (l *List) members(s tickScope) []int {
	matches := l.matcher()
	var idx []int
	for i, it := range l.items {
		if (s.all || it.Group == s.group) && matches(it) {
			idx = append(idx, i)
		}
	}
	return idx
}

// toggleRow ticks what the marked row stands for.
func (l *List) toggleRow() {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return
	}
	switch r := l.rows[l.cursor]; {
	case r.all:
		l.toggleScope(tickScope{all: true})
	case r.item < 0:
		l.toggleScope(tickScope{group: r.group})
	default:
		it := l.items[r.item]
		l.ticked[it.ID] = !l.ticked[it.ID]
		if !l.ticked[it.ID] {
			delete(l.ticked, it.ID)
		}
		// A single toggle makes the remembered partial selections of its group and of all stale.
		delete(l.memory, tickScope{group: it.Group})
		delete(l.memory, tickScope{all: true})
	}
}

// toggleScope steps the shown entries of s through partial selection, all, none and back to the partial
// selection, which it remembers at the first step. Without a partial selection it alternates between all and
// none. The memory belongs to the filter it was made under.
func (l *List) toggleScope(s tickScope) {
	idx := l.members(s)
	if len(idx) == 0 {
		return
	}
	var on []ItemID
	for _, i := range idx {
		if l.ticked[l.items[i].ID] {
			on = append(on, l.items[i].ID)
		}
	}
	set := func(ids ...ItemID) {
		for _, i := range idx {
			delete(l.ticked, l.items[i].ID)
		}
		for _, id := range ids {
			l.ticked[id] = true
		}
	}
	mem, remembered := l.memory[s]
	remembered = remembered && mem.query == l.queryKey()
	all := make([]ItemID, len(idx))
	for j, i := range idx {
		all[j] = l.items[i].ID
	}
	switch {
	case len(on) > 0 && len(on) < len(idx):
		l.memory[s] = tickMemory{ids: on, query: l.queryKey()}
		set(all...)
	case len(on) == len(idx):
		set()
	case remembered:
		set(mem.ids...)
	default:
		set(all...)
	}
	// Scopes overlap only between all and a group: the memory of the other side is stale now.
	if s.all {
		for k := range l.memory {
			if k != s {
				delete(l.memory, k)
			}
		}
	} else {
		delete(l.memory, tickScope{all: true})
	}
}

// window is the range of rows that fits into room rows. The scroll position stays while it shows the
// selection; a selection outside it comes in at the nearest edge, and left-over room is filled from above.
func (l *List) window(room int) (start, end int) {
	n := len(l.rows)
	if n == 0 || room <= 0 {
		return 0, 0
	}
	start = min(max(l.offset, 0), max(n-room, 0))
	switch {
	case l.cursor < start:
		start = l.cursor
	case l.cursor >= start+room:
		start = l.cursor - room + 1
	}
	l.offset = start
	return start, min(start+room, n)
}

// tableLayout is how a table fits a width: the width of every column, 0 for a column left out, and whether
// a column shows its short labels.
type tableLayout struct {
	widths []int
	short  []bool
}

// cell is what column i shows of a row's cells, escaped; a missing cell of a group is empty.
func (l *List) cell(cells []string, i int, short bool) string {
	v := ""
	if i < len(cells) {
		v = cells[i]
	}
	if s, ok := l.spec.Columns[i].Short[v]; ok && short {
		v = s
	}
	return Escape(v)
}

// layout fits the table into width cells. The widths come from every entry, the filtered-out ones included,
// so the columns keep their width while a filter is typed. When a row does not fit, the free text gives way
// first, down to flexFloor; then a column with short labels shows them; then the optional columns are left
// out from the right; and only then are the remaining values cut, the widest first, never below the width
// of their title. The first column is never cut. The free text gets whatever room is left. A described group
// with entries counts like an entry: its cells in their columns and its head, beyond the room before the
// first column, in the first.
func (l *List) layout(width int, styles Styles) tableLayout {
	cols := l.spec.Columns
	n := len(cols)
	lay := tableLayout{widths: make([]int, n), short: make([]bool, n)}
	title := func(i int) int { return lipgloss.Width(l.titles[i]) }
	described := l.describedGroups()
	natural := func(i int) int {
		w := title(i)
		for _, it := range l.items {
			w = max(w, lipgloss.Width(l.cell(it.Cells, i, lay.short[i])))
		}
		for _, g := range described {
			if i == 0 {
				w = max(w, lipgloss.Width(l.groupRow(g.Key, styles))-l.lead())
			} else {
				w = max(w, lipgloss.Width(l.cell(g.Cells, i, lay.short[i])))
			}
		}
		return w
	}
	flex, flexWidth := -1, 0
	for i, c := range cols {
		lay.widths[i] = natural(i)
		if c.Flex {
			flex, flexWidth = i, lay.widths[i]
			lay.widths[i] = min(flexWidth, max(title(i), flexFloor))
		}
	}
	for i, c := range cols {
		if c.Short != nil && i != flex && tableWidth(lay.widths) > width {
			lay.short[i] = true
			lay.widths[i] = natural(i)
		}
	}
	for i := n - 1; i > 0 && tableWidth(lay.widths) > width; i-- {
		if cols[i].Optional {
			lay.widths[i] = 0
		}
	}
	for tableWidth(lay.widths) > width {
		widest := -1
		for i := 1; i < n; i++ {
			if i != flex && lay.widths[i] > title(i) && (widest < 0 || lay.widths[i] >= lay.widths[widest]) {
				widest = i
			}
		}
		if widest < 0 && flex >= 0 && lay.widths[flex] > title(flex) {
			widest = flex
		}
		if widest < 0 {
			break
		}
		lay.widths[widest]--
	}
	if flex >= 0 && lay.widths[flex] > 0 {
		lay.widths[flex] = min(flexWidth, lay.widths[flex]+max(width-tableWidth(lay.widths), 0))
	}
	return lay
}

// tableWidth is how wide a line of columns of the given widths is, the gaps between them included.
func tableWidth(widths []int) int {
	total, shown := 0, 0
	for _, w := range widths {
		if w > 0 {
			total += w
			shown++
		}
	}
	return total + len(columnGap)*max(shown-1, 0)
}

// header is the line of column titles, aligned with the rows.
func (l *List) header(lay tableLayout, styles Styles) string {
	var parts []string
	for i, c := range l.spec.Columns {
		if w := lay.widths[i]; w > 0 {
			parts = append(parts, align(styles.Truncate(l.titles[i], w), w, c.Right))
		}
	}
	return strings.TrimRight(strings.Join(parts, columnGap), " ")
}

// lead is the room before the first column: the indent of a grouped list and the tick box.
func (l *List) lead() int {
	n := 0
	if l.grouped {
		n = len(groupIndent)
	}
	if l.spec.Ticks {
		n += tickWidth
	}
	return n
}

// describedGroups are the groups with a description that have entries, in no order.
func (l *List) describedGroups() []ListGroup {
	if !l.grouped || len(l.groups) == 0 {
		return nil
	}
	has := map[string]bool{}
	for _, it := range l.items {
		has[it.Group] = true
	}
	var out []ListGroup
	for k, g := range l.groups {
		if has[k] {
			out = append(out, g)
		}
	}
	return out
}

// line draws the cells of an entry into the columns, each cut to its width and marked where it is cut.
func (l *List) line(it ListItem, lay tableLayout, styles Styles) string {
	return strings.TrimRight(strings.Join(l.columns(it.Cells, 0, lay, styles), columnGap), " ")
}

// groupLine draws a group row: the group head padded to the first column and, flush with the entries, the
// cells of the group from the second column on. The head is never cut.
func (l *List) groupLine(group string, lay tableLayout, styles Styles) string {
	head := l.groupRow(group, styles)
	g, ok := l.groups[group]
	if !ok || len(g.Cells) == 0 {
		return head
	}
	rest := l.columns(g.Cells, 1, lay, styles)
	if len(rest) == 0 {
		return head
	}
	head += strings.Repeat(" ", max(l.lead()+lay.widths[0]-lipgloss.Width(head), 0))
	return strings.TrimRight(head+columnGap+strings.Join(rest, columnGap), " ")
}

// columns draws the cells of one row into the shown columns from column from on.
func (l *List) columns(cells []string, from int, lay tableLayout, styles Styles) []string {
	var parts []string
	for i := from; i < len(l.spec.Columns); i++ {
		c := l.spec.Columns[i]
		w := lay.widths[i]
		if w <= 0 {
			continue
		}
		text := styles.Truncate(l.cell(cells, i, lay.short[i]), w)
		style := StyleText
		switch {
		case c.Tone != nil:
			raw := ""
			if i < len(cells) {
				raw = cells[i]
			}
			style = c.Tone(raw)
		case c.Dimmed:
			style = StyleHint
		}
		if text != "" {
			pad := w - lipgloss.Width(text)
			text = styles.Render(style, text)
			if c.Right {
				text = strings.Repeat(" ", pad) + text
			} else {
				text += strings.Repeat(" ", pad)
			}
		} else {
			text = strings.Repeat(" ", w)
		}
		parts = append(parts, text)
	}
	return parts
}

func align(text string, width int, right bool) string {
	if right {
		return strings.Repeat(" ", max(width-lipgloss.Width(text), 0)) + text
	}
	return Pad(text, width)
}

// Hints are the keys of the list for the footer and the key line of the modal: the buttons first, then enter,
// toggle on a list with tick boxes, the search key, back and the moves. An action of a button stands once, as
// the button.
func (l *List) Hints() []Hint {
	k := l.spec.Keys
	var hints []Hint
	for _, b := range l.spec.Buttons {
		hints = append(hints, Hint{Action: b.Action, Label: b.Label})
	}
	own := []Hint{{Action: k.Enter, Label: TextKeyChoose}}
	if l.spec.Ticks {
		own = append(own, Hint{Action: k.Toggle, Label: TextKeyToggle, WhileTyping: l.spec.TickWhileTyping})
	}
	if l.spec.Search == SearchByKey {
		own = append(own, Hint{Action: k.Search, Label: TextKeySearch})
	}
	own = append(own, Hint{Action: k.Back, Label: TextKeyBack},
		Hint{Action: k.Up, Label: TextKeyUp, Kind: HintNavigation}, Hint{Action: k.Down, Label: TextKeyDown, Kind: HintNavigation})
	for _, h := range own {
		if !slices.ContainsFunc(l.spec.Buttons, func(b FormButton) bool { return b.Action == h.Action }) {
			hints = append(hints, h)
		}
	}
	return hints
}

// buttonRows draws the buttons in rows of at most width cells, each behind the key now in force, the main one
// bold, with a target per button, counted from the top left of the rows.
func (l *List) buttonRows(width int, styles Styles) ([]string, []Target) {
	var cells []buttonCell
	for j, b := range l.spec.Buttons {
		kind := ButtonNormal
		if b.Primary {
			kind = ButtonPrimary
		}
		label := l.text.Lookup(b.Label)
		if key := l.keys.Label(b.Action); key != "" {
			label = key + " " + label
		}
		cells = append(cells, buttonCell{styles.Button(kind, label, false),
			Target{Zone: ZoneRows, Action: b.Action, Item: listButtonRef + strconv.Itoa(j)}})
	}
	return flowButtons(cells, width)
}

// wanted is the height the list asks for in the modal: the search line, the titles and every row without a
// filter, so the frame keeps its size while a filter is typed.
func (l *List) wanted() int {
	rows := len(l.items)
	if l.grouped {
		groups := map[string]bool{}
		for _, it := range l.items {
			groups[it.Group] = true
		}
		rows += len(groups)
	}
	if l.spec.AllRow {
		rows++
	}
	return 1 + 1 + max(rows, 2)
}

// Overlay draws the list with styles over screen, the drawn screen of width by height cells, in the way of
// Dialog.Overlay: the screen dimmed and without its colours, and in its frame under ListSpec.Title the search
// line, the list, the buttons and the key line of Hints that no button stands for, centred in room, the part
// of the screen it may cover. The list scrolls in the height that is left and keeps the marked row in view.
// The targets are the modal's alone, so the screen behind it takes no click. ok is false where room is too
// small for the search line, the titles, one row, the buttons and the key line; the application then draws
// View in the workspace and Hints in the footer.
func (l *List) Overlay(styles Styles, screen string, width, height int, room Rect) (view string, targets []Target, ok bool) {
	inner := min(dialogMaxInner, room.Width-8)
	if inner < dialogMinInner {
		return "", nil, false
	}
	var rows []string
	var bt []Target
	if len(l.spec.Buttons) > 0 {
		rows, bt = l.buttonRows(inner, styles)
	}
	// The gap above the buttons, the gap above the key line and the key line, or the gap and the key line.
	fixed := 2
	if len(rows) > 0 {
		fixed += len(rows) + 1
	}
	avail := room.Height - 2 - fixed
	listH := min(l.wanted(), avail)
	if listH < 3 {
		return "", nil, false
	}
	body, ts := l.view(styles, inner, listH, true)
	lines := append(strings.Split(body, "\n"), "")
	if len(rows) > 0 {
		ts = append(ts, OffsetTargets(bt, 0, len(lines))...)
		lines = append(append(lines, rows...), "")
	}
	var hints []Hint
	for _, h := range l.Hints() {
		if !slices.ContainsFunc(l.spec.Buttons, func(b FormButton) bool { return b.Action == h.Action }) {
			hints = append(hints, h)
		}
	}
	keyLine, kt := styles.footer(l.keys.Hints(hints, l.Input(), l.text), "", inner)
	ts = append(ts, OffsetTargets(kt, 0, len(lines))...)
	lines = append(lines, keyLine)
	for i, line := range lines {
		lines[i] = Pad(Clip(line, inner), inner)
	}
	title := ""
	if l.spec.Title != "" {
		title = l.text.Lookup(l.spec.Title)
	}
	box := styles.Panel(title, "", lines, inner+4, len(lines)+2, true)
	top := room.Y + (room.Height-len(box))/2
	left := room.X + (room.Width-inner-4)/2
	return overlay(screen, box, width, height, top, left, styles), OffsetTargets(ts, left+2, top+1), true
}

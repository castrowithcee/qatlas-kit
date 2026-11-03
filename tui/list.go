package tui

import (
	"errors"
	"fmt"
	"slices"
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
}

// flexFloor is the narrowest a free text column is cut to before anything else of the row gives way.
const flexFloor = 12

// columnGap is the room between two columns.
const columnGap = "  "

// groupRef starts the target item of a group row; item IDs hold no control character, so it never names one.
const groupRef = "\x00g"

// listRow is one shown row: an entry by its index, or the row of group with item -1.
type listRow struct {
	item  int
	group string
}

// listMark is the selected row by what it shows, so the selection follows an entry, not a position.
type listMark struct {
	id      ItemID
	group   string
	isGroup bool
	ok      bool
}

// ListModel is a list of entries with selection, filter, groups and scroll position. The filter decides only
// which entries show: it never removes one and never changes their order. The selection follows the ID of an
// entry when the entries, the filter or the grouping change.
type ListModel struct {
	spec   ListSpec
	keys   *Keymap
	text   Translator
	titles []string

	items   []ListItem
	grouped bool
	folded  map[string]bool

	rows      []listRow
	cursor    int
	offset    int
	page      int
	query     []rune
	searching bool
}

// NewList checks spec and returns an empty list. It rejects a list without columns, a column without title,
// a first column that is free text or optional, more than one free text column, a list without the text of
// its empty state, and a key that names no action of keys or that two list keys share; only a list that
// filters as typed may go without the search key.
func NewList(spec ListSpec, keys *Keymap, text Translator) (*ListModel, error) {
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
	k := spec.Keys
	named := []struct {
		name string
		id   ActionID
	}{{"up", k.Up}, {"down", k.Down}, {"page up", k.PageUp}, {"page down", k.PageDown}, {"first", k.First},
		{"last", k.Last}, {"left", k.Left}, {"right", k.Right}, {"enter", k.Enter}, {"search", k.Search}, {"back", k.Back}}
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
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	spec.Columns = slices.Clone(spec.Columns)
	l := &ListModel{spec: spec, keys: keys, text: text, folded: map[string]bool{}, page: 1}
	for _, c := range spec.Columns {
		l.titles = append(l.titles, strings.ToUpper(Escape(text.Lookup(c.Title))))
	}
	return l, nil
}

// SetItems replaces the entries and keeps the filter, the fold state and, where it still shows, the selected
// entry. It rejects an entry without ID or with a control character in it, an ID listed twice and an entry
// whose cells do not match the columns, and then changes nothing.
func (l *ListModel) SetItems(items []ListItem) error {
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
	l.rebuild(prev)
	if !prev.ok {
		l.cursor = l.firstEntry()
	}
	return nil
}

// Reset forgets the filter, the selection and the scroll position and marks the first entry; the fold state
// stays.
func (l *ListModel) Reset() {
	l.query, l.searching, l.offset = nil, false, 0
	l.rebuild(listMark{})
	l.cursor = l.firstEntry()
}

// SetGrouped shows the entries under their groups or as one flat list.
func (l *ListModel) SetGrouped(on bool) {
	prev := l.mark()
	l.grouped = on
	l.rebuild(prev)
}

// SetFilter sets the filter text without typing it.
func (l *ListModel) SetFilter(query string) {
	prev := l.mark()
	l.query = []rune(query)
	l.rebuild(prev)
}

// Query is the filter text.
func (l *ListModel) Query() string { return string(l.query) }

// Searching reports whether a filter is being typed after the search key.
func (l *ListModel) Searching() bool { return l.searching }

// Input is the input focus the list reads keys in: text while a filter is typed or the list filters as
// typed, navigation otherwise.
func (l *ListModel) Input() InputFocus {
	if l.searching || l.spec.Search == SearchAsTyped {
		return InputText
	}
	return InputNavigation
}

// Selected is the marked entry; a group row or an empty list mark none.
func (l *ListModel) Selected() (ItemID, bool) {
	m := l.mark()
	return m.id, m.ok && !m.isGroup
}

// Select marks the entry id, unfolding its group where it is folded, and reports whether it shows.
func (l *ListModel) Select(id ItemID) bool {
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
func (l *ListModel) Position() string {
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
func (l *ListModel) Update(msg tea.KeyPressMsg) (open ItemID, handled bool) {
	focus := l.Input()
	k := l.spec.Keys
	for _, id := range []ActionID{k.Back, k.Enter, k.Search, k.Up, k.Down, k.PageUp, k.PageDown, k.First, k.Last, k.Left, k.Right} {
		if id != "" && l.keys.Is(msg, id, focus) {
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
func (l *ListModel) Act(id ActionID) (open ItemID, handled bool) {
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
		if l.folds() {
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
	default:
		return "", false
	}
	return "", true
}

// Click handles a click on a target the list drew, as the MouseMap of its targets reports it. focused says
// whether the rows had the focus before the click. A click marks a row; only a click on the marked row while
// the rows have the focus opens it, as enter does, so a click meant to scroll or to take the focus opens
// nothing. A click on a group row folds or unfolds it, and one on the search line starts the search. A filter
// being typed is kept and stops.
func (l *ListModel) Click(r MouseResult, focused bool) (open ItemID, handled bool) {
	if !r.Handled {
		return "", false
	}
	if r.Item == "" {
		if r.Action != "" && r.Action == l.spec.Keys.Search {
			return l.Act(r.Action)
		}
		return "", false
	}
	target := listMark{id: ItemID(r.Item), ok: true}
	if group, ok := strings.CutPrefix(r.Item, groupRef); ok {
		target = listMark{group: group, isGroup: true, ok: true}
	}
	marked := l.mark()
	if !l.reselect(target) {
		return "", false
	}
	switch {
	case l.searching:
		l.searching = false
	case target.isGroup && l.folds():
		l.setFolded(target.group, !l.folded[target.group])
	case !target.isGroup && focused && marked == target:
		return target.id, true
	}
	return "", true
}

// Wheel moves the selection by the notches of the wheel and stops at the ends.
func (l *ListModel) Wheel(steps int) { l.cursor = WheelStep(l.cursor, len(l.rows), steps) }

// View draws the list in width by height cells: the search line where the list has one, the dimmed column
// titles, and the rows that fit, the selection always among them; an empty list shows its sentence, a filter
// without match names the text and the key that clears it. The scroll position stays while the selection is
// on the page. It returns a target for the free area of the rows, one per drawn row and one for the search
// line, counted from the top left of the list.
func (l *ListModel) View(width, height int, styles Styles) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if height == 0 {
		return "", nil
	}
	targets := []Target{{Rect: Rect{Width: width, Height: height}, Zone: ZoneRows}}
	var lines []string
	if l.searchLine() {
		if l.spec.Keys.Search != "" {
			targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 1}, Zone: ZoneRows, Action: l.spec.Keys.Search})
		}
		lines = append(lines, l.searchField(width, styles))
	}
	indent := ""
	if l.grouped {
		indent = "  "
	}
	room := max(width-2-len(indent), 0)
	layout := l.layout(room)
	switch {
	case len(l.items) == 0:
		lines = append(lines, l.note(l.text.Lookup(l.spec.Empty), width, styles)...)
	default:
		lines = append(lines, "  "+indent+styles.Render(StyleHint, l.header(layout, styles)))
		if len(l.rows) == 0 {
			lines = append(lines, l.note(l.text.Lookup(TextListNoMatch, string(l.query), l.keys.Label(l.spec.Keys.Back)), width, styles)...)
		}
	}
	start, end := l.window(height - len(lines))
	l.page = max(end-start, 1)
	for i := start; i < end; i++ {
		r := l.rows[i]
		ref, text := "", ""
		if r.item < 0 {
			ref, text = groupRef+r.group, l.groupRow(r.group, styles)
		} else {
			ref, text = string(l.items[r.item].ID), indent+l.line(l.items[r.item], layout, styles)
		}
		targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 1}, Zone: ZoneRows,
			Action: l.spec.Keys.Enter, Item: ref})
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

func (l *ListModel) searchLine() bool {
	return !l.spec.SearchInBar && (l.spec.Search == SearchAsTyped || l.searching || len(l.query) > 0)
}

// searchField draws the filter in width cells: after the search key while it is typed or kept, with a
// cursor while it is typed, and the dimmed placeholder while it is empty.
func (l *ListModel) searchField(width int, styles Styles) string {
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
func (l *ListModel) note(text string, width int, styles Styles) []string {
	lines := Wrap(Escape(text), max(width-2, 1))
	for i, line := range lines {
		lines[i] = "  " + styles.Render(StyleHint, line)
	}
	return lines
}

// groupRow is how a group row reads: the fold glyph, the category mark, the title in the accent colour and,
// dimmed, the number of its entries.
func (l *ListModel) groupRow(group string, styles Styles) string {
	glyph := GlyphExpanded
	if l.folded[group] && len(l.query) == 0 {
		glyph = GlyphCollapsed
	}
	title := group
	if group == "" {
		id := l.spec.NoGroup
		if id == "" {
			id = TextListNoGroup
		}
		title = l.text.Lookup(id)
	}
	count := 0
	for _, it := range l.items {
		if it.Group == group {
			count++
		}
	}
	return styles.Render(StyleHint, styles.Glyph(glyph)) + " " + styles.CategoryMark(group) + " " +
		styles.Render(StyleActive, Escape(title)) + styles.Render(StyleHint, fmt.Sprintf(" (%d)", count))
}

func (l *ListModel) folds() bool { return l.grouped && len(l.query) == 0 }

func (l *ListModel) setFolded(group string, folded bool) {
	if folded {
		l.folded[group] = true
	} else {
		delete(l.folded, group)
	}
	l.rebuild(listMark{group: group, isGroup: true, ok: true})
}

func (l *ListModel) jump(by int) { l.cursor = min(max(l.cursor+by, 0), max(len(l.rows)-1, 0)) }

func (l *ListModel) mark() listMark {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		return listMark{}
	}
	r := l.rows[l.cursor]
	if r.item < 0 {
		return listMark{group: r.group, isGroup: true, ok: true}
	}
	return listMark{id: l.items[r.item].ID, ok: true}
}

// reselect marks the row of m and reports whether it shows.
func (l *ListModel) reselect(m listMark) bool {
	if !m.ok {
		return false
	}
	for i, r := range l.rows {
		if (m.isGroup && r.item < 0 && r.group == m.group) || (!m.isGroup && r.item >= 0 && l.items[r.item].ID == m.id) {
			l.cursor = i
			return true
		}
	}
	return false
}

func (l *ListModel) firstEntry() int {
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
func (l *ListModel) rebuild(prev listMark) {
	query := strings.ToLower(strings.TrimSpace(string(l.query)))
	matches := func(it ListItem) bool {
		if query == "" {
			return true
		}
		text := strings.ToLower(strings.Join(it.Cells, " ") + " " + it.SearchText)
		return strings.Contains(text, query)
	}
	l.rows = l.rows[:0]
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
		// Groups go in alphabetical order, the group without key last.
		slices.SortFunc(groups, func(a, b string) int {
			if (a == "") != (b == "") {
				if a == "" {
					return 1
				}
				return -1
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

// window is the range of rows that fits into room rows. The scroll position stays while it shows the
// selection; a selection outside it comes in at the nearest edge, and left-over room is filled from above.
func (l *ListModel) window(room int) (start, end int) {
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

// cell is what column i shows of an entry, escaped.
func (l *ListModel) cell(it ListItem, i int, short bool) string {
	v := it.Cells[i]
	if s, ok := l.spec.Columns[i].Short[v]; ok && short {
		v = s
	}
	return Escape(v)
}

// layout fits the table into width cells. The widths come from every entry, the filtered-out ones included,
// so the columns keep their width while a filter is typed. When a row does not fit, the free text gives way
// first, down to flexFloor; then a column with short labels shows them; then the optional columns are left
// out from the right; and only then are the remaining values cut, the widest first, never below the width
// of their title. The first column is never cut. The free text gets whatever room is left.
func (l *ListModel) layout(width int) tableLayout {
	cols := l.spec.Columns
	n := len(cols)
	lay := tableLayout{widths: make([]int, n), short: make([]bool, n)}
	title := func(i int) int { return lipgloss.Width(l.titles[i]) }
	natural := func(i int) int {
		w := title(i)
		for _, it := range l.items {
			w = max(w, lipgloss.Width(l.cell(it, i, lay.short[i])))
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
		if cols[i].Optional && i != flex {
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
	if flex >= 0 {
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
func (l *ListModel) header(lay tableLayout, styles Styles) string {
	var parts []string
	for i, c := range l.spec.Columns {
		if w := lay.widths[i]; w > 0 {
			parts = append(parts, align(styles.Truncate(l.titles[i], w), w, c.Right))
		}
	}
	return strings.TrimRight(strings.Join(parts, columnGap), " ")
}

// line draws the cells of an entry into the columns, each cut to its width and marked where it is cut.
func (l *ListModel) line(it ListItem, lay tableLayout, styles Styles) string {
	var parts []string
	for i, c := range l.spec.Columns {
		w := lay.widths[i]
		if w <= 0 {
			continue
		}
		text := styles.Truncate(l.cell(it, i, lay.short[i]), w)
		style := StyleText
		switch {
		case c.Tone != nil:
			style = c.Tone(it.Cells[i])
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
	return strings.TrimRight(strings.Join(parts, columnGap), " ")
}

func align(text string, width int, right bool) string {
	if right {
		return strings.Repeat(" ", max(width-lipgloss.Width(text), 0)) + text
	}
	return Pad(text, width)
}

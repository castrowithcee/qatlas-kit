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

// FilterField is one field of a filter bar: a choice field, such as the grouping, a state or the sort order,
// or an action field. A choice field has at least one value; SetOptions replaces them later. Selected is the
// ID of the value in force; empty takes the first. An action field has an Action and no Options: it shows
// Text, which SetActionText replaces, and reports its action when it is pressed, for the application to run,
// as with a dialog for a date. It never counts as a changed filter. A field has Options or an Action, not both.
type FilterField struct {
	ID       FieldID
	Label    TextID
	Options  []Choice
	Selected ChoiceID
	Action   ActionID
	Text     string
}

// FilterBarKeys are the actions of the key table a filter bar reads: Left and Right change the value of the
// field, Next and Prev step through the fields, Enter opens the list of values and takes one, Up and Down
// move in it, PageUp and PageDown move it by a page, text typed there searches the values, and Back closes it
// without change. PageUp and PageDown are optional; empty leaves a page to no key. In the filter modal Up and
// Down choose the field, Left and Right change its value, Enter takes all changes and Back drops them.
type FilterBarKeys struct {
	Left, Right, Next, Prev, Up, Down, Enter, Back ActionID
	PageUp, PageDown                               ActionID
}

// FilterBarSpec is what a filter bar holds and the keys it reads.
type FilterBarSpec struct {
	Fields []FilterField
	Keys   FilterBarKeys
	// List is the list the bar stands above. Where it draws its search in the bar, its search field stands at
	// the right end of the bar; nil is none.
	List *List
}

// The target items of a filter bar start with a control character, which no field or value ID holds.
const (
	fieldRef      = "\x00f"
	optionRef     = "\x00o"
	buttonRef     = "\x00b"
	modalFieldRef = "\x00mf"
	modalValueRef = "\x00mv"
)

// The width of the search field at the right end of the bar.
const (
	searchFieldMin = 14
	searchFieldMax = 30
)

// FilterBar is the bar of choice fields above a list. It changes only what the list shows and in which
// order: the application applies the values to the entries it hands to the list, never to its data. It holds
// the values, the focused field, the open list of values and the open filter modal with its pending values.
type FilterBar struct {
	fields []FilterField
	nav    FilterBarKeys
	list   *List
	keys   *Keymap
	text   Translator

	focus  int
	open   bool
	choice int
	// query is the search text of the open list of values, scroll the first value it shows among the matches
	// and page the number of values it showed last, which a page key moves by.
	query  textLine
	scroll int
	page   int
	// pending is the action of an action field pressed and not yet taken.
	pending ActionID

	// compact is set by View while the fields do not fit and a button stands for them.
	compact bool
	modal   bool
	draft   []int
	mfocus  int
}

// NewFilterBar checks spec and returns a bar without focus. It rejects a bar without fields, a field
// or value without ID or label, an ID with a control character or listed twice, a field without a value, a
// selected value that is no value of its field, a field with both values and an action or neither, an action
// that names no action of keys, text on a choice field, and a key that names no action of keys or is named
// twice.
func NewFilterBar(spec FilterBarSpec, keys *Keymap, text Translator) (*FilterBar, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the filter bar needs a key table and a translator")
	}
	fields, nav := spec.Fields, spec.Keys
	var errs []error
	used := map[ActionID]bool{}
	named := []ActionID{nav.Left, nav.Right, nav.Next, nav.Prev, nav.Up, nav.Down, nav.Enter, nav.Back}
	for _, id := range []ActionID{nav.PageUp, nav.PageDown} {
		if id != "" {
			named = append(named, id)
		}
	}
	for _, id := range named {
		if !keys.known(id) {
			errs = append(errs, fmt.Errorf("filter bar key %q is no action of the key table", id))
		}
		if used[id] {
			errs = append(errs, fmt.Errorf("filter bar key %q is named twice", id))
		}
		used[id] = true
	}
	if len(fields) == 0 {
		errs = append(errs, errors.New("the filter bar has no field"))
	}
	badID := func(id string) bool { return id == "" || strings.IndexFunc(id, unicode.IsControl) >= 0 }
	seen := map[FieldID]bool{}
	list := make([]FilterField, len(fields))
	for i, f := range fields {
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("filter field %q: "+format, append([]any{f.ID}, args...)...))
		}
		switch {
		case badID(string(f.ID)):
			fail("an ID is not empty and holds no control character")
		case seen[f.ID]:
			fail("is listed twice")
		}
		seen[f.ID] = true
		if f.Label == "" {
			fail("no label")
		}
		if f.Action != "" {
			if !keys.known(f.Action) {
				fail("action %q is no action of the key table", f.Action)
			}
			if len(f.Options) > 0 || f.Selected != "" {
				fail("has an action and values; a field has one of them")
			}
			f.Options = nil
			list[i] = f
			continue
		}
		if f.Text != "" {
			fail("text belongs to an action field")
		}
		values := map[ChoiceID]bool{}
		for _, o := range f.Options {
			values[o.ID] = true
		}
		for _, p := range optionProblems(f.Options) {
			fail("%s", p)
		}
		f.Options = append([]Choice(nil), f.Options...)
		switch {
		case f.Selected == "" && len(f.Options) > 0:
			f.Selected = f.Options[0].ID
		case f.Selected != "" && !values[f.Selected]:
			fail("selected value %q is no value of the field", f.Selected)
		}
		list[i] = f
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &FilterBar{fields: list, nav: nav, list: spec.List, keys: keys, text: text, focus: -1}, nil
}

// optionProblems lists what is wrong with the values of a field: none at all, an ID that is empty, holds a
// control character or is listed twice, and a missing label.
func optionProblems(options []Choice) []string {
	if len(options) == 0 {
		return []string{"no value; a field has at least one"}
	}
	var out []string
	seen := map[ChoiceID]bool{}
	for _, o := range options {
		switch {
		case o.ID == "" || strings.IndexFunc(string(o.ID), unicode.IsControl) >= 0:
			out = append(out, fmt.Sprintf("value %q: an ID is not empty and holds no control character", o.ID))
		case seen[o.ID]:
			out = append(out, fmt.Sprintf("value %q is listed twice", o.ID))
		}
		seen[o.ID] = true
		if p := o.problem(); p != "" {
			out = append(out, fmt.Sprintf("value %q %s", o.ID, p))
		}
	}
	return out
}

// SetOptions replaces the values of the field id with options and checks them as NewFilterBar does. The value
// in force, the cursor of an open list of values and the pending value of an open modal stay on their IDs, in
// whatever place the new values put them; the focus stays on its field. A call that leaves out the value in
// force or a pending value of the modal is rejected, as is any other broken input, and changes nothing: the
// application keeps the current value in its list, so no filter changes unseen.
func (b *FilterBar) SetOptions(id FieldID, options []Choice) error {
	i := b.field(id)
	if i < 0 {
		return fmt.Errorf("filter field %q is unknown", id)
	}
	if problems := optionProblems(options); len(problems) > 0 {
		return fmt.Errorf("filter field %q: %s", id, strings.Join(problems, "; "))
	}
	at := func(c ChoiceID) int {
		for j, o := range options {
			if o.ID == c {
				return j
			}
		}
		return -1
	}
	f := &b.fields[i]
	if f.Action != "" {
		return fmt.Errorf("filter field %q is an action field and has no values", id)
	}
	if at(f.Selected) < 0 {
		return fmt.Errorf("filter field %q: the value in force %q is no value of the new list", id, f.Selected)
	}
	draft := -1
	if b.modal {
		draft = at(f.Options[b.draft[i]].ID)
		if draft < 0 {
			return fmt.Errorf("filter field %q: the pending value %q is no value of the new list", id, f.Options[b.draft[i]].ID)
		}
	}
	cursor := at(f.Selected)
	if b.open && b.focus == i {
		if c := at(f.Options[b.choice].ID); c >= 0 {
			cursor = c
		}
		b.choice = cursor
	}
	if b.modal {
		b.draft[i] = draft
	}
	f.Options = append([]Choice(nil), options...)
	if b.open && b.focus == i {
		if m := b.matches(); len(m) > 0 && !slices.Contains(m, b.choice) {
			b.choice = m[0]
		}
	}
	return nil
}

// SetActionText replaces the text of the action field id. It rejects an unknown field and a choice field. The
// text is drawn masked, so it may hold any characters.
func (b *FilterBar) SetActionText(id FieldID, text string) error {
	i := b.field(id)
	if i < 0 {
		return fmt.Errorf("filter field %q is unknown", id)
	}
	if b.fields[i].Action == "" {
		return fmt.Errorf("filter field %q is no action field", id)
	}
	b.fields[i].Text = text
	return nil
}

// TakeAction returns the action of an action field the user pressed with enter, left, right or a click, and
// true once after each press, and then false until the next; the application runs it. The bar runs nothing.
// Where the field was pressed in the filter modal, the modal has taken its pending changes first, as its
// confirmation does, and Update or Click has reported them.
func (b *FilterBar) TakeAction() (ActionID, bool) {
	if b.pending == "" {
		return "", false
	}
	a := b.pending
	b.pending = ""
	return a, true
}

// Fields are the fields with the values in force.
func (b *FilterBar) Fields() []FilterField {
	out := make([]FilterField, len(b.fields))
	for i, f := range b.fields {
		f.Options = append([]Choice(nil), f.Options...)
		out[i] = f
	}
	return out
}

// Value is the ID of the value in force of the field id, empty for an unknown field.
func (b *FilterBar) Value(id FieldID) ChoiceID {
	for _, f := range b.fields {
		if f.ID == id {
			return f.Selected
		}
	}
	return ""
}

// Focus gives the focus to the first field, or to the last one where fromEnd, as tab and shift+tab enter the
// bar.
func (b *FilterBar) Focus(fromEnd bool) {
	b.focus, b.open, b.modal = 0, false, false
	if fromEnd {
		b.focus = len(b.fields) - 1
	}
}

// Blur takes the focus from the bar and closes its list of values and its modal, dropping pending changes;
// the application calls it when the focus leaves the bar.
func (b *FilterBar) Blur() { b.focus, b.open, b.modal = -1, false, false }

// Focused reports whether a field of the bar has the focus.
func (b *FilterBar) Focused() bool { return b.focus >= 0 }

// Open reports whether the list of values of the focused field or the filter modal is open; either takes
// every key.
func (b *FilterBar) Open() bool { return b.open || b.modal }

// Update handles a key while the bar has the focus. changed holds the fields whose value the key changed, in
// the order of the bar, nil where none did; handled is false for a key the bar leaves to the application: tab
// past the last field or shift+tab before the first, which go on to the next zone, and up, down and back on a
// closed field. Where the fields give way to the filter button, it is one stop: tab and shift+tab leave the
// bar at once, left and right are left to the application, and enter opens the modal. While the list of
// values or the modal is open it takes every key.
func (b *FilterBar) Update(msg tea.KeyPressMsg) (changed []FilterField, handled bool) {
	if b.focus < 0 {
		return nil, false
	}
	is := func(id ActionID) bool { return b.keys.Is(msg, id, InputNavigation) }
	if b.modal {
		return b.updateModal(is), true
	}
	f := &b.fields[b.focus]
	if b.open {
		return b.updateList(msg), true
	}
	if b.compact {
		if is(b.nav.Enter) {
			b.openModal()
			return nil, true
		}
		return nil, false
	}
	switch {
	case f.Action != "" && (is(b.nav.Left) || is(b.nav.Right) || is(b.nav.Enter)):
		b.pending = f.Action
	case is(b.nav.Left), is(b.nav.Right):
		step := 1
		if is(b.nav.Left) {
			step = -1
		}
		return b.choose(b.focus, wrap(b.selected(b.focus)+step, len(f.Options))), true
	case is(b.nav.Next):
		if b.focus == len(b.fields)-1 {
			return nil, false
		}
		b.focus++
	case is(b.nav.Prev):
		if b.focus == 0 {
			return nil, false
		}
		b.focus--
	case is(b.nav.Enter):
		b.openList(b.focus)
	default:
		return nil, false
	}
	return nil, true
}

// Click handles a click on a target the bar drew, as the MouseMap of its targets reports it: a click on a
// field focuses it and opens its list of values, as enter does, a second one closes it, and a click on a
// value of the open list takes it. A click on the filter button opens the modal; in the modal a click on a
// field focuses it, one on its value takes the next value, and one on a key of the key line acts as the key.
// A click on an action field focuses it and reports its action, as enter does; in the modal a click on its
// value does, after the pending changes are taken. A click on the search field is the search key of the list;
// List.Click runs it.
func (b *FilterBar) Click(r MouseResult) (changed []FilterField, handled bool) {
	if !r.Handled {
		return nil, false
	}
	if b.modal {
		return b.clickModal(r)
	}
	if r.Item == buttonRef {
		if !b.compact {
			return nil, false
		}
		b.focus = max(b.focus, 0)
		b.openModal()
		return nil, true
	}
	if id, ok := strings.CutPrefix(r.Item, fieldRef); ok {
		i := b.field(FieldID(id))
		if i < 0 {
			return nil, false
		}
		if f := b.fields[i]; f.Action != "" {
			b.focus, b.open, b.pending = i, false, f.Action
			return nil, true
		}
		if b.open && b.focus == i {
			b.open = false
			return nil, true
		}
		b.focus = i
		b.openList(i)
		return nil, true
	}
	if rest, ok := strings.CutPrefix(r.Item, optionRef); ok {
		id, value, _ := strings.Cut(rest, "\x00")
		i := b.field(FieldID(id))
		if i < 0 || !b.open || b.focus != i {
			return nil, false
		}
		for j, o := range b.fields[i].Options {
			if string(o.ID) == value {
				return b.choose(i, j), true
			}
		}
	}
	return nil, false
}

// openList opens the list of values of field i with an empty search and the cursor on the value in force.
func (b *FilterBar) openList(i int) {
	b.open, b.choice, b.scroll = true, b.selected(i), 0
	b.query.clear()
}

// matches are the places of the values of the focused field whose shown text holds the search text, in any
// case, in their order.
func (b *FilterBar) matches() []int {
	f := b.fields[b.focus]
	needle := strings.ToLower(b.query.String())
	var out []int
	for j, o := range f.Options {
		if needle == "" || strings.Contains(strings.ToLower(Escape(o.shown(b.text))), needle) {
			out = append(out, j)
		}
	}
	return out
}

// updateList handles a key while the list of values is open: the cursor keys move among the matches, enter
// takes the value under the cursor, back closes without change, and any other key edits the search text.
func (b *FilterBar) updateList(msg tea.KeyPressMsg) []FilterField {
	is := func(id ActionID) bool { return id != "" && b.keys.Is(msg, id, InputText) }
	match := b.matches()
	at := slices.Index(match, b.choice)
	page := b.page
	if page <= 0 {
		page = maxValueRows
	}
	move := func(to int) {
		if len(match) > 0 {
			b.choice = match[max(min(to, len(match)-1), 0)]
		}
	}
	switch {
	case is(b.nav.Up):
		move(max(at, 1) - 1)
	case is(b.nav.Down):
		move(at + 1)
	case is(b.nav.PageUp):
		move(max(at, 0) - page)
	case is(b.nav.PageDown):
		move(max(at, 0) + page)
	case is(b.nav.Enter):
		if at < 0 {
			return nil
		}
		return b.choose(b.focus, b.choice)
	case is(b.nav.Back):
		b.open = false
	default:
		if _, changed := b.query.edit(msg, false); changed {
			if match = b.matches(); !slices.Contains(match, b.choice) && len(match) > 0 {
				b.choice = match[0]
			}
		}
	}
	return nil
}

func (b *FilterBar) openModal() {
	b.open, b.modal, b.mfocus = false, true, 0
	b.draft = make([]int, len(b.fields))
	for i := range b.fields {
		b.draft[i] = b.selected(i)
	}
}

// updateModal handles a key in the modal; it takes every key.
func (b *FilterBar) updateModal(is func(ActionID) bool) []FilterField {
	switch {
	case is(b.nav.Up):
		b.mfocus = max(b.mfocus-1, 0)
	case is(b.nav.Down):
		b.mfocus = min(b.mfocus+1, len(b.fields)-1)
	case is(b.nav.Left), is(b.nav.Right):
		if b.fields[b.mfocus].Action != "" {
			return b.fire(b.mfocus)
		}
		if is(b.nav.Left) {
			b.step(b.mfocus, -1)
		} else {
			b.step(b.mfocus, 1)
		}
	case is(b.nav.Enter):
		if b.fields[b.mfocus].Action != "" {
			return b.fire(b.mfocus)
		}
		return b.apply()
	case is(b.nav.Back):
		b.modal = false
	}
	return nil
}

func (b *FilterBar) step(i, by int) {
	b.draft[i] = wrap(b.draft[i]+by, len(b.fields[i].Options))
}

func (b *FilterBar) clickModal(r MouseResult) ([]FilterField, bool) {
	if id, ok := strings.CutPrefix(r.Item, modalFieldRef); ok {
		if i := b.field(FieldID(id)); i >= 0 {
			b.mfocus = i
			return nil, true
		}
		return nil, false
	}
	if id, ok := strings.CutPrefix(r.Item, modalValueRef); ok {
		if i := b.field(FieldID(id)); i >= 0 {
			b.mfocus = i
			if b.fields[i].Action != "" {
				return b.fire(i), true
			}
			b.step(i, 1)
			return nil, true
		}
		return nil, false
	}
	if r.Item != "" || r.Action == "" {
		return nil, false
	}
	// A key of the key line, drawn in the modal or in the footer, runs the way its key does.
	for _, key := range []ActionID{b.nav.Enter, b.nav.Back, b.nav.Up, b.nav.Down, b.nav.Left, b.nav.Right} {
		if r.Action == key {
			return b.updateModal(func(id ActionID) bool { return id == key }), true
		}
	}
	return nil, false
}

// fire takes every pending value of the modal as apply does and then reports the action of field i, so no
// pending change is lost; it returns the fields whose value changed.
func (b *FilterBar) fire(i int) []FilterField {
	changed := b.apply()
	b.pending = b.fields[i].Action
	return changed
}

// apply takes every pending value of the modal and closes it; it returns the fields whose value changed.
func (b *FilterBar) apply() []FilterField {
	b.modal = false
	var changed []FilterField
	for i := range b.fields {
		if b.fields[i].Action != "" || b.draft[i] == b.selected(i) {
			continue
		}
		f := &b.fields[i]
		f.Selected = f.Options[b.draft[i]].ID
		out := *f
		out.Options = append([]Choice(nil), f.Options...)
		changed = append(changed, out)
	}
	return changed
}

func (b *FilterBar) field(id FieldID) int {
	for i, f := range b.fields {
		if f.ID == id {
			return i
		}
	}
	return -1
}

func (b *FilterBar) selected(i int) int {
	for j, o := range b.fields[i].Options {
		if o.ID == b.fields[i].Selected {
			return j
		}
	}
	return 0
}

// choose gives field i its value j and closes the list; it returns the field where its value changed.
func (b *FilterBar) choose(i, j int) []FilterField {
	b.open = false
	f := &b.fields[i]
	if f.Options[j].ID == f.Selected {
		return nil
	}
	f.Selected = f.Options[j].ID
	out := *f
	out.Options = append([]Choice(nil), f.Options...)
	return []FilterField{out}
}

// differing is the number of choice fields whose value is not the first one of the field; an action field
// never counts.
func (b *FilterBar) differing() int {
	n := 0
	for _, f := range b.fields {
		if f.Action == "" && f.Selected != f.Options[0].ID {
			n++
		}
	}
	return n
}

func (b *FilterBar) label(i int) (label, value string) {
	f := b.fields[i]
	if f.Action != "" {
		return Escape(b.text.Lookup(f.Label)), Escape(f.Text)
	}
	return Escape(b.text.Lookup(f.Label)), Escape(f.Options[b.selected(i)].shown(b.text))
}

// fieldText is a field of at most width cells, " label: value ▾ ", the value cut where it does not fit.
func fieldText(styles Styles, label, value string, width int) string {
	head, tail := fieldFrame(styles, label)
	return head + styles.Truncate(value, max(width-lipgloss.Width(head)-lipgloss.Width(tail), 1)) + tail
}

func fieldFrame(styles Styles, label string) (head, tail string) {
	return " " + label + ": ", " " + styles.Glyph(GlyphExpanded) + " "
}

// fieldLeast is the narrowest a field is drawn: its frame and one cell of value. Planning and drawing share it,
// as a field cut to less would be drawn wider than planned.
func fieldLeast(styles Styles, label string) int {
	head, tail := fieldFrame(styles, label)
	return lipgloss.Width(head) + lipgloss.Width(tail) + 1
}

// fieldStyle is the surface of a field, bold and in the accent colour while it has the focus. Without colour
// it names nothing; the marker before the field carries the focus then.
func fieldStyle(styles Styles, focused bool) lipgloss.Style {
	style := lipgloss.NewStyle()
	if styles.profile == ProfileNone || styles.profile > ProfileTrueColor {
		return style
	}
	p, colored := styles.palette, !styles.profile.Colorless()
	if colored && p.Surface != "" {
		style = style.Background(lipgloss.Color(string(p.Surface)))
		if p.Text != "" {
			style = style.Foreground(lipgloss.Color(string(p.Text)))
		}
	}
	if focused {
		style = style.Bold(true)
		if colored && p.Accent != "" {
			style = style.Foreground(lipgloss.Color(string(p.Accent)))
		}
	}
	return style
}

func barMarker(focused bool) string {
	if focused {
		return "> "
	}
	return "  "
}

// View draws the bar on one line of width cells: the fields as choice fields "label: value", the focused one
// behind the marker ">", and the search field of the list of the spec at the right end where the list draws
// its search in the bar. Where the fields do not all fit, their values are cut; where even that is not
// enough, a button stands for all fields with the number of those not at their first value, the search field
// at its right and, where that has no room, only the search key. The button never goes, and is cut to the
// width at last. It returns a target for the free area of the bar, one per drawn field or the button, and one
// for the search field or key, counted from the left of the bar.
func (b *FilterBar) View(styles Styles, width int) (string, []Target) {
	width = max(width, 0)
	list := b.list
	n := len(b.fields)
	labels, values := make([]string, n), make([]string, n)
	for i := range b.fields {
		labels[i], values[i] = b.label(i)
	}
	targets := []Target{{Rect: Rect{Width: width, Height: 1}, Zone: ZoneFilters}}
	search := list != nil && list.spec.SearchInBar
	idle := !search || (!list.searching && len(list.query.runes) == 0)
	const gap = 2
	natural := func(i int) int { return lipgloss.Width(fieldText(styles, labels[i], values[i], 1<<20)) }
	least := func(i int) int { return fieldLeast(styles, labels[i]) }
	fit := func(budget int) ([]int, int, bool) {
		widths, total := make([]int, n), gap*(n-1)
		for i := range widths {
			widths[i] = natural(i)
			total += 2 + widths[i]
		}
		for total > budget {
			widest := -1
			for i := range widths {
				if widths[i] > least(i) && (widest < 0 || widths[i] > widths[widest]) {
					widest = i
				}
			}
			if widest < 0 {
				return nil, 0, false
			}
			widths[widest]--
			total--
		}
		return widths, total, true
	}
	searchField := func(x, w int) string {
		targets = append(targets, Target{Rect: Rect{X: x, Width: w, Height: 1}, Zone: ZoneFilters, Action: list.spec.Keys.Search})
		return list.searchField(w, styles)
	}
	draw := func(widths []int, used, searchWidth int) string {
		var line strings.Builder
		x := 0
		for i := range b.fields {
			if i > 0 {
				line.WriteString(strings.Repeat(" ", gap))
				x += gap
			}
			text := fieldText(styles, labels[i], values[i], widths[i])
			line.WriteString(barMarker(i == b.focus) + fieldStyle(styles, i == b.focus).Render(text))
			w := 2 + lipgloss.Width(text)
			action := b.nav.Enter
			if a := b.fields[i].Action; a != "" {
				action = a
			}
			targets = append(targets, Target{Rect: Rect{X: x, Width: w, Height: 1}, Zone: ZoneFilters,
				Action: action, Item: fieldRef + string(b.fields[i].ID)})
			x += w
		}
		if searchWidth > 0 {
			line.WriteString(strings.Repeat(" ", width-used-searchWidth) + searchField(width-searchWidth, searchWidth))
		}
		return line.String()
	}
	b.compact = false
	var line string
	if widths, used, ok := fit(width - gap - searchFieldMin); search && ok {
		line = draw(widths, used, min(searchFieldMax, width-used-gap))
	} else if widths, used, ok := fit(width); ok && idle {
		line = draw(widths, used, 0)
	} else {
		// The list of values hangs under a field that is no longer drawn.
		b.compact, b.open = true, false
		text := b.text.Lookup(TextFilterButton, b.differing())
		button := styles.Button(ButtonNormal, styles.Truncate(text, max(width-4, 1)), b.focus >= 0)
		bw := lipgloss.Width(button)
		if r, ok := clipRect(Rect{Width: bw, Height: 1}, width); ok {
			targets = append(targets, Target{Rect: r, Zone: ZoneFilters, Action: b.nav.Enter, Item: buttonRef})
		}
		line = button
		if rest := width - bw - gap; search && rest > 0 {
			sw, key := 0, ""
			if list.spec.Search == SearchByKey && list.spec.Keys.Search != "" {
				key = Escape(list.keys.Label(list.spec.Keys.Search))
			}
			switch {
			case rest >= searchFieldMin || !idle:
				sw = min(searchFieldMax, rest)
				line += strings.Repeat(" ", width-bw-sw) + searchField(width-sw, sw)
			case key != "" && lipgloss.Width(key) <= rest:
				sw = lipgloss.Width(key)
				targets = append(targets, Target{Rect: Rect{X: width - sw, Width: sw, Height: 1}, Zone: ZoneFilters, Action: list.spec.Keys.Search})
				line += strings.Repeat(" ", width-bw-sw) + styles.Render(StyleHint, key)
			}
		}
	}
	return Pad(Clip(line, width), width), targets
}

// The most values the open list shows at once; it scrolls beyond.
const maxValueRows = 8

// ValueList draws the open list of values of the focused field as a panel to place under the field: the
// label as its title, the position of the cursor among the matches at the right of the top edge, the search
// line as the first row and below it at most eight values, which scroll with the cursor in view. The value in
// force carries a check mark, which shows without colour, the value under the cursor is marked as a selected
// row, and a search without match shows a sentence with the way out in place of the values. It is empty while
// no list is open and never larger than width by height cells, the room the application leaves for it. It
// returns one target per drawn value, counted from the top left of the panel.
func (b *FilterBar) ValueList(styles Styles, width, height int) (string, []Target) {
	if !b.open || b.focus < 0 || width < 5 || height < 3 {
		return "", nil
	}
	f := b.fields[b.focus]
	title := b.text.Lookup(f.Label)
	match := b.matches()
	noteOf := func(pos, shown int) string {
		if len(b.query.runes) > 0 {
			return b.text.Lookup(TextListFiltered, pos, shown, len(f.Options))
		}
		return fmt.Sprintf("%d/%d", pos, len(f.Options))
	}
	mark, blank := styles.Glyph(GlyphOK)+" ", strings.Repeat(" ", lipgloss.Width(styles.Glyph(GlyphOK)+" "))
	inner := lipgloss.Width(Escape(title)) + lipgloss.Width(noteOf(len(match), len(match))) + 6
	labels := make([]string, len(f.Options))
	for j, o := range f.Options {
		labels[j] = Escape(o.shown(b.text))
		inner = max(inner, lipgloss.Width(labels[j])+2+lipgloss.Width(blank))
	}
	prompt := Escape(b.text.Lookup(TextSearchPrompt))
	inner = max(inner, lipgloss.Width(prompt)+2)
	inner = max(min(inner, width-4), 1)
	// The rows below the search line: the values, or the wrapped sentence of a search without match.
	var sentence []string
	rows := min(len(match), maxValueRows, height-3)
	if len(match) == 0 {
		back := b.keys.Label(b.nav.Back)
		sentence = Wrap(Escape(b.text.Lookup(TextFilterNoValue, b.query.String(), back)), max(inner-2, 1))
		rows = min(len(sentence), maxValueRows, height-3)
	}
	rows = max(rows, 0)
	b.page = max(rows, 1)
	at := max(slices.Index(match, b.choice), 0)
	b.scroll = max(min(b.scroll, at, len(match)-rows), 0)
	if rows > 0 && at >= b.scroll+rows {
		b.scroll = at - rows + 1
	}
	query := Pad(Clip("  "+b.query.view(max(inner-2, 0), styles, true, ""), inner), inner)
	if len(b.query.runes) == 0 {
		query = Pad("  "+styles.Render(StyleHint, styles.Truncate(prompt, max(inner-2, 0))), inner)
	}
	lines := []string{query}
	var targets []Target
	for k := 0; k < rows; k++ {
		if len(match) == 0 {
			lines = append(lines, Pad("  "+styles.Render(StyleHint, sentence[k]), inner))
			continue
		}
		j := match[b.scroll+k]
		lead := blank
		if f.Options[j].ID == f.Selected {
			lead = mark
		}
		lines = append(lines, styles.Row(lead+labels[j], inner, j == b.choice))
		targets = append(targets, Target{Rect: Rect{X: 2, Y: 2 + k, Width: inner, Height: 1}, Zone: ZoneFilters,
			Action: b.nav.Enter, Item: optionRef + string(f.ID) + "\x00" + string(f.Options[j].ID)})
	}
	pos := at + 1
	if len(match) == 0 {
		pos = 0
	}
	box := styles.Panel(title, noteOf(pos, len(match)), lines, inner+4, len(lines)+2, true)
	return strings.Join(box, "\n"), targets
}

// Hints are the keys of the open modal for the key line: the answers, which stand in the modal itself where it
// is drawn over the screen, and the moves. Where the modal does not fit, the application puts them into the
// footer, whose targets then act on it. Empty while the modal is closed.
func (b *FilterBar) Hints() []Hint {
	if !b.modal {
		return nil
	}
	n := b.nav
	return []Hint{{Action: n.Enter, Label: TextFilterApply, Kind: HintAnswer}, {Action: n.Back, Label: TextKeyDiscard, Kind: HintAnswer},
		{Action: n.Up, Label: TextKeyUp, Kind: HintNavigation}, {Action: n.Down, Label: TextKeyDown, Kind: HintNavigation},
		{Action: n.Left, Label: TextKeyLeft, Kind: HintNavigation}, {Action: n.Right, Label: TextKeyRight, Kind: HintNavigation}}
}

// modalRows draws one row per field in width cells, label and pending value under a marker for the field in
// focus, and returns two targets per row, counted from the top left of the rows: the label, which focuses the
// field, and the value, which takes the next one.
func (b *FilterBar) modalRows(styles Styles, width int) ([]string, []Target) {
	n := len(b.fields)
	labels, values := make([]string, n), make([]string, n)
	lw := 0
	for i, f := range b.fields {
		labels[i], values[i] = b.label(i)
		if f.Action == "" {
			values[i] = Escape(f.Options[b.draft[i]].shown(b.text))
		}
		lw = max(lw, lipgloss.Width(labels[i]))
	}
	rows := make([]string, n)
	var targets []Target
	for i, f := range b.fields {
		label := Pad(labels[i], lw)
		head, _ := fieldFrame(styles, label)
		text := Pad(fieldText(styles, label, values[i], max(width-2, fieldLeast(styles, label))), max(width-2, 0))
		rows[i] = barMarker(i == b.mfocus) + fieldStyle(styles, i == b.mfocus).Render(text)
		split := 2 + lipgloss.Width(head)
		if r, ok := clipRect(Rect{Y: i, Width: split, Height: 1}, width); ok {
			targets = append(targets, Target{Rect: r, Zone: ZoneFilters, Item: modalFieldRef + string(f.ID)})
		}
		if r, ok := clipRect(Rect{X: split, Y: i, Width: width - split, Height: 1}, width); ok {
			targets = append(targets, Target{Rect: r, Zone: ZoneFilters, Item: modalValueRef + string(f.ID)})
		}
	}
	return rows, targets
}

// modalWidth is the width the rows want and the least they are drawn in.
func (b *FilterBar) modalWidth(styles Styles) (want, least int) {
	lw, vw := 0, 0
	for i, f := range b.fields {
		label, text := b.label(i)
		lw = max(lw, lipgloss.Width(label))
		if f.Action != "" {
			vw = max(vw, lipgloss.Width(text))
		}
		for _, o := range f.Options {
			vw = max(vw, lipgloss.Width(Escape(o.shown(b.text))))
		}
	}
	head, tail := fieldFrame(styles, strings.Repeat(" ", lw))
	frame := 2 + lipgloss.Width(head) + lipgloss.Width(tail)
	return frame + vw, frame + 1
}

// ModalOverlay draws the open modal with styles over screen, the drawn screen of width by height cells, in the
// way of Dialog.Overlay: the screen dimmed and the modal in its frame, with all fields one under the other and
// the key line, centred in room. The targets are the modal's alone. ok is false where room is too small, and
// while the modal is closed; the application then draws ModalView in the workspace and Hints in the footer.
func (b *FilterBar) ModalOverlay(styles Styles, screen string, width, height int, room Rect) (view string, targets []Target, ok bool) {
	if !b.modal {
		return "", nil, false
	}
	want, least := b.modalWidth(styles)
	inner := min(max(want, dialogMinInner), dialogMaxInner, room.Width-8)
	if inner < max(dialogMinInner, least) {
		return "", nil, false
	}
	lines, ts := b.modalRows(styles, inner)
	lines = append(lines, "")
	keyLine, kt := styles.footer(b.keys.Hints(b.Hints(), InputNavigation, b.text), "", inner)
	ts = append(ts, OffsetTargets(kt, 0, len(lines))...)
	lines = append(lines, keyLine)
	for i, l := range lines {
		lines[i] = Pad(Clip(l, inner), inner)
	}
	box := styles.Panel(b.text.Lookup(TextFilterTitle), "", lines, inner+4, len(lines)+2, true)
	if len(box) > room.Height {
		return "", nil, false
	}
	top := room.Y + (room.Height-len(box))/2
	left := room.X + (room.Width-inner-4)/2
	return overlay(screen, box, width, height, top, left, styles), OffsetTargets(ts, left+2, top+1), true
}

// ModalView draws the open modal with styles as the content of a workspace of width by height cells, for a
// terminal too small to draw it over the screen: the fields without frame and key line, which stand in the
// footer, scrolled so the field in focus is in view. Empty while the modal is closed.
func (b *FilterBar) ModalView(styles Styles, width, height int) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if !b.modal || height == 0 {
		return "", nil
	}
	rows, targets := b.modalRows(styles, max(width-2, 1))
	start := max(min(b.mfocus-height+1, len(rows)-height), 0)
	out := make([]string, height)
	for i := range out {
		line := ""
		if start+i < len(rows) {
			line = "  " + rows[start+i]
		}
		out[i] = Pad(Clip(line, width), width)
	}
	var kept []Target
	for _, t := range OffsetTargets(targets, 2, -start) {
		if r, ok := clipRect(t.Rect, width); ok && r.Y >= 0 && r.Y < height {
			t.Rect = r
			kept = append(kept, t)
		}
	}
	return strings.Join(out, "\n"), kept
}

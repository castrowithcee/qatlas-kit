package tui

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// FilterOption is one value of a field of a filter bar.
type FilterOption struct {
	ID    string
	Label TextID
}

// FilterField is one choice field of a filter bar, such as the grouping, a state or the sort order. Selected
// is the ID of the value in force; empty takes the first.
type FilterField struct {
	ID       string
	Label    TextID
	Options  []FilterOption
	Selected string
}

// FilterKeys are the actions of the key table a filter bar reads: Left and Right change the value of the
// field, Next and Prev step through the fields, Enter opens the list of values and takes one, Up and Down move
// in it, and Back closes it without change.
type FilterKeys struct {
	Left, Right, Next, Prev, Up, Down, Enter, Back ActionID
}

// The target items of a filter bar start with a control character, which no field or value ID holds.
const (
	fieldRef  = "\x00f"
	optionRef = "\x00o"
)

// The width of the search field at the right end of the bar.
const (
	searchFieldMin = 14
	searchFieldMax = 30
)

// FilterBar is the bar of choice fields above a list. It changes only what the list shows and in which
// order: the application applies the values to the entries it hands to the list, never to its data. It holds
// the values, the focused field and the open list of values.
type FilterBar struct {
	fields []FilterField
	nav    FilterKeys
	keys   *Keymap
	text   Translator

	focus  int
	open   bool
	choice int
}

// NewFilterBar checks the fields and returns a bar without focus. It rejects a bar without fields, a field
// or value without ID or label, an ID with a control character or listed twice, a field with fewer than two
// values, a selected value that is no value of its field, and a key that names no action of keys or is named
// twice.
func NewFilterBar(fields []FilterField, nav FilterKeys, keys *Keymap, text Translator) (*FilterBar, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the filter bar needs a key table and a translator")
	}
	var errs []error
	used := map[ActionID]bool{}
	for _, id := range []ActionID{nav.Left, nav.Right, nav.Next, nav.Prev, nav.Up, nav.Down, nav.Enter, nav.Back} {
		if _, ok := keys.index[id]; !ok {
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
	seen := map[string]bool{}
	list := make([]FilterField, len(fields))
	for i, f := range fields {
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("filter field %q: "+format, append([]any{f.ID}, args...)...))
		}
		switch {
		case badID(f.ID):
			fail("an ID is not empty and holds no control character")
		case seen[f.ID]:
			fail("is listed twice")
		}
		seen[f.ID] = true
		if f.Label == "" {
			fail("no label")
		}
		if len(f.Options) < 2 {
			fail("%d values; a field to choose from has at least two", len(f.Options))
		}
		values := map[string]bool{}
		for _, o := range f.Options {
			switch {
			case badID(o.ID):
				fail("value %q: an ID is not empty and holds no control character", o.ID)
			case values[o.ID]:
				fail("value %q is listed twice", o.ID)
			}
			values[o.ID] = true
			if o.Label == "" {
				fail("value %q has no label", o.ID)
			}
		}
		f.Options = append([]FilterOption(nil), f.Options...)
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
	return &FilterBar{fields: list, nav: nav, keys: keys, text: text, focus: -1}, nil
}

// Fields are the fields with the values in force.
func (b *FilterBar) Fields() []FilterField {
	out := make([]FilterField, len(b.fields))
	for i, f := range b.fields {
		f.Options = append([]FilterOption(nil), f.Options...)
		out[i] = f
	}
	return out
}

// Value is the ID of the value in force of the field id, empty for an unknown field.
func (b *FilterBar) Value(id string) string {
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
	b.focus, b.open = 0, false
	if fromEnd {
		b.focus = len(b.fields) - 1
	}
}

// Blur takes the focus from the bar and closes its list of values; the application calls it when the focus
// leaves the bar.
func (b *FilterBar) Blur() { b.focus, b.open = -1, false }

// Focused reports whether a field of the bar has the focus.
func (b *FilterBar) Focused() bool { return b.focus >= 0 }

// Open reports whether the list of values of the focused field is open.
func (b *FilterBar) Open() bool { return b.open }

// Update handles a key while the bar has the focus. changed is the field whose value the key changed, the
// zero field where none did; handled is false for a key the bar leaves to the application: tab past the last
// field or shift+tab before the first, which go on to the next zone, and up, down and back on a closed field.
// While the list of values is open it takes every key.
func (b *FilterBar) Update(msg tea.KeyPressMsg) (changed FilterField, handled bool) {
	if b.focus < 0 {
		return FilterField{}, false
	}
	is := func(id ActionID) bool { return b.keys.Is(msg, id, InputNavigation) }
	f := &b.fields[b.focus]
	if b.open {
		switch {
		case is(b.nav.Up):
			b.choice = max(b.choice-1, 0)
		case is(b.nav.Down):
			b.choice = min(b.choice+1, len(f.Options)-1)
		case is(b.nav.Enter):
			return b.choose(b.focus, b.choice), true
		case is(b.nav.Back):
			b.open = false
		}
		return FilterField{}, true
	}
	switch {
	case is(b.nav.Left), is(b.nav.Right):
		step := 1
		if is(b.nav.Left) {
			step = -1
		}
		return b.choose(b.focus, wrap(b.selected(b.focus)+step, len(f.Options))), true
	case is(b.nav.Next):
		if b.focus == len(b.fields)-1 {
			return FilterField{}, false
		}
		b.focus++
	case is(b.nav.Prev):
		if b.focus == 0 {
			return FilterField{}, false
		}
		b.focus--
	case is(b.nav.Enter):
		b.open, b.choice = true, b.selected(b.focus)
	default:
		return FilterField{}, false
	}
	return FilterField{}, true
}

// Click handles a click on a target the bar drew, as the MouseMap of its targets reports it: a click on a
// field focuses it and opens its list of values, as enter does, a second one closes it, and a click on a
// value of the open list takes it. A click on the search field is the search key of the list; ListModel.Click
// runs it.
func (b *FilterBar) Click(r MouseResult) (changed FilterField, handled bool) {
	if !r.Handled {
		return FilterField{}, false
	}
	if id, ok := strings.CutPrefix(r.Item, fieldRef); ok {
		i := b.field(id)
		if i < 0 {
			return FilterField{}, false
		}
		if b.open && b.focus == i {
			b.open = false
			return FilterField{}, true
		}
		b.focus, b.open, b.choice = i, true, b.selected(i)
		return FilterField{}, true
	}
	if rest, ok := strings.CutPrefix(r.Item, optionRef); ok {
		id, value, _ := strings.Cut(rest, "\x00")
		i := b.field(id)
		if i < 0 || !b.open || b.focus != i {
			return FilterField{}, false
		}
		for j, o := range b.fields[i].Options {
			if o.ID == value {
				return b.choose(i, j), true
			}
		}
	}
	return FilterField{}, false
}

func (b *FilterBar) field(id string) int {
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
func (b *FilterBar) choose(i, j int) FilterField {
	b.open = false
	f := &b.fields[i]
	if f.Options[j].ID == f.Selected {
		return FilterField{}
	}
	f.Selected = f.Options[j].ID
	out := *f
	out.Options = append([]FilterOption(nil), f.Options...)
	return out
}

func (b *FilterBar) label(i int) (label, value string) {
	f := b.fields[i]
	return Escape(b.text.Lookup(f.Label)), Escape(b.text.Lookup(f.Options[b.selected(i)].Label))
}

// fieldText is a field of at most width cells, " label: value ▾ ", the value cut where it does not fit.
func fieldText(styles Styles, label, value string, width int) string {
	head, tail := " "+label+": ", " "+styles.Glyph(GlyphExpanded)+" "
	return head + styles.Truncate(value, max(width-lipgloss.Width(head)-lipgloss.Width(tail), 1)) + tail
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
// behind the marker ">", and the search field of list at the right end where list draws its search in the
// bar; list may be nil. Where the fields do not all fit, their values are cut; where even that is not enough,
// the focused field stands alone, an unfocused bar shows its values dimmed or, while a search is typed, the
// search field. It returns a target for the free area of the bar, one per drawn field and one for the search
// field, counted from the left of the bar.
func (b *FilterBar) View(width int, styles Styles, list *ListModel) (string, []Target) {
	width = max(width, 0)
	n := len(b.fields)
	labels, values := make([]string, n), make([]string, n)
	for i := range b.fields {
		labels[i], values[i] = b.label(i)
	}
	targets := []Target{{Rect: Rect{Width: width, Height: 1}, Zone: ZoneFilters}}
	search := list != nil && list.spec.SearchInBar
	idle := !search || (!list.searching && len(list.query) == 0)
	const gap = 2
	natural := func(i int) int { return lipgloss.Width(fieldText(styles, labels[i], values[i], 1<<20)) }
	least := func(i int) int { return lipgloss.Width(fieldText(styles, labels[i], "", 1)) }
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
			targets = append(targets, Target{Rect: Rect{X: x, Width: w, Height: 1}, Zone: ZoneFilters,
				Action: b.nav.Enter, Item: fieldRef + b.fields[i].ID})
			x += w
		}
		if searchWidth > 0 {
			line.WriteString(strings.Repeat(" ", width-used-searchWidth) + searchField(width-searchWidth, searchWidth))
		}
		return line.String()
	}
	var line string
	if widths, used, ok := fit(width - gap - searchFieldMin); search && ok {
		line = draw(widths, used, min(searchFieldMax, width-used-gap))
	} else if widths, used, ok := fit(width); ok && idle {
		line = draw(widths, used, 0)
	} else if b.focus >= 0 {
		text := fieldText(styles, labels[b.focus], values[b.focus], max(width-2, 1))
		targets = append(targets, Target{Rect: Rect{Width: width, Height: 1}, Zone: ZoneFilters,
			Action: b.nav.Enter, Item: fieldRef + b.fields[b.focus].ID})
		line = barMarker(true) + fieldStyle(styles, true).Render(text)
	} else if !idle {
		line = searchField(0, width)
	} else {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = labels[i] + ": " + values[i]
		}
		line = "  " + styles.Render(StyleHint, styles.Truncate(strings.Join(parts, styles.Glyph(GlyphKeySeparator)), max(width-2, 0)))
	}
	return Pad(Clip(line, width), width), targets
}

// ValueList draws the open list of values of the focused field as a panel, the label as its title and the
// value under the cursor marked, to place under the field; empty while no list is open. It returns one target
// per value, counted from the top left of the panel.
func (b *FilterBar) ValueList(styles Styles) (string, []Target) {
	if !b.open || b.focus < 0 {
		return "", nil
	}
	f := b.fields[b.focus]
	title, _ := b.label(b.focus)
	inner := lipgloss.Width(title) + 2
	labels := make([]string, len(f.Options))
	for j, o := range f.Options {
		labels[j] = Escape(b.text.Lookup(o.Label))
		inner = max(inner, lipgloss.Width(labels[j])+2)
	}
	lines := make([]string, len(labels))
	var targets []Target
	for j, label := range labels {
		lines[j] = styles.Row(label, inner, j == b.choice)
		targets = append(targets, Target{Rect: Rect{X: 2, Y: 1 + j, Width: inner, Height: 1}, Zone: ZoneFilters,
			Action: b.nav.Enter, Item: optionRef + f.ID + "\x00" + f.Options[j].ID})
	}
	return strings.Join(styles.Panel(title, "", lines, inner+4, len(lines)+2, true), "\n"), targets
}

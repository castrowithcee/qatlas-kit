package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// HintKind says where a hint stands and how it gives way when a line runs short.
type HintKind uint8

const (
	// HintNormal stands at the left and is dropped by its priority.
	HintNormal HintKind = iota
	// HintAnswer is an answer of an open question: its label is shortened, the hint is never dropped.
	HintAnswer
	// HintAlways stays at every width, such as the help key.
	HintAlways
	// HintNavigation moves through the screen: it stands dimmed at the right and is dropped first.
	HintNavigation
)

// Hint is one group of a key line: the keys of Action, then the text Label. Priority orders the normal
// hints when the line runs short: a lower priority stays longer, and of equal ones the later goes first.
// A hint without an action is text alone; a hint whose action has no key that acts in the input focus of
// the screen is left out, so a line never names a key that does nothing.
type Hint struct {
	Action   ActionID
	Label    TextID
	Priority int
	Kind     HintKind
}

// HintKey is one action of a drawn hint and the keys that trigger it now.
type HintKey struct {
	Action ActionID
	Keys   string
}

// HintGroup is a hint as a line draws it. Hints whose labels read the same in the language of the line
// share one group: their keys are joined by "/" in front of the label, at the place of the first and with
// the lowest priority of them. A language whose words are longer can so fold two hints into one by giving
// them the same text, instead of leaving one out.
type HintGroup struct {
	Keys     []HintKey
	Label    string
	Kind     HintKind
	Priority int
}

// Text is the group as plain text: its keys joined by "/", a blank and the label.
func (g HintGroup) Text() string {
	keys := g.keyText()
	switch {
	case keys == "":
		return g.Label
	case g.Label == "":
		return keys
	}
	return keys + " " + g.Label
}

func (g HintGroup) keyText() string {
	keys := make([]string, len(g.Keys))
	for i, k := range g.Keys {
		keys[i] = k.Keys
	}
	return strings.Join(keys, "/")
}

// Hints resolves hints for a screen in the input focus focus: each action to the keys that act there,
// each label to its text in text. A hint whose action has no such key is left out; an action the table
// does not know shows as its placeholder "{id}", so the mistake is visible. Hints with the same label text
// and kind fold into one group. Keys and labels are escaped.
func (k *Keymap) Hints(hints []Hint, focus InputFocus, text Translator) []HintGroup {
	var groups []HintGroup
	type foldKey struct {
		label string
		kind  HintKind
	}
	at := map[foldKey]int{}
	for _, h := range hints {
		label := ""
		if h.Label != "" {
			label = Escape(lookup(text, h.Label))
		}
		var key *HintKey
		if h.Action != "" {
			keys, ok := k.hintKeys(h.Action, focus)
			if ok && keys == "" {
				continue
			}
			key = &HintKey{Action: h.Action, Keys: Escape(keys)}
		}
		if key == nil && label == "" {
			continue
		}
		fold := foldKey{label, h.Kind}
		if i, ok := at[fold]; ok && label != "" {
			g := &groups[i]
			g.Priority = min(g.Priority, h.Priority)
			if key != nil && !slices.ContainsFunc(g.Keys, func(k HintKey) bool { return k.Action == key.Action }) {
				g.Keys = append(g.Keys, *key)
			}
			continue
		}
		g := HintGroup{Label: label, Kind: h.Kind, Priority: h.Priority}
		if key != nil {
			g.Keys = []HintKey{*key}
		}
		at[fold] = len(groups)
		groups = append(groups, g)
	}
	return groups
}

// hintKeys is the label of the keys of id under focus; false for an action the table does not know, which
// reads as its placeholder.
func (k *Keymap) hintKeys(id ActionID, focus InputFocus) (string, bool) {
	if k != nil {
		if label, ok := k.labelUnder(id, focus); ok {
			return label, true
		}
	}
	return "{" + string(id) + "}", false
}

func lookup(text Translator, id TextID) string {
	if text == nil {
		return string(id)
	}
	return text.Lookup(id)
}

// footerItem is one group of the footer with what decides whether it stays.
type footerItem struct {
	group HintGroup
	right bool
	// position is the plain position text at the right; it names no key.
	position bool
	index    int
}

// footerGap stands between two groups of the footer.
const footerGap = "  "

// minAnswerLabel is the number of cells an answer's label keeps at least before the line is cut instead.
const minAnswerLabel = 1

// footerItems are the groups of the footer in order and the position at the right end.
func footerItems(groups []HintGroup, position string) []footerItem {
	items := make([]footerItem, 0, len(groups)+1)
	for i, g := range groups {
		items = append(items, footerItem{group: g, right: g.Kind == HintNavigation, index: i})
	}
	if position = Escape(position); position != "" {
		items = append(items, footerItem{group: HintGroup{Label: position}, right: true, position: true, index: len(groups)})
	}
	return items
}

// dropOrder ranks an item for dropping, higher first: navigation before the position before the normal
// hints, the normal hints by priority, and among equals the later one. Answers and hints that always stay
// are never dropped.
func (it footerItem) dropOrder() (class, rank int, droppable bool) {
	switch {
	case it.group.Kind == HintNavigation:
		return 2, 0, true
	case it.position:
		return 1, 0, true
	case it.group.Kind == HintNormal:
		return 0, it.group.Priority, true
	}
	return 0, 0, false
}

// fitFooter drops groups until the line fits width, then shortens the labels of the answers, the longest
// first. The remaining groups keep their order.
func (s Styles) fitFooter(items []footerItem, width int) []footerItem {
	items = append([]footerItem(nil), items...)
	for footerWidth(items) > width {
		drop := -1
		var bestClass, bestRank int
		for i, it := range items {
			class, rank, ok := it.dropOrder()
			if !ok {
				continue
			}
			if drop < 0 || class > bestClass || class == bestClass && (rank > bestRank || rank == bestRank && it.index > items[drop].index) {
				drop, bestClass, bestRank = i, class, rank
			}
		}
		if drop < 0 {
			break
		}
		items = append(items[:drop], items[drop+1:]...)
	}
	for footerWidth(items) > width {
		cut := -1
		for i, it := range items {
			w := lipgloss.Width(it.group.Label)
			if it.group.Kind == HintAnswer && w > minAnswerLabel && (cut < 0 || w > lipgloss.Width(items[cut].group.Label)) {
				cut = i
			}
		}
		if cut < 0 {
			break
		}
		label := items[cut].group.Label
		items[cut].group.Label = s.Truncate(label, lipgloss.Width(label)-1)
	}
	return items
}

func footerWidth(items []footerItem) int {
	left, right := 0, 0
	for _, it := range items {
		w := lipgloss.Width(it.group.Text()) + len(footerGap)
		if it.right {
			right += w
		} else {
			left += w
		}
	}
	// Each side has one gap fewer than groups; both sides together have one between them.
	switch {
	case left > 0 && right > 0:
		return left + right - len(footerGap)
	case left > 0:
		return left - len(footerGap)
	case right > 0:
		return right - len(footerGap)
	}
	return 0
}

// footer draws the footer line of width cells: the key groups at the left, the key of a group in
// the footer key style and its label dimmed, navigation and position dimmed at the right end. Each key of
// a group is a target for its action, the label one for the first action of the group.
func (s Styles) footer(groups []HintGroup, position string, width int) (string, []Target) {
	items := s.fitFooter(footerItems(groups, position), width)
	var left, right []footerItem
	for _, it := range items {
		if it.right {
			right = append(right, it)
		} else {
			left = append(left, it)
		}
	}
	var b strings.Builder
	var targets []Target
	x := 0
	place := func(side []footerItem) {
		for i, it := range side {
			if i > 0 {
				b.WriteString(footerGap)
				x += len(footerGap)
			}
			line, spans := s.hintGroup(it.group, it.right)
			b.WriteString(line)
			for _, sp := range spans {
				if r, ok := clipRect(Rect{X: x + sp.start, Width: sp.end - sp.start, Height: 1}, width); ok {
					targets = append(targets, Target{Rect: r, Action: sp.action})
				}
			}
			x += lipgloss.Width(it.group.Text())
		}
	}
	place(left)
	if len(right) > 0 {
		pad := max(width-footerWidth(right)-x, 1)
		if len(left) == 0 {
			pad = max(width-footerWidth(right), 0)
		}
		b.WriteString(strings.Repeat(" ", pad))
		x += pad
		place(right)
	}
	return Pad(Clip(b.String(), width), width), targets
}

// actionSpan is the cells of a drawn hint that trigger one action.
type actionSpan struct {
	start, end int
	action     ActionID
}

// hintGroup draws one group, dimmed as a whole where dim is set, and returns the cells of its actions.
func (s Styles) hintGroup(g HintGroup, dim bool) (string, []actionSpan) {
	keyStyle := StyleFooterKey
	if dim {
		keyStyle = StyleHint
	}
	var b strings.Builder
	var spans []actionSpan
	x := 0
	for i, k := range g.Keys {
		if i > 0 {
			b.WriteString(s.Render(keyStyle, "/"))
			x++
		}
		w := lipgloss.Width(k.Keys)
		b.WriteString(s.Render(keyStyle, k.Keys))
		spans = append(spans, actionSpan{x, x + w, k.Action})
		x += w
	}
	if g.Label == "" {
		return b.String(), spans
	}
	if len(g.Keys) > 0 {
		b.WriteString(" ")
		x++
	}
	b.WriteString(s.Render(StyleHint, g.Label))
	if len(g.Keys) > 0 {
		spans = append(spans, actionSpan{x, x + lipgloss.Width(g.Label), g.Keys[0].Action})
	}
	return b.String(), spans
}

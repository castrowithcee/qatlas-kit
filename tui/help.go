package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-kit/helptopics"
)

// HelpKeys are the actions of the key table the help reads.
type HelpKeys struct {
	// Left and Right change the topic with wrap-around, and so do Next and Prev where the table has them,
	// such as tab and shift+tab on the help level.
	Left, Right, Next, Prev ActionID
	// Up, Down, PageUp and PageDown scroll the text and stop at its ends; First and Last, where the table has
	// them, jump there.
	Up, Down, PageUp, PageDown, First, Last ActionID
	// Back and Help close the help.
	Back, Help ActionID
}

// HelpSpec is what the help shows.
type HelpSpec struct {
	// Topics are the topics the command line and the manpage show, in their order.
	Topics []helptopics.Topic
	// Screen are the hints of the screen the help was opened from, and Global the actions that act on every
	// screen. The first topic ends with both lists, under the headings TextHelpKeysScreen and TextHelpKeysAll,
	// with the keys bound now; a global action is named by its help text.
	Screen []Hint
	Global []ActionID
	Keys   HelpKeys
}

// helpMaxInner is the widest text of the help drawn over the screen.
const helpMaxInner = 72

// helpMinText is the fewest text rows the help keeps over the screen before it fills the workspace instead.
const helpMinText = 3

// helpHeadingWidth is the widest line a topic can hold as a heading.
const helpHeadingWidth = 40

// helpTopicRef starts the target item of a topic tab.
const helpTopicRef = "\x00t"

// Help is the open help: one topic of the application's help topics at a time, scrolled within the room
// it has. It holds the topic and the scroll position.
type Help struct {
	spec   HelpSpec
	keys   *Keymap
	text   Translator
	topic  int
	scroll textScroll
}

// NewHelp checks spec and returns the help on its first topic. It rejects a help without topics, a topic
// without name or text, a name with a blank or control character or one that two topics share, a missing
// key of Left, Right, Up, Down, PageUp, PageDown, Back or Help, a key that names no action of keys or that two
// help keys share, and a key list entry that names no action of keys or a global action listed twice.
func NewHelp(spec HelpSpec, keys *Keymap, text Translator) (*Help, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the help needs a key table and a translator")
	}
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("help: "+format, args...)) }
	if len(spec.Topics) == 0 {
		fail("no topic")
	}
	names := map[string]bool{}
	for i, topic := range spec.Topics {
		switch {
		case topic.Name == "" || strings.IndexFunc(topic.Name, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) }) >= 0:
			fail("topic %d: the name %q is empty or holds a blank or control character", i, topic.Name)
		case names[topic.Name]:
			fail("topic %q is listed twice", topic.Name)
		}
		names[topic.Name] = true
		if strings.TrimSpace(topic.Text) == "" {
			fail("topic %q has no text", topic.Name)
		}
	}
	k := spec.Keys
	errs = append(errs, checkKeys("help", keys, []namedKey{
		{"left", k.Left, true}, {"right", k.Right, true}, {"next", k.Next, false},
		{"prev", k.Prev, false}, {"up", k.Up, true}, {"down", k.Down, true},
		{"page up", k.PageUp, true}, {"page down", k.PageDown, true}, {"first", k.First, false},
		{"last", k.Last, false}, {"back", k.Back, true}, {"help", k.Help, true},
	})...)
	for _, h := range spec.Screen {
		if h.Action != "" && !keys.known(h.Action) {
			fail("screen key %q is no action of the key table", h.Action)
		}
	}
	for i, id := range spec.Global {
		switch {
		case !keys.known(id):
			fail("global key %q is no action of the key table", id)
		case slices.Contains(spec.Global[:i], id):
			fail("global key %q is listed twice", id)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	spec.Topics = slices.Clone(spec.Topics)
	spec.Screen = slices.Clone(spec.Screen)
	spec.Global = slices.Clone(spec.Global)
	return &Help{spec: spec, keys: keys, text: text}, nil
}

// namedKey is one key of a component's key set with the name its refusal uses.
type namedKey struct {
	name     string
	id       ActionID
	required bool
}

// checkKeys reports a required key that is missing, a key that names no action of keys and a key that two
// entries share.
func checkKeys(part string, keys *Keymap, set []namedKey) []error {
	var errs []error
	used := map[ActionID]string{}
	for _, k := range set {
		switch other, dup := used[k.id]; {
		case k.id == "" && k.required:
			errs = append(errs, fmt.Errorf("%s: the %s key is missing", part, k.name))
		case k.id == "":
		case !keys.known(k.id):
			errs = append(errs, fmt.Errorf("%s: %s key %q is no action of the key table", part, k.name, k.id))
		case dup:
			errs = append(errs, fmt.Errorf("%s: %s key %q is also the %s key", part, k.name, k.id, other))
		default:
			used[k.id] = k.name
		}
	}
	return errs
}

// Topic is the name of the topic shown.
func (h *Help) Topic() string { return h.spec.Topics[h.topic].Name }

// SetTopic shows the named topic from its top, such as the topic a command or a link asks for. It reports
// false and keeps the topic for an unknown name.
func (h *Help) SetTopic(name string) bool {
	i := slices.IndexFunc(h.spec.Topics, func(t helptopics.Topic) bool { return t.Name == name })
	if i < 0 {
		return false
	}
	h.topic = i
	h.scroll.offset = 0
	return true
}

// Update handles a key. act is the action the application runs as that key while the help is open: Back or
// Help, which close it. The keys that change the topic or scroll are handled inside; handled is false for
// every other key, which the application handles or drops.
func (h *Help) Update(msg tea.KeyPressMsg) (act ActionID, handled bool) {
	for _, id := range h.navKeys() {
		if h.keys.Is(msg, id, InputNavigation) {
			return h.Act(id)
		}
	}
	return "", false
}

func (h *Help) navKeys() []ActionID {
	k := h.spec.Keys
	var ids []ActionID
	for _, id := range []ActionID{k.Back, k.Help, k.Left, k.Right, k.Next, k.Prev, k.Up, k.Down,
		k.PageUp, k.PageDown, k.First, k.Last} {
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// Act runs one of the help keys as its key does, for a click or a command that stands for the key.
func (h *Help) Act(id ActionID) (act ActionID, handled bool) {
	k := h.spec.Keys
	if id == "" {
		return "", false
	}
	switch id {
	case k.Back, k.Help:
		return id, true
	case k.Left, k.Prev:
		h.SetTopic(h.spec.Topics[wrap(h.topic-1, len(h.spec.Topics))].Name)
	case k.Right, k.Next:
		h.SetTopic(h.spec.Topics[wrap(h.topic+1, len(h.spec.Topics))].Name)
	case k.Up:
		h.scroll.by(-1)
	case k.Down:
		h.scroll.by(1)
	case k.PageUp:
		h.scroll.by(-h.scroll.step())
	case k.PageDown:
		h.scroll.by(h.scroll.step())
	case k.First:
		h.scroll.offset = 0
	case k.Last:
		h.scroll.offset = h.scroll.last
	default:
		return "", false
	}
	return "", true
}

// Click handles a click on a target the help drew, as the MouseMap of its targets reports it: a topic tab
// shows its topic as the topic keys would, a key of the key line acts as that key, and a line of the key
// list yields its action, which the application runs as if its key were pressed while the help is open.
func (h *Help) Click(r MouseResult) (act ActionID, handled bool) {
	if !r.Handled {
		return "", false
	}
	if name, ok := strings.CutPrefix(r.Item, helpTopicRef); ok {
		return "", h.SetTopic(name)
	}
	if slices.Contains(h.navKeys(), r.Action) {
		return h.Act(r.Action)
	}
	if r.Action != "" && h.listed(r.Action) {
		return r.Action, true
	}
	return "", false
}

// listed reports whether the key list names the action.
func (h *Help) listed(id ActionID) bool {
	return slices.Contains(h.spec.Global, id) || slices.ContainsFunc(h.spec.Screen, func(hint Hint) bool { return hint.Action == id })
}

// Wheel scrolls the text by the notches of the wheel, WheelTextLines a notch, and stops at its ends.
func (h *Help) Wheel(steps int) { h.scroll.by(steps * WheelTextLines) }

// Hints are the help keys as hints of a key line. The help drawn over the screen shows them itself; where it
// fills the workspace, the application puts them into the footer.
func (h *Help) Hints() []Hint {
	k := h.spec.Keys
	var hints []Hint
	add := func(label TextID, priority int, kind HintKind, ids ...ActionID) {
		for _, id := range ids {
			if id != "" {
				hints = append(hints, Hint{Action: id, Label: label, Priority: priority, Kind: kind})
			}
		}
	}
	add(TextHelpTopic, 0, HintNormal, k.Left, k.Right, k.Next, k.Prev)
	add(TextHelpScroll, 1, HintNormal, k.Up, k.Down)
	add(TextHelpPage, 2, HintNormal, k.PageUp, k.PageDown)
	add(TextKeyFirst, 3, HintNormal, k.First)
	add(TextKeyLast, 3, HintNormal, k.Last)
	add(TextHelpClose, 0, HintAlways, k.Back, k.Help)
	return hints
}

// textRow is one drawn line and the targets in it, counted from its start.
type textRow struct {
	text    string
	targets []Target
}

// rows is the text of the topic shown in width cells: its lines wrapped as the command line wraps them, a
// heading in bold, and after the first topic the key list with one target per key.
func (h *Help) rows(width int, styles Styles) []textRow {
	var rows []textRow
	for _, line := range strings.Split(h.spec.Topics[h.topic].Text, "\n") {
		line = Escape(line)
		heading := strings.HasSuffix(line, ":") && !strings.HasPrefix(line, " ") && lipgloss.Width(line) <= helpHeadingWidth
		for _, piece := range strings.Split(helptopics.Wrap(line, width), "\n") {
			piece = Clip(piece, width)
			if heading {
				piece = styles.Render(StyleTitle, piece)
			}
			rows = append(rows, textRow{text: piece})
		}
	}
	if h.topic == 0 {
		global := make([]Hint, len(h.spec.Global))
		for i, id := range h.spec.Global {
			global[i] = Hint{Action: id, Label: h.keys.actions[h.keys.index[id]].Help}
		}
		rows = append(rows, h.keyList(TextHelpKeysScreen, h.spec.Screen, width, styles)...)
		rows = append(rows, h.keyList(TextHelpKeysAll, global, width, styles)...)
	}
	return rows
}

// keyList is one list of keys under its heading, after a blank line, one group a line; nothing where no hint
// has a key.
func (h *Help) keyList(heading TextID, hints []Hint, width int, styles Styles) []textRow {
	groups := h.keys.Hints(hints, InputNavigation, h.text)
	if len(groups) == 0 {
		return nil
	}
	rows := []textRow{{}, {text: styles.Render(StyleTitle, styles.Truncate(Escape(h.text.Lookup(heading)), width))}}
	const indent = 2
	for _, g := range groups {
		line, spans := styles.hintGroup(g, false)
		var targets []Target
		for _, sp := range spans {
			if r, ok := clipRect(Rect{X: indent + sp.start, Width: sp.end - sp.start, Height: 1}, width); ok {
				targets = append(targets, Target{Rect: r, Action: sp.action})
			}
		}
		rows = append(rows, textRow{text: Clip(strings.Repeat(" ", indent)+line, width), targets: targets})
	}
	return rows
}

// strip is the line of topic tabs in width cells, the topic shown in brackets or filled, and a target per
// drawn tab. Topics before the first that lets the strip fit are left out, so the topic shown is always on it.
func (h *Help) strip(width int, styles Styles) (string, []Target) {
	names := make([]string, len(h.spec.Topics))
	for i, t := range h.spec.Topics {
		names[i] = t.Name
	}
	first := 0
	line, spans := styles.TabStrip(names, h.topic, true)
	for first < h.topic && lipgloss.Width(line) > width {
		first++
		line, spans = styles.TabStrip(names[first:], h.topic-first, true)
	}
	var targets []Target
	for i, sp := range spans {
		if r, ok := clipRect(Rect{X: sp.Start, Width: sp.End - sp.Start, Height: 1}, width); ok {
			targets = append(targets, Target{Rect: r, Item: helpTopicRef + names[first+i]})
		}
	}
	return Clip(line, width), targets
}

// window is rows cut to room lines from the scroll position, with the position line where they do not fit.
func (h *Help) window(rows []textRow, room, width int, styles Styles) []textRow {
	start, end, position := h.scroll.window(len(rows), room, h.text)
	out := slices.Clone(rows[start:end])
	if position != "" {
		out = append(out, textRow{text: styles.Render(StyleHint, styles.Truncate(Escape(position), width))})
	}
	return out
}

// place appends rows to lines and their targets, moved to the rows' place, to targets.
func place(lines []string, targets []Target, rows []textRow, x int) ([]string, []Target) {
	for _, r := range rows {
		targets = append(targets, OffsetTargets(r.targets, x, len(lines))...)
		lines = append(lines, r.text)
	}
	return lines, targets
}

// Overlay draws the help with styles over screen, the drawn screen of width by height cells: the screen
// dimmed and the help in its frame centred in room, the part of the screen it may cover, with the topic tabs,
// the text of the topic shown, the position where it scrolls, and the key line. The targets are the help's
// alone. ok is false where room is too small; the application then draws View in the workspace and Hints in
// the footer.
func (h *Help) Overlay(styles Styles, screen string, width, height int, room Rect) (view string, targets []Target, ok bool) {
	inner := min(helpMaxInner, room.Width-8)
	textRoom := room.Height - 2 - 2 - 4
	if inner < dialogMinInner || textRoom < helpMinText {
		return "", nil, false
	}
	strip, st := h.strip(inner, styles)
	lines, ts := []string{strip, ""}, st
	lines, ts = place(lines, ts, h.window(h.rows(inner, styles), textRoom, inner, styles), 0)
	keyLine, kt := styles.footer(h.keys.Hints(h.Hints(), InputNavigation, h.text), "", inner)
	lines = append(lines, "")
	ts = append(ts, OffsetTargets(kt, 0, len(lines))...)
	lines = append(lines, keyLine)
	box := styles.Panel(h.text.Lookup(TextHelpTitle), "", lines, inner+4, len(lines)+2, true)
	top := room.Y + (room.Height-len(box))/2
	left := room.X + (room.Width-inner-4)/2
	return overlay(screen, box, width, height, top, left, styles), OffsetTargets(ts, left+2, top+1), true
}

// View draws the help with styles as the content of a workspace of width by height cells, for a terminal too
// small to draw it over the screen: the title and the topic tabs, then the text; the keys stand in the
// footer.
func (h *Help) View(styles Styles, width, height int) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if height == 0 || width == 0 {
		return finishPage(nil, nil, width, height)
	}
	title := styles.Render(StyleTitle, Escape(h.text.Lookup(TextHelpTitle))) + "  "
	tw := lipgloss.Width(title)
	strip, st := h.strip(max(width-tw, 1), styles)
	lines, targets := []string{title + strip, ""}, OffsetTargets(st, tw, 0)
	if height > len(lines) {
		lines, targets = place(lines, targets, h.window(h.rows(width, styles), height-len(lines), width, styles), 0)
	}
	return finishPage(lines, targets, width, height)
}

// finishPage cuts or pads lines to width by height cells and keeps the targets inside them.
func finishPage(lines []string, targets []Target, width, height int) (string, []Target) {
	out := make([]string, height)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out[i] = Pad(Clip(line, width), width)
	}
	var kept []Target
	for _, t := range targets {
		if r, ok := clipRect(t.Rect, width); ok && t.Rect.Y < height {
			t.Rect = r
			kept = append(kept, t)
		}
	}
	return strings.Join(out, "\n"), kept
}

// textScroll is the first shown line of a text, and what the last drawing found: the greatest first line and
// the rows of a page.
type textScroll struct{ offset, last, page int }

// by moves the first line by n and stops at both ends.
func (s *textScroll) by(n int) { s.offset = max(min(s.offset+n, s.last), 0) }

// step is the move of a page key: a page less one line, so a line of the old page stays in view.
func (s *textScroll) step() int { return max(s.page-1, 1) }

// window is the lines from start up to end of a text of n lines shown in room rows, and the position line
// where the text does not fit, which takes one of the rows.
func (s *textScroll) window(n, room int, text Translator) (start, end int, position string) {
	room = max(room, 1)
	if n <= room {
		s.offset, s.last, s.page = 0, 0, room
		return 0, n, ""
	}
	room = max(room-1, 1)
	s.last, s.page = n-room, room
	s.offset = max(min(s.offset, s.last), 0)
	return s.offset, s.offset + room, text.Lookup(TextScrollPosition, s.offset+1, s.offset+room, n)
}

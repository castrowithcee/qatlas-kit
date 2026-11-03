package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// AreaID is the stable, untranslated ID of an area of the tab line.
type AreaID string

// Area is one tab of the tab line: a task the person came to do. An area with more than one tab shows
// them as sub-tabs below the tab line; it opens on its first.
type Area struct {
	ID    AreaID
	Title TextID
	Tabs  []Tab
}

// Tab is one sub-tab of an area.
type Tab struct {
	ID    string
	Title TextID
}

// Rect is a rectangle of terminal cells, the origin at the top left.
type Rect struct{ X, Y, Width, Height int }

// Target is a part of a drawn screen a click can reach: a key that triggers Action, or a tab that opens
// Area and, on the sub-tab line, Tab. A target with nothing but a zone is the free area of that zone.
type Target struct {
	Rect   Rect
	Zone   FocusZone
	Action ActionID
	Area   AreaID
	Tab    string
	// Item names the element a click hit inside a part that draws many, such as a row of a list or a field
	// of a filter bar. It is opaque: the part that drew the target reads it back in its Click.
	Item string
}

// StatusMark is a part of the header after the product name. Kind gives it the status glyph in its colour
// and leaves the word plain; without a kind the text stands dimmed alone. Short is the shorter word the
// header takes when room runs out; empty keeps Text.
type StatusMark struct {
	Kind  StatusKind
	Text  string
	Short string
}

// Message is a message line under the content of the workspace.
type Message struct {
	Kind MessageKind
	Text string
}

// FrameSpec is everything one frame draws. Texts are plain and escaped by the frame, except Workspace,
// which holds styled lines whose data the application escapes.
type FrameSpec struct {
	Width, Height int
	Product       string
	Status        []StatusMark
	// Back is the back button of the header, where the way back leads a level up; nil shows none.
	Back *Hint
	// HeaderKeys stand dimmed at the right end of the header, such as the help and command keys.
	HeaderKeys []Hint
	Areas      []Area
	ActiveArea AreaID
	ActiveTab  string
	Focus      FocusZone
	// Input is the input focus of the screen; hints show only the keys that act in it.
	Input     InputFocus
	Workspace string
	Message   *Message
	Footer    []Hint
	// Position is the position in a list, such as "3/12", dimmed at the right end of the footer.
	Position string
}

// Frame draws the frame every screen of one TUI stands in: header, tab line, sub-tabs, rule, workspace and
// footer. It holds no state of its own; every Render draws from its FrameSpec alone.
type Frame struct {
	styles Styles
	text   Translator
	keys   *Keymap
}

// NewFrame returns a frame that draws with styles, takes its texts from text and its key labels from keys.
func NewFrame(styles Styles, text Translator, keys *Keymap) *Frame {
	return &Frame{styles: styles, text: text, keys: keys}
}

// Workspace is the room the content of spec has: the full width between the rule and the footer, less the
// rows of the message.
func (f *Frame) Workspace(spec FrameSpec) Rect {
	top, rows := f.rows(spec)
	return Rect{X: 0, Y: top, Width: max(spec.Width, 0), Height: rows - len(f.messageLines(spec, rows))}
}

// rows are the number of rows above the workspace and the rows of the workspace.
func (f *Frame) rows(spec FrameSpec) (top, workspace int) {
	top = 2 // header and rule
	if len(spec.Areas) > 0 {
		top++
	}
	if _, ok := subTabs(spec); ok {
		top++
	}
	return top, max(spec.Height-top-1, 0)
}

// messageLines are the lines of the message within room rows; a message longer than the room keeps its
// end, where the way out stands.
func (f *Frame) messageLines(spec FrameSpec, room int) []string {
	if spec.Message == nil || spec.Message.Text == "" || room <= 0 {
		return nil
	}
	lines := strings.Split(f.styles.Message(spec.Message.Kind, spec.Message.Text, max(spec.Width, 1)), "\n")
	return lines[max(len(lines)-room, 0):]
}

// Render draws spec as exactly Height lines of exactly Width cells and returns the targets of the drawn
// header keys, tabs and footer keys. The footer is always the last line; the workspace is cut or padded to
// its rows, and a terminal lower than the frame loses the rows above the footer from the bottom up.
func (f *Frame) Render(spec FrameSpec) (string, []Target) {
	if spec.Height <= 0 {
		return "", nil
	}
	width := max(spec.Width, 0)
	var lines []string
	var targets []Target
	add := func(line string, ts []Target) {
		y := len(lines)
		for _, t := range ts {
			t.Rect.Y = y
			targets = append(targets, t)
		}
		lines = append(lines, Pad(Clip(line, width), width))
	}
	add(f.header(spec, width))
	if len(spec.Areas) > 0 {
		add(f.tabLine(spec, width))
	}
	if area, ok := subTabs(spec); ok {
		add(f.subTabLine(spec, area, width))
	}
	add(f.styles.Render(StyleBorder, strings.Repeat(f.ruleChar(), width)), nil)
	_, rows := f.rows(spec)
	message := f.messageLines(spec, rows)
	body := strings.Split(spec.Workspace, "\n")
	for i := 0; i < rows-len(message); i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		add(line, nil)
	}
	for _, line := range message {
		add(line, nil)
	}
	if len(lines) > spec.Height-1 {
		lines = lines[:spec.Height-1]
		kept := targets[:0]
		for _, t := range targets {
			if t.Rect.Y < spec.Height-1 {
				kept = append(kept, t)
			}
		}
		targets = kept
	}
	foot, footTargets := f.styles.footer(f.keys.Hints(spec.Footer, spec.Input, f.text), spec.Position, width)
	add(foot, footTargets)
	return strings.Join(lines, "\n"), targets
}

func (f *Frame) ruleChar() string {
	if f.styles.charset == CharsetUnicode {
		return "─"
	}
	return "-"
}

// headerGap stands between two parts of the header.
const headerGap = "  "

// header is the header line: the product name and the status marks at the left, the back button and the
// header keys dimmed at the right end. It stays one line: the marks take their short words, then the last
// marks go, before the header keys go, and the header keys go before the back button; the name is cut
// last.
func (f *Frame) header(spec FrameSpec, width int) (string, []Target) {
	product := Escape(spec.Product)
	var blocks []headerBlock
	back, hasBack := f.headerBlock(spec, spec.Back)
	var keys headerBlock
	for _, h := range spec.HeaderKeys {
		b, ok := f.headerBlock(spec, &h)
		switch {
		case !ok:
		case keys.text == "":
			keys = b
		default:
			keys = keys.join(b)
		}
	}
	switch {
	case hasBack && keys.text != "":
		blocks = append(blocks, back.join(keys), back)
	case hasBack:
		blocks = append(blocks, back)
	case keys.text != "":
		blocks = append(blocks, keys)
	}
	blocks = append(blocks, headerBlock{})
	levels := f.statusLevels(spec.Status)
	for _, block := range blocks {
		room := width
		if block.text != "" {
			room -= lipgloss.Width(block.text) + len(headerGap)
		}
		for _, marks := range levels {
			left := f.headerLeft(product, marks)
			if lipgloss.Width(left) > room {
				continue
			}
			if block.text == "" {
				return left, nil
			}
			x := width - lipgloss.Width(block.text)
			ts := make([]Target, len(block.targets))
			for i, t := range block.targets {
				t.Rect.X += x
				ts[i] = t
			}
			return left + strings.Repeat(" ", x-lipgloss.Width(left)) + block.styled, ts
		}
	}
	return f.styles.Render(StyleTitle, f.styles.Truncate(product, width)), nil
}

// headerBlock is a block at the right end of the header with the targets of its keys, counted from its
// start.
type headerBlock struct {
	text, styled string
	targets      []Target
}

func (b headerBlock) join(o headerBlock) headerBlock {
	shift := lipgloss.Width(b.text) + len(headerGap)
	out := headerBlock{text: b.text + headerGap + o.text, styled: b.styled + headerGap + o.styled}
	out.targets = append(out.targets, b.targets...)
	for _, t := range o.targets {
		t.Rect.X += shift
		out.targets = append(out.targets, t)
	}
	return out
}

// headerBlock draws a hint as "[keys] label", dimmed; false where no key of it acts.
func (f *Frame) headerBlock(spec FrameSpec, h *Hint) (headerBlock, bool) {
	if h == nil {
		return headerBlock{}, false
	}
	groups := f.keys.Hints([]Hint{*h}, spec.Input, f.text)
	if len(groups) == 0 || len(groups[0].Keys) == 0 {
		return headerBlock{}, false
	}
	g := groups[0]
	text := "[" + g.keyText() + "]"
	if g.Label != "" {
		text += " " + g.Label
	}
	return headerBlock{
		text:    text,
		styled:  f.styles.Render(StyleHint, text),
		targets: []Target{{Rect: Rect{Width: lipgloss.Width(text), Height: 1}, Action: h.Action}},
	}, true
}

// statusLevels are the marks the header tries, richest first: every mark in full, every mark short, and
// then one mark fewer at a time.
func (f *Frame) statusLevels(marks []StatusMark) [][]StatusMark {
	short := make([]StatusMark, len(marks))
	for i, m := range marks {
		short[i] = m
		if m.Short != "" {
			short[i].Text = m.Short
		}
	}
	levels := [][]StatusMark{marks, short}
	for n := len(short) - 1; n >= 0; n-- {
		levels = append(levels, short[:n])
	}
	return levels
}

func (f *Frame) headerLeft(product string, marks []StatusMark) string {
	parts := []string{f.styles.Render(StyleTitle, product)}
	for _, m := range marks {
		if m.Kind == "" {
			parts = append(parts, f.styles.Render(StyleHint, Escape(m.Text)))
		} else {
			parts = append(parts, f.styles.Status(m.Kind, m.Text))
		}
	}
	return strings.Join(parts, headerGap)
}

// focusMark leads a bar: "> " while it has the focus, blanks otherwise, so nothing moves.
func (f *Frame) focusMark(focused bool) string {
	if focused {
		return f.styles.Render(StyleActive, "> ")
	}
	return "  "
}

// tabLine is the tab line: every area as "digit title", the active one as an accent fill, in brackets
// without colour, and the others dimmed. Where the line does not fit, the gaps around the inactive tabs
// close, then the inactive tabs shrink to their digit; the active tab stays whole.
func (f *Frame) tabLine(spec FrameSpec, width int) (string, []Target) {
	focused := spec.Focus == ZoneTabs
	mark := f.focusMark(focused)
	plain := f.styles.profile.Colorless()
	active := -1
	names := make([]string, len(spec.Areas))
	for i, a := range spec.Areas {
		names[i] = strconv.Itoa(i+1) + " " + Escape(lookup(f.text, a.Title))
		if a.ID == spec.ActiveArea {
			active = i
		}
	}
	variant := func(level int) []string {
		words := make([]string, len(names))
		for i, name := range names {
			switch {
			case i == active && plain:
				words[i] = "[" + name + "]"
			case i == active || level == 0:
				words[i] = " " + name + " "
			case level == 1:
				words[i] = name
			default:
				words[i] = strconv.Itoa(i + 1)
			}
		}
		return words
	}
	words := variant(0)
	for level := 1; level <= 2 && 2+lipgloss.Width(strings.Join(words, " ")) > width; level++ {
		words = variant(level)
	}
	var b strings.Builder
	b.WriteString(mark)
	var targets []Target
	x := 2
	for i, w := range words {
		if i > 0 {
			b.WriteString(" ")
			x++
		}
		style := f.styles.Style(StyleHint)
		if i == active {
			style = f.activeTab(plain)
		}
		b.WriteString(style.Render(w))
		cells := lipgloss.Width(w)
		if r, ok := clipRect(Rect{X: x, Width: cells, Height: 1}, width); ok {
			targets = append(targets, Target{Rect: r, Zone: ZoneTabs, Area: spec.Areas[i].ID})
		}
		x += cells
	}
	return b.String(), targets
}

// activeTab is the style of the active tab: the accent fill, bold without colour, where the brackets mark it.
func (f *Frame) activeTab(plain bool) lipgloss.Style {
	p := f.styles.palette
	if plain || p.Accent == "" || f.styles.profile > ProfileTrueColor {
		return f.styles.Style(StyleActiveRow)
	}
	style := lipgloss.NewStyle().Bold(true).Background(lipgloss.Color(string(p.Accent)))
	if p.Base != "" {
		style = style.Foreground(lipgloss.Color(string(p.Base)))
	}
	return style
}

// subTabs is the active area where it shows a sub-tab line.
func subTabs(spec FrameSpec) (Area, bool) {
	for _, a := range spec.Areas {
		if a.ID == spec.ActiveArea {
			return a, len(a.Tabs) > 1
		}
	}
	return Area{}, false
}

// subTabLine is the sub-tab line of area: its tabs as a tab strip, the active one as a selection fill and an
// accent fill while the line has the focus. A line too narrow for every tab shows the active one alone.
func (f *Frame) subTabLine(spec FrameSpec, area Area, width int) (string, []Target) {
	focused := spec.Focus == ZoneSubtabs
	words := make([]string, len(area.Tabs))
	ids := make([]string, len(area.Tabs))
	active := -1
	for i, t := range area.Tabs {
		words[i] = lookup(f.text, t.Title)
		ids[i] = t.ID
		if t.ID == spec.ActiveTab {
			active = i
		}
	}
	strip, spans := f.styles.TabStrip(words, active, focused)
	if 2+lipgloss.Width(strip) > width && active >= 0 {
		words, ids = words[active:active+1], ids[active:active+1]
		strip, spans = f.styles.TabStrip(words, 0, focused)
	}
	var targets []Target
	for i, sp := range spans {
		if r, ok := clipRect(Rect{X: 2 + sp.Start, Width: sp.End - sp.Start, Height: 1}, width); ok {
			targets = append(targets, Target{Rect: r, Zone: ZoneSubtabs, Area: area.ID, Tab: ids[i]})
		}
	}
	return f.focusMark(focused) + strip, targets
}

// clipRect cuts r at the right edge of a line of width cells; false where nothing of it is left.
func clipRect(r Rect, width int) (Rect, bool) {
	if r.X >= width || r.Width <= 0 {
		return Rect{}, false
	}
	r.Width = min(r.Width, width-r.X)
	return r, true
}

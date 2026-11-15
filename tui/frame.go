package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// AreaID is the stable, untranslated ID of an area of the tab line.
type AreaID string

// TabID is the stable, untranslated ID of a sub-tab of an area.
type TabID string

// Area is one tab of the tab line: a task the person came to do. An area with more than one tab shows
// them as sub-tabs below the tab line; it opens on its first.
type Area struct {
	ID    AreaID
	Title TextID
	// Icon is one glyph that passes CheckIcon and stands before the title on the tab line; empty shows none.
	Icon string
	Tabs []Tab
}

// Tab is one sub-tab of an area.
type Tab struct {
	ID    TabID
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
	Tab    TabID
	// Item names the element a click hit inside a part that draws many, such as a row of a list or a field
	// of a filter bar. It is opaque: the part that drew the target reads it back in its Click.
	Item string
}

// StatusMark is a part of the header after the product name; the marks stand in the order of
// FrameSpec.Status. Kind gives it the status glyph in its colour and leaves the word plain; without a kind
// the text stands dimmed alone.
type StatusMark struct {
	Kind StatusKind
	Text string
	// Short are the shorter forms of Text the header takes as room runs out, longest first; empty ones are
	// skipped.
	Short []string
	// Rank says what the mark gives way to; the zero value gives way first.
	Rank MarkRank
	// NoGlyph draws the text in the colour of Kind without the status glyph, for a mark whose words carry its
	// state alone.
	NoGlyph bool
}

// MarkRank is the place of a status mark in the order the header fills its room: the name and the marks
// that always stand, then the marks before keys, then the widest block at the right that fits two cells
// apart from them (the back button with the header keys, the back button, or nothing), and last the
// normal marks in the room left.
type MarkRank uint8

const (
	// MarkNormal takes the room the back button and the header keys leave: the normal marks take their
	// shorter forms together and then go, the last first, before the header keys go.
	MarkNormal MarkRank = iota
	// MarkBeforeKeys takes the longest of its forms that fits before the back button and the header keys are
	// placed, and goes only where none fits.
	MarkBeforeKeys
	// MarkAlways stands at every width: where the name and these marks do not fit, they take their shorter
	// forms together, and then the line is cut at its end with the ellipsis, so none goes unmarked.
	MarkAlways
)

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
	// ProductShort replaces Product where the full name, the marks that always stand and the widest block at
	// the right do not fit side by side; empty keeps Product.
	ProductShort string
	// Icon is the logo before the product name: one glyph that passes CheckIcon; empty shows none. The frame
	// draws no glyph that fails CheckIcon, and CheckFrame reports one.
	Icon   string
	Status []StatusMark
	// Back is the back button of the header, where the way back leads a level up; nil shows none.
	Back *Hint
	// HeaderKeys stand dimmed at the right end of the header, such as the help and command keys.
	HeaderKeys []Hint
	Areas      []Area
	ActiveArea AreaID
	ActiveTab  TabID
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
	text Translator
	keys *Keymap
}

// NewFrame returns a frame that takes its key labels from keys and its texts from text.
func NewFrame(keys *Keymap, text Translator) *Frame {
	return &Frame{text: text, keys: keys}
}

// frameRun is one drawing of a frame with the styles of that drawing.
type frameRun struct {
	styles Styles
	text   Translator
	keys   *Keymap
}

// Workspace is the room the content of spec has: the full width between the rule and the footer, less the
// rows of the message. The footer takes one row, or two where its hints do not fit one.
func (f *Frame) Workspace(spec FrameSpec) Rect {
	// The rows of the footer and the message do not depend on the styles.
	return (&frameRun{text: f.text, keys: f.keys}).workspace(spec)
}

// Render draws spec with styles as exactly Height lines of exactly Width cells and returns the targets of the
// drawn header keys, tabs and footer keys. The footer is always the last line or the last two; the workspace
// is cut or padded to its rows, and a terminal lower than the frame loses the rows above the footer from the
// bottom up.
func (f *Frame) Render(styles Styles, spec FrameSpec) (string, []Target) {
	return (&frameRun{styles: styles, text: f.text, keys: f.keys}).render(spec)
}

func (f *frameRun) workspace(spec FrameSpec) Rect {
	top, rows, _ := f.rows(spec)
	return Rect{X: 0, Y: top, Width: max(spec.Width, 0), Height: rows - len(f.messageLines(spec, rows))}
}

// rows are the number of rows above the workspace, the rows of the workspace and the rows of the footer.
func (f *frameRun) rows(spec FrameSpec) (top, workspace, footer int) {
	top = 2 // header and rule
	if len(spec.Areas) > 0 {
		top++
	}
	if _, ok := subTabs(spec); ok {
		top++
	}
	footer = footerRows(f.keys.Hints(spec.Footer, spec.Input, f.text), spec.Position, max(spec.Width, 0))
	return top, max(spec.Height-top-footer, 0), footer
}

// messageLines are the lines of the message within room rows; a message longer than the room keeps its
// end, where the way out stands.
func (f *frameRun) messageLines(spec FrameSpec, room int) []string {
	if spec.Message == nil || spec.Message.Text == "" || room <= 0 {
		return nil
	}
	lines := strings.Split(f.styles.Message(spec.Message.Kind, spec.Message.Text, max(spec.Width, 1)), "\n")
	return lines[max(len(lines)-room, 0):]
}

func (f *frameRun) render(spec FrameSpec) (string, []Target) {
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
	_, rows, footerRows := f.rows(spec)
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
	foot, footTargets := f.styles.footerLines(f.keys.Hints(spec.Footer, spec.Input, f.text), spec.Position, width, footerRows)
	// A terminal lower than the frame loses the rows above the footer first, then the first footer row.
	if drop := len(foot) - spec.Height; drop > 0 {
		foot = foot[drop:]
		footTargets = OffsetTargets(footTargets, 0, -drop)
	}
	if keep := spec.Height - len(foot); len(lines) > keep {
		lines = lines[:keep]
		kept := targets[:0]
		for _, t := range targets {
			if t.Rect.Y < keep {
				kept = append(kept, t)
			}
		}
		targets = kept
	}
	for _, t := range footTargets {
		if t.Rect.Y >= 0 {
			t.Rect.Y += len(lines)
			targets = append(targets, t)
		}
	}
	lines = append(lines, foot...)
	return strings.Join(lines, "\n"), targets
}

func (f *frameRun) ruleChar() string {
	if f.styles.charset == CharsetUnicode {
		return "─"
	}
	return "-"
}

// headerGap stands between two parts of the header.
const headerGap = "  "

// header is the header line: the product name and the status marks at the left, the back button and the
// header keys dimmed at the right end. It stays one line and fills its room in the order MarkRank names.
func (f *frameRun) header(spec FrameSpec, width int) (string, []Target) {
	blocks := f.headerBlocks(spec)
	marks := spec.Status
	forms := make([][]string, len(marks))
	var always, beforeKeys, normal []int
	for i, m := range marks {
		forms[i] = markForms(m)
		switch m.Rank {
		case MarkAlways:
			always = append(always, i)
		case MarkBeforeKeys:
			beforeKeys = append(beforeKeys, i)
		default:
			normal = append(normal, i)
		}
	}
	// pick holds the form each mark is drawn in; -1 leaves it out.
	pick := make([]int, len(marks))
	for i := range pick {
		pick[i] = -1
	}
	level := func(idx []int, depth int) {
		for _, i := range idx {
			pick[i] = min(depth, len(forms[i])-1)
		}
	}
	left := func(product string) string { return f.headerLeft(product, marks, forms, pick) }

	level(always, 0)
	product := f.productName(spec.Icon, spec.Product)
	if spec.ProductShort != "" {
		need := lipgloss.Width(left(product))
		if len(blocks) > 0 {
			need += len(headerGap) + lipgloss.Width(blocks[0].text)
		}
		if need > width {
			product = f.productName(spec.Icon, spec.ProductShort)
		}
	}
	fits := false
	for depth := range max(formDepth(forms, always), 1) {
		level(always, depth)
		if fits = lipgloss.Width(left(product)) <= width; fits {
			break
		}
	}
	if !fits && len(always) == 0 {
		return f.styles.Render(StyleTitle, f.styles.Truncate(product, width)), nil
	}
	if !fits {
		return f.styles.Truncate(left(product), width), nil
	}
	used := lipgloss.Width(left(product))
	for _, i := range beforeKeys {
		for k, form := range forms[i] {
			if w := len(headerGap) + lipgloss.Width(f.markText(marks[i], form)); used+w <= width {
				pick[i], used = k, used+w
				break
			}
		}
	}
	var block headerBlock
	room := width
	for _, b := range blocks {
		if w := lipgloss.Width(b.text); width-used-w >= len(headerGap) {
			block, room = b, width-w-len(headerGap)
			break
		}
	}
	for depth := range max(formDepth(forms, normal), 1) {
		level(normal, depth)
		if lipgloss.Width(left(product)) <= room {
			break
		}
	}
	for n := len(normal) - 1; n >= 0 && lipgloss.Width(left(product)) > room; n-- {
		pick[normal[n]] = -1
	}
	line := left(product)
	if block.text == "" {
		return line, nil
	}
	x := width - lipgloss.Width(block.text)
	ts := make([]Target, len(block.targets))
	for i, t := range block.targets {
		t.Rect.X += x
		ts[i] = t
	}
	return line + strings.Repeat(" ", x-lipgloss.Width(line)) + block.styled, ts
}

// headerBlocks are the blocks the right end of the header can show, widest first: the back button with the
// header keys, the back button alone, or the header keys alone where there is no back button.
func (f *frameRun) headerBlocks(spec FrameSpec) []headerBlock {
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
		return []headerBlock{back.join(keys), back}
	case hasBack:
		return []headerBlock{back}
	case keys.text != "":
		return []headerBlock{keys}
	}
	return nil
}

func (f *frameRun) productName(icon, name string) string {
	name = Escape(name)
	if icon := f.icon(icon); icon != "" {
		return icon + " " + name
	}
	return name
}

// markForms are the forms of m, longest first.
func markForms(m StatusMark) []string {
	forms := []string{m.Text}
	for _, s := range m.Short {
		if s != "" {
			forms = append(forms, s)
		}
	}
	return forms
}

// formDepth is the number of forms of the mark among idx that has the most.
func formDepth(forms [][]string, idx []int) int {
	depth := 0
	for _, i := range idx {
		depth = max(depth, len(forms[i]))
	}
	return depth
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
func (f *frameRun) headerBlock(spec FrameSpec, h *Hint) (headerBlock, bool) {
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

// headerLeft is the name and every mark pick draws, in the order of the marks.
func (f *frameRun) headerLeft(product string, marks []StatusMark, forms [][]string, pick []int) string {
	parts := []string{f.styles.Render(StyleTitle, product)}
	for i, m := range marks {
		if pick[i] >= 0 {
			parts = append(parts, f.markText(m, forms[i][pick[i]]))
		}
	}
	return strings.Join(parts, headerGap)
}

func (f *frameRun) markText(m StatusMark, text string) string {
	switch {
	case m.Kind == "":
		return f.styles.Render(StyleHint, Escape(text))
	case m.NoGlyph:
		return f.styles.Render(statusStyle(m.Kind), Escape(text))
	}
	return f.styles.Status(m.Kind, text)
}

// icon is glyph where it may stand: the terminal draws UTF-8 and glyph passes CheckIcon. The check runs again
// here, so text that never went through CheckFrame cannot reach the terminal unescaped.
func (f *frameRun) icon(glyph string) string {
	if glyph == "" || f.styles.charset != CharsetUnicode || CheckIcon(glyph) != nil {
		return ""
	}
	return glyph
}

// CheckFrame reports why spec cannot be drawn as it is asked: an icon of the frame or of an area that fails
// CheckIcon. Applications run it in their tests or at start, with CheckAreas.
func CheckFrame(spec FrameSpec) error {
	var errs []error
	if spec.Icon != "" {
		if err := CheckIcon(spec.Icon); err != nil {
			errs = append(errs, fmt.Errorf("frame: %w", err))
		}
	}
	return errors.Join(append(errs, checkAreaIcons(spec.Areas)...)...)
}

// focusMark leads a bar: "> " while it has the focus, blanks otherwise, so nothing moves.
func (f *frameRun) focusMark(focused bool) string {
	if focused {
		return f.styles.Render(StyleActive, "> ")
	}
	return "  "
}

// tabLine is the tab line: every area as "digit title", the active one as an accent fill, in brackets
// without colour, and the others dimmed. Where the line does not fit, the gaps around the inactive tabs
// close, then the inactive tabs shrink to their digit; the active tab stays whole.
func (f *frameRun) tabLine(spec FrameSpec, width int) (string, []Target) {
	focused := spec.Focus == ZoneTabs
	mark := f.focusMark(focused)
	plain := f.styles.profile.Colorless()
	active := -1
	names := make([]string, len(spec.Areas))
	iconNames := make([]string, len(spec.Areas))
	anyIcon := false
	for i, a := range spec.Areas {
		title := Escape(lookup(f.text, a.Title))
		names[i] = strconv.Itoa(i+1) + " " + title
		iconNames[i] = names[i]
		if icon := f.icon(a.Icon); icon != "" {
			iconNames[i] = strconv.Itoa(i+1) + " " + icon + " " + title
			anyIcon = true
		}
		if a.ID == spec.ActiveArea {
			active = i
		}
	}
	variant := func(level int, icons bool) []string {
		words := make([]string, len(names))
		for i, name := range names {
			if icons {
				name = iconNames[i]
			}
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
	// The icons go before any other cut; the digit-only level never holds one.
	fits := func(words []string) bool { return 2+lipgloss.Width(strings.Join(words, " ")) <= width }
	words := variant(0, anyIcon)
	if anyIcon && !fits(words) {
		words = variant(0, false)
	}
	for level := 1; level <= 2 && !fits(words); level++ {
		words = variant(level, false)
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
func (f *frameRun) activeTab(plain bool) lipgloss.Style {
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
func (f *frameRun) subTabLine(spec FrameSpec, area Area, width int) (string, []Target) {
	focused := spec.Focus == ZoneSubtabs
	words := make([]string, len(area.Tabs))
	ids := make([]TabID, len(area.Tabs))
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

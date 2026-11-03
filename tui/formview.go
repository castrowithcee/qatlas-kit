package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// formListMinWidth is the width from which the sections stand as a list at the left of the fields.
	formListMinWidth = 100
	// minLabelColumn is the narrowest the label column is cut to; a longer label pushes its own value.
	minLabelColumn = 12
	// minBoxInner is the narrowest value a framed field shows; below it fields lose their frames.
	minBoxInner = 8
	// minFlatValue is the narrowest value a one-line field shows; below it the form asks for a larger terminal.
	minFlatValue = 6
)

// problemMark leads the line of a problem, so the line reads as an error without colour.
const problemMark = "[!]"

// formLayout is one way to draw a form; View tries them from the richest to the plainest.
type formLayout struct {
	list, boxed, headings bool
}

// formBlock is what one field draws: its rows, the hint under the focused one and its problems.
type formBlock struct {
	field   int
	lines   []string
	targets []Target
	// row is the line the value of the field starts on, and x the column, for the list of values.
	row, x int
}

// View draws the form in width by height cells. Where the sections stand as a list at the left, the fields
// of the focused section stand beside it in a panel under the title and lead of the section; otherwise each
// section is a heading above its fields. Each field is its label in one column and its value in a frame, the
// focused one behind the marker ">" with the frame and label in the accent colour. Where that does not fit,
// the fields lose their frames, then the headings go, then the fields around the focus stand alone with "…"
// for the rest; where even the focused field does not fit, the form asks for a larger terminal. The focus is
// always in view. Problems stand under their fields, those without a field above the form. It returns a
// target for the free area of the form, one per field, tick box, section and value of the open list.
func (f *FormModel) View(width, height int, styles Styles) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if height == 0 {
		return "", nil
	}
	var layouts []formLayout
	if len(f.sections) >= 2 && width >= formListMinWidth && height >= len(f.sections)+8 {
		layouts = append(layouts, formLayout{list: true, boxed: true}, formLayout{list: true})
	}
	layouts = append(layouts, formLayout{boxed: true, headings: true}, formLayout{headings: true}, formLayout{})
	for _, lay := range layouts {
		lines, targets, row, x, ok := f.draw(lay, width, styles)
		if ok && len(lines) <= height {
			return f.finish(lines, targets, row, x, width, height, styles)
		}
	}
	if lines, targets, row, x, ok := f.window(width, height, styles); ok {
		return f.finish(lines, targets, row, x, width, height, styles)
	}
	return f.sizeHint(width, height, styles), nil
}

// finish cuts the lines to the screen, draws the open list of values below the focused field and adds the
// free area of the form beneath every other target.
func (f *FormModel) finish(lines []string, targets []Target, row, x, width, height int, styles Styles) (string, []Target) {
	out := make([]string, height)
	for i := range out {
		if i < len(lines) {
			out[i] = Pad(Clip(lines[i], width), width)
		} else {
			out[i] = strings.Repeat(" ", width)
		}
	}
	all := []Target{{Rect: Rect{Width: width, Height: height}, Zone: ZoneForm}}
	for _, t := range targets {
		if t.Rect.Y < height {
			all = append(all, t)
		}
	}
	if f.open {
		box, ts := f.valueList(styles, width-x)
		// Below the field, else above it; it never covers the field itself.
		top := row + 1
		if top+len(box) > height && row-len(box) >= 0 {
			top = row - len(box)
		}
		for i, line := range box {
			if y := top + i; y < height {
				out[y] = ansi.Cut(out[y], 0, x) + line + ansi.Cut(out[y], x+lipgloss.Width(line), width)
			}
		}
		for _, t := range OffsetTargets(ts, x, top) {
			if t.Rect.Y < height {
				all = append(all, t)
			}
		}
	}
	return strings.Join(out, "\n"), all
}

// valueList draws the open list of values of the focused field in at most width cells and returns a target
// per value, counted from its top left.
func (f *FormModel) valueList(styles Styles, width int) ([]string, []Target) {
	field := f.fields[f.focus]
	labels := make([]string, len(field.Choices))
	inner := 0
	for j, c := range field.Choices {
		labels[j] = Escape(f.text.Lookup(c.Label))
		inner = max(inner, lipgloss.Width(labels[j])+2)
	}
	inner = max(min(inner, width-4), 1)
	lines := make([]string, len(labels))
	var targets []Target
	for j, label := range labels {
		lines[j] = styles.Row(label, inner, j == f.pick)
		targets = append(targets, Target{Rect: Rect{X: 2, Y: 1 + j, Width: inner, Height: 1}, Zone: ZoneForm,
			Action: f.keys.Enter, Item: formOptionRef + field.ID + "\x00" + string(field.Choices[j].ID)})
	}
	return styles.Panel("", "", lines, inner+4, len(lines)+2, true), targets
}

// draw lays the form out in lay; ok is false where the fields do not fit the width that way.
func (f *FormModel) draw(lay formLayout, width int, styles Styles) (lines []string, targets []Target, row, x int, ok bool) {
	lines = f.otherProblems(width, styles)
	top := len(lines)
	if !lay.list {
		blocks, ok := f.blocks(width, lay.boxed, lay.headings, "", styles)
		if !ok {
			return nil, nil, 0, 0, false
		}
		for _, b := range blocks {
			if b.field == f.focus {
				row, x = len(lines)+b.row, b.x
			}
			targets = append(targets, OffsetTargets(b.targets, 0, len(lines))...)
			lines = append(lines, b.lines...)
		}
		return lines, targets, row, x, true
	}
	list, listTargets, listWidth := f.sectionList(styles)
	inner := width - listWidth - 1 - 4
	current := f.fields[f.focus].Section
	section, _ := f.section(current)
	head := []string{styles.Render(StyleTitle, styles.Truncate(Escape(f.text.Lookup(section.Title)), inner))}
	if section.Lead != "" {
		head = append(head, styles.Render(StyleHint, styles.Truncate(Escape(f.text.Lookup(section.Lead)), inner)))
	}
	if lay.boxed {
		head = append(head, "")
	}
	blocks, ok := f.blocks(inner, lay.boxed, false, current, styles)
	if !ok {
		return nil, nil, 0, 0, false
	}
	content := head
	x0 := listWidth + 1 + 2
	for _, b := range blocks {
		if b.field == f.focus {
			row, x = top+1+len(content)+b.row, x0+b.x
		}
		targets = append(targets, OffsetTargets(b.targets, x0, top+1+len(content))...)
		content = append(content, b.lines...)
	}
	panel := styles.Panel("", "", content, inner+4, len(content)+2, true)
	for i := range max(len(panel), len(list)) {
		left := strings.Repeat(" ", listWidth)
		if i < len(list) {
			left = list[i]
		}
		right := ""
		if i < len(panel) {
			right = panel[i]
		}
		lines = append(lines, left+" "+right)
	}
	targets = append(OffsetTargets(listTargets, 0, top), targets...)
	return lines, targets, row, x, true
}

// window draws the one-line fields around the focus that fit height, with "…" where fields are left out.
func (f *FormModel) window(width, height int, styles Styles) (lines []string, targets []Target, row, x int, ok bool) {
	blocks, ok := f.blocks(width, false, false, "", styles)
	if !ok {
		return nil, nil, 0, 0, false
	}
	at := 0
	for i, b := range blocks {
		if b.field == f.focus {
			at = i
		}
	}
	lines = f.otherProblems(width, styles)
	body := func(s, e int) int {
		n := 0
		for _, b := range blocks[s:e] {
			n += len(b.lines)
		}
		return n
	}
	start, end := at, at+1
	if len(lines)+body(start, end) > height {
		lines = nil
	}
	if body(start, end) > height {
		return nil, nil, 0, 0, false
	}
	// Each side that leaves fields out takes one line for its mark.
	size := func(s, e int) int {
		n := len(lines) + body(s, e)
		if s > 0 {
			n++
		}
		if e < len(blocks) {
			n++
		}
		return n
	}
	for grew := true; grew; {
		grew = false
		if end < len(blocks) && size(start, end+1) <= height {
			end, grew = end+1, true
		}
		if start > 0 && size(start-1, end) <= height {
			start, grew = start-1, true
		}
	}
	more := styles.Render(StyleHint, "  "+styles.Glyph(GlyphEllipsis))
	shown := len(lines) + body(start, end)
	top := start > 0 && shown+1 <= height
	bottom := end < len(blocks) && shown+1+boolInt(top) <= height
	if top {
		lines = append(lines, more)
	}
	for _, b := range blocks[start:end] {
		if b.field == f.focus {
			row, x = len(lines)+b.row, b.x
		}
		targets = append(targets, OffsetTargets(b.targets, 0, len(lines))...)
		lines = append(lines, b.lines...)
	}
	if bottom {
		lines = append(lines, more)
	}
	return lines, targets, row, x, true
}

// sizeHint asks for a terminal large enough for the focused field.
func (f *FormModel) sizeHint(width, height int, styles Styles) string {
	needH := 1
	if blocks, ok := f.blocks(max(width, 2+minLabelColumn+1+minFlatValue), false, false, "", styles); ok {
		for _, b := range blocks {
			if b.field == f.focus {
				needH = len(b.lines)
			}
		}
	}
	needW := 2 + min(f.labelWidth(), minLabelColumn) + 1 + minFlatValue
	lines := []string{
		styles.Render(StyleTitle, Escape(f.text.Lookup(TextResizeTitle))),
		Escape(f.text.Lookup(TextResizeNeed, needW, needH)),
		Escape(f.text.Lookup(TextResizeHave, width, height)),
	}
	out := make([]string, height)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out[i] = Pad(Clip(line, width), width)
	}
	return strings.Join(out, "\n")
}

// otherProblems are the lines of the problems that stand under no field.
func (f *FormModel) otherProblems(width int, styles Styles) []string {
	var lines []string
	for _, p := range f.other {
		lines = append(lines, f.problemLines(p, width, "", styles)...)
	}
	return lines
}

func (f *FormModel) problemLines(p FormProblem, width int, indent string, styles Styles) []string {
	text := p.Cause
	if p.NextStep != "" {
		text += " - " + p.NextStep
	}
	var lines []string
	for _, l := range Wrap(problemMark+" "+Escape(f.redact(text)), max(width-len(indent), 1)) {
		lines = append(lines, indent+styles.Render(StyleFail, l))
	}
	return lines
}

// redact replaces what was typed into a masked field wherever a problem repeats it.
func (f *FormModel) redact(text string) string {
	for i := range f.fields {
		if field := &f.fields[i]; field.Kind == FieldMasked {
			text = redactSecret(text, field.line.String())
		}
	}
	return text
}

func (f *FormModel) label(i int) string { return Escape(f.text.Lookup(f.fields[i].Label)) }

func (f *FormModel) labelWidth() int {
	w := 0
	for i := range f.fields {
		w = max(w, lipgloss.Width(f.label(i)))
	}
	return w
}

// sectionTitle is the title of a section with the marks of a closed section and of one with a problem.
func (f *FormModel) sectionTitle(s FormSection, styles Styles) (string, bool) {
	closed := f.closed(s.ID)
	text := Escape(f.text.Lookup(s.Title))
	if closed {
		text += " " + Escape(f.text.Lookup(TextFormSectionClosed))
	}
	for i := range f.fields {
		if f.fields[i].Section == s.ID && len(f.problems[f.fields[i].ID]) > 0 {
			text += " " + problemMark
			break
		}
	}
	return text, closed
}

// sectionList draws the numbered sections in a panel, the one of the focus behind ">" as a filled bar and a
// closed one dimmed, and returns it with a target per section and its width.
func (f *FormModel) sectionList(styles Styles) ([]string, []Target, int) {
	current := f.fields[f.focus].Section
	texts := make([]string, len(f.sections))
	dim := make([]bool, len(f.sections))
	inner := 0
	for i, s := range f.sections {
		title, closed := f.sectionTitle(s, styles)
		texts[i], dim[i] = strconv.Itoa(i+1)+" "+title, closed
		inner = max(inner, 2+lipgloss.Width(texts[i]))
	}
	lines := make([]string, len(texts))
	var targets []Target
	for i, s := range f.sections {
		line := Pad("  "+texts[i], inner)
		switch {
		case s.ID == current:
			line = styles.Render(StyleActiveRow, Pad("> "+texts[i], inner))
		case dim[i]:
			line = styles.Render(StyleHint, line)
		}
		lines[i] = line
		targets = append(targets, Target{Rect: Rect{X: 2, Y: 1 + i, Width: inner, Height: 1}, Zone: ZoneForm,
			Item: formSectionRef + s.ID})
	}
	return styles.Panel("", "", lines, inner+4, len(lines)+2, false), targets, inner + 4
}

// blocks lays out the fields in width cells, framed or on one line, with the headings of the sections where
// headings is set, and only the fields of the section only where it is set. ok is false where the values do
// not fit beside the labels.
func (f *FormModel) blocks(width int, boxed, headings bool, only string, styles Styles) ([]formBlock, bool) {
	labelCol := min(f.labelWidth(), max(minLabelColumn, width/3))
	headW := 2 + labelCol + 1
	valueW := width - headW
	if boxed && valueW-4 < minBoxInner || valueW < minFlatValue {
		return nil, false
	}
	var out []formBlock
	last := ""
	for i := range f.fields {
		field := &f.fields[i]
		if only != "" && field.Section != only {
			continue
		}
		if headings && field.Section != "" && field.Section != last {
			last = field.Section
			s, _ := f.section(field.Section)
			title, _ := f.sectionTitle(s, styles)
			out = append(out, formBlock{field: -1, lines: []string{styles.Render(StyleTitle, title)},
				targets: []Target{{Rect: Rect{Width: width, Height: 1}, Zone: ZoneForm, Item: formSectionRef + s.ID}}})
		}
		if f.closed(field.Section) {
			continue
		}
		out = append(out, f.block(i, width, headW, labelCol, boxed, styles))
	}
	return out, true
}

// block draws field i: the marker and label in their column, the value framed or on its surface, the hint
// under the focused field and the problems of the field.
func (f *FormModel) block(i, width, headW, labelCol int, boxed bool, styles Styles) formBlock {
	field := &f.fields[i]
	focused := i == f.focus
	valueW := width - headW
	label := f.label(i)
	marker := "  "
	if focused {
		marker = "> "
	}
	head := marker + label + strings.Repeat(" ", max(labelCol-lipgloss.Width(label), 0)) + " "
	if focused {
		head = styles.Render(StyleActive, head)
	}
	if lipgloss.Width(head) > headW {
		// A label longer than its column pushes its own value.
		valueW = max(width-lipgloss.Width(head), 1)
	}
	headW = width - valueW
	blank := strings.Repeat(" ", headW)
	b := formBlock{field: i}
	var values []string
	var ticks []Target
	if boxed {
		values, ticks = f.valueLines(i, valueW-4, styles)
		box := styles.Panel("", "", values, valueW, len(values)+2, focused)
		for j, line := range box {
			if j == 1 {
				b.lines = append(b.lines, head+line)
			} else {
				b.lines = append(b.lines, blank+line)
			}
		}
		b.targets = append(b.targets, OffsetTargets(ticks, headW+2, 1)...)
		b.row, b.x = len(box)-1, headW
	} else {
		values, ticks = f.valueLines(i, valueW, styles)
		surface := fieldStyle(styles, focused)
		for j, v := range values {
			line := surface.Render(keepOpen(Pad(Clip(v, valueW), valueW), openingOf(surface)))
			if j == 0 {
				b.lines = append(b.lines, head+line)
			} else {
				b.lines = append(b.lines, blank+line)
			}
		}
		b.targets = append(b.targets, OffsetTargets(ticks, headW, 0)...)
		b.row, b.x = 0, headW
	}
	action := ActionID("")
	if field.Kind == FieldChoice || field.Kind == FieldList {
		action = f.keys.Enter
	}
	if field.Kind != FieldReadOnly {
		row := Target{Rect: Rect{Width: width, Height: len(b.lines)}, Zone: ZoneForm, Action: action, Item: formFieldRef + field.ID}
		b.targets = append([]Target{row}, b.targets...)
	}
	indent := blank
	if valueW < 2*minFlatValue {
		indent = "    "
	}
	if focused && field.Hint != "" && field.Kind != FieldReadOnly {
		for _, l := range Wrap(Escape(f.text.Lookup(field.Hint)), max(width-len(indent), 1)) {
			b.lines = append(b.lines, indent+styles.Render(StyleHint, l))
		}
	}
	for _, p := range f.problems[field.ID] {
		b.lines = append(b.lines, f.problemLines(p, width, indent, styles)...)
	}
	return b
}

// valueLines draws the value of field i in width cells and returns the targets of its tick boxes, counted
// from the top left of the value.
func (f *FormModel) valueLines(i, width int, styles Styles) ([]string, []Target) {
	field := &f.fields[i]
	focused := i == f.focus
	hint := func(id TextID) []string {
		return []string{styles.Render(StyleHint, styles.Truncate(Escape(f.text.Lookup(id)), width))}
	}
	text := field.line.String()
	switch field.Kind {
	case FieldText:
		switch {
		case focused:
			return []string{field.line.view(width, styles, true, "")}, nil
		case text == "":
			return hint(TextFormEmpty), nil
		}
		return Wrap(Escape(text), width), nil
	case FieldMasked:
		switch {
		case focused:
			return []string{field.line.view(width, styles, true, styles.Glyph(GlyphMask))}, nil
		case text != "":
			return hint(TextFormMaskedEntered), nil
		case field.Stored:
			return hint(TextFormMaskedKept), nil
		}
		return hint(TextFormEmpty), nil
	case FieldChoice:
		label := Escape(f.text.Lookup(field.Choices[field.choice].Label))
		room := max(width-2, 1)
		return []string{Pad(styles.Truncate(label, room), room) + " " + styles.Glyph(GlyphExpanded)}, nil
	case FieldMultiple:
		return f.ticks(i, width, styles)
	case FieldKey:
		switch {
		case focused && f.capture:
			return hint(TextFormKeyPress), nil
		case text == "":
			return hint(TextFormEmpty), nil
		}
		return []string{styles.Truncate(Escape(text), width)}, nil
	case FieldList:
		if text == "" {
			return hint(TextValueNone), nil
		}
		return []string{styles.Truncate(Escape(text), width)}, nil
	}
	line := Escape(text)
	if field.Hint != "" {
		line += " (" + Escape(f.text.Lookup(field.Hint)) + ")"
	}
	return Wrap(line, width), nil
}

// ticks draws the values of a multiple choice as tick boxes, the one under the cursor of the focused field
// behind ">", and breaks the line between values only. Each value is a target of the toggle key.
func (f *FormModel) ticks(i, width int, styles Styles) ([]string, []Target) {
	field := &f.fields[i]
	var lines []string
	var targets []Target
	line, x := "", 0
	for j, c := range field.Choices {
		lead := " "
		if i == f.focus && j == field.cursor {
			lead = styles.Render(StyleActive, ">")
		}
		part := lead + styles.TickBox(field.ticked[j]) + " " + Escape(f.text.Lookup(c.Label))
		w := lipgloss.Width(part)
		if x > 0 && x+1+w > width {
			lines, line, x = append(lines, line), "", 0
		}
		if x > 0 {
			line += " "
			x++
		}
		targets = append(targets, Target{Rect: Rect{X: x, Y: len(lines), Width: min(w, width-x), Height: 1}, Zone: ZoneForm,
			Action: f.keys.Toggle, Item: formTickRef + field.ID + "\x00" + string(c.ID)})
		line += part
		x += w
	}
	return append(lines, line), targets
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

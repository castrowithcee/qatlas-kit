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
	// minLabelColumn is the narrowest the label column is cut to; a longer label wraps inside it.
	minLabelColumn = 12
	// minBoxInner is the narrowest value a framed field shows; below it the fields lose their frames.
	minBoxInner = 8
	// minFlatValue is the narrowest value a one-line field shows; below it the form asks for a larger terminal.
	minFlatValue = 6
)

// problemMark leads the line of a problem, so the line reads as an error without colour.
const problemMark = "[!]"

// formLayout is one way to draw a form: the sections as a list beside the fields, framed fields, and the
// headings of the sections between the fields.
type formLayout struct {
	list, boxed, headings bool
}

// layoutResult says why a layout was not drawn: the width does not hold it, or the focused field does not fit
// the height even when the form scrolls.
type layoutResult int

const (
	layoutDrawn layoutResult = iota
	layoutNarrow
	layoutShort
)

// formBlock is what one field draws: its rows, the hint under the focused one and its problems.
type formBlock struct {
	field   int
	lines   []string
	targets []Target
	// row is the line the value of the field starts on, and x the column, for the list of values.
	row, x int
	// top is the line the frame of the field starts on, which a calendar above it ends before.
	top int
}

// formDrawing is a layout drawn into lines, with the position of the focused value.
type formDrawing struct {
	lines   []string
	targets []Target
	row, x  int
	top     int
}

// View draws the form with styles in width by height cells. Where the sections stand as a list at the left,
// the fields of the focused section stand beside it in a panel under the title and lead of the section;
// otherwise each section is a heading above its fields. Each field is its label in a column per section and
// its value in a frame, the focused one behind the marker ">" with the frame and label in the accent colour.
// A label longer than its column wraps inside it. The frames go only where the width does not hold them; then
// a value stands on its own surface only in the focused field, so two fields never run together. Where the
// height is short, the form scrolls: whole fields around the focus stand in the same layout, with "…" above
// and below where fields are left out, and the focus is always in view. Where even the focused field does not
// fit, the form asks for a larger terminal; it never drops the frames for the height. Problems stand under
// their fields, those without a field above the form. It returns a target for the free area of the form, one
// per visible field, tick box, section and value of the open list.
func (f *Form) View(styles Styles, width, height int) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if height == 0 {
		return "", nil
	}
	if rows, targets := f.buttonRows(width, styles); len(rows) > 0 {
		// The buttons keep the bottom rows. A field with the focus gives way to them only if it cannot be drawn
		// above them; a button with the focus never does, and the fields get what is left.
		need := len(rows) + 1
		var view string
		var ts []Target
		ok := false
		if height > need {
			view, ts, ok = f.fieldsView(styles, width, height-need)
		}
		if ok {
			lines := strings.Split(view, "\n")
			lines = append(lines, Pad("", width))
			for _, row := range rows {
				lines = append(lines, Pad(Clip(row, width), width))
			}
			// The free area of the form is the first target and reaches down to the buttons.
			ts[0].Rect.Height = height
			return strings.Join(lines, "\n"), append(ts, OffsetTargets(targets, 0, height-len(rows))...)
		}
		if f.btn >= 0 {
			if height < len(rows) {
				return f.sizeHint(width, height, styles), nil
			}
			lines := make([]string, height)
			for i := range lines {
				lines[i] = Pad("", width)
			}
			for i, row := range rows {
				lines[height-len(rows)+i] = Pad(Clip(row, width), width)
			}
			free := Target{Rect: Rect{Width: width, Height: height}, Zone: ZoneForm}
			return strings.Join(lines, "\n"), append([]Target{free}, OffsetTargets(targets, 0, height-len(rows))...)
		}
	}
	view, targets, _ := f.fieldsView(styles, width, height)
	return view, targets
}

// fieldsView is View of the fields alone; ok is false where it asks for a larger terminal.
func (f *Form) fieldsView(styles Styles, width, height int) (view string, targets []Target, ok bool) {
	try := func(lay formLayout, strict bool) (string, []Target, layoutResult) {
		d, res := f.draw(lay, width, height, strict, styles)
		if res != layoutDrawn {
			return "", nil, res
		}
		view, targets := f.finish(d.lines, d.targets, d.row, d.top, d.x, width, height, styles)
		return view, targets, res
	}
	if len(f.sections) >= 2 && width >= formListMinWidth {
		if view, targets, res := try(formLayout{list: true, boxed: true}, true); res == layoutDrawn {
			return view, targets, true
		} else if res == layoutNarrow {
			if view, targets, res := try(formLayout{list: true}, true); res == layoutDrawn {
				return view, targets, true
			}
		}
	}
	view, targets, res := try(formLayout{boxed: true, headings: true}, true)
	if res == layoutDrawn {
		return view, targets, true
	}
	// The frames and headings give way to the width alone; where the height is short, the form asks for more.
	if res == layoutNarrow {
		if view, targets, res = try(formLayout{headings: true}, true); res == layoutDrawn {
			return view, targets, true
		}
		if view, targets, res = try(formLayout{}, false); res == layoutDrawn {
			return view, targets, true
		}
	}
	return f.sizeHint(width, height, styles), nil, false
}

// finish cuts the lines to the screen, draws the open list of values below the focused field and adds the
// free area of the form beneath every other target.
func (f *Form) finish(lines []string, targets []Target, row, above, x, width, height int, styles Styles) (string, []Target) {
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
		// Under the open calendar only the symbol of its own field still answers.
		if t.Rect.Y < height && (!f.cal || t.Item == formCalendarRef+string(f.fields[f.focus].ID)) {
			all = append(all, t)
		}
	}
	if f.cal {
		box, ts := f.calendar(styles)
		boxW := lipgloss.Width(box[0])
		x0 := max(min(x, width-boxW), 0)
		// Below the field, else above it; it never covers the field itself.
		top := row + 1
		if top+len(box) > height && above-len(box) >= 0 {
			top = above - len(box)
		}
		for i, line := range box {
			if y := top + i; y < height {
				line = Clip(line, width-x0)
				out[y] = ansi.Cut(out[y], 0, x0) + line + ansi.Cut(out[y], x0+lipgloss.Width(line), width)
			}
		}
		for _, t := range OffsetTargets(ts, x0, top) {
			if t.Rect.Y < height && t.Rect.X+t.Rect.Width <= width {
				all = append(all, t)
			}
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
func (f *Form) valueList(styles Styles, width int) ([]string, []Target) {
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
			Action: f.keys.Enter, Item: formOptionRef + string(field.ID) + "\x00" + string(field.Choices[j].ID)})
	}
	return styles.Panel("", "", lines, inner+4, len(lines)+2, true), targets
}

// draw lays the form out in lay and cuts it to height. With strict, the focused field must fit with the marks
// of the fields left out; otherwise the marks give way before the field does.
func (f *Form) draw(lay formLayout, width, height int, strict bool, styles Styles) (formDrawing, layoutResult) {
	problems := f.otherProblems(width, styles)
	for {
		d, res := f.drawBelow(lay, problems, width, height-len(problems), strict, styles)
		if res != layoutShort || len(problems) == 0 {
			return d, res
		}
		// The problems without a field give way when the focused field does not fit beside them.
		problems = nil
	}
}

// drawBelow is draw under the lines of the problems without a field, in the height that is left.
func (f *Form) drawBelow(lay formLayout, problems []string, width, height int, strict bool, styles Styles) (formDrawing, layoutResult) {
	top := len(problems)
	if !lay.list {
		blocks, ok := f.blocks(width, lay.boxed, lay.headings, "", styles)
		if !ok {
			return formDrawing{}, layoutNarrow
		}
		body, ok := f.fit(blocks, height, strict, styles)
		if !ok {
			return formDrawing{}, layoutShort
		}
		body.targets = OffsetTargets(body.targets, 0, top)
		body.row += top
		body.top += top
		body.lines = append(append([]string(nil), problems...), body.lines...)
		return body, layoutDrawn
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
		return formDrawing{}, layoutNarrow
	}
	if len(list) > height {
		return formDrawing{}, layoutShort
	}
	body, ok := f.fit(blocks, height-2-len(head), strict, styles)
	if !ok {
		return formDrawing{}, layoutShort
	}
	content := append(head, body.lines...)
	x0 := listWidth + 1 + 2
	d := formDrawing{lines: problems, row: top + 1 + len(head) + body.row, top: top + 1 + len(head) + body.top, x: x0 + body.x}
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
		d.lines = append(d.lines, left+" "+right)
	}
	d.targets = append(OffsetTargets(listTargets, 0, top), OffsetTargets(body.targets, x0, top+1+len(head))...)
	return d, layoutDrawn
}

// fit cuts blocks to height lines: all of them where they fit, otherwise whole fields around the focus with a
// line "…" above and below where fields are left out. It is not ok where the focused field does not fit.
func (f *Form) fit(blocks []formBlock, height int, strict bool, styles Styles) (formDrawing, bool) {
	if height < 0 {
		return formDrawing{}, false
	}
	at := 0
	for i, b := range blocks {
		if b.field == f.focus {
			at = i
		}
	}
	body := func(s, e int) int {
		n := 0
		for _, b := range blocks[s:e] {
			n += len(b.lines)
		}
		return n
	}
	// Each side that leaves fields out takes one line for its mark.
	size := func(s, e int) int {
		n := body(s, e)
		if s > 0 {
			n++
		}
		if e < len(blocks) {
			n++
		}
		return n
	}
	start, end := 0, len(blocks)
	if body(0, len(blocks)) > height {
		start, end = at, at+1
		if body(start, end) > height || strict && size(start, end) > height {
			return formDrawing{}, false
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
	}
	more := styles.Render(StyleHint, "  "+styles.Glyph(GlyphEllipsis))
	shown := body(start, end)
	top := start > 0 && shown+1 <= height
	bottom := end < len(blocks) && shown+1+boolInt(top) <= height
	var d formDrawing
	if top {
		d.lines = append(d.lines, more)
	}
	for _, b := range blocks[start:end] {
		if b.field == f.focus {
			d.row, d.x, d.top = len(d.lines)+b.row, b.x, len(d.lines)+b.top
		}
		d.targets = append(d.targets, OffsetTargets(b.targets, 0, len(d.lines))...)
		d.lines = append(d.lines, b.lines...)
	}
	if bottom {
		d.lines = append(d.lines, more)
	}
	return d, true
}

// sizeHint asks for a terminal large enough for the focused field.
func (f *Form) sizeHint(width, height int, styles Styles) string {
	needH := 1
	if blocks, ok := f.blocks(max(width, 2+minLabelColumn+1+minFlatValue), false, false, "", styles); ok {
		for _, b := range blocks {
			if b.field == f.focus {
				needH = len(b.lines)
			}
		}
	}
	needW := 2 + min(f.labelWidth(f.fields[f.focus].Section), minLabelColumn) + 1 + minFlatValue
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
func (f *Form) otherProblems(width int, styles Styles) []string {
	var lines []string
	for _, p := range f.other {
		lines = append(lines, f.problemLines(p, width, "", styles)...)
	}
	return lines
}

func (f *Form) problemLines(p FormProblem, width int, indent string, styles Styles) []string {
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
func (f *Form) redact(text string) string {
	for i := range f.fields {
		if field := &f.fields[i]; field.Kind == FieldMasked {
			text = redactSecret(text, field.line.String())
		}
	}
	return text
}

func (f *Form) label(i int) string { return Escape(f.text.Lookup(f.fields[i].Label)) }

// labelWidth is the width of the longest label of the fields of a section.
func (f *Form) labelWidth(section SectionID) int {
	w := 0
	for i := range f.fields {
		if f.fields[i].Section == section {
			w = max(w, lipgloss.Width(f.label(i)))
		}
	}
	return w
}

// sectionTitle is the title of a section with the marks of a closed section and of one with a problem.
func (f *Form) sectionTitle(s FormSection, styles Styles) (string, bool) {
	closed := f.closed(s.ID)
	text := Escape(f.text.Lookup(s.Title))
	if closed {
		text += " " + Escape(f.text.Lookup(TextFormSectionClosed))
	}
	for i := range f.fields {
		if f.fields[i].Section == s.ID && (len(f.problems[f.fields[i].ID]) > 0 || f.dateShown(i)) {
			text += " " + problemMark
			break
		}
	}
	return text, closed
}

// sectionList draws the numbered sections in a panel, the one of the focus behind ">" as a filled bar and a
// closed one dimmed, and returns it with a target per section and its width.
func (f *Form) sectionList(styles Styles) ([]string, []Target, int) {
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
			Item: formSectionRef + string(s.ID)})
	}
	return styles.Panel("", "", lines, inner+4, len(lines)+2, false), targets, inner + 4
}

// blocks lays out the fields in width cells, framed or on one line, with the headings of the sections where
// headings is set, and only the fields of the section only where it is set. Each section has its own label
// column. ok is false where the values do not fit beside the labels. A heading stands in the block of the
// first field of its section, so it scrolls with it.
func (f *Form) blocks(width int, boxed, headings bool, only SectionID, styles Styles) ([]formBlock, bool) {
	cols := map[SectionID]int{}
	for i := range f.fields {
		section := f.fields[i].Section
		if _, ok := cols[section]; !ok {
			cols[section] = min(f.labelWidth(section), max(minLabelColumn, width/3))
		}
		valueW := width - (2 + cols[section] + 1)
		if (only == "" || section == only) && !f.closed(section) &&
			(boxed && valueW-4 < minBoxInner || valueW < minFlatValue) {
			return nil, false
		}
	}
	var out []formBlock
	var last SectionID
	var head *formBlock
	flush := func() {
		if head != nil {
			out = append(out, *head)
			head = nil
		}
	}
	for i := range f.fields {
		field := &f.fields[i]
		if only != "" && field.Section != only {
			continue
		}
		if headings && field.Section != "" && field.Section != last {
			flush()
			last = field.Section
			s, _ := f.section(field.Section)
			title, _ := f.sectionTitle(s, styles)
			head = &formBlock{field: -1, lines: []string{styles.Render(StyleTitle, title)},
				targets: []Target{{Rect: Rect{Width: width, Height: 1}, Zone: ZoneForm, Item: formSectionRef + string(s.ID)}}}
		}
		if f.closed(field.Section) {
			continue
		}
		b := f.block(i, width, cols[field.Section], boxed, styles)
		if head != nil {
			n := len(head.lines)
			b = formBlock{field: i, lines: append(head.lines, b.lines...), row: n + b.row, top: n + b.top, x: b.x,
				targets: append(head.targets, OffsetTargets(b.targets, 0, n)...)}
			head = nil
		}
		out = append(out, b)
	}
	flush()
	return out, true
}

// block draws field i: the marker and label in their column, the value framed or on its surface, the hint
// under the focused field and the problems of the field. A label longer than the column wraps inside it.
func (f *Form) block(i, width, labelCol int, boxed bool, styles Styles) formBlock {
	field := &f.fields[i]
	focused := f.fieldFocused(i)
	headW := 2 + labelCol + 1
	valueW := width - headW
	labels := Wrap(f.label(i), max(labelCol, 1))
	heads := make([]string, len(labels))
	for j, l := range labels {
		marker := "  "
		if focused && j == 0 {
			marker = "> "
		}
		heads[j] = marker + Pad(l, labelCol) + " "
		if focused {
			heads[j] = styles.Render(StyleActive, heads[j])
		}
	}
	blank := strings.Repeat(" ", headW)
	b := formBlock{field: i}
	// head is the label part of line j of the value, counted from the first line of the value.
	head := func(j int) string {
		if j < len(heads) {
			return heads[j]
		}
		return blank
	}
	if boxed {
		values, ticks := f.valueLines(i, valueW-4, styles)
		for len(values) < len(heads) {
			values = append(values, "")
		}
		box := styles.Panel("", "", values, valueW, len(values)+2, focused)
		for j, line := range box {
			if j == 0 || j == len(box)-1 {
				b.lines = append(b.lines, blank+line)
			} else {
				b.lines = append(b.lines, head(j-1)+line)
			}
		}
		b.targets = append(b.targets, OffsetTargets(ticks, headW+2, 1)...)
		b.row, b.x = len(box)-1, headW
	} else {
		values, ticks := f.valueLines(i, valueW, styles)
		for len(values) < len(heads) {
			values = append(values, "")
		}
		// Only the focused field stands on a surface, so two fields never run together.
		surface := lipgloss.NewStyle()
		if focused {
			surface = fieldStyle(styles, true)
		}
		for j, v := range values {
			b.lines = append(b.lines, head(j)+surface.Render(keepOpen(Pad(Clip(v, valueW), valueW), openingOf(surface))))
		}
		b.targets = append(b.targets, OffsetTargets(ticks, headW, 0)...)
		b.row, b.x = 0, headW
	}
	action := ActionID("")
	if field.Kind == FieldChoice || field.Kind == FieldList {
		action = f.keys.Enter
	}
	if field.Kind != FieldReadOnly {
		row := Target{Rect: Rect{Width: width, Height: len(b.lines)}, Zone: ZoneForm, Action: action, Item: formFieldRef + string(field.ID)}
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
	if p, ok := f.dateProblem(i); ok {
		b.lines = append(b.lines, f.problemLines(p, width, indent, styles)...)
	}
	return b
}

// valueLines draws the value of field i in width cells and returns the targets of its tick boxes, counted
// from the top left of the value.
func (f *Form) valueLines(i, width int, styles Styles) ([]string, []Target) {
	field := &f.fields[i]
	focused := f.fieldFocused(i)
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
	case FieldDate:
		symbol := styles.Glyph(GlyphCalendar)
		sw := lipgloss.Width(symbol)
		room, targets := width, []Target(nil)
		if width-sw-1 >= minFlatValue {
			// The symbol is the entry to the calendar and stays at the right end of the value.
			room = width - sw - 1
			targets = []Target{{Rect: Rect{X: room + 1, Width: sw, Height: 1}, Zone: ZoneForm, Action: f.keys.Enter, Item: formCalendarRef + string(field.ID)}}
		}
		var value string
		switch {
		case focused:
			value = field.line.view(room, styles, true, "")
		case text == "":
			value = Pad(styles.Render(StyleHint, styles.Truncate(Escape(f.text.Lookup(TextFormEmpty)), room)), room)
		default:
			value = Pad(styles.Truncate(Escape(text), room), room)
		}
		if targets == nil {
			return []string{value}, nil
		}
		return []string{value + " " + symbol}, targets
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
func (f *Form) ticks(i, width int, styles Styles) ([]string, []Target) {
	field := &f.fields[i]
	var lines []string
	var targets []Target
	line, x := "", 0
	for j, c := range field.Choices {
		lead := " "
		if f.fieldFocused(i) && j == field.cursor {
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
			Action: f.keys.Toggle, Item: formTickRef + string(field.ID) + "\x00" + string(c.ID)})
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

// buttonRows draws the buttons in rows of at most width cells, each behind the key now in force, the main one
// bold, and returns a target per button, counted from the top left of the rows.
func (f *Form) buttonRows(width int, styles Styles) ([]string, []Target) {
	var rows []string
	var targets []Target
	line, x := "", 0
	for j, b := range f.buttons {
		kind := ButtonNormal
		if b.Primary {
			kind = ButtonPrimary
		}
		label := f.text.Lookup(b.Label)
		if key := f.table.Label(b.Action); key != "" {
			label = key + " " + label
		}
		drawn := styles.Button(kind, label, f.btn == j)
		w := lipgloss.Width(drawn)
		if x > 0 && x+len(buttonGap)+w > width {
			rows, line, x = append(rows, line), "", 0
		}
		if x > 0 {
			line += buttonGap
			x += len(buttonGap)
		}
		if r, ok := clipRect(Rect{X: x, Y: len(rows), Width: w, Height: 1}, width); ok {
			targets = append(targets, Target{Rect: r, Zone: ZoneForm, Action: b.Action, Item: formButtonRef + strconv.Itoa(j)})
		}
		line += drawn
		x += w
	}
	if line != "" {
		rows = append(rows, line)
	}
	return rows, targets
}

// fieldRows is the height the form needs at width with no list or calendar open.
func (f *Form) fieldRows(styles Styles, width int) int {
	c := *f
	// The buttons keep the bottom rows of the form, so they are measured apart from the fields, and with the
	// focus on a field, which may add the line of its hint.
	c.open, c.cal, c.btn = false, false, -1
	buttons, _ := c.buttonRows(width, styles)
	c.buttons = nil
	view, _ := c.View(styles, width, 60)
	rows := strings.Split(view, "\n")
	for len(rows) > 1 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	return len(rows) + len(buttons) + 1
}

// Overlay draws the form with styles over screen, the drawn screen of width by height cells, in the way of
// Dialog.Overlay: the screen dimmed and without its colours, and the form with its buttons and the key line of
// Hints in its frame under FormSpec.Title, centred in room, the part of the screen it may cover. The targets are
// the form's alone, so the screen behind it takes no click. ok is false where room is too small for the
// form, also where only the open list of values or the calendar no longer fits; the application then draws
// View in the workspace and Hints in the footer.
func (f *Form) Overlay(styles Styles, screen string, width, height int, room Rect) (view string, targets []Target, ok bool) {
	title := ""
	if f.title != "" {
		title = f.text.Lookup(f.title)
	}
	return f.drawModal(styles, title, f.Hints(), f.table, f.text, screen, width, height, room)
}

// drawModal is the form in its frame over screen, shared by Form.Overlay and the modal of the log view: the
// fields with the open list or calendar, the buttons, a gap and the key line made of hints.
func (f *Form) drawModal(styles Styles, title string, hints []Hint, keys *Keymap, text Translator, screen string, width, height int, room Rect) (string, []Target, bool) {
	inner := min(dialogMaxInner, room.Width-8)
	if inner < dialogMinInner {
		return "", nil, false
	}
	extra := 0
	switch {
	case f.cal:
		box, _ := f.calendar(styles)
		extra = len(box)
	case f.open:
		extra = len(f.fields[f.focus].Choices) + 2
	}
	formH := f.fieldRows(styles, inner) + extra
	if formH+2+2 > room.Height {
		return "", nil, false
	}
	body, ts := f.View(styles, inner, formH)
	lines := append(strings.Split(body, "\n"), "")
	keyLine, kt := styles.footer(keys.Hints(hints, f.Input(), text), "", inner)
	ts = append(ts, OffsetTargets(kt, 0, len(lines))...)
	lines = append(lines, keyLine)
	for i, line := range lines {
		lines[i] = Pad(Clip(line, inner), inner)
	}
	box := styles.Panel(title, "", lines, inner+4, len(lines)+2, true)
	top := room.Y + (room.Height-len(box))/2
	left := room.X + (room.Width-inner-4)/2
	return overlay(screen, box, width, height, top, left, styles), OffsetTargets(ts, left+2, top+1), true
}

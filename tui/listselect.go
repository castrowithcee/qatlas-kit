package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// listChar is one drawn character of the cells of a row. ord is the place of its column among the shown ones;
// fill marks what is no value: the room a value leaves in its column, the gap between columns and the count of
// a group head.
type listChar struct {
	r    rune
	ord  int
	fill bool
}

// listDrawn is the selectable part of one drawn row: its characters from column x on, and the cells of the
// tick box, which a press never starts a selection on.
type listDrawn struct {
	x, boxX, boxW int
	chars         []listChar
}

// listSel is what the last drawing of a list holds for the selection with the mouse: the drawn rows from row
// start of the list on, so the text is what was drawn, and the selected range to draw it.
type listSel struct {
	start  int
	rows   []listDrawn
	layout selLayout
	sel    textRange
}

func valueChars(text string, ord int) []listChar {
	var out []listChar
	for _, r := range text {
		out = append(out, listChar{r: r, ord: ord})
	}
	return out
}

func fillChars(n, ord int) []listChar {
	out := make([]listChar, max(n, 0))
	for i := range out {
		out[i] = listChar{r: ' ', ord: ord, fill: true}
	}
	return out
}

// trimFill drops the fill at the end of a row, as the drawing does.
func trimFill(chars []listChar) []listChar {
	for len(chars) > 0 && chars[len(chars)-1].fill {
		chars = chars[:len(chars)-1]
	}
	return chars
}

// rowChars are the characters of the shown columns from column from on, whose first shown one has the place
// ord, with the fill and the gaps as drawn.
func (l *List) rowChars(cells []string, from, ord int, lay tableLayout, styles Styles) []listChar {
	var out []listChar
	for i := from; i < len(l.spec.Columns); i++ {
		w := lay.widths[i]
		if w <= 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, fillChars(len(columnGap), ord)...)
		}
		text := l.shown(cells, i, lay, styles)
		fill := fillChars(w-lipgloss.Width(text), ord)
		if l.spec.Columns[i].Right {
			out = append(append(out, fill...), valueChars(text, ord)...)
		} else {
			out = append(append(out, valueChars(text, ord)...), fill...)
		}
		ord++
	}
	return trimFill(out)
}

// groupChars are the characters of a group row from its title on: the title, then the cells of the group from
// the second column on where the group has any, in their columns as groupLine draws them.
func (l *List) groupChars(group string, lay tableLayout, styles Styles) (x int, chars []listChar) {
	prefix, title, suffix := l.groupParts(group, styles)
	x = 2 + lipgloss.Width(prefix)
	name := Escape(l.groupTitle(group))
	chars = valueChars(name, 0)
	g, ok := l.groups[group]
	if !ok || len(g.Cells) == 0 {
		return x, chars
	}
	rest := l.rowChars(g.Cells, 1, 1, lay, styles)
	if len(rest) == 0 {
		return x, chars
	}
	head := lipgloss.Width(prefix) + lipgloss.Width(title) + lipgloss.Width(suffix)
	chars = append(chars, fillChars(max(l.lead()+lay.widths[0], head)-lipgloss.Width(prefix)-lipgloss.Width(name), 0)...)
	return x, append(append(chars, fillChars(len(columnGap), 1)...), rest...)
}

// highlight draws the characters [a, b) of a drawn row in the selection fill. The cursor row has that fill
// already, so there the selection is its reverse; a terminal without colour shows none.
func (d listDrawn) highlight(text string, a, b int, cursor bool, styles Styles) string {
	if a >= b {
		return text
	}
	from, to := d.x-2, d.x-2
	for i, c := range d.chars[:b] {
		if i < a {
			from += runeCells(c.r)
		}
		to += runeCells(c.r)
	}
	style := styles.Style(StyleActiveRow)
	if cursor && style.Render("x") != "x" {
		style = style.Reverse(true)
	}
	mid := style.Render(ansi.Strip(ansi.Cut(text, from, to)))
	return ansi.Truncate(text, from, "") + mid + ansi.TruncateLeft(text, to, "")
}

func (d listDrawn) selRow(line uint64) selRow {
	w := make([]int, len(d.chars))
	for i, c := range d.chars {
		w[i] = runeCells(c.r)
	}
	return selRow{line: line, x: d.x, widths: w, end: true, lineLen: len(d.chars)}
}

func (l *List) selectPos(x, y int) (textPos, bool) {
	if y >= 0 && y < len(l.drawn.rows) {
		if r := l.drawn.rows[y]; x >= r.boxX && x < r.boxX+r.boxW {
			return textPos{}, false
		}
	}
	return l.drawn.layout.pos(x, y)
}

func (l *List) setSelection(sel textRange) { l.drawn.sel = sel }

// selectionText is the selected text of the rows as they were drawn: the values of the cells with a tab
// between cells and a line break between rows, escaped and cut as shown. The header, the rule, tick boxes, fold
// glyphs, marks, indent, counts and the fill never come with it.
func (l *List) selectionText(sel textRange) string {
	var b strings.Builder
	for line := sel.from.line; line <= sel.to.line; line++ {
		if line > sel.from.line {
			b.WriteByte('\n')
		}
		i := int(line) - l.drawn.start
		if i < 0 || i >= len(l.drawn.rows) {
			continue
		}
		chars := l.drawn.rows[i].chars
		from, to := sel.cells(line, len(chars))
		last := -1
		for _, c := range chars[from:to] {
			if c.fill {
				continue
			}
			if last >= 0 && c.ord > last {
				b.WriteString(strings.Repeat("\t", c.ord-last))
			}
			last = c.ord
			b.WriteRune(c.r)
		}
	}
	return b.String()
}

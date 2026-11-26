package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// holeLine is the line of a drawn row that holds no selectable text.
const holeLine = math.MaxUint64

// selDoc is the selectable text of a widget that draws paragraphs: a dialog, help, an explanation, the value
// of a form field. Each paragraph is a line of the selection, however many rows it wraps to, and the widget
// refills the document on every drawing.
type selDoc struct {
	paras  []selPara
	layout selLayout
	sel    textRange
	// mask is the glyph of a masked paragraph.
	mask string
}

type selPara struct {
	text   []rune
	masked bool
}

// reset starts a drawing; mask is the glyph that stands for a masked paragraph.
func (d *selDoc) reset(mask string) { d.paras, d.layout, d.mask = d.paras[:0], nil, mask }

// para adds the paragraph orig, drawn as the plain pieces, the first at column x of its row, and returns the
// selection rows of the pieces. The blanks a wrap dropped at a break stay in the text, and what a piece draws
// beyond orig, such as a hint, is not selectable.
func (d *selDoc) para(orig string, pieces []string, x int, masked bool) []selRow {
	text := []rune(orig)
	line := uint64(len(d.paras))
	d.paras = append(d.paras, selPara{text: text, masked: masked})
	rows := make([]selRow, len(pieces))
	pos := 0
	for i, piece := range pieces {
		runes := []rune(ansi.Strip(piece))
		for pos < len(text) && text[pos] == ' ' && (len(runes) == 0 || runes[0] != ' ') {
			pos++
		}
		n := 0
		for n < len(runes) && pos+n < len(text) && text[pos+n] == runes[n] {
			n++
		}
		w := make([]int, n)
		for j := range w {
			w[j] = runeCells(runes[j])
		}
		rows[i] = selRow{line: line, x: x, from: pos, widths: w, end: i == len(pieces)-1, lineLen: len(text)}
		pos += n
	}
	return rows
}

// mark draws the selected part of a drawn row in the selection fill; a terminal without colour shows none.
func (d *selDoc) mark(text string, r selRow, styles Styles) string {
	if r.line == holeLine {
		return text
	}
	a, b := d.sel.cells(r.line, r.lineLen)
	a, b = max(a-r.from, 0), min(b-r.from, len(r.widths))
	if a >= b {
		return text
	}
	from, to := r.x, r.x
	for i, w := range r.widths[:b] {
		if i < a {
			from += w
		}
		to += w
	}
	mid := styles.Style(StyleActiveRow).Render(ansi.Strip(ansi.Cut(text, from, to)))
	return ansi.Truncate(text, from, "") + mid + ansi.TruncateLeft(text, to, "")
}

// target is the selection target over rows, which the widget places after its click targets.
func (d *selDoc) target(rows []selRow, rect Rect, zone FocusZone) Target {
	d.layout = rows
	return Target{Rect: rect, Zone: zone, selection: d}
}

func (d *selDoc) selectPos(x, y int) (textPos, bool) {
	p, ok := d.layout.pos(x, y)
	return p, ok && p.line != holeLine
}

func (d *selDoc) setSelection(sel textRange) { d.sel = sel }

// selectionText is the selected text, a line break between paragraphs; a masked paragraph is the mask of fixed
// length, so neither its content nor its length leaves the view.
func (d *selDoc) selectionText(sel textRange) string {
	var b strings.Builder
	for line := sel.from.line; line <= sel.to.line && line < uint64(len(d.paras)); line++ {
		if line > sel.from.line {
			b.WriteByte('\n')
		}
		p := d.paras[line]
		from, to := sel.cells(line, len(p.text))
		switch {
		case from >= to:
		case p.masked:
			b.WriteString(strings.Repeat(d.mask, textMaskCells))
		default:
			b.WriteString(string(p.text[from:to]))
		}
	}
	return b.String()
}

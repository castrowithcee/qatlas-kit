package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// valueLines draws the value of field i in width cells and returns the targets of its tick boxes, counted
// from the top left of the value, and of the selection of what it shows.
func (f *Form) valueLines(i, width int, styles Styles) ([]string, []Target) {
	rows, targets := f.valueRows(i, width, styles)
	text, masked, ok := f.shownValue(i, rows)
	if !ok {
		return rows, targets
	}
	if len(f.docs) != len(f.fields) {
		f.docs = make([]selDoc, len(f.fields))
	}
	doc := &f.docs[i]
	doc.reset(styles.Glyph(GlyphMask))
	sels := doc.para(text, rows, 0, masked)
	marked := make([]string, len(rows))
	for j, r := range rows {
		marked[j] = doc.mark(r, sels[j], styles)
	}
	return marked, append(targets, doc.target(sels, Rect{Width: width, Height: len(rows)}, ZoneForm))
}

// shownValue is the text field i shows as its value, which the mouse can select: not the input of a field with
// the focus while it is typed into, no hint and no choice of a list of ticks. A masked field gives the line it
// shows, never what was typed, and the selection holds the mask of fixed length instead.
func (f *Form) shownValue(i int, rows []string) (text string, masked, ok bool) {
	field := &f.fields[i]
	focused := f.showsFocus(i)
	value := field.line.String()
	switch field.Kind {
	case FieldText, FieldPath, FieldList, FieldReadOnly:
		return Escape(value), false, value != "" && !(focused && (field.Kind == FieldText || field.Kind == FieldPath))
	case FieldKey:
		return Escape(value), false, value != "" && !(focused && f.capture)
	case FieldDate:
		return Escape(value), false, value != "" && !focused
	case FieldChoice:
		return Escape(field.Choices[field.choice].shown(f.text)), false, true
	case FieldMasked:
		has := (value != "" || field.Stored) && !focused
		return strings.TrimRight(ansi.Strip(rows[0]), " "), true, has
	case FieldMaskedBlock:
		return strings.TrimRight(ansi.Strip(rows[0]), " "), true, value != "" || field.Stored
	}
	return "", false, false
}

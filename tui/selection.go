package tui

import (
	"math"

	tea "charm.land/bubbletea/v2"
)

// textPos is a character of the content of a selectable part: a line as the part counts it and the index of
// a drawn character in that line, not a screen column. The index of the line's length stands for its end.
type textPos struct {
	line uint64
	cell int
}

func (p textPos) before(q textPos) bool {
	return p.line < q.line || p.line == q.line && p.cell < q.cell
}

// selectable is a part whose drawn text the mouse can select. The part maps a cell of its last drawing,
// counted from the top left of its target, to a character of its content, keeps the selected range to draw it
// and builds the text of a range from its content, with masked sections and control characters as it shows them.
type selectable interface {
	selectPos(x, y int) (textPos, bool)
	setSelection(sel textRange)
	selectionText(sel textRange) string
}

// textRange is the selected characters from from to to, both included; the zero value selects nothing.
type textRange struct {
	from, to textPos
	on       bool
}

// cells is the selected part [a, b) of line, which draws n characters.
func (r textRange) cells(line uint64, n int) (a, b int) {
	if !r.on || line < r.from.line || line > r.to.line {
		return 0, 0
	}
	b = n
	if line == r.from.line {
		a = r.from.cell
	}
	if line == r.to.line {
		b = r.to.cell + 1
	}
	return min(a, n), min(max(b, a), n)
}

// selRow is one drawn row of selectable text: the characters of line from from on, of the widths given, drawn
// from column x. end says that the right of the row is the end of the line, as on its last or clipped row.
type selRow struct {
	line    uint64
	x, from int
	widths  []int
	end     bool
	lineLen int
}

// selLayout are the rows of selectable text of one drawing, row y of the part at index y.
type selLayout []selRow

// pos is the character under the cell x, y. Left of the text is the first character of the row, right of it
// the end of the line or the last character of a wrapped row, and below the last row the end of that row.
func (l selLayout) pos(x, y int) (textPos, bool) {
	if len(l) == 0 {
		return textPos{}, false
	}
	if y >= len(l) {
		y, x = len(l)-1, math.MaxInt
	}
	r := l[max(y, 0)]
	cell, at, i := r.from, r.x, 0
	if x >= r.x {
		for i < len(r.widths) && x >= at+r.widths[i] {
			at += r.widths[i]
			i++
		}
		cell = r.from + i
		switch {
		case i < len(r.widths):
		case r.end:
			cell = r.lineLen
		default:
			cell--
		}
	}
	return textPos{line: r.line, cell: min(max(cell, 0), r.lineLen)}, true
}

// TextSelection is the selection of text with the mouse, one for the screen, the zero value empty. Dragging
// with the left button held selects characters of one part, also over several lines, and the part highlights
// them; a click without motion selects nothing and stays a click. The selection holds until the next click or
// until the application calls Clear, which it does on every key. A press on selectable text is held back, so a
// drag never acts as a click: Holds says so after Mouse read the press, and Replay hands the press back at the
// release without motion, for the application to treat as the click it was.
type TextSelection struct {
	part           selectable
	origin         Rect
	anchor, head   textPos
	pressed, moved bool
	// click is the press held back, and replay says the release came without motion, so it is due.
	click  tea.MouseClickMsg
	replay bool
}

// Mouse reads one mouse event with the map of the frame on screen, before the application hands a click to
// Handle as before. A press of the left button on the text of a part that supports selection starts one,
// motion with the button held selects, and the release returns the selected text and true. A press anywhere
// ends the selection shown, and a disabled map selects nothing. The text holds content only, with masked
// sections and control characters as the part shows them; handing it to the clipboard is the application's.
func (s *TextSelection) Mouse(m *MouseMap, msg tea.MouseMsg) (text string, copied bool) {
	event := msg.Mouse()
	switch click := msg.(type) {
	case tea.MouseClickMsg:
		s.Clear()
		if m == nil || !m.enabled || event.Button != tea.MouseLeft {
			return "", false
		}
		t, ok := m.top(event.X, event.Y)
		if !ok || t.selection == nil {
			return "", false
		}
		p, ok := t.selection.selectPos(event.X-t.Rect.X, event.Y-t.Rect.Y)
		if !ok {
			return "", false
		}
		s.part, s.origin, s.anchor, s.head, s.pressed, s.click = t.selection, t.Rect, p, p, true, click
	case tea.MouseMotionMsg:
		if s.pressed {
			s.drag(event.X, event.Y)
		}
	case tea.MouseReleaseMsg:
		if !s.pressed {
			return "", false
		}
		s.drag(event.X, event.Y)
		s.pressed = false
		if s.moved {
			return s.part.selectionText(s.selected()), true
		}
		s.part, s.replay = nil, true
	}
	return "", false
}

// Holds reports whether the press Mouse just read is held back: it started a selection, so the application does
// not hand it to Handle yet. The application asks right after Mouse read a click.
func (s *TextSelection) Holds() bool { return s.pressed }

// Replay returns the held press once the release came without motion, which the application then handles as
// the click it was; a drag never returns it.
func (s *TextSelection) Replay() (tea.MouseClickMsg, bool) {
	if !s.replay {
		return tea.MouseClickMsg{}, false
	}
	s.replay = false
	return s.click, true
}

// drag moves the selected end to the screen cell x, y; a cell outside the part counts as its nearest edge,
// above it the start of the first row and below it the end of the last.
func (s *TextSelection) drag(x, y int) {
	x, y = x-s.origin.X, y-s.origin.Y
	switch {
	case y < 0:
		x, y = -1, 0
	case y >= s.origin.Height:
		x, y = math.MaxInt, s.origin.Height-1
	}
	p, ok := s.part.selectPos(x, y)
	if !ok || !s.moved && p == s.anchor {
		return
	}
	s.head, s.moved = p, true
	s.part.setSelection(s.selected())
}

func (s *TextSelection) selected() textRange {
	from, to := s.anchor, s.head
	if to.before(from) {
		from, to = to, from
	}
	return textRange{from: from, to: to, on: true}
}

// Clear ends the selection and its highlight.
func (s *TextSelection) Clear() {
	if s.part != nil {
		s.part.setSelection(textRange{})
	}
	*s = TextSelection{}
}

// Active reports whether text is selected or being selected.
func (s *TextSelection) Active() bool { return s.part != nil }

package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// textArea is the text of a multi-line input and its cursor. Its lines wrap at the width of the last drawing,
// and its view scrolls so the cursor row stays in view; the text itself is never cut.
type textArea struct {
	runes  []rune
	cursor int
	top    int // first shown row
	limit  int // most bytes the text holds
	width  int // the width of the last drawing, for up and down
}

// areaRow is one drawn row of a text area: the runes from up to to, and whether it ends its line.
type areaRow struct {
	from, to int
	last     bool
}

func (t *textArea) String() string { return string(t.runes) }

// cleanText is text as a text area takes it: line breaks as "\n", a tab as a space, and no other control or
// unprintable character.
func cleanText(text string) []rune {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var out []rune
	for _, r := range text {
		switch {
		case r == '\n':
			out = append(out, r)
		case r == '\t':
			out = append(out, ' ')
		case unicode.IsPrint(r):
			out = append(out, r)
		}
	}
	return out
}

// set replaces the text and puts the cursor at its end. Text over the limit is cut at a whole character.
func (t *textArea) set(text string) {
	r := cleanText(text)
	for len(string(r)) > t.limit {
		r = r[:len(r)-1]
	}
	t.runes, t.cursor, t.top = r, len(r), 0
}

// insert puts text at the cursor and reports whether it was taken; text that would pass the limit is refused
// whole.
func (t *textArea) insert(text string) bool {
	add := cleanText(text)
	if len(add) == 0 || len(string(t.runes))+len(string(add)) > t.limit {
		return false
	}
	t.runes = append(t.runes[:t.cursor], append(add, t.runes[t.cursor:]...)...)
	t.cursor += len(add)
	return true
}

// edit applies a key press that types or deletes text and reports whether it was one.
func (t *textArea) edit(msg tea.KeyPressMsg) bool {
	switch {
	case msg.Code == tea.KeyBackspace && msg.Mod == 0:
		if t.cursor > 0 {
			t.runes = append(t.runes[:t.cursor-1], t.runes[t.cursor:]...)
			t.cursor--
		}
		return true
	case msg.Code == tea.KeyDelete && msg.Mod == 0:
		if t.cursor < len(t.runes) {
			t.runes = append(t.runes[:t.cursor], t.runes[t.cursor+1:]...)
		}
		return true
	case msg.Text != "" && msg.Mod&^tea.ModShift == 0:
		t.insert(msg.Text)
		return true
	}
	return false
}

func (t *textArea) move(to int) { t.cursor = max(min(to, len(t.runes)), 0) }

// home and end put the cursor at the start or the end of its line.
func (t *textArea) home() {
	for t.cursor > 0 && t.runes[t.cursor-1] != '\n' {
		t.cursor--
	}
}

func (t *textArea) end() {
	for t.cursor < len(t.runes) && t.runes[t.cursor] != '\n' {
		t.cursor++
	}
}

// rows breaks the text into rows of at most width cells; every line has at least one row. The last cell of a
// row stays free for the cursor at the end of a line.
func (t *textArea) rows(width int) []areaRow {
	width = max(width-1, 1)
	var out []areaRow
	start := 0
	for i := 0; i <= len(t.runes); i++ {
		if i < len(t.runes) && t.runes[i] != '\n' {
			continue
		}
		ranges := wrapRanges(t.runes[start:i], width)
		for k, rg := range ranges {
			out = append(out, areaRow{from: start + rg[0], to: start + rg[1], last: k == len(ranges)-1})
		}
		start = i + 1
	}
	return out
}

// cursorRow is the row that holds the cursor; at the end of a wrapped row it stands at the start of the next.
func (t *textArea) cursorRow(rows []areaRow) int {
	for r, row := range rows {
		if t.cursor >= row.from && (t.cursor < row.to || row.last && t.cursor == row.to) {
			return r
		}
	}
	return max(len(rows)-1, 0)
}

// vertical moves the cursor by n rows of the last drawing and keeps its column where the row is long enough.
// It stops at the first and the last row.
func (t *textArea) vertical(n int) {
	rows := t.rows(t.width)
	r := t.cursorRow(rows)
	to := max(min(r+n, len(rows)-1), 0)
	if to == r {
		return
	}
	col := t.cursor - rows[r].from
	end := rows[to].to
	if !rows[to].last && end > rows[to].from {
		end--
	}
	t.cursor = min(rows[to].from+col, end)
}

// view draws at most n rows of the text in width cells, scrolled so the cursor row is in view. While focused,
// the cell of the cursor is drawn in reverse video, or as "_" at the end of a line in a terminal without
// attributes. An empty text shows placeholder dimmed.
func (t *textArea) view(width, n int, styles Styles, focused bool, placeholder string) []string {
	t.width = width
	rows := t.rows(width)
	cur := t.cursorRow(rows)
	n = max(min(n, len(rows)), 1)
	t.top = max(min(t.top, cur, len(rows)-n), cur-n+1, 0)
	plain := styles.profile == ProfileNone || styles.profile > ProfileTrueColor
	cursorCell := func(cell string, atEnd bool) string {
		switch {
		case plain && atEnd:
			return "_"
		case plain:
			return cell
		}
		return styles.Render(StyleActiveRow, cell)
	}
	var out []string
	for r := t.top; r < t.top+n; r++ {
		row := rows[r]
		var b strings.Builder
		if len(t.runes) == 0 {
			if focused {
				b.WriteString(cursorCell(" ", true))
			}
			b.WriteString(styles.Render(StyleHint, styles.Truncate(Escape(placeholder), width-lipgloss.Width(b.String()))))
			out = append(out, Pad(b.String(), width))
			break
		}
		for i := row.from; i < row.to; i++ {
			cell := Escape(string(t.runes[i]))
			if focused && i == t.cursor {
				cell = cursorCell(cell, false)
			}
			b.WriteString(cell)
		}
		if focused && r == cur && t.cursor == row.to {
			b.WriteString(cursorCell(" ", true))
		}
		out = append(out, Pad(b.String(), width))
	}
	return out
}

package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// textLine is the text of a one-line input and its cursor. Its view scrolls sideways so the cursor stays in
// view; the text itself is never cut.
type textLine struct {
	runes  []rune
	cursor int
	offset int
}

func newTextLine(text string) textLine {
	r := []rune(text)
	return textLine{runes: r, cursor: len(r)}
}

func (t *textLine) String() string { return string(t.runes) }

// insert puts the printable characters of text at the cursor and reports whether one was taken.
func (t *textLine) insert(text string) bool {
	var add []rune
	for _, r := range text {
		if unicode.IsPrint(r) {
			add = append(add, r)
		}
	}
	if len(add) == 0 {
		return false
	}
	t.runes = append(t.runes[:t.cursor], append(add, t.runes[t.cursor:]...)...)
	t.cursor += len(add)
	return true
}

// edit applies a key press that types or deletes text and reports whether it was one.
func (t *textLine) edit(msg tea.KeyPressMsg) bool {
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

func (t *textLine) move(to int) { t.cursor = max(min(to, len(t.runes)), 0) }

func (t *textLine) clear() { t.runes, t.cursor, t.offset = nil, 0, 0 }

// view draws the line in width cells. A masked line draws mask for every character and never the character.
// While focused, the cell of the cursor is drawn in reverse video, or as "_" at the end of a line in a
// terminal without attributes.
func (t *textLine) view(width int, styles Styles, focused bool, mask string) string {
	if width <= 0 {
		return ""
	}
	cells := make([]string, len(t.runes))
	for i, r := range t.runes {
		if mask != "" {
			cells[i] = mask
		} else {
			cells[i] = Escape(string(r))
		}
	}
	span := func(from, to int) int {
		w := 0
		for _, c := range cells[from:to] {
			w += lipgloss.Width(c)
		}
		return w
	}
	t.offset = min(t.offset, t.cursor)
	for t.offset < t.cursor && span(t.offset, t.cursor)+1 > width {
		t.offset++
	}
	plain := styles.profile == ProfileNone || styles.profile > ProfileTrueColor
	var b strings.Builder
	used := 0
	for i := t.offset; i <= len(cells); i++ {
		cell := " "
		if i < len(cells) {
			cell = cells[i]
		}
		w := lipgloss.Width(cell)
		if used+w > width || i == len(cells) && !(focused && i == t.cursor) {
			break
		}
		switch {
		case focused && i == t.cursor && plain && i == len(cells):
			b.WriteString("_")
		case focused && i == t.cursor && !plain:
			b.WriteString(styles.Render(StyleActiveRow, cell))
		default:
			b.WriteString(cell)
		}
		used += w
	}
	return Pad(b.String(), width)
}

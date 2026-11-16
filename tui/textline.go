package tui

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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

// insert puts pasted text at the cursor and reports whether any of it was taken. The text arrives whole and is
// never a key. Line breaks at its end are dropped, line breaks and tabs inside it become one space each, and
// other control characters, invalid UTF-8, direction marks and unprintable characters are dropped.
func (t *textLine) insert(text string) bool {
	text = strings.TrimRight(text, "\r\n")
	var add []rune
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		switch {
		case r == utf8.RuneError && size <= 1:
		case r == '\r':
			if i < len(text) && text[i] == '\n' {
				i++
			}
			add = append(add, ' ')
		case r == '\n' || r == '\t':
			add = append(add, ' ')
		case unicode.Is(unicode.Bidi_Control, r) || !unicode.IsPrint(r):
		default:
			add = append(add, r)
		}
	}
	return t.put(add)
}

// typed puts the printable characters of a key press at the cursor.
func (t *textLine) typed(text string) {
	var add []rune
	for _, r := range text {
		if unicode.IsPrint(r) && !unicode.Is(unicode.Bidi_Control, r) {
			add = append(add, r)
		}
	}
	t.put(add)
}

func (t *textLine) put(add []rune) bool {
	if len(add) == 0 {
		return false
	}
	t.runes = slices.Insert(t.runes, t.cursor, add...)
	t.cursor += len(add)
	return true
}

// edit applies a key press that moves the cursor, types or deletes text. handled is false for any other key;
// changed reports that the text is not what it was. A masked line has no word boundaries to show: a word
// jump goes to the end of the line and a word deletion removes everything on that side of the cursor.
func (t *textLine) edit(msg tea.KeyPressMsg, masked bool) (handled, changed bool) {
	if op, ok := lineEditOf(msg); ok {
		return true, t.apply(op, masked)
	}
	if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
		before := len(t.runes)
		t.typed(msg.Text)
		return true, len(t.runes) != before
	}
	return false, false
}

func (t *textLine) apply(op lineEdit, masked bool) (changed bool) {
	switch op {
	case editLeft:
		t.move(t.cursor - 1)
	case editRight:
		t.move(t.cursor + 1)
	case editWordLeft:
		t.move(t.wordStart(masked))
	case editWordRight:
		t.move(t.wordEnd(masked))
	case editStart:
		t.move(0)
	case editEnd:
		t.move(len(t.runes))
	case editDeleteLeft:
		return t.cut(t.cursor-1, t.cursor)
	case editDeleteRight:
		return t.cut(t.cursor, t.cursor+1)
	case editDeleteWordLeft:
		return t.cut(t.wordStart(masked), t.cursor)
	case editDeleteWordRight:
		return t.cut(t.cursor, t.wordEnd(masked))
	case editDeleteToStart:
		return t.cut(0, t.cursor)
	case editDeleteToEnd:
		return t.cut(t.cursor, len(t.runes))
	}
	return false
}

// cut removes the characters from to and puts the cursor at from.
func (t *textLine) cut(from, to int) bool {
	from, to = max(from, 0), min(to, len(t.runes))
	if from >= to {
		return false
	}
	t.runes = slices.Delete(t.runes, from, to)
	t.cursor = from
	return true
}

// wordStart is where the word before the cursor begins: the spaces before the cursor are skipped, then the
// word. A word ends at white space.
func (t *textLine) wordStart(masked bool) int {
	if masked {
		return 0
	}
	i := t.cursor
	for i > 0 && unicode.IsSpace(t.runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(t.runes[i-1]) {
		i--
	}
	return i
}

// wordEnd is where the word at or after the cursor ends.
func (t *textLine) wordEnd(masked bool) int {
	if masked {
		return len(t.runes)
	}
	i := t.cursor
	for i < len(t.runes) && unicode.IsSpace(t.runes[i]) {
		i++
	}
	for i < len(t.runes) && !unicode.IsSpace(t.runes[i]) {
		i++
	}
	return i
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
	// The cursor cell is at least one cell wide and never wider than the line: a character of no width gets a
	// blank to stand on, and a character too wide for the line is shown as a blank.
	at := " "
	if t.cursor < len(cells) {
		at = cells[t.cursor]
	}
	if w := lipgloss.Width(at); focused && w == 0 {
		at = " " + at
	} else if focused && w > width {
		at = " "
	}
	cursorWidth := max(lipgloss.Width(at), 1)
	span := func(from, to int) int {
		w := 0
		for _, c := range cells[from:to] {
			w += lipgloss.Width(c)
		}
		return w
	}
	t.offset = min(t.offset, t.cursor)
	for t.offset < t.cursor && span(t.offset, t.cursor)+cursorWidth > width {
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
		if i == t.cursor {
			cell = at
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

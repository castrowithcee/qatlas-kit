package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// lineKey is the key press a terminal reports for a key name of the text-line table.
func lineKey(name string) tea.KeyPressMsg {
	codes := map[string]rune{"left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd,
		"backspace": tea.KeyBackspace, "delete": tea.KeyDelete}
	var mod tea.KeyMod
	rest := name
	for {
		switch {
		case strings.HasPrefix(rest, "ctrl+"):
			mod |= tea.ModCtrl
			rest = rest[len("ctrl+"):]
			continue
		case strings.HasPrefix(rest, "alt+"):
			mod |= tea.ModAlt
			rest = rest[len("alt+"):]
			continue
		}
		break
	}
	if code, ok := codes[rest]; ok {
		return tea.KeyPressMsg{Code: code, Mod: mod}
	}
	return tea.KeyPressMsg{Code: []rune(rest)[0], Mod: mod}
}

func lineAt(text string, cursor int) textLine {
	t := newTextLine(text)
	t.cursor = cursor
	return t
}

func TestLineKeyBuildersGiveTheTableNames(t *testing.T) {
	for name := range lineEdits {
		if got := KeyName(lineKey(name)); got != name {
			t.Errorf("lineKey(%q) is reported as %q", name, got)
		}
	}
}

func TestEveryEditKeyOfTheTableActsAtEveryPosition(t *testing.T) {
	// Reference results for "one two" at the cursors 0, 3, 5 and 7, and for an empty line.
	type want struct {
		text   string
		cursor int
	}
	const text = "one two"
	cursors := []int{0, 3, 5, 7}
	refs := map[lineEdit][]want{
		editLeft:            {{text, 0}, {text, 2}, {text, 4}, {text, 6}},
		editRight:           {{text, 1}, {text, 4}, {text, 6}, {text, 7}},
		editWordLeft:        {{text, 0}, {text, 0}, {text, 4}, {text, 4}},
		editWordRight:       {{text, 3}, {text, 7}, {text, 7}, {text, 7}},
		editStart:           {{text, 0}, {text, 0}, {text, 0}, {text, 0}},
		editEnd:             {{text, 7}, {text, 7}, {text, 7}, {text, 7}},
		editDeleteLeft:      {{text, 0}, {"on two", 2}, {"one wo", 4}, {"one tw", 6}},
		editDeleteRight:     {{"ne two", 0}, {"onetwo", 3}, {"one to", 5}, {text, 7}},
		editDeleteWordLeft:  {{text, 0}, {" two", 0}, {"one wo", 4}, {"one ", 4}},
		editDeleteWordRight: {{" two", 0}, {"one", 3}, {"one t", 5}, {text, 7}},
		editDeleteToStart:   {{text, 0}, {" two", 0}, {"wo", 0}, {"", 0}},
		editDeleteToEnd:     {{"", 0}, {"one", 3}, {"one t", 5}, {text, 7}},
	}
	for name, op := range lineEdits {
		for i, c := range cursors {
			line := lineAt(text, c)
			handled, changed := line.edit(lineKey(name), false)
			w := refs[op][i]
			if !handled || line.String() != w.text || line.cursor != w.cursor || changed != (w.text != text) {
				t.Errorf("%s at %d: handled %v changed %v gives %q cursor %d, want %q cursor %d", name, c, handled,
					changed, line.String(), line.cursor, w.text, w.cursor)
			}
		}
		var empty textLine
		if handled, changed := empty.edit(lineKey(name), false); !handled || changed || empty.String() != "" || empty.cursor != 0 {
			t.Errorf("%s on an empty line: handled %v changed %v cursor %d", name, handled, changed, empty.cursor)
		}
	}
	if len(refs) != 12 {
		t.Errorf("the reference covers %d edits", len(refs))
	}
}

func TestOtherKeysAreNoLineEdits(t *testing.T) {
	line := lineAt("abc", 1)
	for _, msg := range []tea.KeyPressMsg{{Code: 'z', Mod: tea.ModCtrl}, {Code: tea.KeyUp}, {Code: tea.KeyDown},
		{Code: tea.KeyF2}, {Code: tea.KeyEnter}, {Code: tea.KeyTab}, {Code: tea.KeyEscape}, {Code: 'x', Mod: tea.ModAlt},
		{Code: tea.KeyPgUp}, {Code: tea.KeyBackspace, Mod: tea.ModShift}} {
		if handled, changed := line.edit(msg, false); handled || changed || line.String() != "abc" || line.cursor != 1 {
			t.Errorf("%s: handled %v changed %v text %q", KeyName(msg), handled, changed, line.String())
		}
	}
	if handled, changed := line.edit(tea.KeyPressMsg{Code: 'X', Text: "X", Mod: tea.ModShift}, false); !handled || !changed || line.String() != "aXbc" {
		t.Errorf("a shifted letter gives %q", line.String())
	}
	if handled, changed := line.edit(tea.KeyPressMsg{Code: tea.KeyTab, Text: "\t"}, false); !handled || changed || line.String() != "aXbc" {
		t.Errorf("a tab typed as text gives %q", line.String())
	}
}

func TestWordsEndAtWhiteSpace(t *testing.T) {
	const text = "  ab   cd ef  "
	// Runes: 2 spaces, "ab" (2-3), 3 spaces, "cd" (7-8), no-break space (9), "ef" (10-11), 2 spaces.
	cases := []struct {
		cursor, back, forward int
	}{
		{0, 0, 4}, {1, 0, 4}, {2, 0, 4}, {4, 2, 9}, {5, 2, 9}, {7, 2, 9}, {9, 7, 12}, {12, 10, 14}, {14, 10, 14},
	}
	for _, c := range cases {
		line := lineAt(text, c.cursor)
		if got := line.wordStart(false); got != c.back {
			t.Errorf("wordStart at %d = %d, want %d", c.cursor, got, c.back)
		}
		if got := line.wordEnd(false); got != c.forward {
			t.Errorf("wordEnd at %d = %d, want %d", c.cursor, got, c.forward)
		}
	}
	line := lineAt("  ", 2)
	line.edit(lineKey("ctrl+w"), false)
	if line.String() != "" {
		t.Errorf("deleting the word of spaces leaves %q", line.String())
	}
}

func TestMaskedLinesShowNoWordBoundary(t *testing.T) {
	const text = "ab cd ef"
	for _, name := range []string{"ctrl+left", "alt+left", "alt+b"} {
		line := lineAt(text, 5)
		line.edit(lineKey(name), true)
		if line.cursor != 0 {
			t.Errorf("%s in a masked line stops at %d", name, line.cursor)
		}
	}
	for _, name := range []string{"ctrl+right", "alt+right", "alt+f"} {
		line := lineAt(text, 3)
		line.edit(lineKey(name), true)
		if line.cursor != len(text) {
			t.Errorf("%s in a masked line stops at %d", name, line.cursor)
		}
	}
	for _, name := range []string{"ctrl+w", "alt+backspace"} {
		line := lineAt(text, 5)
		if _, changed := line.edit(lineKey(name), true); !changed || line.String() != " ef" || line.cursor != 0 {
			t.Errorf("%s in a masked line leaves %q", name, line.String())
		}
	}
	for _, name := range []string{"alt+d", "alt+delete"} {
		line := lineAt(text, 5)
		if _, changed := line.edit(lineKey(name), true); !changed || line.String() != "ab cd" || line.cursor != 5 {
			t.Errorf("%s in a masked line leaves %q", name, line.String())
		}
	}
	// Character edits are the same as in a plain line.
	line := lineAt(text, 5)
	line.edit(lineKey("ctrl+h"), true)
	if line.String() != "ab c ef" {
		t.Errorf("backspace leaves %q", line.String())
	}
}

func TestPasteIsCleanedAsOneText(t *testing.T) {
	cases := []struct {
		name, in, want string
		taken          bool
	}{
		{"plain", "hello", "hello", true},
		{"line break at the end", "hello\n", "hello", true},
		{"windows line break at the end", "hello\r\n", "hello", true},
		{"several line breaks at the end", "hello\r\n\n\r", "hello", true},
		{"only a line break", "\n", "", false},
		{"windows line break inside", "a\r\nb", "a b", true},
		{"unix line break inside", "a\nb", "a b", true},
		{"carriage return inside", "a\rb", "a b", true},
		{"two line breaks inside", "a\n\nb", "a  b", true},
		{"tab", "a\tb", "a b", true},
		{"escape sequence", "\x1b[31mred\x1b[0m", "[31mred[0m", true},
		{"operating system command", "a\x1b]52;c;QQ==\x07b", "a]52;c;QQ==b", true},
		{"NUL", "a\x00b", "ab", true},
		{"other controls", "a\x01\x7f\u0085b", "ab", true},
		{"right-to-left override", "a‮b", "ab", true},
		{"other direction marks", "a‪⁦⁩‏؜b", "ab", true},
		{"invalid UTF-8", "a\xff\xc0b", "ab", true},
		{"replacement character", "a�b", "a�b", true},
		{"wide characters", "日本語😀", "日本語😀", true},
		{"nothing printable", "\x00\x1b‮", "", false},
	}
	for _, c := range cases {
		line := newTextLine("")
		if got := line.insert(c.in); got != c.taken || line.String() != c.want {
			t.Errorf("%s: taken %v gives %q, want %v and %q", c.name, got, line.String(), c.taken, c.want)
		}
		if line.cursor != len(line.runes) {
			t.Errorf("%s: the cursor is at %d", c.name, line.cursor)
		}
	}
	line := lineAt("abef", 2)
	line.insert("cd\n")
	if line.String() != "abcdef" || line.cursor != 4 {
		t.Errorf("a paste in the middle gives %q with the cursor at %d", line.String(), line.cursor)
	}
}

func TestLineViewKeepsTheCursorInViewAndWithinTheWidth(t *testing.T) {
	texts := []string{"abcdef", "日本語テキスト", "a😀b😀c", "éábc", "x​z日́本"}
	st := looks(t)["dark"]
	plain := looks(t)["noneascii"]
	for _, text := range texts {
		for _, mask := range []string{"", "*", "••"} {
			for width := 1; width <= 8; width++ {
				n := len([]rune(text))
				for cursor := 0; cursor <= n; cursor++ {
					line := lineAt(text, cursor)
					for _, s := range []Styles{st, plain} {
						view := line.view(width, s, true, mask)
						if w := lipgloss.Width(view); w != width {
							t.Fatalf("%q mask %q cursor %d width %d: the line is %d cells", text, mask, cursor, width, w)
						}
					}
					view := line.view(width, st, true, mask)
					if !strings.Contains(view, "\x1b[") {
						t.Errorf("%q mask %q cursor %d width %d: no cursor in %q", text, mask, cursor, width, view)
					}
					if mask != "" {
						for _, r := range text {
							if r > 0x7f && strings.ContainsRune(view, r) {
								t.Errorf("a masked line shows %q", r)
							}
						}
					}
				}
			}
		}
	}
}

func TestLineViewScrollsByOneAtWidthOneAndBackToTheLeft(t *testing.T) {
	plain := looks(t)["noneascii"]
	line := newTextLine("abcdef")
	if got := strings.TrimSpace(line.view(1, plain, true, "")); got != "_" {
		t.Errorf("width 1 at the end shows %q", got)
	}
	for cursor := len(line.runes) - 1; cursor >= 0; cursor-- {
		line.move(cursor)
		if got := line.view(1, plain, true, ""); got != string(line.runes[cursor]) {
			t.Errorf("width 1, cursor %d shows %q", cursor, got)
		}
	}
	line = newTextLine("abcdefgh")
	line.view(3, plain, true, "")
	for cursor := len(line.runes); cursor >= 0; cursor-- {
		line.move(cursor)
		view := line.view(3, plain, true, "")
		if cursor < len(line.runes) && !strings.Contains(view, string(line.runes[cursor])) {
			t.Errorf("width 3, cursor %d: %q hides the cursor", cursor, view)
		}
	}
	if line.offset != 0 {
		t.Errorf("the line does not scroll back to its start: offset %d", line.offset)
	}
}

func TestEditKeysAreHeldBackFromActions(t *testing.T) {
	for name, op := range lineEdits {
		switch op {
		case editLeft, editRight, editStart, editEnd:
			if name == "left" || name == "right" || name == "home" || name == "end" {
				continue
			}
		}
		if takenKeys[name] == "" {
			t.Errorf("%s edits text but an action may hold it", name)
		}
	}
	for _, name := range []string{"alt+backspace", "alt+delete"} {
		if takenKeys[name] != "edits text in a text line" {
			t.Errorf("%s is not reserved for text lines", name)
		}
	}
}

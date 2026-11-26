package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// framed draws a part at column 1 and row 1 of the screen, as inside a frame, and returns its rows as they
// stand on that screen and the map of its targets.
func framed(t *testing.T, view func(Styles, int, int) (string, []Target), look string, w, h int) ([]string, *MouseMap) {
	t.Helper()
	out, targets := view(looks(t)[look], w, h)
	rows := strings.Split(ansi.Strip(out), "\n")
	for i := range rows {
		rows[i] = "|" + rows[i] + "|"
	}
	return rows, NewMouseMap(OffsetTargets(targets, 1, 1), true)
}

// at is the screen cell of the first occurrence of sub in the screen rows, ASCII before it.
func at(t *testing.T, rows []string, sub string) (x, y int) {
	t.Helper()
	for i, r := range rows {
		if j := strings.Index(r, sub); j >= 0 {
			return ansi.StringWidth(r[:j]), i + 1
		}
	}
	t.Fatalf("no %q in %q", sub, rows)
	return 0, 0
}

func drag(s *TextSelection, m *MouseMap, x0, y0, x1, y1 int) (string, bool) {
	s.Mouse(m, tea.MouseClickMsg{X: x0, Y: y0, Button: tea.MouseLeft})
	s.Mouse(m, tea.MouseMotionMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	return s.Mouse(m, tea.MouseReleaseMsg{X: x1, Y: y1, Button: tea.MouseLeft})
}

func TestTextSelectionCopiesTextWithoutNumbersOrFrame(t *testing.T) {
	v := newText(t, TextViewSpec{Numbers: true}, i18n.English)
	v.SetText("alpha one\nbeta two\ngamma three", nil)
	rows, m := framed(t, v.View, "noneascii", 30, 6)
	var s TextSelection
	// From the column left of the numbers to the frame column right of the last line.
	if got, ok := drag(&s, m, 1, 1, 31, 3); !ok || got != "alpha one\nbeta two\ngamma three" {
		t.Errorf("whole text: %q %v", got, ok)
	}
	x0, y0 := at(t, rows, "one")
	x1, y1 := at(t, rows, "beta")
	if got, _ := drag(&s, m, x0, y0, x1+3, y1); got != "one\nbeta" {
		t.Errorf("part: %q", got)
	}
	// Backwards gives the same, and below the last line reaches its end.
	if got, _ := drag(&s, m, x1+3, y1, x0, y0); got != "one\nbeta" {
		t.Errorf("backwards: %q", got)
	}
	if got, _ := drag(&s, m, x1, y1, 2, 5); got != "beta two\ngamma three" {
		t.Errorf("below the text: %q", got)
	}
}

func TestTextSelectionFollowsLinesNotWrapping(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyz0123456789"
	for _, wrap := range []bool{true, false} {
		v := newText(t, TextViewSpec{Wrap: wrap, Numbers: true}, i18n.English)
		v.SetText(long+"\n"+long, nil)
		_, m := framed(t, v.View, "noneascii", 20, 8)
		var s TextSelection
		got, _ := drag(&s, m, 1, 1, 40, 7)
		if got != long+"\n"+long {
			t.Errorf("wrap %v: %q", wrap, got)
		}
	}
}

func TestTextSelectionWideCharactersAndTabs(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText("a\tb世界c", nil)
	rows, m := framed(t, v.View, "noneascii", 30, 4)
	xa, y := at(t, rows, "a ")
	var s TextSelection
	// a, four cells of the tab, b, two cells each for the wide characters, c.
	for _, tt := range []struct {
		from, to int
		want     string
	}{
		{5, 6, "b世"}, {6, 9, "世界"}, {7, 8, "世界"}, {9, 10, "界c"}, {2, 5, "   b"}, {0, 1, "a "},
	} {
		if got, _ := drag(&s, m, xa+tt.from, y, xa+tt.to, y); got != tt.want {
			t.Errorf("cells %d to %d: %q, want %q", tt.from, tt.to, got, tt.want)
		}
	}

	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(LogLine{Text: "x\ty世界z"})
	rows, m = framed(t, l.View, "noneascii", 30, 4)
	xx, y := at(t, rows, "x\\t")
	if got, _ := drag(&s, m, xx+1, y, xx+5, y); got != `\ty世` {
		t.Errorf("log: %q", got)
	}
}

func TestTextSelectionNeverCopiesAMaskedSection(t *testing.T) {
	for _, look := range []string{"dark", "noneascii"} {
		for _, revealed := range []bool{false, true} {
			v := newText(t, TextViewSpec{Wrap: true, Numbers: true}, i18n.English)
			v.SetText(maskText, maskSpans())
			v.SetRevealed(revealed)
			rows, m := framed(t, v.View, look, 24, 20)
			glyph := strings.Repeat(looks(t)[look].Glyph(GlyphMask), textMaskCells)
			var s TextSelection
			copies := map[string]string{}
			copies["all"], _ = drag(&s, m, 1, 1, 40, 20)
			xs, ys := at(t, rows, "session")
			// Into the masked section by one, two and three cells, and from inside it out.
			for n := 1; n <= 3; n++ {
				got, _ := drag(&s, m, xs, ys, xs+len("session = ")-1+n, ys)
				copies["into"+string(rune('0'+n))] = got
			}
			copies["out"], _ = drag(&s, m, xs+len("session = ")+2, ys, 40, ys+3)
			if out, _ := v.View(looks(t)[look], 24, 20); !revealed {
				for _, secret := range maskSecrets {
					if strings.Contains(ansi.Strip(out), secret) {
						t.Errorf("%s: the highlight shows %q", look, secret)
					}
				}
			}
			for name, got := range copies {
				for _, secret := range append(maskSecrets, "hun", "QUJ", "-va") {
					if strings.Contains(got, secret) {
						t.Errorf("%s revealed %v %s: %q holds %q", look, revealed, name, got, secret)
					}
				}
				if !strings.Contains(got, glyph) {
					t.Errorf("%s revealed %v %s: %q without the mask", look, revealed, name, got)
				}
			}
			if want := "session = " + glyph; copies["into1"] != want {
				t.Errorf("%s revealed %v: %q, want %q", look, revealed, copies["into1"], want)
			}
		}
	}
}

func TestTextSelectionKeepsControlCharactersEscaped(t *testing.T) {
	raw := "a\x1b[31mred\x07‮end"
	v := newText(t, TextViewSpec{Wrap: true}, i18n.English)
	v.SetText(raw, nil)
	_, m := framed(t, v.View, "noneascii", 60, 4)
	var s TextSelection
	got, _ := drag(&s, m, 1, 1, 70, 1)
	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(LogLine{Text: raw})
	_, m = framed(t, l.View, "noneascii", 60, 4)
	gotLog, _ := drag(&s, m, 1, 1, 70, 1)
	for name, g := range map[string]string{"text": got, "log": gotLog} {
		if g != Escape(raw) || strings.ContainsAny(g, "\x1b\x07‮") {
			t.Errorf("%s: %q, want %q", name, g, Escape(raw))
		}
	}
}

func TestTextSelectionLogCopiesTextWithoutColumns(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	l := newLog(t, LogViewSpec{Timestamps: true}, i18n.English)
	l.Append(LogLine{Text: "first line of text", Time: stamp, Source: "worker"},
		LogLine{Text: "second line", Time: stamp, Source: "worker"})
	rows, m := framed(t, l.View, "noneascii", 60, 6)
	var s TextSelection
	x, y := at(t, rows, "line of")
	if got, _ := drag(&s, m, x, y, x+6, y); got != "line of" {
		t.Errorf("part: %q", got)
	}
	if got, _ := drag(&s, m, 1, 1, 61, 2); got != "first line of text\nsecond line" {
		t.Errorf("lines: %q", got)
	}
	// Wrapped, a line still ends only where it ends.
	l.Act(ActionLogWrap)
	_, m = framed(t, l.View, "noneascii", 36, 8)
	if got, _ := drag(&s, m, 1, 1, 40, 7); got != "first line of text\nsecond line" {
		t.Errorf("wrapped: %q", got)
	}
}

func TestTextSelectionClickStaysAClick(t *testing.T) {
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText("alpha one\nbeta two", nil)
	before, _ := v.View(looks(t)["dark"], 30, 6)
	_, m := framed(t, v.View, "dark", 30, 6)
	var s TextSelection
	if _, ok := drag(&s, m, 3, 1, 3, 1); ok || s.Active() {
		t.Error("a click without motion selected")
	}
	if res := clickAt(m, 3, 1); !res.Handled || res.Zone != ZoneRows || res.Action != "" {
		t.Errorf("the click reports %+v", res)
	}
	if after, _ := v.View(looks(t)["dark"], 30, 6); after != before {
		t.Error("a click changed the drawing")
	}
	// A button of the status row lies over the area and acts as before.
	_, targets := v.View(looks(t)["dark"], 30, 6)
	wrap := findTarget(t, targets, func(tg Target) bool { return tg.Action == ActionLogWrap })
	bx, by := wrap.Rect.X+1, wrap.Rect.Y+1
	s.Mouse(m, tea.MouseClickMsg{X: bx, Y: by, Button: tea.MouseLeft})
	s.Mouse(m, tea.MouseMotionMsg{X: 3, Y: 1, Button: tea.MouseLeft})
	if _, ok := s.Mouse(m, tea.MouseReleaseMsg{X: 3, Y: 1, Button: tea.MouseLeft}); ok || s.Active() {
		t.Error("a drag from a button selected")
	}
	if res := clickAt(m, bx, by); res.Action != ActionLogWrap {
		t.Errorf("the button reports %+v", res)
	}
	// A part drawn later over the text and a disabled map take no selection.
	over := NewMouseMap(append(OffsetTargets(targets, 1, 1), Target{Rect: Rect{Width: 40, Height: 10}, Zone: ZoneOutside}), true)
	off := NewMouseMap(OffsetTargets(targets, 1, 1), false)
	for name, mm := range map[string]*MouseMap{"covered": over, "disabled": off} {
		if _, ok := drag(&s, mm, 3, 1, 8, 2); ok || s.Active() {
			t.Errorf("%s: selected", name)
		}
	}
}

func TestTextSelectionHighlightsUntilClickOrClear(t *testing.T) {
	st := looks(t)["dark"]
	v := newText(t, TextViewSpec{}, i18n.English)
	v.SetText("alpha one\nbeta two", nil)
	plain, _ := v.View(st, 30, 6)
	rows, m := framed(t, v.View, "dark", 30, 6)
	x, y := at(t, rows, "one")
	var s TextSelection
	if got, ok := drag(&s, m, x, y, x+2, y); !ok || got != "one" {
		t.Fatalf("%q %v", got, ok)
	}
	if out, _ := v.View(st, 30, 6); !strings.Contains(out, st.Render(StyleActiveRow, "one")) {
		t.Error("no highlight")
	}
	s.Mouse(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	s.Mouse(m, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if out, _ := v.View(st, 30, 6); out != plain || s.Active() {
		t.Error("a click left the highlight")
	}
	drag(&s, m, x, y, x+2, y)
	s.Clear()
	if out, _ := v.View(st, 30, 6); out != plain || s.Active() {
		t.Error("Clear left the highlight")
	}
	// The highlight of a log view, and a motion within the pressed cell selects nothing.
	l := newLog(t, LogViewSpec{}, i18n.English)
	l.Append(LogLine{Text: "alpha one"})
	rows, m = framed(t, l.View, "dark", 30, 4)
	x, y = at(t, rows, "one")
	s.Mouse(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	s.Mouse(m, tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	if _, ok := s.Mouse(m, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}); ok {
		t.Error("a motion within the cell selected")
	}
	drag(&s, m, x, y, x+2, y)
	if out, _ := l.View(st, 30, 4); !strings.Contains(out, st.Render(StyleActiveRow, "one")) {
		t.Error("no highlight in the log")
	}
}

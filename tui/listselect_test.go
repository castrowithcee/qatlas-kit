package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func TestListSelectionCopiesOneCellValue(t *testing.T) {
	l := filledList(t, nil)
	rows, m := framed(t, func(s Styles, w, h int) (string, []Target) { return l.View(s, w, h) }, "noneascii", 90, 12)
	x, y := at(t, rows, "spreads fast")
	var s TextSelection
	if got, ok := drag(&s, m, x, y, x+len("spreads fast")-1, y); !ok || got != "spreads fast" {
		t.Errorf("one cell: %q %v", got, ok)
	}
	// A drag past the end of the row ends at its last value, and the header row starts nothing.
	if got, _ := drag(&s, m, x+8, y, 88, y); got != "fast" {
		t.Errorf("to the end of the row: %q", got)
	}
	hx, hy := at(t, rows, "NAME")
	if got, ok := drag(&s, m, hx, hy, 40, y); ok || got != "" {
		t.Errorf("a drag from the header copied %q", got)
	}
}

func TestListSelectionSeparatesCellsAndRows(t *testing.T) {
	l := filledList(t, nil)
	rows, m := framed(t, func(s Styles, w, h int) (string, []Target) { return l.View(s, w, h) }, "noneascii", 90, 12)
	x0, y0 := at(t, rows, "basil")
	x1, y1 := at(t, rows, "30")
	got, ok := drag(new(TextSelection), m, x0, y0, x1+1, y1)
	want := "basil\tblooming\t12\ton the sill by the kitchen window\nmint\tresting\t30"
	if !ok || got != want {
		t.Errorf("block: %q %v, want %q", got, ok, want)
	}
	// A cell in the middle of a row, with an empty note at the end of the next one.
	xs, ys := at(t, rows, "resting")
	if got, _ := drag(new(TextSelection), m, xs, ys, 88, ys+2); got != "resting\t30\tspreads fast\noak\tresting\t1200\tplanted long ago\nthyme\tblooming\t8" {
		t.Errorf("from a middle cell: %q", got)
	}
}

func TestListSelectionLeavesOutTicksFoldGlyphsAndIndent(t *testing.T) {
	for _, look := range []string{"noneascii", "dark"} {
		l := groupedList(t, func(s *ListSpec) { s.Ticks = true; s.AllRow = true })
		_, m := framed(t, func(s Styles, w, h int) (string, []Target) { return l.View(s, w, h) }, look, 90, 14)
		got, ok := drag(new(TextSelection), m, 1, 3, 89, 14)
		if !ok {
			t.Fatalf("%s: nothing copied", look)
		}
		lines := strings.Split(got, "\n")
		if lines[0] != "all" || lines[1] != "Big trees\tresting\t1200" || lines[2] != "oak\tresting\t1200\tplanted long ago" {
			t.Errorf("%s: %q", look, lines)
		}
		for _, line := range lines {
			if strings.HasPrefix(line, " ") {
				t.Errorf("%s: indent in %q", look, line)
			}
		}
		for _, bad := range []string{"NAME", "[ ]", "(", "─"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s: %q in %q", look, bad, got)
			}
		}
	}
}

func TestListSelectionKeepsControlCharactersEscaped(t *testing.T) {
	l, _ := newSampleList(t, Styles{}, i18n.English, nil)
	if err := l.SetItems([]ListItem{{ID: "a", Cells: []string{"x\x1b[2J", "\x07bell", "1", "\x1b[31mred"}}}); err != nil {
		t.Fatal(err)
	}
	_, m := framed(t, func(s Styles, w, h int) (string, []Target) { return l.View(s, w, h) }, "noneascii", 80, 6)
	got, _ := drag(new(TextSelection), m, 1, 3, 79, 3)
	if want := `x\x1b[2J` + "\t" + `\abell` + "\t1\t" + `\x1b[31mred`; got != want {
		t.Errorf("%q, want %q", got, want)
	}
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("a control character was copied: %q", got)
	}
}

func TestListSelectionClickStaysAClick(t *testing.T) {
	l := tickList(t, nil)
	before, _ := l.View(looks(t)["dark"], 90, 14)
	rows, m := framed(t, func(s Styles, w, h int) (string, []Target) { return l.View(s, w, h) }, "dark", 90, 14)
	x, y := at(t, rows, "mint")
	var s TextSelection
	if _, ok := drag(&s, m, x, y, x, y); ok || s.Active() {
		t.Error("a click without motion selected")
	}
	if res := clickAt(m, x, y); !res.Handled || res.Item != "mint" || res.Action != "enter" {
		t.Errorf("the click reports %+v", res)
	}
	if after, _ := l.View(looks(t)["dark"], 90, 14); after != before {
		t.Error("a click changed the drawing")
	}
	// The tick box keeps its click and starts no selection.
	bx, by := at(t, rows, "[ ] mint")
	s.Mouse(m, tea.MouseClickMsg{X: bx + 1, Y: by, Button: tea.MouseLeft})
	s.Mouse(m, tea.MouseMotionMsg{X: bx + 20, Y: by, Button: tea.MouseLeft})
	if _, ok := s.Mouse(m, tea.MouseReleaseMsg{X: bx + 20, Y: by, Button: tea.MouseLeft}); ok || s.Active() {
		t.Error("a drag from a tick box selected")
	}
	if res := clickAt(m, bx+1, by); res.Action != "toggle" {
		t.Errorf("the box reports %+v", res)
	}
}

func TestListSelectionHighlightsTheRangeOnly(t *testing.T) {
	l := filledList(t, nil)
	st := looks(t)["dark"]
	plain, _ := l.View(st, 90, 12)
	rows, m := framed(t, func(s Styles, w, h int) (string, []Target) { return l.View(s, w, h) }, "dark", 90, 12)
	x, y := at(t, rows, "spreads")
	var s TextSelection
	drag(&s, m, x, y, x+5, y)
	marked, _ := l.View(st, 90, 12)
	if marked == plain || stripped(marked) != stripped(plain) {
		t.Error("the selection changes the text or shows no highlight")
	}
	s.Clear()
	if cleared, _ := l.View(st, 90, 12); cleared != plain {
		t.Error("Clear leaves the highlight")
	}
}

func stripped(s string) string { return strings.Join(strings.Fields(ansi.Strip(s)), " ") }

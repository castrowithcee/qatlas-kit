package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func TestFormSelectionCopiesShownValuesOnly(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	look := looks(t)["noneascii"]
	rows, m := framed(t, f.View, "noneascii", 70, 22)
	// A read-only value without its hint, and not the label.
	x, y := at(t, rows, "greenhouse")
	if got, ok := drag(new(TextSelection), m, x, y, 68, y); !ok || got != "greenhouse" {
		t.Errorf("read-only value: %q %v", got, ok)
	}
	// The field with the focus is being typed into, so a press there selects nothing.
	nx, ny := at(t, rows, "basil")
	var s TextSelection
	if got, ok := drag(&s, m, nx, ny, nx+4, ny); ok || got != "" || s.Active() {
		t.Errorf("the typed field copied %q", got)
	}
	// Without the focus the value is shown as text and copies as it.
	f.Update(keyOf("down"))
	f.Update(keyOf("down"))
	rows, m = framed(t, f.View, "noneascii", 70, 22)
	nx, ny = at(t, rows, "basil")
	if got, ok := drag(&s, m, nx, ny, nx+4, ny); !ok || got != "basil" {
		t.Errorf("shown value: %q %v", got, ok)
	}
	// A choice copies its label, the list of values its text.
	kx, ky := at(t, rows, "herb")
	if got, _ := drag(&s, m, kx, ky, 68, ky); got != "herb" {
		t.Errorf("choice: %q", got)
	}
	// A masked field gives the mask of fixed length, whatever was stored or typed, never the line it shows.
	tx, ty := at(t, rows, "(stored")
	want := strings.Repeat(look.Glyph(GlyphMask), textMaskCells)
	if got, ok := drag(&s, m, tx, ty, 68, ty); !ok || got != want {
		t.Errorf("stored masked value: %q %v, want %q", got, ok, want)
	}
}

func TestFormSelectionNeverCopiesTypedMaskedText(t *testing.T) {
	f := newSampleForm(t, i18n.English, func(s *FormSpec) { s.Fields[4].Stored = false })
	for range 3 {
		f.Update(keyOf("down"))
	}
	typeForm(f, "hunter22")
	// Typing: the field shows its input line and is no selection target.
	rows, m := framed(t, f.View, "noneascii", 70, 22)
	tx, ty := at(t, rows, "feeder token")
	if got, ok := drag(new(TextSelection), m, tx+16, ty, 68, ty); ok || strings.Contains(got, "hunter") {
		t.Errorf("typing: %q %v", got, ok)
	}
	// Moved on: the field shows that a value was entered, and the copy is the mask of fixed length.
	f.Update(keyOf("down"))
	rows, m = framed(t, f.View, "noneascii", 70, 22)
	tx, ty = at(t, rows, "(entered")
	got, ok := drag(new(TextSelection), m, tx, ty, 68, ty)
	if want := strings.Repeat(looks(t)["noneascii"].Glyph(GlyphMask), textMaskCells); !ok || got != want {
		t.Errorf("entered: %q %v, want %q", got, ok, want)
	}
}

func TestDialogSelectionCopiesQuestionAndDetails(t *testing.T) {
	d := newSampleDialog(t, confirmSpec(), i18n.English)
	rows, m := framed(t, d.View, "noneascii", 60, 12)
	x, y := at(t, rows, "Water every")
	_, ey := at(t, rows, "resting")
	got, ok := drag(new(TextSelection), m, x, y, 59, ey)
	if want := "Water every plant now?\nThe pump runs for a minute.\n5 plants\n2 of them are resting"; !ok || got != want {
		t.Errorf("%q %v, want %q", got, ok, want)
	}
	// The prompt sign of a command is not part of it.
	p := newSampleDialog(t, previewSpec(), i18n.English)
	rows, m = framed(t, p.View, "noneascii", 60, 14)
	x, y = at(t, rows, "example-tool stop demo")
	if got, _ := drag(new(TextSelection), m, x, y, 59, y); got != "example-tool stop demo" {
		t.Errorf("command: %q", got)
	}
	// A press on the input line selects nothing.
	u := newSampleDialog(t, unlockSpec(), i18n.English)
	_, m = framed(t, u.View, "noneascii", 60, 12)
	if got, ok := drag(new(TextSelection), m, 1, 3, 20, 3); ok {
		t.Errorf("input copied %q", got)
	}
}

func TestHelpSelectionCopiesTextNotKeys(t *testing.T) {
	h := newSampleHelp(t, nil, i18n.English)
	rows, m := framed(t, h.View, "noneascii", 80, 20)
	x, y := at(t, rows, "Plant a seed")
	_, ey := at(t, rows, "1. Choose")
	got, ok := drag(new(TextSelection), m, x, y, 79, ey)
	if want := "Plant a seed first.\n\nBeds:\n1. Choose a bed and dig a small hole for the seed."; !ok || got != want {
		t.Errorf("%q %v, want %q", got, ok, want)
	}
	// The key list under the text stays out of reach.
	_, ky := at(t, rows, "This screen")
	if got, ok := drag(new(TextSelection), m, 1, ky, 79, ky+1); ok {
		t.Errorf("a key list copied %q", got)
	}
}

func TestExplanationSelectionCopiesTextAndAddress(t *testing.T) {
	e := newSampleExplanation(t, sampleExplanation(), i18n.English)
	rows, m := framed(t, e.View, "noneascii", 90, 14)
	x, y := at(t, rows, "The bed")
	_, uy := at(t, rows, "https://")
	item := sampleExplanation()
	want := item.Body + "\n" + item.Term.Title + ": " + item.Term.Text + "\n" + item.URL
	if got, ok := drag(new(TextSelection), m, x, y, 89, uy); !ok || got != want {
		t.Errorf("%q %v, want %q", got, ok, want)
	}
}

// A press on selectable text is held back and comes back at a release without motion; a drag never returns it.
func TestSelectionHoldsPressUntilRelease(t *testing.T) {
	l := tickList(t, func(s *ListSpec) { s.ClickOpens = true })
	rows, m := framed(t, func(s Styles, w, h int) (string, []Target) { return l.View(s, w, h) }, "dark", 90, 14)
	x, y := at(t, rows, "mint")
	press := tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
	var s TextSelection
	s.Mouse(m, press)
	if !s.Holds() {
		t.Error("the press on a row is not held")
	}
	if _, ok := s.Replay(); ok {
		t.Error("the press came back before the release")
	}
	s.Mouse(m, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if c, ok := s.Replay(); !ok || c != press {
		t.Errorf("the click without motion: %+v %v", c, ok)
	}
	if _, ok := s.Replay(); ok {
		t.Error("the click came back twice")
	}
	// A drag drops it.
	s.Mouse(m, press)
	s.Mouse(m, tea.MouseMotionMsg{X: x + 3, Y: y, Button: tea.MouseLeft})
	s.Mouse(m, tea.MouseReleaseMsg{X: x + 3, Y: y, Button: tea.MouseLeft})
	if _, ok := s.Replay(); ok {
		t.Error("a drag came back as a click")
	}
	// The tick box and a place without selection hold nothing.
	bx, by := at(t, rows, "[ ] mint")
	s.Mouse(m, tea.MouseClickMsg{X: bx + 1, Y: by, Button: tea.MouseLeft})
	if s.Holds() {
		t.Error("the tick box is held")
	}
	s.Mouse(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if s.Holds() {
		t.Error("a place without selection is held")
	}
}

package tui

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// overlayFrame is the frame of a drawn overlay, found by its corners.
func overlayFrame(t *testing.T, view string) Rect {
	t.Helper()
	rows, top, bottom := frameRows(view)
	if top < 0 || bottom < 0 {
		t.Fatalf("no frame in %q", rows)
	}
	row := rows[top]
	left, end := strings.Index(row, "╭"), strings.Index(row, "╮")
	return Rect{X: left, Y: top, Width: utf8.RuneCountInString(row[left : end+len("╮")]), Height: bottom - top + 1}
}

func (r Rect) has(x, y int) bool { return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height }

// checkOutside draws an overlay with and without the option and checks that the option adds targets outside
// the frame only, and changes neither the drawing nor the targets inside. It returns the targets with the option.
func checkOutside(t *testing.T, width, height int, draw func(on bool) (string, []Target, bool)) (Rect, *MouseMap) {
	t.Helper()
	offView, offTargets, ok := draw(false)
	if !ok {
		t.Fatal("no overlay without the option")
	}
	onView, onTargets, ok := draw(true)
	if !ok {
		t.Fatal("no overlay with the option")
	}
	if onView != offView {
		t.Error("the option changed the drawing")
	}
	frame := overlayFrame(t, onView)
	// The parts of the two drawings are two values, so only their presence is compared.
	var rest, off2 []Target
	for _, tg := range onTargets {
		if tg.Zone != ZoneOutside {
			rest = append(rest, withoutPart(tg))
		}
	}
	for _, tg := range offTargets {
		off2 = append(off2, withoutPart(tg))
	}
	if !reflect.DeepEqual(rest, off2) {
		t.Error("the option changed the targets of the frame")
	}
	off, on := NewMouseMap(offTargets, true), NewMouseMap(onTargets, true)
	for y := range height {
		for x := range width {
			res := clickAt(on, x, y)
			if got, want := res.Zone == ZoneOutside, !frame.has(x, y); got != want {
				t.Fatalf("cell %d,%d: outside is %v, want %v", x, y, got, want)
			}
			if clickAt(off, x, y).Zone == ZoneOutside {
				t.Fatalf("cell %d,%d: outside without the option", x, y)
			}
		}
	}
	return frame, on
}

func withoutPart(t Target) Target {
	if t.selection != nil {
		t.selection = selectable(&List{})
	}
	return t
}

// outsideAt is the click on a cell beyond the frame, blankAt one on its padding, where no target lies.
func outsideAt(m *MouseMap, f Rect) MouseResult { return clickAt(m, f.X-1, f.Y+1) }
func blankAt(m *MouseMap, f Rect) MouseResult   { return clickAt(m, f.X+1, f.Y+1) }

func TestDialogCloseOutside(t *testing.T) {
	spec := confirmSpec()
	spec.CloseOutside = "no"
	draw := func(on bool) (string, []Target, bool) {
		s := confirmSpec()
		if on {
			s.CloseOutside = "no"
		}
		return newSampleDialog(t, s, i18n.English).Overlay(looks(t)["dark"], sampleScreen(80, 24), 80, 24, Rect{Y: 2, Width: 80, Height: 22})
	}
	frame, m := checkOutside(t, 80, 24, draw)
	d := newSampleDialog(t, spec, i18n.English)
	if a, h := d.Click(outsideAt(m, frame)); a != "no" || !h {
		t.Errorf("click outside: %q %v", a, h)
	}
	if a, h := d.Click(blankAt(m, frame)); a != "" || h {
		t.Errorf("click inside without target: %q %v", a, h)
	}
	// Without the option a click outside is no event for the dialog.
	plain := newSampleDialog(t, confirmSpec(), i18n.English)
	if a, h := plain.Click(MouseResult{Zone: ZoneOutside, Handled: true}); a != "" || h {
		t.Errorf("without the option: %q %v", a, h)
	}
}

func TestDialogCloseOutsideIgnoredWithDanger(t *testing.T) {
	spec := leaveSpec()
	spec.CloseOutside = "back"
	d := newSampleDialog(t, spec, i18n.English)
	_, targets, ok := d.Overlay(looks(t)["dark"], sampleScreen(80, 24), 80, 24, Rect{Y: 2, Width: 80, Height: 22})
	if !ok {
		t.Fatal("no overlay")
	}
	for _, tg := range targets {
		if tg.Zone == ZoneOutside {
			t.Fatalf("outside target on a destructive question: %+v", tg)
		}
	}
	if a, h := d.Click(MouseResult{Zone: ZoneOutside, Handled: true}); a != "" || h {
		t.Errorf("click outside a destructive question: %q %v", a, h)
	}
}

func TestNewDialogRejectsCloseOutsideThatIsNoAnswer(t *testing.T) {
	spec := confirmSpec()
	spec.CloseOutside = "nonesuch"
	keys := sampleKeymap(t, CharsetUnicode)
	if _, err := NewDialog(spec, keys, newTranslator(t, i18n.English, dialogCatalogs(), keys)); err == nil {
		t.Fatal("accepted")
	}
}

func TestHelpCloseOutside(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	build := func(on bool) *Help {
		spec := sampleHelpSpec()
		spec.CloseOutside = on
		h, err := NewHelp(spec, keys, newTranslator(t, i18n.English, helpCatalogs(), keys))
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	frame, m := checkOutside(t, 100, 40, func(on bool) (string, []Target, bool) {
		return build(on).Overlay(looks(t)["dark"], sampleScreen(100, 40), 100, 40, Rect{Y: 2, Width: 100, Height: 37})
	})
	h := build(true)
	if a, ok := h.Click(outsideAt(m, frame)); a != "back" || !ok {
		t.Errorf("click outside: %q %v", a, ok)
	}
	if a, ok := h.Click(blankAt(m, frame)); a != "" || ok {
		t.Errorf("click inside without target: %q %v", a, ok)
	}
	if a, ok := build(false).Click(MouseResult{Zone: ZoneOutside, Handled: true}); a != "" || ok {
		t.Errorf("without the option: %q %v", a, ok)
	}
}

func TestFormCloseOutside(t *testing.T) {
	build := func(on bool) *Form {
		return newSampleForm(t, i18n.English, func(s *FormSpec) { s.CloseOutside = on })
	}
	frame, m := checkOutside(t, 100, 40, func(on bool) (string, []Target, bool) {
		return build(on).Overlay(looks(t)["dark"], sampleScreen(100, 40), 100, 40, Rect{Y: 2, Width: 100, Height: 37})
	})
	f := build(true)
	if err := f.SetValue("name", FormValue{Text: "mint"}); err != nil {
		t.Fatal(err)
	}
	if !f.Changed() {
		t.Fatal("the form is not changed")
	}
	// Like Esc: back, not handled, so the application asks; nothing is discarded.
	if a, h := f.Click(outsideAt(m, frame)); a != "back" || h {
		t.Errorf("click outside: %q %v", a, h)
	}
	if !f.Changed() || f.Values()["name"].Text != "mint" {
		t.Error("a click outside dropped the change")
	}
	if a, h := f.Click(blankAt(m, frame)); a != "" || h {
		t.Errorf("click inside without target: %q %v", a, h)
	}
	// An open list of values closes first, as on Esc.
	f.Focus("kind")
	f.Act("enter")
	if !f.Open() {
		t.Fatal("the list of values did not open")
	}
	if a, h := f.Click(outsideAt(m, frame)); a != "" || !h || f.Open() {
		t.Errorf("click outside an open list: %q %v open %v", a, h, f.Open())
	}
	if a, h := build(false).Click(MouseResult{Zone: ZoneOutside, Handled: true}); a != "" || h {
		t.Errorf("without the option: %q %v", a, h)
	}
}

func TestListCloseOutside(t *testing.T) {
	st := looks(t)["dark"]
	build := func(on bool) *List {
		return newModalList(t, st, 0, func(s *ListSpec) { s.CloseOutside = on })
	}
	frame, m := checkOutside(t, 100, 40, func(on bool) (string, []Target, bool) {
		return build(on).Overlay(st, sampleScreen(100, 40), 100, 40, Rect{Y: 2, Width: 100, Height: 37})
	})
	l := build(true)
	l.SetFilter("bas")
	if _, h := l.Click(blankAt(m, frame)); h || l.query.String() != "bas" {
		t.Errorf("click inside without target: handled %v filter %q", h, l.query.String())
	}
	// Like Back: the first click clears the filter, the next leaves the close to the application.
	if o, h := l.Click(outsideAt(m, frame)); o != "" || !h || l.query.String() != "" {
		t.Errorf("click outside with a filter: %q %v filter %q", o, h, l.query.String())
	}
	if o, h := l.Click(outsideAt(m, frame)); o != "" || h {
		t.Errorf("click outside: %q %v", o, h)
	}
	off := build(false)
	off.SetFilter("bas")
	if _, h := off.Click(MouseResult{Zone: ZoneOutside, Handled: true}); h || off.query.String() != "bas" {
		t.Errorf("without the option: handled %v filter %q", h, off.query.String())
	}
}

func TestPathNavCloseOutside(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	build := func(on bool) *PathNav {
		k := samplePathNavKeys()
		k.CloseOutside = on
		p, err := NewPathNav(k, keys, newTranslator(t, i18n.English, frameCatalogs(), keys))
		if err != nil {
			t.Fatal(err)
		}
		p.OpenOverview(pathSpec(80))
		return p
	}
	frame, m := checkOutside(t, 80, 20, func(on bool) (string, []Target, bool) {
		return build(on).Overlay(looks(t)["dark"], sampleScreen(80, 20), 80, 20, pathNavRoom(80, 20))
	})
	p := build(true)
	if c, _ := p.Click(blankAt(m, frame)); c != (PathChoice{}) || !p.Open() {
		t.Errorf("click inside without target: %+v open %v", c, p.Open())
	}
	if c, h := p.Click(outsideAt(m, frame)); c != (PathChoice{}) || !h || p.Open() {
		t.Errorf("click outside: %+v %v open %v", c, h, p.Open())
	}
	off := build(false)
	if c, h := off.Click(MouseResult{Zone: ZoneOutside, Handled: true}); c != (PathChoice{}) || h || !off.Open() {
		t.Errorf("without the option: %+v %v open %v", c, h, off.Open())
	}
}

func TestCommandLineCloseOutside(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	build := func(on bool) *CommandLine {
		spec := CommandLineSpec{Commands: sampleCommands(), Keys: sampleCommandKeys, CloseOutside: on}
		c, err := NewCommandLine(spec, keys, newTranslator(t, i18n.English, commandCatalogs(), keys))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	frame, m := checkOutside(t, 100, 40, func(on bool) (string, []Target, bool) {
		return build(on).Overlay(looks(t)["dark"], sampleScreen(100, 40), 100, 40, Rect{Y: 2, Width: 100, Height: 37})
	})
	c := build(true)
	if _, done := c.Click(blankAt(m, frame)); done {
		t.Error("click inside without target closed")
	}
	if _, done := c.Click(outsideAt(m, frame)); !done {
		t.Error("click outside did not close")
	}
	if _, done := build(false).Click(MouseResult{Zone: ZoneOutside, Handled: true}); done {
		t.Error("without the option the click closed")
	}
}

func TestExplanationCloseOutside(t *testing.T) {
	keys := explanationKeymap(t)
	build := func(on bool) *Explanation {
		e, err := NewExplanation(ExplanationSpec{Item: sampleExplanation(), Keys: sampleExplanationKeys(), CloseOutside: on},
			keys, newTranslator(t, i18n.English, nil, keys))
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	frame, m := checkOutside(t, 100, 40, func(on bool) (string, []Target, bool) {
		return build(on).Overlay(looks(t)["dark"], sampleScreen(100, 40), 100, 40, Rect{Y: 2, Width: 100, Height: 37})
	})
	if a, ok := build(true).Click(outsideAt(m, frame)); a != "back" || !ok {
		t.Errorf("click outside: %q %v", a, ok)
	}
	if a, ok := build(false).Click(MouseResult{Zone: ZoneOutside, Handled: true}); a != "" || ok {
		t.Errorf("without the option: %q %v", a, ok)
	}
}

package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// withButtons gives the sample form a main button for save, one for back and one for an action of its own.
func withButtons(s *FormSpec) {
	s.Buttons = []FormButton{{Action: "save", Label: "b.save", Primary: true}, {Action: "back", Label: "b.cancel"},
		{Action: "reload", Label: "b.reload"}}
}

func focusPlace(f *Form) string {
	if a, ok := f.FocusedButton(); ok {
		return "button:" + string(a)
	}
	return string(f.Focused())
}

func TestFormButtonsJoinTheTabCycle(t *testing.T) {
	f := newSampleForm(t, i18n.English, withButtons)
	f.Focus("notes")
	var seen []string
	for range 6 {
		if _, handled := f.Update(press("tab")); !handled {
			t.Fatal("tab is always taken")
		}
		seen = append(seen, focusPlace(f))
	}
	// The read-only field and the closed section stay skipped; the cycle ends on the first field.
	if got := strings.Join(seen, ","); got != "button:save,button:back,button:reload,name,kind,water" {
		t.Errorf("tab walks %s", got)
	}
	f.Focus("name")
	seen = nil
	for range 5 {
		if _, handled := f.Update(press("shift+tab")); !handled {
			t.Fatal("shift+tab is always taken")
		}
		seen = append(seen, focusPlace(f))
	}
	if got := strings.Join(seen, ","); got != "button:reload,button:back,button:save,notes,token" {
		t.Errorf("shift+tab walks %s", got)
	}
}

func TestFormWithOneFieldAndButtonsCyclesBetweenThem(t *testing.T) {
	f := newSampleForm(t, i18n.English, func(s *FormSpec) {
		s.Sections, s.Fields = nil, []FormField{{ID: "name", Kind: FieldText, Label: "f.name"}}
		s.Buttons = []FormButton{{Action: "save", Label: "b.save"}}
	})
	f.Update(press("tab"))
	if focusPlace(f) != "button:save" {
		t.Errorf("tab gave %s", focusPlace(f))
	}
	f.Update(press("tab"))
	if focusPlace(f) != "name" {
		t.Errorf("tab gave %s", focusPlace(f))
	}
}

func TestFormButtonEnterClickAndKeyGiveTheSameAction(t *testing.T) {
	for i, tt := range []struct {
		action  ActionID
		key     string
		act     ActionID
		handled bool
	}{{"save", "f2", "save", true}, {"back", "esc", "back", false}, {"reload", "ctrl+r", "reload", true}} {
		f := newSampleForm(t, i18n.English, withButtons)
		f.Focus("notes")
		for range i + 1 {
			f.Update(press("tab"))
		}
		if got, _ := f.FocusedButton(); got != tt.action {
			t.Fatalf("button %d: focus on %q", i, got)
		}
		act, handled := f.Update(press("enter"))
		if act != tt.act || handled != tt.handled {
			t.Errorf("enter on %q gave %q, %v", tt.action, act, handled)
		}
		f.Focus("name")
		act, handled = f.Update(press(tt.key))
		if act != tt.act || handled != tt.handled {
			t.Errorf("key of %q gave %q, %v", tt.action, act, handled)
		}
		act, handled = f.Click(MouseResult{Handled: true, Action: tt.action, Item: formButtonRef + strconv.Itoa(i)})
		if act != tt.act || handled != tt.handled {
			t.Errorf("click on %q gave %q, %v", tt.action, act, handled)
		}
		if f.Focused() != "name" {
			t.Errorf("a click or key on %q moved the focus to %q", tt.action, f.Focused())
		}
	}
}

func TestFormButtonClickTargetsAreDrawn(t *testing.T) {
	f := newSampleForm(t, i18n.English, withButtons)
	view, targets := f.View(looks(t)["noneascii"], 80, 20)
	rows := strings.Split(view, "\n")
	found := 0
	for j := range 3 {
		for _, tg := range targets {
			if tg.Item != formButtonRef+strconv.Itoa(j) {
				continue
			}
			found++
			text := cellsAt(rows[tg.Rect.Y], tg.Rect.X, tg.Rect.Width)
			if !strings.HasPrefix(text, "[ ") || !strings.HasSuffix(text, " ]") || tg.Action != f.buttons[j].Action {
				t.Errorf("target %d covers %q with action %q", j, text, tg.Action)
			}
		}
	}
	if found != 3 {
		t.Errorf("%d button targets in\n%s", found, view)
	}
}

func TestFormButtonFocusShowsWithoutColour(t *testing.T) {
	f := newSampleForm(t, i18n.English, withButtons)
	st := looks(t)["noneascii"]
	view, _ := f.View(st, 80, 20)
	if !strings.Contains(view, "[ F2 save ]") || !strings.Contains(view, "[ esc cancel ]") || strings.Contains(view, "> F2") {
		t.Errorf("no button has the focus:\n%s", view)
	}
	f.Focus("notes")
	f.Update(press("tab"))
	view, _ = f.View(st, 80, 20)
	if !strings.Contains(view, "> F2 save") || !strings.Contains(view, "[ esc cancel ]") {
		t.Errorf("the focused button is not marked:\n%s", view)
	}
	if strings.Contains(view, "> notes") || strings.Contains(view, ">  notes") {
		t.Errorf("a field is marked while a button has the focus:\n%s", view)
	}
}

func TestFormButtonShowsTheKeyInForce(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	if err := keys.Bind(map[ActionID]string{"save": "f4"}); err != nil {
		t.Fatal(err)
	}
	spec := sampleFormSpec()
	withButtons(&spec)
	f, err := NewForm(spec, keys, newTranslator(t, i18n.English, formCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	view, _ := f.View(looks(t)["noneascii"], 80, 20)
	if !strings.Contains(view, "F4 save") || strings.Contains(view, "F2") {
		t.Errorf("the rebound key is not on the button:\n%s", view)
	}
}

func TestFormButtonLabelsAreEscaped(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	spec := sampleFormSpec()
	spec.Buttons = []FormButton{{Action: "save", Label: "b.evil"}}
	text := newTranslator(t, i18n.English, i18n.Catalogs[TextID]{i18n.English: {"b.evil": "go\x1b[31m"}}, keys)
	f, err := NewForm(spec, keys, text)
	if err != nil {
		t.Fatal(err)
	}
	if view, _ := f.View(looks(t)["noneascii"], 80, 20); strings.Contains(view, "\x1b") {
		t.Errorf("a control character reached the screen: %q", view)
	}
}

func TestFormOnAButtonTypingChangesNoField(t *testing.T) {
	f := newSampleForm(t, i18n.English, withButtons)
	f.Focus("notes")
	f.Update(press("tab"))
	before := f.Values()
	if f.Input() != InputNavigation {
		t.Errorf("input is %v", f.Input())
	}
	typeForm(f, "zq")
	if f.Paste("pasted") {
		t.Error("a button took a paste")
	}
	for _, k := range []string{"left", "right", "home", "end", "space"} {
		f.Update(keyOf(k))
	}
	for _, k := range []string{"up", "down", "pgup", "pgdown"} {
		if _, handled := f.Update(keyOf(k)); !handled && (k == "up" || k == "down") {
			t.Errorf("%s left the button", k)
		}
	}
	f.Wheel(2)
	if place := focusPlace(f); place != "button:save" {
		t.Errorf("focus moved to %s", place)
	}
	for id, v := range f.Values() {
		if !v.equal(before[id]) {
			t.Errorf("field %q changed to %+v", id, v)
		}
	}
	// A click on a field takes the focus from the button.
	f.Click(MouseResult{Handled: true, Item: formFieldRef + "name"})
	if focusPlace(f) != "name" {
		t.Errorf("focus is on %s after a click on a field", focusPlace(f))
	}
}

func TestFormButtonsShowInTheHints(t *testing.T) {
	f := newSampleForm(t, i18n.English, withButtons)
	f.Focus("notes")
	f.Update(press("tab"))
	h := f.Hints()
	if len(h) == 0 || h[0].Action != "enter" || h[0].Label != "b.save" {
		t.Errorf("hints on a button: %+v", h)
	}
}

func TestNewFormRejectsBrokenButtons(t *testing.T) {
	for _, tt := range []struct {
		name    string
		buttons []FormButton
		want    string
	}{
		{"unknown action", []FormButton{{Action: "nowhere", Label: "b.save"}}, "no action of the key table"},
		{"no label", []FormButton{{Action: "save"}}, "no label"},
		{"moving key", []FormButton{{Action: "enter", Label: "b.save"}}, "moves in the form"},
		{"twice", []FormButton{{Action: "save", Label: "b.save"}, {Action: "save", Label: "b.cancel"}}, "another button"},
	} {
		keys := sampleKeymap(t, CharsetUnicode)
		spec := sampleFormSpec()
		spec.Buttons = tt.buttons
		_, err := NewForm(spec, keys, newTranslator(t, i18n.English, formCatalogs(), keys))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
}

func TestFormWithButtonsDrawsWithoutThemWhereTheFieldsNeedTheRoom(t *testing.T) {
	st := looks(t)["noneascii"]
	for h := 1; h <= 20; h++ {
		for w := 10; w <= 120; w += 10 {
			f := newSampleForm(t, i18n.English, withButtons)
			view, targets := f.View(st, w, h)
			if got := len(strings.Split(view, "\n")); got != h {
				t.Fatalf("%dx%d: %d rows", w, h, got)
			}
			for _, tg := range targets {
				if tg.Rect.X < 0 || tg.Rect.X+tg.Rect.Width > w || tg.Rect.Y < 0 || tg.Rect.Y+tg.Rect.Height > h {
					t.Fatalf("%dx%d: target %+v outside", w, h, tg)
				}
			}
		}
	}
}

func TestGoldenFormButtons(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		w, h       int
		onButton   bool
	}{
		{"formbuttons.narrow.golden", "noneascii", 40, 16, false},
		{"formbuttons.wide.golden", "noneascii", 120, 16, true},
		{"formbuttons.wide.dark.golden", "dark", 120, 16, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSampleForm(t, i18n.English, withButtons)
			if tt.onButton {
				f.Focus("notes")
				f.Update(press("tab"))
			}
			out, _ := f.View(looks(t)[tt.look], tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

func TestFormFocusIsAlwaysInView(t *testing.T) {
	st := looks(t)["noneascii"]
	for h := 1; h <= 20; h++ {
		for w := 10; w <= 120; w += 10 {
			f := newSampleForm(t, i18n.English, withButtons)
			f.Focus("notes")
			f.Update(press("tab"))
			view, targets := f.View(st, w, h)
			if got := len(strings.Split(view, "\n")); got != h {
				t.Fatalf("%dx%d: %d rows", w, h, got)
			}
			if strings.Contains(view, "Resize") {
				continue // the form asks for a larger terminal
			}
			if !strings.Contains(view, "> F2 save") {
				t.Errorf("%dx%d: the focused button is not in view:\n%s", w, h, view)
			}
			for _, tg := range targets {
				if tg.Rect.X < 0 || tg.Rect.X+tg.Rect.Width > w || tg.Rect.Y < 0 || tg.Rect.Y+tg.Rect.Height > h {
					t.Fatalf("%dx%d: target %+v outside", w, h, tg)
				}
			}
			// With a field focused, its marker is in view whenever the form draws at all.
			g := newSampleForm(t, i18n.English, withButtons)
			gv, _ := g.View(st, w, h)
			if !strings.Contains(gv, "Resize") && !strings.Contains(gv, "> name") {
				t.Errorf("%dx%d: the focused field is not in view:\n%s", w, h, gv)
			}
		}
	}
}

func TestFormBackIsReportedByEveryWayToPressIt(t *testing.T) {
	f := newSampleForm(t, i18n.English, withButtons)
	f.Focus("notes")
	f.Update(press("tab"))
	f.Update(press("tab"))
	if a, _ := f.FocusedButton(); a != "back" {
		t.Fatalf("focus on %q", a)
	}
	// An application reads nothing but the result of Update and Click.
	for name, got := range map[string]func() (ActionID, bool){
		"enter on the button": func() (ActionID, bool) { return f.Update(press("enter")) },
		"the back key":        func() (ActionID, bool) { return f.Update(press("esc")) },
		"a click": func() (ActionID, bool) {
			return f.Click(MouseResult{Handled: true, Action: "back", Item: formButtonRef + "1"})
		},
	} {
		if act, handled := got(); act != "back" || handled {
			t.Errorf("%s gave %q, %v; want back, not handled", name, act, handled)
		}
	}
}

func withModal(s *FormSpec) {
	withButtons(s)
	s.Title = "t.plant"
}

func TestFormOverlayStandsCentredWithTitleAndOwnTargets(t *testing.T) {
	st := looks(t)["dark"]
	f := newSampleForm(t, i18n.English, withModal)
	room := Rect{Y: 3, Width: 100, Height: 46}
	view, targets, ok := f.Overlay(st, sampleScreen(100, 50), 100, 50, room)
	if !ok {
		t.Fatal("the form does not fit 100x30")
	}
	plain := strings.Split(ansi.Strip(view), "\n")
	if len(plain) != 50 {
		t.Fatalf("%d lines", len(plain))
	}
	top, bottom, left, right := -1, -1, -1, -1
	for i, line := range plain {
		if lipgloss.Width(line) != 100 {
			t.Fatalf("line %d has %d cells", i, lipgloss.Width(line))
		}
		if j := strings.Index(line, "╭"); j >= 0 && top < 0 {
			top, left = i, lipgloss.Width(line[:j])
			right = 100 - left - lipgloss.Width(line[j:strings.Index(line, "╮")+len("╮")])
		}
		if strings.Contains(line, "╰") {
			bottom = i
		}
	}
	if top < room.Y || abs(left-right) > 1 || abs((top-room.Y)-(room.Y+room.Height-1-bottom)) > 1 {
		t.Errorf("the form spans rows %d-%d, columns from %d, not centred in its room:\n%s", top, bottom, left, ansi.Strip(view))
	}
	if !strings.Contains(plain[top], "Edit plant") {
		t.Errorf("the frame has no title: %q", plain[top])
	}
	if !strings.HasPrefix(plain[0], "Garden") || strings.Contains(view, "\x1b[31m") {
		t.Error("the screen behind is not dimmed and stripped")
	}
	if !strings.Contains(strings.Join(plain, "\n"), "[ F2 save ]") {
		t.Errorf("no save button:\n%s", ansi.Strip(view))
	}
	if !strings.Contains(plain[bottom-1], "tab") {
		t.Errorf("no key line above the frame: %q", plain[bottom-1])
	}
	if len(targets) == 0 {
		t.Fatal("no targets")
	}
	for _, tg := range targets {
		r := tg.Rect
		if r.X < left || r.X+r.Width > 100-right || r.Y <= top || r.Y >= bottom {
			t.Errorf("target %+v lies outside the frame (rows %d-%d, columns %d-%d)", tg, top, bottom, left, 100-right)
		}
	}
	// The buttons stand where their targets say, and a click on one gives its action.
	var got []ActionID
	for _, tg := range targets {
		if !strings.HasPrefix(tg.Item, formButtonRef) {
			continue
		}
		row := plain[tg.Rect.Y]
		if cellsAt(row, tg.Rect.X, tg.Rect.Width) == "" || !strings.Contains(row, strings.TrimSpace(cellsAt(row, tg.Rect.X, tg.Rect.Width))) {
			t.Errorf("button target %+v does not cover its button in %q", tg, row)
		}
		got = append(got, pressFor(t, f, tg))
	}
	if !slices.Equal(got, []ActionID{"save", "back", "reload"}) {
		t.Errorf("the buttons gave %v", got)
	}
	// A click on a field focuses it.
	for _, tg := range targets {
		if tg.Item == formFieldRef+"soil" {
			pressFor(t, f, tg)
			if f.Focused() != "soil" {
				t.Errorf("a click on the field focused %q", f.Focused())
			}
			if !strings.Contains(plain[tg.Rect.Y], "soil") {
				t.Errorf("the field target is not on its row: %q", plain[tg.Rect.Y])
			}
		}
	}
}

func TestFormOverlayWithoutTitleHasNoTitle(t *testing.T) {
	f := newSampleForm(t, i18n.English, withButtons)
	view, _, ok := f.Overlay(looks(t)["dark"], sampleScreen(100, 50), 100, 50, Rect{Y: 2, Width: 100, Height: 48})
	if !ok {
		t.Fatal("does not fit")
	}
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if j := strings.Index(line, "╭"); j >= 0 {
			if frame := line[j:]; strings.Trim(frame[:strings.Index(frame, "╮")+len("╮")], "╭─╮") != "" {
				t.Errorf("the frame carries a title: %q", line)
			}
		}
	}
}

func TestFormOverlayNeedsRoom(t *testing.T) {
	st := looks(t)["noneascii"]
	minHeight := func(f *Form) int {
		for h := 1; h <= 60; h++ {
			if _, _, ok := f.Overlay(st, sampleScreen(100, 60), 100, 60, Rect{Width: 100, Height: h}); ok {
				return h
			}
		}
		t.Fatal("never fits")
		return 0
	}
	if _, _, ok := newSampleForm(t, i18n.English, withModal).Overlay(st, sampleScreen(40, 30), 40, 30, Rect{Width: 43, Height: 30}); ok {
		t.Error("the form fits a room narrower than the modal minimum")
	}
	closed := newSampleForm(t, i18n.English, withModal)
	base := minHeight(closed)
	if v, ts, ok := closed.Overlay(st, "", 100, 60, Rect{Width: 100, Height: base - 1}); ok || v != "" || ts != nil {
		t.Error("a room one row short still draws")
	}
	open := newSampleForm(t, i18n.English, withModal)
	open.Focus("kind")
	open.Update(press("enter"))
	if !open.Open() {
		t.Fatal("the list did not open")
	}
	if got := minHeight(open); got <= base {
		t.Errorf("the open list needs %d rows, the closed form %d", got, base)
	}
	if _, _, ok := open.Overlay(st, sampleScreen(100, 60), 100, 60, Rect{Width: 100, Height: base}); ok {
		t.Error("the open list fits where only the closed form does")
	}
	cal := newDateForm(t, i18n.English, func(s *FormSpec) { s.Title = "" })
	cb := minHeight(cal)
	cal.Focus("from")
	cal.Click(MouseResult{Handled: true, Item: formCalendarRef + "from"})
	if !cal.cal {
		t.Fatal("the calendar did not open")
	}
	if _, _, ok := cal.Overlay(st, sampleScreen(100, 60), 100, 60, Rect{Width: 100, Height: cb}); ok {
		t.Error("the open calendar fits where only the closed form does")
	}
	if got := minHeight(cal); got <= cb {
		t.Errorf("the calendar needs %d rows, the closed form %d", got, cb)
	}
}

func TestFormOverlayNeverShowsAMaskedValue(t *testing.T) {
	f := newSampleForm(t, i18n.English, withModal)
	f.Focus("token")
	typeForm(f, "hunter2")
	view, _, ok := f.Overlay(looks(t)["noneascii"], sampleScreen(100, 40), 100, 40, Rect{Width: 100, Height: 40})
	if !ok {
		t.Fatal("does not fit")
	}
	if strings.Contains(view, "hunter2") {
		t.Errorf("the modal shows the secret:\n%s", view)
	}
}

func TestFormOverlayFallsBackToViewAndHints(t *testing.T) {
	f := newSampleForm(t, i18n.English, withModal)
	if _, _, ok := f.Overlay(looks(t)["noneascii"], sampleScreen(40, 8), 40, 8, Rect{Width: 40, Height: 8}); ok {
		t.Fatal("the form fits 40x8")
	}
	view, _ := f.View(looks(t)["noneascii"], 40, 8)
	if len(strings.Split(view, "\n")) != 8 || len(f.Hints()) == 0 {
		t.Error("View or Hints give nothing for the fallback")
	}
}

func TestGoldenFormModal(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		w, h       int
	}{
		{"formmodal.narrow.golden", "noneascii", 50, 50},
		{"formmodal.wide.golden", "noneascii", 120, 30},
		{"formmodal.wide.dark.golden", "dark", 120, 30},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSampleForm(t, i18n.English, withModal)
			out, _, ok := f.Overlay(looks(t)[tt.look], sampleScreen(tt.w, tt.h), tt.w, tt.h, Rect{Y: 2, Width: tt.w, Height: tt.h - 2})
			if !ok {
				t.Fatal("does not fit")
			}
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

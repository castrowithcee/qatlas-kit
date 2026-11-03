package tui

import (
	"strconv"
	"strings"
	"testing"

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

package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// focusCase puts the focus of a sample form somewhere and names what then shows only with the focus.
type focusCase struct {
	name  string
	w, h  int
	edit  func(*FormSpec)
	setup func(*Form)
	signs []string
}

func formFocusCases() []focusCase {
	return []focusCase{
		{"text field with hint", 80, 30, nil, func(f *Form) { f.Focus("name") },
			[]string{"> name", "A short name of the plant."}},
		{"section list", 120, 30, nil, func(f *Form) { f.Focus("token") }, []string{"> 2 Care", "> feeder token"}},
		{"flat narrow", 30, 30, nil, func(f *Form) { f.Focus("name") }, []string{"> name"}},
		{"tick cursor", 80, 30, nil, func(f *Form) { f.Focus("water") }, []string{"> watering", ">[ ] Monday"}},
		{"key capture", 80, 30, nil, func(f *Form) {
			f.Focus("key")
			formKeys(f, "enter")
		}, []string{"> shortcut", "press the new key"}},
		{"button", 80, 20, withButtons, func(f *Form) {
			f.Focus("notes")
			formKeys(f, "tab")
		}, []string{"> F2 save"}},
	}
}

func newFocusForm(t *testing.T, c focusCase) *Form {
	t.Helper()
	f := newSampleForm(t, i18n.English, c.edit)
	c.setup(f)
	return f
}

// Without the focus the form draws no marker, frame, accent, cursor or hint of the focus, in colour as
// without, and with it again exactly what a form that never lost it draws.
func TestFormWithoutFocusDrawsNoFocus(t *testing.T) {
	for _, c := range formFocusCases() {
		for _, look := range []string{"dark", "mono", "noneascii"} {
			st := looks(t)[look]
			name := c.name + "/" + look
			fresh := newFocusForm(t, c)
			want, _ := fresh.View(st, c.w, c.h)
			for _, s := range c.signs {
				if look != "noneascii" && !strings.Contains(ansi.Strip(want), s) {
					t.Fatalf("%s: the focused form lacks %q:\n%s", name, s, ansi.Strip(want))
				}
			}
			f := newFocusForm(t, c)
			f.SetFocused(false)
			view, _ := f.View(st, c.w, c.h)
			plain := ansi.Strip(view)
			for _, s := range append([]string{"> ", ">[", "A short name of the plant.", "press the new key"}, c.signs...) {
				if strings.Contains(plain, s) {
					t.Errorf("%s: %q without the focus:\n%s", name, s, plain)
				}
			}
			// The focused frame without colour is double; the unfocused one is single.
			if strings.ContainsAny(plain, "═║╔╗╚╝") {
				t.Errorf("%s: a focused frame without the focus:\n%s", name, plain)
			}
			if look == "noneascii" && strings.Contains(plain, "basil_") {
				t.Errorf("%s: the cursor without the focus:\n%s", name, plain)
			}
			if look == "dark" {
				for _, seq := range []string{openingOf(st.Style(StyleActive)), openingOf(st.Style(StyleActiveRow)), "\x1b[7m"} {
					if seq != "" && strings.Contains(view, seq) {
						t.Errorf("%s: the focus sequence %q without the focus", name, seq)
					}
				}
			}
			f.SetFocused(true)
			if got, _ := f.View(st, c.w, c.h); got != want {
				t.Errorf("%s: with the focus back the form differs from a new one:\n%s\nwant\n%s", name, ansi.Strip(got), ansi.Strip(want))
			}
		}
	}
}

// The modal always draws the focus, whatever SetFocused says.
func TestFormOverlayDrawsTheFocusWithoutIt(t *testing.T) {
	st := looks(t)["dark"]
	screen := strings.TrimSuffix(strings.Repeat(strings.Repeat(".", 120)+"\n", 40), "\n")
	room := Rect{Width: 120, Height: 40}
	f := newSampleForm(t, i18n.English, withButtons)
	f.Focus("name")
	want, wantTs, ok := f.Overlay(st, screen, 120, 40, room)
	if !ok || !strings.Contains(ansi.Strip(want), "> name") {
		t.Fatalf("the modal shows no focus:\n%s", ansi.Strip(want))
	}
	f.SetFocused(false)
	got, ts, _ := f.Overlay(st, screen, 120, 40, room)
	if got != want || len(ts) != len(wantTs) {
		t.Errorf("the modal without the focus differs:\n%s", ansi.Strip(got))
	}
	if view, _ := f.View(st, 80, 30); strings.Contains(ansi.Strip(view), "> name") {
		t.Errorf("the modal gave the view its focus back:\n%s", ansi.Strip(view))
	}
}

// SetFocused only draws: keys, values, hints, the focused field and the button go as with the focus.
func TestFormFocusChangesNoInput(t *testing.T) {
	keys := []string{"tab", "down", "space", "tab", "tab", "tab", "enter", "down", "enter", "shift+tab", "f2", "esc"}
	a := newSampleForm(t, i18n.English, withButtons)
	b := newSampleForm(t, i18n.English, withButtons)
	b.SetFocused(false)
	for i, k := range keys {
		if i == 4 {
			b.SetFocused(true)
			b.SetFocused(false)
		}
		actA, okA := a.Update(keyOf(k))
		actB, okB := b.Update(keyOf(k))
		if actA != actB || okA != okB {
			t.Fatalf("key %q: %q %v with the focus, %q %v without", k, actA, okA, actB, okB)
		}
		if a.Focused() != b.Focused() || focusPlace(a) != focusPlace(b) || a.Open() != b.Open() {
			t.Fatalf("key %q: focus %s, without it %s", k, focusPlace(a), focusPlace(b))
		}
		if !valuesEqual(a.Values(), b.Values()) {
			t.Fatalf("key %q: values %v, without the focus %v", k, a.Values(), b.Values())
		}
		if a.Input() != b.Input() || !reflect.DeepEqual(a.Hints(), b.Hints()) {
			t.Fatalf("key %q: input %v and hints %v, without the focus %v and %v", k, a.Input(), a.Hints(), b.Input(), b.Hints())
		}
	}
}

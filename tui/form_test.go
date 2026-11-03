package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

var sampleFormKeys = FormKeys{Up: "up", Down: "down", Next: "next", Prev: "prev", PageUp: "pageup", PageDown: "pagedown",
	Left: "left", Right: "right", First: "first", Last: "last", Enter: "enter", Toggle: "toggle", Save: "save", Back: "back"}

func formCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {
			"sec.general": "General", "sec.care": "Care", "sec.extra": "Extra", "lead.care": "How the plant is kept.",
			"f.name": "name", "f.kind": "kind", "f.water": "watering", "f.token": "feeder token", "f.notes": "notes",
			"f.key": "shortcut", "f.origin": "origin", "f.soil": "soil",
			"h.name": "A short name of the plant.", "h.origin": "set by the nursery",
			"c.herb": "herb", "c.tree": "tree", "c.fern": "fern", "c.mon": "Monday", "c.wed": "Wednesday", "c.fri": "Friday",
		},
		i18n.German: {
			"sec.general": "Allgemein", "sec.care": "Pflege", "sec.extra": "Erweitert", "lead.care": "Wie die Pflanze gehalten wird.",
			"f.name": "Name", "f.kind": "Art", "f.water": "Gießen", "f.token": "Futtertoken", "f.notes": "Notizen",
			"f.key": "Kürzel", "f.origin": "Herkunft", "f.soil": "Erde",
			"h.name": "Ein kurzer Name der Pflanze.", "h.origin": "von der Gärtnerei gesetzt",
			"c.herb": "Kraut", "c.tree": "Baum", "c.fern": "Farn", "c.mon": "Montag", "c.wed": "Mittwoch", "c.fri": "Freitag",
		},
	}
}

// sampleFormSpec is an invented plant form with every field kind and an advanced section.
func sampleFormSpec() FormSpec {
	return FormSpec{
		Sections: []FormSection{
			{ID: "general", Title: "sec.general"},
			{ID: "care", Title: "sec.care", Lead: "lead.care"},
			{ID: "extra", Title: "sec.extra", Advanced: true},
		},
		Fields: []FormField{
			{ID: "name", Section: "general", Kind: FieldText, Label: "f.name", Value: FormValue{Text: "basil"}, Hint: "h.name"},
			{ID: "kind", Section: "general", Kind: FieldChoice, Label: "f.kind", Choices: []Choice{
				{ID: "herb", Label: "c.herb"}, {ID: "tree", Label: "c.tree"}, {ID: "fern", Label: "c.fern"}}},
			{ID: "origin", Section: "general", Kind: FieldReadOnly, Label: "f.origin", Value: FormValue{Text: "greenhouse"}, Hint: "h.origin"},
			{ID: "water", Section: "care", Kind: FieldMultiple, Label: "f.water", Value: FormValue{Selected: []ChoiceID{"wed"}},
				Choices: []Choice{{ID: "mon", Label: "c.mon"}, {ID: "wed", Label: "c.wed"}, {ID: "fri", Label: "c.fri"}}},
			{ID: "token", Section: "care", Kind: FieldMasked, Label: "f.token", Stored: true},
			{ID: "notes", Section: "care", Kind: FieldList, Label: "f.notes", Value: FormValue{Text: "2 notes"}},
			{ID: "key", Section: "extra", Kind: FieldKey, Label: "f.key"},
			{ID: "soil", Section: "extra", Kind: FieldText, Label: "f.soil"},
		},
		Keys: sampleFormKeys,
	}
}

func newSampleForm(t *testing.T, lang i18n.Lang, edit func(*FormSpec)) *Form {
	t.Helper()
	keys := sampleKeymap(t, CharsetUnicode)
	spec := sampleFormSpec()
	if edit != nil {
		edit(&spec)
	}
	f, err := NewForm(spec, keys, newTranslator(t, lang, formCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func formKeys(f *Form, names ...string) (act ActionID, handled bool) {
	for _, n := range names {
		act, handled = f.Update(keyOf(n))
	}
	return act, handled
}

func typeForm(f *Form, text string) {
	for _, r := range text {
		f.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestNewFormRejectsBrokenSpecs(t *testing.T) {
	field := func(i int, edit func(*FormField)) func(*FormSpec) {
		return func(s *FormSpec) { edit(&s.Fields[i]) }
	}
	cases := []struct {
		name string
		edit func(*FormSpec)
		want string
	}{
		{"no field", func(s *FormSpec) { s.Fields, s.Sections = nil, nil }, "no field"},
		{"only read-only", func(s *FormSpec) {
			s.Fields, s.Sections = []FormField{{ID: "a", Kind: FieldReadOnly, Label: "f.origin"}}, nil
		}, "takes input"},
		{"empty ID", field(0, func(f *FormField) { f.ID = "" }), "an ID is not empty"},
		{"control ID", field(0, func(f *FormField) { f.ID = "a\x00b" }), "an ID is not empty"},
		{"twice", field(1, func(f *FormField) { f.ID = "name" }), "listed twice"},
		{"unknown kind", field(0, func(f *FormField) { f.Kind = "slider" }), "unknown kind"},
		{"no label", field(0, func(f *FormField) { f.Label = "" }), "no label"},
		{"masked with value", field(4, func(f *FormField) { f.Value.Text = "kept-secret" }), "masked field is handed no value"},
		{"stored text", field(0, func(f *FormField) { f.Stored = true }), "only a masked field is stored"},
		{"control text", field(0, func(f *FormField) { f.Value.Text = "a\nb" }), "control character"},
		{"choice without choices", field(1, func(f *FormField) { f.Choices = nil }), "has choices"},
		{"choices on text", field(0, func(f *FormField) { f.Choices = []Choice{{ID: "x", Label: "c.herb"}} }), "only a choice field"},
		{"text on choice", field(1, func(f *FormField) { f.Value.Text = "herb" }), "holds no text"},
		{"unknown choice", field(1, func(f *FormField) { f.Value.Choice = "moss" }), "no choice of the field"},
		{"choice twice", field(1, func(f *FormField) { f.Choices[1].ID = "herb" }), `choice "herb" is listed twice`},
		{"choice without label", field(1, func(f *FormField) { f.Choices[0].Label = "" }), "has no label"},
		{"selected unknown", field(3, func(f *FormField) { f.Value.Selected = []ChoiceID{"sun"} }), "no choice of the field"},
		{"selected twice", field(3, func(f *FormField) { f.Value.Selected = []ChoiceID{"wed", "wed"} }), "listed twice"},
		{"selected on choice", field(1, func(f *FormField) { f.Value.Selected = []ChoiceID{"herb"} }), "only a multiple"},
		{"undeclared section", field(0, func(f *FormField) { f.Section = "roots" }), "not declared"},
		{"no section", field(0, func(f *FormField) { f.Section = "" }), "every field stands in a declared section"},
		{"empty section", func(s *FormSpec) { s.Sections = append(s.Sections, FormSection{ID: "roots", Title: "sec.extra"}) }, "holds no field"},
		{"section twice", func(s *FormSpec) { s.Sections[1].ID = "general" }, "listed twice"},
		{"section title", func(s *FormSpec) { s.Sections[0].Title = "" }, "has no title"},
		{"unknown key", func(s *FormSpec) { s.Keys.Save = "store" }, `"store" is no action`},
		{"shared key", func(s *FormSpec) { s.Keys.Toggle = "enter" }, "are both"},
	}
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, formCatalogs(), keys)
	for _, c := range cases {
		spec := sampleFormSpec()
		c.edit(&spec)
		if _, err := NewForm(spec, keys, text); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	if _, err := NewForm(sampleFormSpec(), nil, text); err == nil {
		t.Error("a form without key table was accepted")
	}
	f := newSampleForm(t, i18n.English, nil)
	if err := f.SetValue("token", FormValue{Text: "kept-secret"}); err == nil {
		t.Error("SetValue handed a masked field a value")
	}
	if err := f.SetValue("token", FormValue{}); err != nil {
		t.Errorf("SetValue cannot empty a masked field: %v", err)
	}
	if err := f.SetValue("kind", FormValue{Choice: "moss"}); err == nil || f.Values()["kind"].Choice != "herb" {
		t.Error("SetValue took a value NewForm refuses")
	}
	if err := f.SetValue("gone", FormValue{}); err == nil {
		t.Error("SetValue took an unknown field")
	}
}

func TestFormOpensOnTheFirstFieldInSectionOrder(t *testing.T) {
	f := newSampleForm(t, i18n.English, func(s *FormSpec) {
		s.Fields[0], s.Fields[6] = s.Fields[6], s.Fields[0]
	})
	var order []string
	for _, field := range f.fields {
		order = append(order, string(field.ID))
	}
	if got := strings.Join(order, ","); got != "kind,origin,name,water,token,notes,key,soil" {
		t.Errorf("fields stand as %s, want them in the order of their sections", got)
	}
	if f.Focused() != "kind" || f.Input() != InputNavigation {
		t.Errorf("focus %s in %v, want the kind as a choice", f.Focused(), f.Input())
	}
}

// F2 saves from every field and enter never does: on a text it goes on, on a choice it opens the values.
func TestSaveFromEveryFieldAndEnterNeverSaves(t *testing.T) {
	f := newSampleForm(t, i18n.English, func(s *FormSpec) { s.Sections[2].Advanced = false })
	for range f.fields {
		if act, handled := f.Update(press("f2")); act != "save" || !handled {
			t.Errorf("F2 on %s gave %q, %v", f.Focused(), act, handled)
		}
		f.Act("next")
	}
	f = newSampleForm(t, i18n.English, nil)
	if act, _ := formKeys(f, "enter"); act != "" || f.Focused() != "kind" {
		t.Errorf("enter on the name gave %q and focus %s", act, f.Focused())
	}
	if act, _ := formKeys(f, "enter"); act != "" || !f.Open() {
		t.Error("enter on a choice does not open its values")
	}
	if act, _ := formKeys(f, "enter"); act != "" || f.Open() {
		t.Error("enter in the open values does not take one")
	}
	f.Focus("notes")
	if act, handled := formKeys(f, "enter"); act != "enter" || !handled {
		t.Errorf("enter on a list gave %q; the application opens its editor", act)
	}
}

func TestFormTabCyclesThroughTheFields(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	if _, handled := formKeys(f, "up"); handled {
		t.Error("up on the first field is the application's, for the zone above")
	}
	if _, handled := f.Update(press("shift+tab")); !handled || f.Focused() != "notes" {
		t.Errorf("shift+tab on the first field gave focus %q; it wraps to the last field", f.Focused())
	}
	var seen []string
	for range 6 {
		seen = append(seen, string(f.Focused()))
		if _, handled := f.Update(press("tab")); !handled {
			t.Fatal("tab is always taken")
		}
	}
	if got := strings.Join(seen, ","); got != "notes,name,kind,water,token,notes" {
		t.Errorf("tab walks %s; it wraps from the last field to the first and steps over the read-only field and the closed section", got)
	}
	for _, want := range []FieldID{"notes", "token", "water", "kind", "name"} {
		if _, handled := f.Update(press("shift+tab")); !handled || f.Focused() != want {
			t.Fatalf("shift+tab gave focus %q, want %q", f.Focused(), want)
		}
	}
	f.Focus("notes")
	if _, handled := formKeys(f, "down"); !handled || f.Focused() != "notes" {
		t.Error("down on the last field stays")
	}
	if _, handled := f.Update(press("esc")); handled {
		t.Error("back is the application's, which asks before it leaves a changed form")
	}
	if _, handled := f.Update(press("q")); handled {
		t.Error("a letter the form does not read is taken")
	}
}

func TestFormTabWrapsOverAnOpenSection(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	f.Focus("key")
	if _, handled := f.Update(press("tab")); !handled || f.Focused() != "soil" {
		t.Fatalf("tab in the open section gave %q", f.Focused())
	}
	if _, handled := f.Update(press("tab")); !handled || f.Focused() != "name" {
		t.Errorf("tab on the last field gave %q; it wraps to the first", f.Focused())
	}
}

func TestFormWithOneFieldKeepsTheFocus(t *testing.T) {
	f := newSampleForm(t, i18n.English, func(s *FormSpec) {
		s.Sections, s.Fields = nil, []FormField{
			{ID: "name", Kind: FieldText, Label: "f.name"}, {ID: "origin", Kind: FieldReadOnly, Label: "f.origin"}}
	})
	for _, k := range []string{"tab", "shift+tab"} {
		if _, handled := f.Update(press(k)); !handled || f.Focused() != "name" {
			t.Errorf("%s gave focus %q handled %v; the only field keeps the focus", k, f.Focused(), handled)
		}
	}
}

func TestFormTabChangesNothingWhileAListCalendarOrKeyTakesTheKeys(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	f.Focus("kind")
	f.Update(press("enter"))
	if !f.open {
		t.Fatal("enter does not open the list")
	}
	for _, k := range []string{"tab", "shift+tab"} {
		if _, handled := f.Update(press(k)); !handled || f.Focused() != "kind" || !f.open {
			t.Errorf("%s with an open list: focus %q open %v handled %v", k, f.Focused(), f.open, handled)
		}
	}
	f.Focus("key")
	f.Update(press("enter"))
	if !f.capture {
		t.Fatal("enter does not start taking a key")
	}
	f.Update(press("tab"))
	if f.Focused() != "key" || f.capture {
		t.Errorf("a key taken while capturing: focus %q capture %v", f.Focused(), f.capture)
	}
	d := newDateForm(t, i18n.English, nil)
	d.Focus("until")
	d.Update(press("enter"))
	if !d.cal {
		t.Fatal("enter does not open the calendar")
	}
	for _, k := range []string{"tab", "shift+tab"} {
		if _, handled := d.Update(press(k)); d.Focused() != "until" || !d.cal {
			t.Errorf("%s with an open calendar: focus %q cal %v handled %v", k, d.Focused(), d.cal, handled)
		}
	}
}

func TestTextFieldEditsAndScrollsWithoutCutting(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	formKeys(f, "home")
	typeForm(f, "sweet ")
	formKeys(f, "right", "backspace")
	f.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	formKeys(f, "end")
	typeForm(f, "!")
	if got := f.Values()["name"].Text; got != "sweet sil!" {
		t.Fatalf("the edits give %q", got)
	}
	f.Paste("\x1b[31mred\nleaf")
	long := f.Values()["name"].Text
	if long != "sweet sil![31mredleaf" {
		t.Errorf("a paste drops control characters only: %q", long)
	}
	view, _ := f.View(Styles{}, 26, 12)
	row := ""
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "> name") {
			row = line
		}
	}
	if !strings.Contains(row, "redleaf") || strings.Contains(row, "sweet") {
		t.Errorf("the focused text does not scroll to its cursor: %q", row)
	}
	formKeys(f, "tab")
	view, _ = f.View(Styles{}, 26, 14)
	if plain := strings.Join(strings.Fields(view), ""); !strings.Contains(plain, "sweetsil![31mredleaf") {
		t.Errorf("an unfocused long text is cut:\n%s", view)
	}
}

func TestChoiceStepsInPlaceAndOpensItsValues(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	f.Focus("kind")
	formKeys(f, "left")
	if got := f.Values()["kind"].Choice; got != "fern" {
		t.Errorf("left from the first value wraps to %q", got)
	}
	formKeys(f, "right", "enter", "down")
	if !f.Open() || f.Input() != InputNavigation {
		t.Fatal("enter does not open the values")
	}
	if _, handled := f.Update(press("q")); handled {
		t.Error("the open values take a key that is no form key")
	}
	formKeys(f, "esc")
	if f.Open() || f.Values()["kind"].Choice != "herb" {
		t.Errorf("back changed the value to %q", f.Values()["kind"].Choice)
	}
	formKeys(f, "enter", "down", "down", "down", "enter")
	if got := f.Values()["kind"].Choice; got != "fern" {
		t.Errorf("the list stops at its end and takes %q", got)
	}
	view, _ := f.View(Styles{}, 60, 20)
	if !strings.Contains(view, "fern") || !strings.Contains(view, "v") {
		t.Errorf("a choice shows its value and the arrow:\n%s", view)
	}
}

func TestMultipleChoiceTicksTheValueUnderTheCursor(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	f.Focus("water")
	formKeys(f, "space", "right", "right", "space", "left", "space")
	if got := f.Values()["water"].Selected; !slices.Equal(got, []ChoiceID{"mon", "fri"}) {
		t.Errorf("ticked %v, want mon and fri in the order of the choices", got)
	}
	if _, handled := formKeys(f, "enter"); !handled || f.Focused() != "token" {
		t.Error("enter on a multiple choice does not go on")
	}
	view, _ := f.View(Styles{}, 70, 20)
	if !strings.Contains(view, "[x] Monday") || !strings.Contains(view, "[ ] Wednesday") {
		t.Errorf("tick boxes are missing:\n%s", view)
	}
}

func TestKeyFieldTakesTheNextKey(t *testing.T) {
	f := newSampleForm(t, i18n.English, func(s *FormSpec) { s.Sections[2].Advanced = false })
	f.Focus("key")
	formKeys(f, "enter")
	if !f.Capturing() || f.Input() != InputKeyCapture {
		t.Fatal("enter does not start taking a key")
	}
	if view, _ := f.View(Styles{}, 70, 30); !strings.Contains(view, "press the new key") {
		t.Errorf("the field does not ask for the key:\n%s", view)
	}
	f.Update(press("f3"))
	if got := f.Values()["key"].Text; got != "f3" || f.Capturing() {
		t.Errorf("the key field holds %q", got)
	}
	formKeys(f, "enter", "esc")
	if got := f.Values()["key"].Text; got != "f3" || f.Capturing() {
		t.Errorf("back while taking a key changed it to %q", got)
	}
	formKeys(f, "enter")
	if _, handled := f.Update(press("ctrl+c")); handled || f.Values()["key"].Text != "f3" {
		t.Error("ctrl+c while taking a key is not left to the application")
	}
}

// An advanced section stays closed while it holds nothing; a jump or a click into it opens it, and a value or
// a problem keeps it open.
func TestAdvancedSectionStaysClosedUntilItHoldsSomething(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	view, _ := f.View(Styles{}, 70, 30)
	if !strings.Contains(view, "Extra (closed)") || strings.Contains(view, "shortcut") {
		t.Errorf("the advanced section is not closed:\n%s", view)
	}
	formKeys(f, "pgdown", "pgdown")
	if f.Focused() != "key" {
		t.Fatalf("page down leads to %s", f.Focused())
	}
	if view, _ := f.View(Styles{}, 70, 30); strings.Contains(view, "(closed)") || !strings.Contains(view, "shortcut") {
		t.Errorf("the section the focus jumped into stays closed:\n%s", view)
	}
	g := newSampleForm(t, i18n.English, nil)
	g.SetProblems([]FormProblem{{Field: "soil", Cause: "the soil is unknown", NextStep: "name a soil"}})
	if g.Focused() != "soil" {
		t.Errorf("a problem in a closed section does not take the focus there: %s", g.Focused())
	}
	if view, _ := g.View(Styles{}, 70, 30); !strings.Contains(view, "Extra [!]") {
		t.Errorf("the section with a problem is not marked:\n%s", view)
	}
	h := newSampleForm(t, i18n.English, func(s *FormSpec) { s.Fields[7].Value.Text = "loam" })
	if view, _ := h.View(Styles{}, 70, 30); strings.Contains(view, "(closed)") {
		t.Errorf("a section with a value is closed:\n%s", view)
	}
}

func TestProblemsStandUnderTheirFieldUntilItChanges(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	f.Focus("kind")
	formKeys(f, "right")
	f.SetProblems([]FormProblem{
		{Field: "name", Cause: "the name is taken", NextStep: "choose another"},
		{Cause: "the garden file is newer", NextStep: "reload"},
	})
	if f.Focused() != "name" || f.Values()["kind"].Choice != "tree" {
		t.Errorf("a refused save moved the focus to %s or dropped a value", f.Focused())
	}
	view, _ := f.View(Styles{}, 70, 30)
	lines := strings.Split(view, "\n")
	if !strings.HasPrefix(lines[0], "[!] the garden file is newer - reload") {
		t.Errorf("a problem without a field does not stand above the form:\n%s", view)
	}
	under := -1
	for i, l := range lines {
		if strings.Contains(l, "[!] the name is taken - choose another") {
			under = i
		}
	}
	if under < 0 || !strings.Contains(strings.Join(lines[:under], "\n"), "name") {
		t.Errorf("the problem does not stand under its field:\n%s", view)
	}
	typeForm(f, "x")
	if view, _ := f.View(Styles{}, 70, 30); strings.Contains(view, "name is taken") || !strings.Contains(view, "garden file") {
		t.Errorf("changing the field does not retire its problem alone:\n%s", view)
	}
}

func TestChangedFollowsTheValues(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	if f.Changed() {
		t.Fatal("a fresh form is changed")
	}
	f.Focus("kind")
	formKeys(f, "right", "left")
	if f.Changed() {
		t.Error("a value changed and changed back counts")
	}
	f.Focus("token")
	typeForm(f, "x")
	if !f.Changed() {
		t.Error("typing a secret is no change")
	}
	values := f.Values()
	values["water"].Selected[0] = "mon"
	if f.Values()["water"].Selected[0] != "wed" {
		t.Error("Values is no copy")
	}
}

// A masked field never shows what was typed: not focused or unfocused, framed or on one line, beside the
// sections or cut to the focus, in no target and in no problem.
func TestAMaskedFieldNeverShowsItsText(t *testing.T) {
	const canary = "canary-feed-7f3a"
	for name, st := range looks(t) {
		f := newSampleForm(t, i18n.English, nil)
		f.Focus("token")
		typeForm(f, canary)
		f.Paste("more")
		f.SetProblems([]FormProblem{{Field: "token", Cause: "token " + canary + "more is refused", NextStep: "type " + canary + "more"}})
		f.Focus("token")
		for _, size := range []struct{ w, h int }{{120, 40}, {80, 30}, {60, 14}, {40, 6}, {30, 3}} {
			for _, focus := range []FieldID{"token", "name"} {
				f.Focus(focus)
				view, targets := f.View(st, size.w, size.h)
				plain := ansi.Strip(view)
				for _, part := range []string{canary, "feed-7f3a", "canary"} {
					if strings.Contains(plain, part) {
						t.Fatalf("%s %dx%d focus %s shows %q:\n%s", name, size.w, size.h, focus, part, plain)
					}
				}
				for _, tg := range targets {
					if strings.Contains(tg.Item, "canary") {
						t.Fatalf("a target names the secret: %+v", tg)
					}
				}
			}
		}
		if got := f.Values()["token"].Text; got != canary+"more" {
			t.Fatalf("the typed secret is %q", got)
		}
	}
}

func TestStoredSecretShowsKeptAndEmptyKeepsIt(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	view, _ := f.View(Styles{}, 70, 30)
	if !strings.Contains(view, "(stored, unchanged)") {
		t.Errorf("a stored secret does not say it is kept:\n%s", view)
	}
	if got := f.Values()["token"].Text; got != "" {
		t.Errorf("an untouched masked field holds %q", got)
	}
	f.Focus("token")
	typeForm(f, "abc")
	f.Focus("name")
	if view, _ := f.View(Styles{}, 70, 30); !strings.Contains(view, "(entered, hidden)") {
		t.Errorf("a typed secret does not say so:\n%s", view)
	}
}

// The form keeps its frames while the width holds them and scrolls where the height is short: whole framed
// fields around the focus, and the size hint where even the focused one does not fit. The focus is
// always in view and every line keeps the width.
func TestFormLayoutsGiveWayAndKeepTheFocus(t *testing.T) {
	cases := []struct {
		w, h      int
		want, not []string
	}{
		{120, 30, []string{"1 General", "> 2 Care", "How the plant is kept.", "╭"}, []string{"Extra (closed)\n"}},
		{80, 30, []string{"General", "Care", "Extra (closed)", "╭"}, []string{"1 General"}},
		{80, 10, []string{"> feeder token", "╭", "…"}, []string{"General"}},
		{80, 6, []string{"> feeder token", "╭", "…"}, []string{"General"}},
		{80, 4, []string{"Resize terminal"}, []string{"> ", "╭"}},
		{80, 1, []string{"Resize terminal"}, []string{"> ", "╭"}},
		{18, 3, []string{"Resize terminal"}, []string{"> "}},
		{18, 10, []string{"Resize terminal"}, []string{"> "}},
	}
	for _, c := range cases {
		f := newSampleForm(t, i18n.English, nil)
		f.Focus("token")
		view, targets := f.View(looks(t)["dark"], c.w, c.h)
		plain := ansi.Strip(view)
		lines := strings.Split(plain, "\n")
		if len(lines) != c.h {
			t.Errorf("%dx%d has %d lines", c.w, c.h, len(lines))
		}
		for _, l := range lines {
			if lipgloss.Width(l) != c.w {
				t.Errorf("%dx%d line of %d cells: %q", c.w, c.h, lipgloss.Width(l), l)
			}
		}
		for _, w := range c.want {
			if !strings.Contains(plain, w) {
				t.Errorf("%dx%d lacks %q:\n%s", c.w, c.h, w, plain)
			}
		}
		for _, n := range c.not {
			if strings.Contains(plain, n) {
				t.Errorf("%dx%d shows %q:\n%s", c.w, c.h, n, plain)
			}
		}
		if strings.Contains(plain, "Resize") && len(targets) > 0 {
			t.Errorf("the size hint has targets: %+v", targets)
		}
	}
}

// tallFormSpec is fifteen text fields in three sections; field NN has the ID fNN.
func tallFormSpec(label func(i int) string) (FormSpec, i18n.Catalogs[TextID]) {
	en := map[TextID]string{"sec.a": "Section A", "sec.b": "Section B", "sec.c": "Section C"}
	spec := FormSpec{Sections: []FormSection{{ID: "a", Title: "sec.a"}, {ID: "b", Title: "sec.b"}, {ID: "c", Title: "sec.c"}}, Keys: sampleFormKeys}
	for i := range 15 {
		id := TextID(fmt.Sprintf("l.%02d", i))
		en[id] = label(i)
		spec.Fields = append(spec.Fields, FormField{ID: FieldID(fmt.Sprintf("f%02d", i)), Section: SectionID(string(rune('a' + i/5))),
			Kind: FieldText, Label: id, Value: FormValue{Text: fmt.Sprintf("value %02d", i)}})
	}
	return spec, i18n.Catalogs[TextID]{i18n.English: en}
}

func newTallForm(t *testing.T, label func(i int) string) *Form {
	t.Helper()
	keys := sampleKeymap(t, CharsetUnicode)
	spec, cat := tallFormSpec(label)
	f, err := NewForm(spec, keys, newTranslator(t, i18n.English, cat, keys))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func trimmed(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimSpace(l)
	}
	return out
}

func fieldLabel(i int) string { return fmt.Sprintf("Field %02d", i) }

// visibleFields are the indexes of the tall form's fields whose label stands in the lines.
func visibleFields(lines []string) []int {
	var out []int
	for i := range 15 {
		for _, l := range lines {
			if strings.Contains(l, fieldLabel(i)+" ") {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

func TestTallFormKeepsItsFramesAndScrollsWithTheFocus(t *testing.T) {
	st := looks(t)["noneascii"]
	for _, size := range []struct{ w, h int }{{80, 12}, {60, 7}, {99, 9}} {
		for focus := range 15 {
			f := newTallForm(t, fieldLabel)
			f.Focus(FieldID(fmt.Sprintf("f%02d", focus)))
			view, targets := f.View(st, size.w, size.h)
			name := fmt.Sprintf("%dx%d focus %d", size.w, size.h, focus)
			lines := strings.Split(view, "\n")
			if len(lines) != size.h {
				t.Fatalf("%s: %d lines", name, len(lines))
			}
			if !strings.Contains(view, "+--") && !strings.Contains(view, "#==") {
				t.Errorf("%s: the frames are gone:\n%s", name, view)
			}
			seen := visibleFields(lines)
			if len(seen) == 0 || !slices.Contains(seen, focus) || !strings.Contains(view, "> "+fieldLabel(focus)) {
				t.Fatalf("%s: the focus is out of view (%v):\n%s", name, seen, view)
			}
			first, below := strings.TrimSpace(lines[0]), slices.Contains(trimmed(lines[1:]), "~")
			if hasMore := first == "~"; hasMore != (seen[0] > 0) {
				t.Errorf("%s: mark above %v with fields %v:\n%s", name, hasMore, seen, view)
			}
			if hasMore := below; hasMore != (seen[len(seen)-1] < 14) {
				t.Errorf("%s: mark below %v with fields %v:\n%s", name, hasMore, seen, view)
			}
			m := NewMouseMap(targets, true)
			for _, tg := range targets[1:] {
				if tg.Rect.Y < 0 || tg.Rect.Y+tg.Rect.Height > size.h {
					t.Errorf("%s: target %q outside the view", name, tg.Item)
					continue
				}
				if !strings.HasPrefix(tg.Item, formFieldRef) {
					continue
				}
				got := m.Handle(click(tg.Rect.X+1, tg.Rect.Y))
				id := strings.TrimPrefix(tg.Item, formFieldRef)
				var n int
				fmt.Sscanf(id, "f%d", &n)
				if got.Item != tg.Item || !strings.Contains(lines[tg.Rect.Y], fieldLabel(n)) && !strings.Contains(lines[tg.Rect.Y+1], fieldLabel(n)) {
					t.Errorf("%s: the target of %s does not hit its field (%q)", name, id, got.Item)
				}
			}
		}
	}
}

// The panel of the list view scrolls with the focus and keeps its frames and the list of sections.
func TestListViewScrollsInsideItsPanel(t *testing.T) {
	st := looks(t)["noneascii"]
	for focus := range 15 {
		f := newTallForm(t, fieldLabel)
		f.Focus(FieldID(fmt.Sprintf("f%02d", focus)))
		view, targets := f.View(st, 120, 12)
		lines := strings.Split(view, "\n")
		if len(lines) != 12 || !strings.Contains(view, "> 1 Section A") && !strings.Contains(view, "> 2 Section B") && !strings.Contains(view, "> 3 Section C") ||
			!strings.Contains(view, "> "+fieldLabel(focus)) || !strings.Contains(view, "#==") {
			t.Fatalf("focus %d:\n%s", focus, view)
		}
		inPanel := 0
		for _, l := range lines {
			if strings.Contains(l, "~") {
				inPanel++
			}
		}
		seen := visibleFields(lines)
		if want := boolInt(seen[0]%5 > 0) + boolInt(seen[len(seen)-1]%5 < 4); inPanel != want {
			t.Errorf("focus %d: %d marks for fields %v:\n%s", focus, inPanel, seen, view)
		}
		for _, tg := range targets {
			if tg.Rect.Y+tg.Rect.Height > 12 {
				t.Errorf("focus %d: target %q below the view", focus, tg.Item)
			}
		}
	}
}

// The height never takes the frames: a wide terminal too low for a framed field asks for more room, and a
// narrow one still shows flat fields.
func TestLowHeightKeepsFramesOrAsksForRoom(t *testing.T) {
	st := looks(t)["noneascii"]
	f := newTallForm(t, fieldLabel)
	f.Focus("f07")
	wide, targets := f.View(st, 80, 4)
	if !strings.Contains(wide, "Resize") || strings.Contains(wide, "> Field") || len(targets) > 0 {
		t.Errorf("a wide, low terminal:\n%s", wide)
	}
	narrow, _ := f.View(st, 20, 3)
	if strings.Contains(narrow, "Resize") || !strings.Contains(narrow, "> Field 07") || strings.Contains(narrow, "+--") {
		t.Errorf("a narrow, low terminal:\n%s", narrow)
	}
}

// Where the width does not hold the frames, the fields stand flat and only the focused one has a surface.
func TestNarrowFormNeverRunsFieldsTogether(t *testing.T) {
	st := looks(t)["dark"]
	surface := strings.TrimSuffix(strings.TrimPrefix(openingOf(lipgloss.NewStyle().Background(lipgloss.Color(string(st.palette.Surface)))), "\x1b["), "m")
	if surface == "" {
		t.Fatal("the look has no surface colour")
	}
	f := newTallForm(t, fieldLabel)
	f.Focus("f07")
	view, _ := f.View(st, 20, 40)
	plain := ansi.Strip(view)
	if strings.Contains(plain, "╭") || !strings.Contains(plain, "> Field 07") || !strings.Contains(plain, "Field 14") {
		t.Fatalf("the narrow form:\n%s", plain)
	}
	on := 0
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, surface) {
			on++
			if !strings.Contains(ansi.Strip(l), "> Field 07") {
				t.Errorf("a field other than the focused one stands on the surface: %q", ansi.Strip(l))
			}
		}
	}
	if on != 1 {
		t.Errorf("%d lines stand on the surface", on)
	}
}

func TestEachSectionHasItsOwnLabelColumn(t *testing.T) {
	f := newTallForm(t, func(i int) string {
		if i >= 5 && i < 10 {
			return fmt.Sprintf("a much longer label %02d", i)
		}
		return fmt.Sprintf("Fld %02d", i)
	})
	view, _ := f.View(looks(t)["noneascii"], 80, 60)
	column := func(label string) int {
		for _, l := range strings.Split(view, "\n") {
			if strings.Contains(l, label) {
				return strings.Index(l, "|")
			}
		}
		t.Fatalf("no line with %q:\n%s", label, view)
		return -1
	}
	short, long, again := column("Fld 00"), column("a much longer label 05"), column("Fld 12")
	if short >= long || short != again {
		t.Errorf("columns %d, %d, %d:\n%s", short, long, again, view)
	}
}

func TestLongLabelWrapsInItsColumn(t *testing.T) {
	long := "a rather long label of words"
	f := newTallForm(t, func(i int) string {
		if i == 1 {
			return long
		}
		return fmt.Sprintf("Fld %02d", i)
	})
	f.Focus("f01")
	for _, boxed := range []bool{true, false} {
		width := 40
		if !boxed {
			width = 24
		}
		view, targets := f.View(looks(t)["noneascii"], width, 40)
		lines := strings.Split(view, "\n")
		first := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "> a ") || strings.HasPrefix(l, "> a rather") })
		if first < 0 {
			t.Fatalf("no wrapped label:\n%s", view)
		}
		valueAt := func(label string) int {
			for _, l := range lines {
				if strings.Contains(l, label) {
					return strings.Index(l, "value")
				}
			}
			return -1
		}
		if boxed {
			valueAt = func(label string) int {
				for _, l := range lines {
					if strings.Contains(l, label) {
						return strings.Index(l, "|")
					}
				}
				return -1
			}
		}
		if got, want := valueAt("> a"), valueAt("Fld 00"); got != want {
			t.Errorf("boxed=%v: the value of the long label starts at %d, the others at %d:\n%s", boxed, got, want, view)
		}
		text := strings.Join(strings.Fields(strings.Join(lines[first:first+4], " ")), " ")
		for _, word := range strings.Fields(long) {
			if !strings.Contains(text, word) {
				t.Errorf("boxed=%v: the label lost %q:\n%s", boxed, word, view)
			}
		}
		field := slices.IndexFunc(targets, func(tg Target) bool { return tg.Item == formFieldRef+"f01" })
		last := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "words") })
		if r := targets[field].Rect; last < r.Y+1 || last >= r.Y+r.Height {
			t.Errorf("boxed=%v: the target %+v does not cover the last label line %d", boxed, r, last)
		}
	}
}

// Every width and height keeps the lines at the width and the height, and the focus in view or the size hint.
func TestFormSweepKeepsTheFocusInView(t *testing.T) {
	st := looks(t)["mono"]
	forms := []func(t *testing.T) *Form{
		func(t *testing.T) *Form { return newTallForm(t, fieldLabel) },
		func(t *testing.T) *Form { return newSampleForm(t, i18n.English, nil) },
	}
	for fi, mk := range forms {
		f := mk(t)
		for focus := range len(f.fields) {
			if len(f.fields) > 10 && focus%3 != 0 && focus != 14 {
				continue
			}
			for _, w := range []int{20, 21, 27, 34, 48, 62, 76, 90, 99, 100, 118, 160} {
				for h := 3; h <= 40; h++ {
					f.setFocus(focus)
					view, targets := f.View(st, w, h)
					lines := strings.Split(view, "\n")
					if len(lines) != h {
						t.Fatalf("form %d %dx%d focus %d: %d lines", fi, w, h, focus, len(lines))
					}
					for _, l := range lines {
						if lipgloss.Width(l) != w {
							t.Fatalf("form %d %dx%d focus %d: line of %d cells", fi, w, h, focus, lipgloss.Width(l))
						}
					}
					if !strings.Contains(view, "> "+f.label(focus)) && !strings.Contains(view, "Resize") {
						t.Fatalf("form %d %dx%d focus %d is out of view:\n%s", fi, w, h, focus, view)
					}
					if fi == 0 && w >= 40 && !strings.Contains(view, "Resize") && !strings.ContainsAny(view, "│|┃║") {
						t.Fatalf("form %d %dx%d focus %d lost its frames to the height:\n%s", fi, w, h, focus, view)
					}
					for _, tg := range targets {
						if tg.Rect.Y+tg.Rect.Height > h {
							t.Fatalf("form %d %dx%d focus %d: target %q below the view", fi, w, h, focus, tg.Item)
						}
					}
				}
			}
		}
	}
}

func TestFormReadsWithoutColour(t *testing.T) {
	for _, look := range []string{"noneascii", "mono"} {
		st := looks(t)[look]
		f := newSampleForm(t, i18n.English, nil)
		f.Focus("water")
		view, _ := f.View(st, 80, 30)
		noColorSequence(t, look, view)
		plain := ansi.Strip(view)
		if !strings.Contains(plain, "> watering") || !strings.Contains(plain, ">[x] Wednesday") && !strings.Contains(plain, ">[ ] Monday") {
			t.Errorf("%s: focus or cursor does not read without colour:\n%s", look, plain)
		}
	}
}

func TestGermanFormFits(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 30}, {80, 24}, {50, 30}} {
		f := newSampleForm(t, i18n.German, nil)
		view, _ := f.View(looks(t)["dark"], size.w, size.h)
		plain := ansi.Strip(view)
		for _, l := range strings.Split(plain, "\n") {
			if lipgloss.Width(l) != size.w {
				t.Fatalf("%dx%d: line of %d cells", size.w, size.h, lipgloss.Width(l))
			}
		}
		if !strings.Contains(plain, "> Name") || strings.Contains(plain, "(closed)") || !strings.Contains(plain, "(zu)") {
			t.Errorf("%dx%d German form:\n%s", size.w, size.h, plain)
		}
	}
}

func TestWheelMovesTheFocusAndStops(t *testing.T) {
	f := newSampleForm(t, i18n.English, nil)
	f.Wheel(-3)
	if f.Focused() != "name" {
		t.Errorf("the wheel up from the first field leads to %s", f.Focused())
	}
	f.Wheel(9)
	if f.Focused() != "notes" {
		t.Errorf("the wheel stops at %s, want the last field that takes input", f.Focused())
	}
	f.Focus("kind")
	formKeys(f, "enter")
	f.Wheel(5)
	if f.pick != 2 || !f.Open() {
		t.Errorf("the wheel in the open values stops at %d", f.pick)
	}
}

// Every control the form draws has a target, and a click on it does what its key does.
func TestEveryFormControlHasATargetAndClicksAsItsKey(t *testing.T) {
	st := looks(t)["noneascii"]
	for _, size := range []struct{ w, h int }{{120, 30}, {80, 30}, {80, 10}} {
		for _, focus := range []FieldID{"name", "kind", "water"} {
			for _, open := range []bool{false, true} {
				setup := func() *Form {
					f := newSampleForm(t, i18n.English, nil)
					f.Focus(focus)
					if open && focus == "kind" {
						formKeys(f, "enter")
					}
					return f
				}
				f := setup()
				view, targets := f.View(st, size.w, size.h)
				name := fmt.Sprintf("%dx%d %s open=%v", size.w, size.h, focus, open)
				checkFormTargetsCover(t, name, f, view, targets)
				m := NewMouseMap(targets, true)
				for _, tg := range targets[1:] {
					clicked, byKey := setup(), setup()
					got := m.Handle(click(tg.Rect.X, tg.Rect.Y))
					if got.Item != tg.Item && tg.Rect.Width > 0 {
						continue
					}
					clickAct, _ := clicked.Click(got)
					keyAct := pressFor(t, byKey, tg)
					if clickAct != keyAct || clicked.Focused() != byKey.Focused() || clicked.Open() != byKey.Open() ||
						clicked.Open() && clicked.pick != byKey.pick || !valuesEqual(clicked.Values(), byKey.Values()) {
						t.Errorf("%s: a click on %q does not do what its key does", name, tg.Item)
					}
				}
			}
		}
	}
}

// pressFor does with keys what a click on tg stands for: a field takes the focus, and a target with an action
// presses it there.
func pressFor(t *testing.T, f *Form, tg Target) ActionID {
	t.Helper()
	id, rest, _ := strings.Cut(tg.Item[2:], "\x00")
	if f.Open() && tg.Item[:2] != formOptionRef && FieldID(id) != f.Focused() {
		// A click outside the open values closes them, as back does.
		f.Act("back")
	}
	switch tg.Item[:2] {
	case formSectionRef:
		target := slices.IndexFunc(f.sections, func(s FormSection) bool { return string(s.ID) == id })
		for range f.sections {
			at := slices.IndexFunc(f.sections, func(s FormSection) bool { return s.ID == f.fields[f.focus].Section })
			switch {
			case at < target:
				f.Act("pagedown")
			case at > target:
				f.Act("pageup")
			}
		}
		for _, field := range f.fields {
			if string(field.Section) == id && field.Kind != FieldReadOnly {
				// Within its own section the click goes to the first field, where no key leads.
				f.Focus(field.ID)
				break
			}
		}
		return ""
	case formOptionRef:
		for f.pick < slices.IndexFunc(f.fields[f.focus].Choices, func(c Choice) bool { return string(c.ID) == rest }) {
			f.Act("down")
		}
		for f.pick > slices.IndexFunc(f.fields[f.focus].Choices, func(c Choice) bool { return string(c.ID) == rest }) {
			f.Act("up")
		}
	case formTickRef:
		f.Focus(FieldID(id))
		for string(f.fields[f.focus].Choices[f.fields[f.focus].cursor].ID) != rest {
			f.Act("right")
		}
	default:
		if string(f.Focused()) == id && f.Open() && tg.Action == "enter" {
			// A second click on a choice with its values open closes them, as back does.
			f.Act("back")
			return ""
		}
		f.Focus(FieldID(id))
	}
	if tg.Action == "" {
		return ""
	}
	act, _ := f.Act(tg.Action)
	return act
}

func valuesEqual(a, b map[FieldID]FormValue) bool {
	for k, v := range a {
		if !v.equal(b[k]) {
			return false
		}
	}
	return len(a) == len(b)
}

// checkFormTargetsCover checks that every drawn field label, section, tick box and open value has a target over
// its cells.
func checkFormTargetsCover(t *testing.T, name string, f *Form, view string, targets []Target) {
	t.Helper()
	lines := strings.Split(ansi.Strip(view), "\n")
	covered := func(x, y int, prefix string) bool {
		for _, tg := range targets {
			r := tg.Rect
			if strings.HasPrefix(tg.Item, prefix) && x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
				return true
			}
		}
		return false
	}
	at := func(line, text string) int { return ansi.StringWidth(line[:strings.Index(line, text)]) }
	for y, line := range lines {
		for i := range f.fields {
			if label := f.label(i); f.fields[i].Kind != FieldReadOnly && strings.Contains(line, "  "+label+" ") ||
				strings.Contains(line, "> "+label) {
				if !covered(at(line, label), y, formFieldRef) {
					t.Errorf("%s: field %q on line %d has no target", name, label, y)
				}
			}
		}
		for _, box := range []string{"[x]", "[ ]"} {
			for off := 0; strings.Contains(line[off:], box); {
				i := off + strings.Index(line[off:], box)
				if !covered(ansi.StringWidth(line[:i]), y, formTickRef) {
					t.Errorf("%s: tick box on line %d has no target:\n%s", name, y, line)
				}
				off = i + len(box)
			}
		}
		for _, s := range f.sections {
			title := f.text.Lookup(s.Title)
			if strings.HasPrefix(strings.TrimLeft(line, " |>0123456789"), title) && !strings.Contains(line, ":") {
				if !covered(at(line, title), y, formSectionRef) {
					t.Errorf("%s: section %q on line %d has no target", name, title, y)
				}
			}
		}
	}
	if f.Open() {
		options := 0
		for _, tg := range targets {
			if strings.HasPrefix(tg.Item, formOptionRef) {
				options++
			}
		}
		if options != len(f.fields[f.focus].Choices) {
			t.Errorf("%s: %d targets for the open values", name, options)
		}
	}
}

package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// local builds a time in the time zone of the process, as the field reads it, so no test depends on the
// machine's zone. The times stand away from the hours a zone change skips.
func local(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.Local)
}

func dateCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {"f.name": "name", "f.from": "from", "f.until": "until"},
		i18n.German:  {"f.name": "Name", "f.from": "Von", "f.until": "Bis"},
	}
}

func newDateForm(t *testing.T, lang i18n.Lang, edit func(*FormSpec)) *Form {
	t.Helper()
	spec := FormSpec{
		Fields: []FormField{
			{ID: "name", Kind: FieldText, Label: "f.name", Value: FormValue{Text: "report"}},
			{ID: "from", Kind: FieldDate, Label: "f.from", Value: FormValue{Time: local(2026, 3, 14, 0, 0)}},
			{ID: "until", Kind: FieldDate, Label: "f.until", Date: DateField{Time: true}},
		},
		Keys: sampleFormKeys,
	}
	if edit != nil {
		edit(&spec)
	}
	keys := sampleKeymap(t, CharsetUnicode)
	f, err := NewForm(spec, keys, newTranslator(t, lang, dateCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// retype replaces the text of the focused date field with text.
func retype(f *Form, text string) {
	field := &f.fields[f.focus]
	for range len(field.line.runes) {
		f.Update(keyOf("backspace"))
	}
	typeForm(f, text)
}

// setText puts text into the focused date field as one edit, so the value before it stays the last valid one.
func setText(f *Form, text string) {
	f.fields[f.focus].line = newTextLine(text)
	f.changed(f.focus)
}

func TestDateFieldTakesTypedValues(t *testing.T) {
	for _, tt := range []struct {
		name  string
		field FieldID
		text  string
		want  time.Time
	}{
		{"date", "from", "2026-02-28", local(2026, 2, 28, 0, 0)},
		{"leap day", "from", "2028-02-29", local(2028, 2, 29, 0, 0)},
		{"surrounding blanks", "from", " 2026-05-01 ", local(2026, 5, 1, 0, 0)},
		{"date and time", "until", "2026-02-28 17:45", local(2026, 2, 28, 17, 45)},
		{"midnight", "until", "2026-02-28 00:00", local(2026, 2, 28, 0, 0)},
		{"empty", "from", "", time.Time{}},
		{"empty with time", "until", "", time.Time{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newDateForm(t, i18n.English, nil)
			f.Focus(tt.field)
			retype(f, tt.text)
			if got := f.Values()[tt.field].Time; !got.Equal(tt.want) {
				t.Errorf("value %v, want %v", got, tt.want)
			}
			if _, bad := f.dateProblem(f.index(tt.field)); bad {
				t.Error("a valid text shows a problem")
			}
			if view, _ := f.View(Styles{}, 80, 20); strings.Contains(view, "[!]") {
				t.Errorf("a valid text shows a problem:\n%s", view)
			}
			if act, handled := f.Act("save"); act != "save" || !handled {
				t.Errorf("a valid date does not let the form save: %q %v", act, handled)
			}
		})
	}
}

func TestDateFieldRefusesInvalidInputAndKeepsTheLastValue(t *testing.T) {
	min, max := local(2026, 1, 1, 0, 0), local(2026, 12, 31, 0, 0)
	edit := func(s *FormSpec) {
		s.Fields[1].Date = DateField{Min: min, Max: max}
		s.Fields[1].Value = FormValue{Time: local(2026, 6, 1, 0, 0)}
		s.Fields[2].Date = DateField{Time: true, Min: local(2026, 3, 1, 8, 30), Max: local(2026, 3, 1, 18, 0)}
		s.Fields[2].Value = FormValue{Time: local(2026, 3, 1, 12, 0)}
	}
	for _, tt := range []struct {
		name  string
		field FieldID
		text  string
		cause string
		step  string
	}{
		{"wrong format", "from", "14.03.2026", "not a date", "type it as YYYY-MM-DD"},
		{"short", "from", "2026-3-4", "not a date", "type it as YYYY-MM-DD"},
		{"partial", "from", "2026-0", "not a date", "type it as YYYY-MM-DD"},
		{"time missing", "until", "2026-03-01", "not a date", "type it as YYYY-MM-DD HH:MM"},
		{"time on a date", "from", "2026-03-01 10:00", "not a date", "type it as YYYY-MM-DD"},
		{"impossible day", "from", "2026-02-30", "that date or time does not exist", "type it as YYYY-MM-DD"},
		{"impossible month", "from", "2026-13-01", "that date or time does not exist", "type it as YYYY-MM-DD"},
		{"impossible hour", "until", "2026-03-01 24:00", "that date or time does not exist", "type it as YYYY-MM-DD HH:MM"},
		{"before min", "from", "2025-12-31", "too early", "type 2026-01-01 or later"},
		{"after max", "from", "2027-01-01", "too late", "type 2026-12-31 or earlier"},
		{"before min time", "until", "2026-03-01 08:29", "too early", "type 2026-03-01 08:30 or later"},
		{"after max time", "until", "2026-03-01 18:01", "too late", "type 2026-03-01 18:00 or earlier"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newDateForm(t, i18n.English, edit)
			before := f.Values()[tt.field]
			f.Focus(tt.field)
			setText(f, tt.text)
			if got := f.Values()[tt.field]; !got.equal(before) {
				t.Errorf("an invalid input was taken over: %v", got.Time)
			}
			// While the text is typed the field shows no hint; leaving it, or a refused save, does.
			if view, _ := f.View(Styles{}, 100, 20); strings.Contains(view, problemMark) {
				t.Errorf("a hint shows while the field is typed in:\n%s", view)
			}
			if f.Focus("name"); !f.dateShown(f.index(tt.field)) {
				t.Error("the hint does not show after leaving the field")
			}
			f.Focus(tt.field)
			if f.dateShown(f.index(tt.field)) {
				t.Error("the hint shows again on entering the field")
			}
			if act, handled := f.Act("save"); act != "" || !handled || f.Focused() != tt.field {
				t.Errorf("the save key went through with an invalid date: %q %v focus %s", act, handled, f.Focused())
			}
			view, _ := f.View(Styles{}, 100, 20)
			if want := problemMark + " " + tt.cause + " - " + tt.step; !strings.Contains(view, want) {
				t.Errorf("no hint %q under the field:\n%s", want, view)
			}
			// The hint stands under the row of its field.
			lines := strings.Split(view, "\n")
			row := -1
			for i, l := range lines {
				if strings.Contains(l, problemMark) {
					row = i
				}
			}
			if row < 1 || !strings.Contains(strings.Join(lines[:row], "\n"), f.label(f.index(tt.field))) {
				t.Errorf("the hint does not stand under its field:\n%s", view)
			}
			if !f.Changed() {
				t.Error("a form with a text that is no value is not changed")
			}
			setText(f, "")
			if view, _ := f.View(Styles{}, 100, 20); strings.Contains(view, problemMark) {
				t.Errorf("emptying the field leaves the hint:\n%s", view)
			}
		})
	}
}

func TestDateBoundsAreInclusiveAndADateComparesByItsDay(t *testing.T) {
	f := newDateForm(t, i18n.English, func(s *FormSpec) {
		// Bounds with a clock part: a date without time compares by its day.
		s.Fields[1].Date = DateField{Min: local(2026, 1, 1, 15, 0), Max: local(2026, 12, 31, 3, 0)}
		s.Fields[1].Value = FormValue{}
	})
	f.Focus("from")
	for _, text := range []string{"2026-01-01", "2026-12-31"} {
		retype(f, text)
		if got := f.Values()["from"].Time; got.IsZero() || f.fields[f.focus].issue != dateOK {
			t.Errorf("the bound day %s is refused", text)
		}
	}
}

func TestDateValueFollowsTheSpec(t *testing.T) {
	f := newDateForm(t, i18n.English, func(s *FormSpec) {
		// A date without time is cut to its day, a date with time to its minute.
		s.Fields[1].Value = FormValue{Time: time.Date(2026, 3, 14, 17, 45, 59, 7, time.Local)}
		s.Fields[2].Value = FormValue{Time: time.Date(2026, 3, 14, 17, 45, 59, 7, time.UTC)}
	})
	if got := f.Values()["from"].Time; !got.Equal(local(2026, 3, 14, 0, 0)) || got.Location() != time.Local {
		t.Errorf("date value %v", got)
	}
	// A value given in another zone is the same instant, read in the local one and cut to its minute.
	in := time.Date(2026, 3, 14, 17, 45, 59, 7, time.UTC).In(time.Local)
	if got := f.Values()["until"].Time; !got.Equal(local(in.Year(), in.Month(), in.Day(), in.Hour(), in.Minute())) {
		t.Errorf("time value %v", got)
	}
	if f.Changed() {
		t.Error("a form with its opening values is changed")
	}
	if err := f.SetValue("from", FormValue{Time: local(2026, 4, 1, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	f.Focus("from")
	if view, _ := f.View(Styles{}, 80, 20); !strings.Contains(view, "from") {
		t.Fatal("no field")
	}
	if got := f.fields[f.index("from")].line.String(); got != "2026-04-01" {
		t.Errorf("the text of a set value is %q", got)
	}
	if err := f.SetValue("from", FormValue{}); err != nil || f.fields[f.index("from")].line.String() != "" {
		t.Errorf("an empty value is refused or keeps its text: %v", err)
	}
}

func TestNewFormRejectsBrokenDateFields(t *testing.T) {
	for name, edit := range map[string]func(*FormSpec){
		"text": func(s *FormSpec) { s.Fields[1].Value = FormValue{Text: "2026-03-14"} },
		"min after max": func(s *FormSpec) {
			s.Fields[1].Date = DateField{Min: local(2026, 5, 1, 0, 0), Max: local(2026, 4, 1, 0, 0)}
		},
		"below min": func(s *FormSpec) {
			s.Fields[1].Date = DateField{Min: local(2026, 5, 1, 0, 0)}
		},
		"above max": func(s *FormSpec) {
			s.Fields[1].Date = DateField{Max: local(2026, 1, 1, 0, 0)}
		},
		"time on a text":     func(s *FormSpec) { s.Fields[0].Value = FormValue{Time: local(2026, 1, 1, 0, 0)} },
		"settings on a text": func(s *FormSpec) { s.Fields[0].Date = DateField{Time: true} },
		"choice":             func(s *FormSpec) { s.Fields[1].Value = FormValue{Choice: "x"} },
	} {
		keys := sampleKeymap(t, CharsetUnicode)
		spec := FormSpec{Fields: []FormField{
			{ID: "name", Kind: FieldText, Label: "f.name"},
			{ID: "from", Kind: FieldDate, Label: "f.from", Value: FormValue{Time: local(2026, 3, 14, 0, 0)}},
		}, Keys: sampleFormKeys}
		edit(&spec)
		if _, err := NewForm(spec, keys, newTranslator(t, i18n.English, dateCatalogs(), keys)); err == nil {
			t.Errorf("%s: the form was accepted", name)
		}
	}
	f := newDateForm(t, i18n.English, nil)
	if err := f.SetValue("from", FormValue{Text: "x"}); err == nil {
		t.Error("SetValue took a text for a date")
	}
}

func TestDateFieldTypesAndPastes(t *testing.T) {
	f := newDateForm(t, i18n.English, nil)
	f.Focus("until")
	if f.Input() != InputText {
		t.Errorf("input focus %v", f.Input())
	}
	if !f.Paste("2026-07-04 09:15\n") || !f.Values()["until"].Time.Equal(local(2026, 7, 4, 9, 15)) {
		t.Errorf("a paste is not taken: %v", f.Values()["until"].Time)
	}
}

func TestClickOnADateRowFocusesAndTypesNothing(t *testing.T) {
	st := looks(t)["noneascii"]
	f := newDateForm(t, i18n.English, nil)
	view, targets := f.View(st, 80, 20)
	row := -1
	for i, tg := range targets {
		if tg.Item == formFieldRef+"from" {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("no target for the row:\n%s", view)
	}
	before := f.Values()
	m := NewMouseMap(targets, true)
	got := m.Handle(click(targets[row].Rect.X+targets[row].Rect.Width-1, targets[row].Rect.Y))
	if got.Item != formFieldRef+"from" {
		t.Fatalf("the click hits %q", got.Item)
	}
	if act, handled := f.Click(got); act != "" || !handled || f.Focused() != "from" {
		t.Errorf("click: %q %v focus %s", act, handled, f.Focused())
	}
	if !valuesEqual(before, f.Values()) || f.Changed() || f.fields[f.index("from")].line.String() != "2026-03-14" {
		t.Error("a click changed the field")
	}
	if view, _ := f.View(st, 80, 20); strings.Contains(view, "[!]") {
		t.Errorf("a click raised a hint:\n%s", view)
	}
}

func TestCalendarSymbolIsATargetAtTheEndOfTheValue(t *testing.T) {
	for _, name := range []string{"noneascii", "dark"} {
		st := looks(t)[name]
		for _, width := range []int{80, 40} {
			for _, focus := range []FieldID{"name", "from"} {
				f := newDateForm(t, i18n.English, nil)
				f.Focus(focus)
				view, targets := f.View(st, width, 20)
				symbol := st.Glyph(GlyphCalendar)
				seen := 0
				for _, tg := range targets {
					if !strings.HasPrefix(tg.Item, formCalendarRef) {
						continue
					}
					seen++
					line := strings.Split(ansi.Strip(view), "\n")[tg.Rect.Y]
					if got := cellsAt(line, tg.Rect.X, tg.Rect.Width); got != symbol {
						t.Errorf("%s %d: target over %q, want the symbol %q", name, width, got, symbol)
					}
					m := NewMouseMap(targets, true)
					r := m.Handle(click(tg.Rect.X, tg.Rect.Y))
					g := newDateForm(t, i18n.English, nil)
					if act, handled := g.Click(r); act != "" || !handled || g.Focused() != FieldID(strings.TrimPrefix(tg.Item, formCalendarRef)) {
						t.Errorf("symbol click: %q %v focus %s", act, handled, g.Focused())
					}
					if g.Changed() || g.Open() {
						t.Error("the symbol click changed the form")
					}
				}
				if seen != 2 {
					t.Errorf("%s %d focus %s: %d calendar targets, want 2:\n%s", name, width, focus, seen, view)
				}
			}
		}
	}
}

func TestDateTextIsEscapedAndGermanHintsFit(t *testing.T) {
	f := newDateForm(t, i18n.German, nil)
	f.Focus("from")
	retype(f, "2026-02-30")
	f.Focus("name")
	view, _ := f.View(Styles{}, 100, 20)
	if !strings.Contains(view, "[!] dieses Datum oder diese Uhrzeit gibt es nicht - als JJJJ-MM-TT eingeben") {
		t.Errorf("no German hint:\n%s", view)
	}
	f = newDateForm(t, i18n.English, nil)
	f.Focus("from")
	retype(f, "\x1b[31mred")
	typeForm(f, "\x1b")
	if view, _ := f.View(Styles{}, 60, 20); strings.Contains(view, "\x1b") {
		t.Errorf("a control character reached the screen: %q", view)
	}
	if got := f.fields[f.index("from")].line.String(); strings.ContainsRune(got, 0x1b) {
		t.Errorf("a control character was typed: %q", got)
	}
}

func goldenDateForm(t *testing.T) *Form {
	t.Helper()
	return newDateForm(t, i18n.English, func(s *FormSpec) {
		s.Fields[2].Value = FormValue{Time: local(2026, 3, 14, 17, 45)}
		s.Fields = append(s.Fields, FormField{ID: "since", Kind: FieldDate, Label: "f.from"})
	})
}

func TestGoldenDateField(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		w, h       int
	}{
		{"dateform.narrow.golden", "noneascii", 40, 14},
		{"dateform.wide.golden", "noneascii", 120, 14},
		{"dateform.wide.dark.golden", "dark", 120, 14},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := goldenDateForm(t).View(looks(t)[tt.look], tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

// cellsAt is the text of width cells of line from cell x.
func cellsAt(line string, x, width int) string {
	var out strings.Builder
	at := 0
	for _, r := range line {
		w := lipgloss.Width(string(r))
		if at >= x && at < x+width {
			out.WriteRune(r)
		}
		at += w
	}
	return out.String()
}

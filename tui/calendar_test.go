package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// calNow is the invented "today" of the calendar tests, a Saturday.
func calNow() time.Time { return local(2026, 3, 14, 10, 30) }

// newCalForm is a form with a date field "when", a date and time field "at" and a text field, whose clock stands
// at calNow.
func newCalForm(t *testing.T, lang i18n.Lang, edit func(*FormSpec)) *Form {
	t.Helper()
	return newDateForm(t, lang, func(s *FormSpec) {
		s.Fields[1].ID, s.Fields[2].ID = "when", "at"
		s.Fields[1].Value = FormValue{}
		s.Fields[1].Date.Now, s.Fields[2].Date.Now = calNow, calNow
		if edit != nil {
			edit(s)
		}
	})
}

func openCal(t *testing.T, f *Form, id FieldID) {
	t.Helper()
	f.Focus(id)
	if act, handled := f.Update(keyOf("enter")); act != "" || !handled || !f.cal {
		t.Fatalf("enter on %s does not open the calendar: %q %v open %v", id, act, handled, f.cal)
	}
}

// targetOf is the target with item in targets.
func targetOf(t *testing.T, targets []Target, item string) Target {
	t.Helper()
	for _, tg := range targets {
		if tg.Item == item {
			return tg
		}
	}
	t.Fatalf("no target %q", strings.ReplaceAll(item, "\x00", "|"))
	return Target{}
}

func clickTarget(t *testing.T, f *Form, targets []Target, tg Target) (ActionID, bool) {
	t.Helper()
	r := NewMouseMap(targets, true).Handle(click(tg.Rect.X, tg.Rect.Y))
	if r.Item != tg.Item {
		t.Fatalf("a click on %q hits %q", strings.ReplaceAll(tg.Item, "\x00", "|"), strings.ReplaceAll(r.Item, "\x00", "|"))
	}
	return f.Click(r)
}

func dayItem(id FieldID, day string) string { return formDayRef + string(id) + "\x00" + day }

func TestCalendarByKeyAndByClickTakeTheSameDay(t *testing.T) {
	st := looks(t)["noneascii"]
	byKey := newCalForm(t, i18n.English, nil)
	openCal(t, byKey, "when")
	for _, k := range []string{"up", "right", "right"} {
		byKey.Update(keyOf(k))
	}
	if !byKey.calDay.Equal(local(2026, 3, 9, 0, 0)) {
		t.Fatalf("the cursor stands on %v", byKey.calDay)
	}
	byKey.Update(keyOf("enter"))

	byClick := newCalForm(t, i18n.English, nil)
	byClick.Focus("when")
	_, targets := byClick.View(st, 80, 30)
	symbol := targetOf(t, targets, formCalendarRef+"when")
	if _, handled := clickTarget(t, byClick, targets, symbol); !handled || !byClick.cal {
		t.Fatal("a click on the symbol does not open the calendar")
	}
	_, targets = byClick.View(st, 80, 30)
	if act, handled := clickTarget(t, byClick, targets, targetOf(t, targets, dayItem("when", "2026-03-09"))); act != "" || !handled {
		t.Fatalf("click on a day: %q %v", act, handled)
	}
	for name, f := range map[string]*Form{"key": byKey, "click": byClick} {
		if got := f.Values()["when"].Time; !got.Equal(local(2026, 3, 9, 0, 0)) || f.cal {
			t.Errorf("%s: value %v, calendar open %v", name, got, f.cal)
		}
		if got := f.fields[f.index("when")].line.String(); got != "2026-03-09" {
			t.Errorf("%s: the field shows %q", name, got)
		}
	}
	if !valuesEqual(byKey.Values(), byClick.Values()) {
		t.Error("key and click took different values")
	}
}

func TestCalendarOpensOnTheValueOrToday(t *testing.T) {
	f := newCalForm(t, i18n.English, nil)
	openCal(t, f, "when")
	if !f.calDay.Equal(local(2026, 3, 14, 0, 0)) {
		t.Errorf("an empty field opens on %v", f.calDay)
	}
	g := newCalForm(t, i18n.English, func(s *FormSpec) { s.Fields[1].Value = FormValue{Time: local(2025, 12, 24, 0, 0)} })
	openCal(t, g, "when")
	if !g.calDay.Equal(local(2025, 12, 24, 0, 0)) {
		t.Errorf("a field with a value opens on %v", g.calDay)
	}
	g.Update(keyOf("right"))
	g.Update(keyOf("home"))
	if !g.calDay.Equal(local(2026, 3, 14, 0, 0)) {
		t.Errorf("home goes to %v, not today", g.calDay)
	}
}

func TestCalendarEscDiscards(t *testing.T) {
	f := newCalForm(t, i18n.English, func(s *FormSpec) { s.Fields[1].Value = FormValue{Time: local(2026, 3, 1, 0, 0)} })
	openCal(t, f, "when")
	f.Update(keyOf("down"))
	f.Update(keyOf("pgdown"))
	if act, handled := f.Update(keyOf("esc")); act != "" || !handled || f.cal {
		t.Errorf("esc: %q %v open %v", act, handled, f.cal)
	}
	if got := f.Values()["when"].Time; !got.Equal(local(2026, 3, 1, 0, 0)) || f.Changed() {
		t.Errorf("esc changed the value to %v", got)
	}
	// Closed, esc is the application's again.
	if _, handled := f.Update(keyOf("esc")); handled {
		t.Error("the form takes esc without a calendar")
	}
}

func TestCalendarChangesMonthAcrossTheYearBoundary(t *testing.T) {
	st := looks(t)["noneascii"]
	for _, tt := range []struct {
		name         string
		from, to     time.Time
		key, buttonN string
	}{
		{"december to january", local(2026, 12, 15, 0, 0), local(2027, 1, 15, 0, 0), "pgdown", calNext},
		{"january to december", local(2027, 1, 15, 0, 0), local(2026, 12, 15, 0, 0), "pgup", calPrev},
		{"a shorter month", local(2026, 1, 31, 0, 0), local(2026, 2, 28, 0, 0), "pgdown", calNext},
	} {
		t.Run(tt.name, func(t *testing.T) {
			byKey := newCalForm(t, i18n.English, func(s *FormSpec) { s.Fields[1].Value = FormValue{Time: tt.from} })
			openCal(t, byKey, "when")
			byKey.Update(keyOf(tt.key))
			byButton := newCalForm(t, i18n.English, func(s *FormSpec) { s.Fields[1].Value = FormValue{Time: tt.from} })
			openCal(t, byButton, "when")
			_, targets := byButton.View(st, 80, 30)
			btn := targetOf(t, targets, formCalBtnRef+"when\x00"+tt.buttonN)
			if act, handled := clickTarget(t, byButton, targets, btn); act != "" || !handled {
				t.Errorf("button: %q %v", act, handled)
			}
			for name, f := range map[string]*Form{"key": byKey, "button": byButton} {
				if !f.calDay.Equal(tt.to) || !f.cal {
					t.Errorf("%s: the cursor stands on %v, want %v", name, f.calDay, tt.to)
				}
				if got := f.Values()["when"].Time; !got.Equal(tt.from) {
					t.Errorf("%s: moving the month changed the value to %v", name, got)
				}
			}
		})
	}
}

func TestCalendarWeekStartComesFromTheLanguage(t *testing.T) {
	st := looks(t)["noneascii"]
	for _, tt := range []struct {
		lang   i18n.Lang
		header string
		sameRo bool
	}{
		// 2026-03-01 is a Sunday: it opens a week in English and closes one in German.
		{i18n.English, "Su Mo Tu We Th Fr Sa", true},
		{i18n.German, "Mo Di Mi Do Fr Sa So", false},
	} {
		f := newCalForm(t, tt.lang, nil)
		openCal(t, f, "when")
		view, targets := f.View(st, 80, 30)
		if !strings.Contains(view, tt.header) {
			t.Errorf("%s: no header %q:\n%s", tt.lang, tt.header, view)
		}
		first, second := targetOf(t, targets, dayItem("when", "2026-03-01")), targetOf(t, targets, dayItem("when", "2026-03-02"))
		if (first.Rect.Y == second.Rect.Y) != tt.sameRo {
			t.Errorf("%s: the first of the month stands in the wrong column", tt.lang)
		}
	}
}

func TestCalendarDaysOutsideTheBoundsAreNotSelectable(t *testing.T) {
	st := looks(t)["noneascii"]
	f := newCalForm(t, i18n.English, func(s *FormSpec) {
		s.Fields[1].Date.Min, s.Fields[1].Date.Max = local(2026, 3, 10, 0, 0), local(2026, 3, 20, 0, 0)
		s.Fields[1].Value = FormValue{Time: local(2026, 3, 10, 0, 0)}
	})
	openCal(t, f, "when")
	view, targets := f.View(st, 80, 30)
	days := 0
	for _, tg := range targets {
		if strings.HasPrefix(tg.Item, formDayRef) {
			days++
		}
	}
	if days != 11 {
		t.Errorf("%d day targets, want the 11 days from 10 to 20:\n%s", days, view)
	}
	for _, day := range []string{"2026-03-09", "2026-03-21"} {
		for _, tg := range targets {
			if tg.Item == dayItem("when", day) {
				t.Errorf("the day %s outside the bounds has a target", day)
			}
		}
	}
	// A key cannot land outside, a click on a day outside is no click on a target, and enter takes only a day inside.
	f.Update(keyOf("left"))
	f.Update(keyOf("up"))
	f.Update(keyOf("pgup"))
	if !f.calDay.Equal(local(2026, 3, 10, 0, 0)) {
		t.Errorf("a key left the bounds: %v", f.calDay)
	}
	f.Update(keyOf("pgdown"))
	if !f.calDay.Equal(local(2026, 3, 10, 0, 0)) {
		t.Errorf("a month without an allowed day was entered: %v", f.calDay)
	}
	for range 3 {
		f.Update(keyOf("down"))
	}
	if !f.calDay.Equal(local(2026, 3, 17, 0, 0)) {
		t.Errorf("down stands on %v", f.calDay)
	}
	f.Update(keyOf("down"))
	if !f.calDay.Equal(local(2026, 3, 17, 0, 0)) {
		t.Errorf("down left the bounds: %v", f.calDay)
	}
	if f.calendarClick(MouseResult{Handled: true, Item: dayItem("when", "2026-03-25")}); f.Values()["when"].Time.Equal(local(2026, 3, 25, 0, 0)) {
		t.Error("a day outside the bounds was taken")
	}

	// Where today is outside, the calendar opens on the nearest allowed day and home refuses.
	g := newCalForm(t, i18n.English, func(s *FormSpec) {
		s.Fields[1].Date.Min, s.Fields[1].Date.Max = local(2026, 3, 20, 0, 0), local(2026, 3, 25, 0, 0)
	})
	openCal(t, g, "when")
	if !g.calDay.Equal(local(2026, 3, 20, 0, 0)) {
		t.Errorf("opens on %v", g.calDay)
	}
	g.Update(keyOf("right"))
	g.Update(keyOf("home"))
	if !g.calDay.Equal(local(2026, 3, 21, 0, 0)) {
		t.Errorf("home went to a day outside: %v", g.calDay)
	}
}

func TestCalendarMonthMoveClampsIntoTheBounds(t *testing.T) {
	f := newCalForm(t, i18n.English, func(s *FormSpec) {
		s.Fields[1].Date.Min, s.Fields[1].Date.Max = local(2026, 2, 20, 0, 0), local(2026, 3, 20, 0, 0)
		s.Fields[1].Value = FormValue{Time: local(2026, 3, 5, 0, 0)}
	})
	openCal(t, f, "when")
	f.Update(keyOf("pgup"))
	if !f.calDay.Equal(local(2026, 2, 20, 0, 0)) {
		t.Errorf("the month before stands on %v", f.calDay)
	}
}

func TestCalendarBackgroundIsNotClickable(t *testing.T) {
	st := looks(t)["noneascii"]
	f := newCalForm(t, i18n.English, nil)
	openCal(t, f, "when")
	_, targets := f.View(st, 80, 30)
	for _, tg := range targets {
		if strings.HasPrefix(tg.Item, formFieldRef) || strings.HasPrefix(tg.Item, formSectionRef) || strings.HasPrefix(tg.Item, formTickRef) {
			t.Errorf("the background keeps the target %q", tg.Item)
		}
	}
	before := f.Values()
	for _, item := range []string{formFieldRef + "name", formFieldRef + "at", formFieldRef + "when", formCalendarRef + "at", ""} {
		if act, handled := f.Click(MouseResult{Handled: true, Item: item, Zone: ZoneForm}); act != "" || !handled {
			t.Errorf("click on %q: %q %v", item, act, handled)
		}
		if !f.cal || f.Focused() != "when" || !valuesEqual(before, f.Values()) {
			t.Fatalf("a click on %q reached the form under the calendar", item)
		}
	}
	// The symbol of its own field closes it, as back does.
	f.Click(MouseResult{Handled: true, Item: formCalendarRef + "when"})
	if f.cal {
		t.Error("the symbol does not close the calendar")
	}
}

func TestCalendarKeepsTheTimeOfDay(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*FormSpec)
		want time.Time
	}{
		{"empty", nil, local(2026, 3, 15, 0, 0)},
		{"with time", func(s *FormSpec) { s.Fields[2].Value = FormValue{Time: local(2026, 3, 1, 17, 45)} }, local(2026, 3, 15, 17, 45)},
		{"moved into the bounds", func(s *FormSpec) {
			s.Fields[2].Date.Min = local(2026, 3, 15, 8, 30)
			s.Fields[2].Value = FormValue{Time: local(2026, 3, 16, 7, 0)}
		}, local(2026, 3, 15, 8, 30)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newCalForm(t, i18n.English, tt.edit)
			openCal(t, f, "at")
			f.calDay = local(2026, 3, 15, 0, 0)
			f.Update(keyOf("enter"))
			if got := f.Values()["at"].Time; !got.Equal(tt.want) {
				t.Errorf("value %v, want %v", got, tt.want)
			}
			if got := f.fields[f.index("at")].line.String(); got != tt.want.Format(dateTimeLayout) {
				t.Errorf("the field shows %q", got)
			}
		})
	}
}

func TestCalendarRetiresAHintAndSwallowsKeys(t *testing.T) {
	f := newCalForm(t, i18n.English, nil)
	f.Focus("when")
	setText(f, "nonsense")
	f.Act("save")
	if !f.dateShown(f.index("when")) {
		t.Fatal("no hint after a refused save")
	}
	openCal(t, f, "when")
	if f.Input() != InputNavigation {
		t.Error("typing stays on while the calendar is open")
	}
	if act, handled := f.Update(keyOf("f2")); act != "" || !handled {
		t.Errorf("save in the calendar: %q %v", act, handled)
	}
	typeForm(f, "x")
	if f.fields[f.index("when")].line.String() != "nonsense" {
		t.Error("a key typed into the open calendar")
	}
	f.Update(keyOf("enter"))
	if f.dateShown(f.index("when")) || f.fields[f.index("when")].issue != dateOK {
		t.Error("taking a day leaves the problem")
	}
}

func TestCalendarWheelMovesByWeeks(t *testing.T) {
	f := newCalForm(t, i18n.English, nil)
	openCal(t, f, "when")
	f.Wheel(2)
	f.Wheel(-1)
	if !f.calDay.Equal(local(2026, 3, 21, 0, 0)) {
		t.Errorf("the cursor stands on %v", f.calDay)
	}
}

func TestCalendarHints(t *testing.T) {
	f := newCalForm(t, i18n.English, nil)
	f.Focus("when")
	if h := f.Hints(); h[0].Action != "enter" || h[0].Label != TextKeyCalendar {
		t.Errorf("hints of a date field: %v", h)
	}
	openCal(t, f, "when")
	want := map[ActionID]TextID{"enter": TextKeyChoose, "back": TextKeyBack, "pageup": TextCalPrevMonth,
		"pagedown": TextCalNextMonth, "first": TextCalToday}
	for _, h := range f.Hints() {
		if label, ok := want[h.Action]; ok {
			if h.Label != label {
				t.Errorf("hint %s says %s", h.Action, h.Label)
			}
			delete(want, h.Action)
		}
	}
	if len(want) > 0 {
		t.Errorf("hints missing: %v", want)
	}
}

// Every control the open calendar draws has a target over its cells, and a click on it does what its key does.
func TestEveryCalendarControlHasATargetAndClicksAsItsKey(t *testing.T) {
	for _, name := range []string{"noneascii", "dark"} {
		st := looks(t)[name]
		for _, width := range []int{80, 120, 40} {
			f := newCalForm(t, i18n.English, func(s *FormSpec) {
				s.Fields[1].Date.Max = local(2026, 3, 28, 0, 0)
			})
			openCal(t, f, "when")
			view, targets := f.View(st, width, 40)
			lines := strings.Split(ansi.Strip(view), "\n")
			cover := func(tg Target) string { return cellsAt(lines[tg.Rect.Y], tg.Rect.X, tg.Rect.Width) }
			days := 0
			for _, tg := range targets {
				switch {
				case strings.HasPrefix(tg.Item, formDayRef):
					days++
					day := strings.TrimLeft(tg.Item[len(tg.Item)-2:], "0")
					if got := strings.Trim(cover(tg), " >*●"); got != day {
						t.Errorf("%s %d: the target of day %s covers %q", name, width, day, got)
					}
				case strings.HasPrefix(tg.Item, formCalBtnRef):
					key := map[string]ActionID{calPrev: "pageup", calNext: "pagedown", calToday: "first"}[strings.Split(tg.Item, "\x00")[2]]
					if tg.Action != key || !strings.Contains(cover(tg), f.table.Label(key)) {
						t.Errorf("%s %d: button %q covers %q with action %q", name, width, tg.Item, cover(tg), tg.Action)
					}
					g := newCalForm(t, i18n.English, nil)
					openCal(t, g, "when")
					h := newCalForm(t, i18n.English, nil)
					openCal(t, h, "when")
					g.Click(MouseResult{Handled: true, Item: tg.Item})
					h.Act(key)
					if !g.calDay.Equal(h.calDay) || g.cal != h.cal {
						t.Errorf("%s %d: a click on %q does not do what its key does", name, width, tg.Item)
					}
				}
			}
			if days != 28 {
				t.Errorf("%s %d: %d day targets, want 28", name, width, days)
			}
			// The drawn buttons are all there.
			for _, text := range []string{"< pgup", "pgdown >", "today home"} {
				if !strings.Contains(strings.Join(lines, "\n"), text) {
					t.Errorf("%s %d: no button %q:\n%s", name, width, text, view)
				}
			}
		}
	}
}

func TestCalendarFitsEveryWidthAndHeight(t *testing.T) {
	st := looks(t)["noneascii"]
	for width := 20; width <= 70; width += 3 {
		for _, height := range []int{6, 12, 16, 24} {
			for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
				f := newCalForm(t, lang, nil)
				openCal(t, f, "at")
				view, targets := f.View(st, width, height)
				lines := strings.Split(view, "\n")
				if len(lines) != height {
					t.Fatalf("%dx%d: %d lines", width, height, len(lines))
				}
				for _, l := range lines {
					if w := lipgloss.Width(l); w != width {
						t.Fatalf("%dx%d: a line is %d cells wide", width, height, w)
					}
				}
				for _, tg := range targets {
					if tg.Rect.X+tg.Rect.Width > width || tg.Rect.Y >= height {
						t.Fatalf("%dx%d: target %v lies outside the screen", width, height, tg.Rect)
					}
				}
			}
		}
	}
}

func TestCalendarStaysBelowOrAboveTheField(t *testing.T) {
	st := looks(t)["noneascii"]
	f := newCalForm(t, i18n.English, nil)
	openCal(t, f, "when")
	view, _ := f.View(st, 80, 40)
	lines := strings.Split(view, "\n")
	at, title := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "> from") {
			at = i
		}
		if strings.Contains(l, "March 2026") {
			title = i
		}
	}
	if at < 0 || title <= at {
		t.Errorf("the calendar does not stand below the field:\n%s", view)
	}
	// Where the room below is short, it stands above the field and leaves it whole.
	g := newCalForm(t, i18n.English, func(s *FormSpec) {
		var fill []FormField
		for i := range 5 {
			fill = append(fill, FormField{ID: FieldID(fmt.Sprint("p", i)), Kind: FieldText, Label: "f.name"})
		}
		s.Fields = append(fill, s.Fields...)
		s.Fields[len(s.Fields)-1].Value = FormValue{Time: local(2026, 3, 1, 9, 5)}
	})
	openCal(t, g, "at")
	view, _ = g.View(st, 80, 30)
	lines = strings.Split(view, "\n")
	at, title = -1, -1
	for i, l := range lines {
		if strings.Contains(l, "> until") {
			at = i
		}
		if strings.Contains(l, "March 2026") {
			title = i
		}
	}
	if at < 0 || title < 0 || title >= at || !strings.Contains(lines[at], "2026-03-01 09:05") {
		t.Errorf("the calendar does not stand above the field and leave it whole:\n%s", view)
	}
}

func goldenCalendarForm(t *testing.T, lang i18n.Lang) *Form {
	t.Helper()
	f := newCalForm(t, lang, func(s *FormSpec) {
		s.Fields[1].Value = FormValue{Time: local(2026, 3, 9, 0, 0)}
		s.Fields[1].Date.Min, s.Fields[1].Date.Max = local(2026, 3, 3, 0, 0), local(2026, 3, 28, 0, 0)
	})
	f.Focus("when")
	f.Update(keyOf("enter"))
	f.Update(keyOf("right"))
	return f
}

func TestGoldenCalendar(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		lang       i18n.Lang
		w, h       int
	}{
		{"calendar.narrow.golden", "noneascii", i18n.English, 36, 24},
		{"calendar.wide.golden", "noneascii", i18n.English, 120, 24},
		{"calendar.wide.de.golden", "noneascii", i18n.German, 120, 24},
		{"calendar.wide.dark.golden", "dark", i18n.English, 120, 24},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := goldenCalendarForm(t, tt.lang).View(looks(t)[tt.look], tt.w, tt.h)
			for _, l := range strings.Split(out, "\n") {
				if lipgloss.Width(l) > tt.w {
					t.Fatalf("a line is wider than %d", tt.w)
				}
			}
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

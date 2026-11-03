package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// The targets of the open calendar start with a control character, which no ID holds.
const (
	formDayRef    = "\x00D"
	formCalBtnRef = "\x00B"
)

const (
	calCols     = 7
	calRows     = 6
	calCellW    = 3
	calMinInner = calCols * calCellW
	calDayKey   = "2006-01-02"
)

// The buttons of the calendar, each standing for one form key.
const (
	calPrev  = "prev"
	calNext  = "next"
	calToday = "today"
)

// now is the clock of the field in the local time zone: Now where set, time.Now otherwise.
func (d DateField) now() time.Time {
	if d.Now != nil {
		return d.Now().In(time.Local)
	}
	return time.Now()
}

func dayOf(t time.Time) time.Time {
	t = t.In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

func addDays(day time.Time, n int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day()+n, 0, 0, 0, 0, time.Local)
}

// addMonths is the day n months on, cut to the last day of a shorter month.
func addMonths(day time.Time, n int) time.Time {
	first := time.Date(day.Year(), day.Month()+time.Month(n), 1, 0, 0, 0, 0, time.Local)
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	return time.Date(first.Year(), first.Month(), min(day.Day(), last), 0, 0, 0, 0, time.Local)
}

// selectable reports whether a day lies within Min and Max, by its day: with a time of day, a day holding any
// allowed minute is one.
func (d DateField) selectable(day time.Time) bool {
	return (d.Min.IsZero() || !day.Before(dayOf(d.Min))) && (d.Max.IsZero() || !day.After(dayOf(d.Max)))
}

// clampDay is day, or the nearest bound where it lies outside them.
func (d DateField) clampDay(day time.Time) time.Time {
	switch {
	case !d.Min.IsZero() && day.Before(dayOf(d.Min)):
		return dayOf(d.Min)
	case !d.Max.IsZero() && day.After(dayOf(d.Max)):
		return dayOf(d.Max)
	}
	return day
}

// weekStart is the first column of a week, 0 for Sunday to 6 for Saturday, as the catalog of the language
// says; a text that is no weekday number means Monday.
func (f *Form) weekStart() int {
	n, err := strconv.Atoi(strings.TrimSpace(f.text.Lookup(TextCalWeekStart)))
	if err != nil || n < 0 || n > 6 {
		return int(time.Monday)
	}
	return n
}

func (f *Form) monthName(m time.Month) string {
	return f.text.Lookup([...]TextID{TextCalMonthJan, TextCalMonthFeb, TextCalMonthMar, TextCalMonthApr, TextCalMonthMay,
		TextCalMonthJun, TextCalMonthJul, TextCalMonthAug, TextCalMonthSep, TextCalMonthOct, TextCalMonthNov,
		TextCalMonthDec}[m-1])
}

func (f *Form) weekdayName(w time.Weekday) string {
	return f.text.Lookup([...]TextID{TextCalDaySun, TextCalDayMon, TextCalDayTue, TextCalDayWed, TextCalDayThu,
		TextCalDayFri, TextCalDaySat}[w])
}

// openCalendar opens the calendar of the focused date field on its day, or on today where it is empty, moved
// into the bounds.
func (f *Form) openCalendar() {
	field := &f.fields[f.focus]
	day := dayOf(field.Date.now())
	if !field.date.IsZero() {
		day = dayOf(field.date)
	}
	f.cal, f.calDay = true, field.Date.clampDay(day)
	f.open, f.capture = false, false
}

// calendarAct is Act while the calendar is open. It takes every key: a move that would land on a day outside
// the bounds stays where it is, and a month move to a month without an allowed day does too.
func (f *Form) calendarAct(id ActionID) (act ActionID, handled bool) {
	k := f.keys
	field := &f.fields[f.focus]
	d := field.Date
	goTo := func(day time.Time) {
		if d.selectable(day) {
			f.calDay = day
		}
	}
	byMonth := func(n int) {
		day := d.clampDay(addMonths(f.calDay, n))
		want := addMonths(f.calDay, n)
		if day.Year() == want.Year() && day.Month() == want.Month() {
			f.calDay = day
		}
	}
	switch id {
	case k.Back:
		f.cal = false
	case k.Enter:
		f.takeDay(f.calDay)
	case k.Left:
		goTo(addDays(f.calDay, -1))
	case k.Right:
		goTo(addDays(f.calDay, 1))
	case k.Up:
		goTo(addDays(f.calDay, -7))
	case k.Down:
		goTo(addDays(f.calDay, 7))
	case k.PageUp:
		byMonth(-1)
	case k.PageDown:
		byMonth(1)
	case k.First:
		goTo(dayOf(d.now()))
	}
	return "", true
}

// takeDay makes day the value of the focused date field and closes the calendar. With a time of day the field
// keeps its hour and minute, 00:00 where it is empty, moved into the bounds where the day holds no such time.
func (f *Form) takeDay(day time.Time) {
	field := &f.fields[f.focus]
	d := field.Date
	f.cal = false
	if !d.selectable(day) {
		return
	}
	t := day
	if d.Time && !field.date.IsZero() {
		t = time.Date(day.Year(), day.Month(), day.Day(), field.date.Hour(), field.date.Minute(), 0, 0, time.Local)
	}
	switch d.bound(t) {
	case dateEarly:
		t = d.norm(d.Min)
	case dateLate:
		t = d.norm(d.Max)
	}
	field.line = newTextLine(d.format(t))
	f.changed(f.focus)
}

// calendarClick handles a click while the calendar is open: only its own targets and the symbol of the field
// act, the rest of the screen under it does nothing.
func (f *Form) calendarClick(r MouseResult) (act ActionID, handled bool) {
	switch {
	case strings.HasPrefix(r.Item, formDayRef):
		id, value, _ := strings.Cut(strings.TrimPrefix(r.Item, formDayRef), "\x00")
		day, err := time.ParseInLocation(calDayKey, value, time.Local)
		if FieldID(id) == f.Focused() && err == nil && f.fields[f.focus].Date.selectable(day) {
			f.calDay = day
			return f.calendarAct(f.keys.Enter)
		}
	case strings.HasPrefix(r.Item, formCalBtnRef):
		id, button, _ := strings.Cut(strings.TrimPrefix(r.Item, formCalBtnRef), "\x00")
		key := map[string]ActionID{calPrev: f.keys.PageUp, calNext: f.keys.PageDown, calToday: f.keys.First}[button]
		if FieldID(id) == f.Focused() && key != "" {
			return f.calendarAct(key)
		}
	case strings.HasPrefix(r.Item, formCalendarRef):
		if FieldID(strings.TrimPrefix(r.Item, formCalendarRef)) == f.Focused() {
			return f.calendarAct(f.keys.Back)
		}
	}
	return "", true
}

// calendar draws the month of the open calendar in at most width cells and returns its targets, counted from
// its top left: a target per selectable day and one per button, each of the key it stands for.
func (f *Form) calendar(styles Styles) ([]string, []Target) {
	field := &f.fields[f.focus]
	d := field.Date
	key := func(id ActionID) string { return Escape(f.table.Label(id)) }
	prev := styles.Button(ButtonNormal, "< "+key(f.keys.PageUp), false)
	next := styles.Button(ButtonNormal, key(f.keys.PageDown)+" >", false)
	today := styles.Button(ButtonNormal, f.text.Lookup(TextCalToday)+" "+key(f.keys.First), false)
	inner := max(calMinInner, lipgloss.Width(prev)+1+lipgloss.Width(next), lipgloss.Width(today))
	month := time.Date(f.calDay.Year(), f.calDay.Month(), 1, 0, 0, 0, 0, time.Local)
	now := dayOf(d.now())
	start := f.weekStart()

	lines := []string{Pad(styles.Render(StyleTitle, styles.Truncate(
		Escape(f.text.Lookup(TextCalTitle, f.monthName(month.Month()), month.Year())), inner)), inner)}
	head := ""
	for i := range calCols {
		head += " " + Pad(Escape(f.weekdayName(time.Weekday((start+i)%7))), calCellW-1)
	}
	lines = append(lines, styles.Render(StyleHint, Pad(styles.Truncate(head, inner), inner)))
	var targets []Target
	first := addDays(month, -((int(month.Weekday()) - start + 7) % 7))
	for row := range calRows {
		line := ""
		for col := range calCols {
			day := addDays(first, row*calCols+col)
			if day.Month() != month.Month() {
				line += strings.Repeat(" ", calCellW)
				continue
			}
			lead := " "
			switch {
			case day.Equal(f.calDay):
				lead = ">"
			case day.Equal(now):
				lead = styles.Glyph(GlyphStatus)
			}
			cell := lead + fmt.Sprintf("%2d", day.Day())
			switch {
			case day.Equal(f.calDay):
				cell = styles.Render(StyleActiveRow, cell)
			case !d.selectable(day):
				// Dimmed, and dashes so a terminal without colour tells it too.
				cell = styles.Render(StyleHint, " --")
			case day.Equal(now):
				cell = styles.Render(StyleActive, cell)
			}
			line += cell
			if d.selectable(day) {
				targets = append(targets, Target{Rect: Rect{X: 2 + col*calCellW, Y: 1 + len(lines), Width: calCellW, Height: 1},
					Zone: ZoneForm, Action: f.keys.Enter, Item: formDayRef + string(field.ID) + "\x00" + day.Format(calDayKey)})
			}
		}
		lines = append(lines, Pad(line, inner))
	}
	button := func(text, name string, action ActionID, x, y int) Target {
		return Target{Rect: Rect{X: 2 + x, Y: 1 + y, Width: lipgloss.Width(text), Height: 1}, Zone: ZoneForm, Action: action,
			Item: formCalBtnRef + string(field.ID) + "\x00" + name}
	}
	pw, nw := lipgloss.Width(prev), lipgloss.Width(next)
	lines = append(lines, prev+strings.Repeat(" ", max(inner-pw-nw, 0))+next, Pad(today, inner))
	targets = append(targets,
		button(prev, calPrev, f.keys.PageUp, 0, len(lines)-2),
		button(next, calNext, f.keys.PageDown, inner-nw, len(lines)-2),
		button(today, calToday, f.keys.First, 0, len(lines)-1))
	return styles.Panel("", "", lines, inner+4, len(lines)+2, true), targets
}

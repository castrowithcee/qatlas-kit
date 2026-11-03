package tui

import (
	"strings"
	"time"
)

const (
	dateLayout     = "2006-01-02"
	dateTimeLayout = "2006-01-02 15:04"
)

// dateIssue is what is wrong with the text of a date field.
type dateIssue uint8

const (
	dateOK dateIssue = iota
	dateFormat
	dateNoSuch
	dateEarly
	dateLate
)

func (d DateField) layout() string {
	if d.Time {
		return dateTimeLayout
	}
	return dateLayout
}

func (d DateField) format(t time.Time) string { return t.Format(d.layout()) }

// norm is t in the local time zone, cut to the day, or to the minute with a time of day.
func (d DateField) norm(t time.Time) time.Time {
	t = t.In(time.Local)
	if d.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

// bound says whether t, already cut by norm, is within Min and Max.
func (d DateField) bound(t time.Time) dateIssue {
	switch {
	case !d.Min.IsZero() && t.Before(d.norm(d.Min)):
		return dateEarly
	case !d.Max.IsZero() && t.After(d.norm(d.Max)):
		return dateLate
	}
	return dateOK
}

// shaped reports whether s has exactly the digits and separators of the layout.
func (d DateField) shaped(s string) bool {
	want := "dddd-dd-dd"
	if d.Time {
		want = "dddd-dd-dd dd:dd"
	}
	if len(s) != len(want) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if want[i] == 'd' && (s[i] < '0' || s[i] > '9') || want[i] != 'd' && s[i] != want[i] {
			return false
		}
	}
	return true
}

// parse reads text typed into the field, in the local time zone of the process.
func (d DateField) parse(text string) (time.Time, dateIssue) {
	text = strings.TrimSpace(text)
	if !d.shaped(text) {
		return time.Time{}, dateFormat
	}
	t, err := time.ParseInLocation(d.layout(), text, time.Local)
	if err != nil {
		return time.Time{}, dateNoSuch
	}
	t = d.norm(t)
	return t, d.bound(t)
}

// recheck reads the text of the field: an empty one is the empty date, a valid one the new value, and any
// other leaves the last valid value in place and names what is wrong.
func (f *formField) recheck() {
	text := strings.TrimSpace(f.line.String())
	if text == "" {
		f.date, f.issue, f.refused = time.Time{}, dateOK, false
		return
	}
	t, issue := f.Date.parse(text)
	if f.issue = issue; issue == dateOK {
		f.date, f.refused = t, false
	}
}

// dateProblem is the problem of the text of date field i, shown under its row once the field is left or a
// save was refused over it, never while the text is still being typed.
func (f *Form) dateProblem(i int) (FormProblem, bool) {
	field := &f.fields[i]
	if i == f.focus && !field.refused {
		return FormProblem{}, false
	}
	d := field.Date
	use := TextFormDateUseDate
	if d.Time {
		use = TextFormDateUseTime
	}
	p := FormProblem{Field: field.ID}
	switch field.issue {
	case dateOK:
		return p, false
	case dateFormat:
		p.Cause, p.NextStep = f.text.Lookup(TextFormDateFormat), f.text.Lookup(use)
	case dateNoSuch:
		p.Cause, p.NextStep = f.text.Lookup(TextFormDateNoSuch), f.text.Lookup(use)
	case dateEarly:
		p.Cause, p.NextStep = f.text.Lookup(TextFormDateEarly), f.text.Lookup(TextFormDateFrom, d.format(d.norm(d.Min)))
	case dateLate:
		p.Cause, p.NextStep = f.text.Lookup(TextFormDateLate), f.text.Lookup(TextFormDateUntil, d.format(d.norm(d.Max)))
	}
	return p, true
}

// dateShown reports whether date field i shows a problem.
func (f *Form) dateShown(i int) bool {
	_, shown := f.dateProblem(i)
	return shown
}

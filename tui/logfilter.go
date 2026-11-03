package tui

import (
	"errors"
	"slices"
	"time"
)

// LogFilterKind says how a LogFilter picks its period.
type LogFilterKind int

// The kinds of LogFilter. The zero value is LogFilterAll, so the zero LogFilter has no limit.
const (
	// LogFilterAll shows every line.
	LogFilterAll LogFilterKind = iota
	// LogFilterLast shows the lines of the last Count units up to now.
	LogFilterLast
	// LogFilterRange shows the lines from From to Until.
	LogFilterRange
)

// LogFilterUnit is the unit of the count of a LogFilterLast filter.
type LogFilterUnit int

// The units of LogFilter.Unit; the zero value is none.
const (
	LogUnitMinutes LogFilterUnit = iota + 1
	LogUnitHours
	LogUnitDays
	LogUnitMonths
)

// LogFilterMaxCount is the highest count of a LogFilterLast filter.
const LogFilterMaxCount = 9999

// The reasons LogFilter.Validate gives; an application tells them apart with errors.Is.
var (
	ErrLogFilterKind       = errors.New("log filter: unknown kind")
	ErrLogFilterCount      = errors.New("log filter: the count must be a whole number from 1 to 9999")
	ErrLogFilterUnit       = errors.New("log filter: the unit is unknown or not offered")
	ErrLogFilterRangeOpen  = errors.New("log filter: a range needs a start and an end")
	ErrLogFilterRangeOrder = errors.New("log filter: the end of a range cannot be before its start")
)

func allLogUnits() []LogFilterUnit {
	return []LogFilterUnit{LogUnitMinutes, LogUnitHours, LogUnitDays, LogUnitMonths}
}

func (u LogFilterUnit) valid() bool { return u >= LogUnitMinutes && u <= LogUnitMonths }

// LogFilter is the period a LogView shows. The zero value shows every line. Only the fields of its Kind count:
// Count and Unit for LogFilterLast, From and Until for LogFilterRange, in the local time zone of the process
// and with a resolution of minutes.
type LogFilter struct {
	Kind  LogFilterKind
	Count int
	Unit  LogFilterUnit
	From  time.Time
	Until time.Time
}

// Validate checks the fields of the kind: a count from 1 to LogFilterMaxCount and a known unit for
// LogFilterLast, a start and an end that is not before it for LogFilterRange.
func (f LogFilter) Validate() error {
	switch f.Kind {
	case LogFilterAll:
		return nil
	case LogFilterLast:
		if f.Count < 1 || f.Count > LogFilterMaxCount {
			return ErrLogFilterCount
		}
		if !f.Unit.valid() {
			return ErrLogFilterUnit
		}
		return nil
	case LogFilterRange:
		switch {
		case f.From.IsZero() || f.Until.IsZero():
			return ErrLogFilterRangeOpen
		case f.Until.Before(f.From):
			return ErrLogFilterRangeOrder
		}
		return nil
	}
	return ErrLogFilterKind
}

// Bounds returns the begin and the end of the period at the time now; a zero time is no limit. LogFilterAll
// has none, LogFilterLast begins Count units before now and has no end, LogFilterRange is From to Until. Days
// and months count by the calendar in the zone of now: a month back from a day the target month lacks lands on
// the last day of that month, at the same time of day. Bounds of an invalid filter are not meaningful.
func (f LogFilter) Bounds(now time.Time) (begin, end time.Time) {
	switch f.Kind {
	case LogFilterLast:
		switch f.Unit {
		case LogUnitMinutes:
			return now.Add(-time.Duration(f.Count) * time.Minute), time.Time{}
		case LogUnitHours:
			return now.Add(-time.Duration(f.Count) * time.Hour), time.Time{}
		case LogUnitDays:
			return now.AddDate(0, 0, -f.Count), time.Time{}
		case LogUnitMonths:
			return subMonths(now, f.Count), time.Time{}
		}
	case LogFilterRange:
		return f.From, f.Until
	}
	return time.Time{}, time.Time{}
}

// normalized drops the fields its kind does not use, so equal periods compare equal.
func (f LogFilter) normalized() LogFilter {
	switch f.Kind {
	case LogFilterLast:
		return LogFilter{Kind: f.Kind, Count: f.Count, Unit: f.Unit}
	case LogFilterRange:
		return LogFilter{Kind: f.Kind, From: f.From, Until: f.Until}
	}
	return LogFilter{}
}

func (f LogFilter) same(g LogFilter) bool {
	f, g = f.normalized(), g.normalized()
	return f.Kind == g.Kind && f.Count == g.Count && f.Unit == g.Unit && f.From.Equal(g.From) && f.Until.Equal(g.Until)
}

// closed reports whether the period has ended at now, so no line can arrive for it any more.
func (f LogFilter) closed(now time.Time) bool {
	return f.Kind == LogFilterRange && f.Until.Before(now)
}

// subMonths goes n months back and clamps the day to the last day of the target month.
func subMonths(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	idx := y*12 + int(m) - 1 - n
	ty, tm := idx/12, idx%12
	if tm < 0 {
		ty, tm = ty-1, tm+12
	}
	last := time.Date(ty, time.Month(tm+2), 0, 0, 0, 0, 0, time.UTC).Day()
	hh, mm, ss := t.Clock()
	return time.Date(ty, time.Month(tm+1), min(d, last), hh, mm, ss, t.Nanosecond(), t.Location())
}

// inPeriod reports whether a line at time at belongs to the period between begin and end, either of which
// may be zero. A line without time cannot be judged and belongs.
func inPeriod(at, begin, end time.Time) bool {
	if at.IsZero() {
		return true
	}
	return !(!begin.IsZero() && at.Before(begin) || !end.IsZero() && at.After(end))
}

// checkFilter validates f and, for a count, that its unit is one the view offers.
func (l *LogView) checkFilter(f LogFilter) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if f.Kind == LogFilterLast && !slices.Contains(l.units, f.Unit) {
		return ErrLogFilterUnit
	}
	return nil
}

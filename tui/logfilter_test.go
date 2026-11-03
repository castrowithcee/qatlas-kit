package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func lt(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, time.Local)
}

func TestLogFilterBounds(t *testing.T) {
	now := lt(2026, time.March, 31, 14, 30, 15)
	for _, tt := range []struct {
		name  string
		now   time.Time
		f     LogFilter
		begin time.Time
	}{
		{"minutes", now, LogFilter{Kind: LogFilterLast, Count: 45, Unit: LogUnitMinutes}, now.Add(-45 * time.Minute)},
		{"hours", now, LogFilter{Kind: LogFilterLast, Count: 30, Unit: LogUnitHours}, now.Add(-30 * time.Hour)},
		{"days", now, LogFilter{Kind: LogFilterLast, Count: 3, Unit: LogUnitDays}, lt(2026, time.March, 28, 14, 30, 15)},
		{"days over a month end", now, LogFilter{Kind: LogFilterLast, Count: 31, Unit: LogUnitDays}, lt(2026, time.February, 28, 14, 30, 15)},
		{"month clamps to the end of February", now, LogFilter{Kind: LogFilterLast, Count: 1, Unit: LogUnitMonths}, lt(2026, time.February, 28, 14, 30, 15)},
		{"month clamps in a leap year", lt(2024, time.March, 31, 8, 0, 0), LogFilter{Kind: LogFilterLast, Count: 1, Unit: LogUnitMonths}, lt(2024, time.February, 29, 8, 0, 0)},
		{"month to a 30-day month", lt(2026, time.May, 31, 23, 59, 59), LogFilter{Kind: LogFilterLast, Count: 1, Unit: LogUnitMonths}, lt(2026, time.April, 30, 23, 59, 59)},
		{"months keep a day that exists", lt(2026, time.May, 15, 9, 5, 0), LogFilter{Kind: LogFilterLast, Count: 2, Unit: LogUnitMonths}, lt(2026, time.March, 15, 9, 5, 0)},
		{"months over the year boundary", lt(2026, time.January, 31, 10, 0, 0), LogFilter{Kind: LogFilterLast, Count: 2, Unit: LogUnitMonths}, lt(2025, time.November, 30, 10, 0, 0)},
		{"one month from January", lt(2026, time.January, 15, 10, 0, 0), LogFilter{Kind: LogFilterLast, Count: 1, Unit: LogUnitMonths}, lt(2025, time.December, 15, 10, 0, 0)},
		{"twelve months from a leap day", lt(2024, time.February, 29, 6, 0, 0), LogFilter{Kind: LogFilterLast, Count: 12, Unit: LogUnitMonths}, lt(2023, time.February, 28, 6, 0, 0)},
		{"many months", now, LogFilter{Kind: LogFilterLast, Count: 25, Unit: LogUnitMonths}, lt(2024, time.February, 29, 14, 30, 15)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			begin, end := tt.f.Bounds(tt.now)
			if !begin.Equal(tt.begin) || !end.IsZero() {
				t.Errorf("got %v to %v, want %v to open", begin, end, tt.begin)
			}
		})
	}
	if b, e := (LogFilter{}).Bounds(now); !b.IsZero() || !e.IsZero() {
		t.Errorf("all: %v %v", b, e)
	}
	from, until := lt(2026, time.March, 1, 8, 0, 0), lt(2026, time.March, 2, 9, 30, 0)
	if b, e := (LogFilter{Kind: LogFilterRange, From: from, Until: until}).Bounds(now); !b.Equal(from) || !e.Equal(until) {
		t.Errorf("range: %v %v", b, e)
	}
}

func TestLogFilterValidate(t *testing.T) {
	at := lt(2026, time.March, 1, 8, 0, 0)
	for _, tt := range []struct {
		name string
		f    LogFilter
		want error
	}{
		{"zero is all", LogFilter{}, nil},
		{"last", LogFilter{Kind: LogFilterLast, Count: 1, Unit: LogUnitHours}, nil},
		{"highest count", LogFilter{Kind: LogFilterLast, Count: LogFilterMaxCount, Unit: LogUnitDays}, nil},
		{"count zero", LogFilter{Kind: LogFilterLast, Unit: LogUnitHours}, ErrLogFilterCount},
		{"count negative", LogFilter{Kind: LogFilterLast, Count: -2, Unit: LogUnitHours}, ErrLogFilterCount},
		{"count too high", LogFilter{Kind: LogFilterLast, Count: LogFilterMaxCount + 1, Unit: LogUnitHours}, ErrLogFilterCount},
		{"no unit", LogFilter{Kind: LogFilterLast, Count: 3}, ErrLogFilterUnit},
		{"unknown unit", LogFilter{Kind: LogFilterLast, Count: 3, Unit: 9}, ErrLogFilterUnit},
		{"range", LogFilter{Kind: LogFilterRange, From: at, Until: at.Add(time.Minute)}, nil},
		{"range of one moment", LogFilter{Kind: LogFilterRange, From: at, Until: at}, nil},
		{"range without start", LogFilter{Kind: LogFilterRange, Until: at}, ErrLogFilterRangeOpen},
		{"range without end", LogFilter{Kind: LogFilterRange, From: at}, ErrLogFilterRangeOpen},
		{"range backwards", LogFilter{Kind: LogFilterRange, From: at, Until: at.Add(-time.Minute)}, ErrLogFilterRangeOrder},
		{"unknown kind", LogFilter{Kind: 7}, ErrLogFilterKind},
	} {
		if got := tt.f.Validate(); !errors.Is(got, tt.want) || (tt.want == nil) != (got == nil) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

// clock is a log view clock the test sets.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func clockLog(t *testing.T, spec LogViewSpec, c *clock, lang i18n.Lang) *LogView {
	t.Helper()
	spec.Now = c.now
	return newLog(t, spec, lang)
}

func TestNewLogViewChecksTheFilterSpec(t *testing.T) {
	keys := logKeymap(t)
	at := lt(2026, time.March, 1, 8, 0, 0)
	for name, spec := range map[string]LogViewSpec{
		"invalid filter":   {Filter: LogFilter{Kind: LogFilterLast, Unit: LogUnitDays}},
		"backwards range":  {Filter: LogFilter{Kind: LogFilterRange, From: at, Until: at.Add(-time.Hour)}},
		"unknown unit":     {Units: []LogFilterUnit{LogUnitDays, 0}},
		"listed twice":     {Units: []LogFilterUnit{LogUnitDays, LogUnitDays}},
		"unit not offered": {Units: []LogFilterUnit{LogUnitDays}, Filter: LogFilter{Kind: LogFilterLast, Count: 2, Unit: LogUnitHours}},
	} {
		spec.Keys = logKeys()
		if _, err := NewLogView(spec, keys, newTranslator(t, i18n.English, nil, keys)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, key := range map[string]ActionID{"filter key unknown": "nope", "filter key shared": ActionLogWrap} {
		spec := LogViewSpec{Keys: logKeys()}
		spec.Keys.Filter = key
		if _, err := NewLogView(spec, keys, newTranslator(t, i18n.English, nil, keys)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	l := newLog(t, LogViewSpec{}, i18n.English)
	if l.Filter() != (LogFilter{}) || len(l.FilterUnits()) != 4 {
		t.Errorf("defaults: %+v %v", l.Filter(), l.FilterUnits())
	}
	l = newLog(t, LogViewSpec{Units: []LogFilterUnit{LogUnitHours}, Filter: LogFilter{Kind: LogFilterLast, Count: 2, Unit: LogUnitHours, From: at}}, i18n.English)
	if l.Filter().Count != 2 || !l.Filter().From.IsZero() || len(l.FilterUnits()) != 1 {
		t.Errorf("start filter: %+v %v", l.Filter(), l.FilterUnits())
	}
	// A view built without a filter key stays valid and has no button.
	spec := LogViewSpec{Keys: logKeys()}
	spec.Keys.Filter = ""
	old, err := NewLogView(spec, keys, newTranslator(t, i18n.English, nil, keys))
	if err != nil {
		t.Fatal(err)
	}
	old.Append(numbered(3)...)
	if row, _ := statusTargets(t, old, 160, 4); strings.Contains(row, "period") {
		t.Errorf("button without a key: %q", row)
	}
}

func TestLogViewHidesLinesOutsideThePeriod(t *testing.T) {
	c := &clock{lt(2026, time.November, 3, 15, 0, 0)}
	l := clockLog(t, LogViewSpec{Filter: LogFilter{Kind: LogFilterLast, Count: 30, Unit: LogUnitMinutes}}, c, i18n.English)
	l.Append(
		LogLine{Text: "old one", Time: c.at.Add(-time.Hour)},
		LogLine{Text: "recent one", Time: c.at.Add(-10 * time.Minute)},
		LogLine{Text: "no time"},
		LogLine{Text: "edge", Time: c.at.Add(-30 * time.Minute)},
		LogLine{Text: "old two\nold three", Time: c.at.Add(-2 * time.Hour)},
	)
	if l.Len() != 3 {
		t.Fatalf("%d lines held", l.Len())
	}
	all := strings.Join(viewLines(t, l, 60, 8), "\n")
	for _, want := range []string{"recent one", "no time", "edge", "of 3"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q missing in\n%s", want, all)
		}
	}
	if strings.Contains(all, "old") {
		t.Errorf("a line outside the period is shown:\n%s", all)
	}
	// Search, match count and position count what is shown.
	l.Update(typed('/'))
	typeText(l, "e")
	l.Act("enter")
	if len(l.hits) != 3 {
		t.Errorf("hits %v", l.hits)
	}
	// Narrowing drops what is held; the search and the position follow.
	if err := l.SetFilter(LogFilter{Kind: LogFilterLast, Count: 15, Unit: LogUnitMinutes}); err != nil {
		t.Fatal(err)
	}
	if l.Len() != 2 || len(l.hits) != 2 || l.hasCur {
		t.Errorf("after narrowing: %d lines, hits %v, cur %v", l.Len(), l.hits, l.hasCur)
	}
	if row := strings.Join(viewLines(t, l, 60, 8), "\n"); strings.Contains(row, "edge") || !strings.Contains(row, "2 matches") && !strings.Contains(row, "o 2") {
		t.Errorf("view after narrowing:\n%s", row)
	}
	// Later lines are judged at their arrival, with the clock of then.
	c.at = c.at.Add(time.Hour)
	l.Append(LogLine{Text: "late", Time: c.at.Add(-5 * time.Minute)}, LogLine{Text: "later", Time: c.at.Add(-2 * time.Minute)})
	if l.Len() != 2+2 {
		t.Errorf("held %d", l.Len())
	}
	l.Append(LogLine{Text: "too old", Time: c.at.Add(-time.Hour)})
	if l.Len() != 4 {
		t.Errorf("a line of the old period was kept: %d", l.Len())
	}
	// Back to all keeps what is held and takes every line again.
	if err := l.SetFilter(LogFilter{}); err != nil {
		t.Fatal(err)
	}
	l.Append(LogLine{Text: "ancient", Time: c.at.Add(-24 * time.Hour)})
	if l.Len() != 5 {
		t.Errorf("held %d", l.Len())
	}
	// The ring still works over a rebuilt buffer.
	small := clockLog(t, LogViewSpec{Lines: 3}, c, i18n.English)
	for i := range 5 {
		small.Append(LogLine{Text: fmt.Sprint("n", i), Time: c.at.Add(time.Duration(i-10) * time.Minute)})
	}
	if err := small.SetFilter(LogFilter{Kind: LogFilterLast, Count: 8, Unit: LogUnitMinutes}); err != nil {
		t.Fatal(err)
	}
	small.Append(LogLine{Text: "n5", Time: c.at}, LogLine{Text: "n6", Time: c.at})
	if got := strings.Join(viewLines(t, small, 30, 5), "|"); !strings.Contains(got, "n4") || !strings.Contains(got, "n6") || strings.Contains(got, "n3") {
		t.Errorf("ring after a filter change: %s", got)
	}
}

func TestLogViewSetFilterRefusesWhatIsInvalid(t *testing.T) {
	l := newLog(t, LogViewSpec{Units: []LogFilterUnit{LogUnitHours, LogUnitDays}}, i18n.English)
	l.Append(numbered(5)...)
	for _, f := range []LogFilter{
		{Kind: LogFilterLast, Count: 0, Unit: LogUnitHours},
		{Kind: LogFilterLast, Count: 5, Unit: LogUnitMinutes},
		{Kind: LogFilterRange},
	} {
		if err := l.SetFilter(f); err == nil {
			t.Errorf("%+v accepted", f)
		}
		if l.Filter() != (LogFilter{}) || l.Len() != 5 {
			t.Errorf("a refused filter changed the view: %+v", l.Filter())
		}
	}
	if err := l.SetFilter(LogFilter{Kind: LogFilterLast, Count: 2, Unit: LogUnitDays}); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.TakeFilterChange(); ok {
		t.Error("a change the application made is reported back")
	}
}

func TestLogViewFollowStopsForAnEndedRange(t *testing.T) {
	c := &clock{lt(2026, time.November, 3, 15, 0, 0)}
	past := LogFilter{Kind: LogFilterRange, From: c.at.Add(-2 * time.Hour), Until: c.at.Add(-time.Hour)}
	l := clockLog(t, LogViewSpec{Filter: past}, c, i18n.English)
	l.Append(numbered(40)...)
	if l.Following() {
		t.Error("follows a range that has ended")
	}
	row, _ := statusTargets(t, l, 160, 8)
	if !strings.Contains(row, "[ L range ended ]") {
		t.Errorf("the follow button does not say so: %q", row)
	}
	row, _ = statusTargets(t, l, 70, 8)
	if !strings.Contains(row, "[ L off ]") {
		t.Errorf("short form: %q", row)
	}
	for _, id := range []ActionID{ActionLogFollow, "last"} {
		l.Act(id)
		if l.Following() {
			t.Errorf("%s resumes following in an ended range", id)
		}
	}
	l.Wheel(1000)
	if l.Following() {
		t.Error("the wheel resumes following in an ended range")
	}
	// All and last follow; a range that ends later follows until the clock passes its end.
	for _, f := range []LogFilter{{}, {Kind: LogFilterLast, Count: 5, Unit: LogUnitHours}} {
		if err := l.SetFilter(f); err != nil {
			t.Fatal(err)
		}
		l.Act("last")
		if !l.Following() {
			t.Errorf("%+v does not follow", f)
		}
	}
	open := LogFilter{Kind: LogFilterRange, From: c.at.Add(-time.Hour), Until: c.at.Add(time.Hour)}
	if err := l.SetFilter(open); err != nil {
		t.Fatal(err)
	}
	l.Act("last")
	if !l.Following() {
		t.Error("a range that has not ended does not follow")
	}
	c.at = c.at.Add(2 * time.Hour)
	l.View(plainLog(t), 80, 8)
	if l.Following() || l.following {
		t.Error("following goes on after the end of the range")
	}
	// Switching to an ended range stops following at once.
	l2 := clockLog(t, LogViewSpec{}, c, i18n.English)
	l2.Append(numbered(40)...)
	if err := l2.SetFilter(LogFilter{Kind: LogFilterRange, From: c.at.Add(-3 * time.Hour), Until: c.at.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if l2.Following() {
		t.Error("SetFilter to an ended range keeps following")
	}
}

func TestLogViewFilterButton(t *testing.T) {
	c := &clock{lt(2026, time.November, 3, 15, 0, 0)}
	l := clockLog(t, LogViewSpec{}, c, i18n.English)
	l.Append(numbered(30)...)
	_, targets := statusTargets(t, l, 160, 8)
	var hit *Target
	for i := range targets {
		if targets[i].Action == ActionLogFilter {
			hit = &targets[i]
		}
	}
	if hit == nil {
		t.Fatal("no filter target")
	}
	// A click is the key.
	byKey := clockLog(t, LogViewSpec{}, c, i18n.English)
	byKey.Append(numbered(30)...)
	byKey.View(plainLog(t), 160, 8)
	m := NewMouseMap(targets, true)
	if !l.Click(m.Handle(tea.MouseClickMsg{X: hit.Rect.X + 1, Y: 7, Button: tea.MouseLeft})) || !byKey.Update(typed('T')) {
		t.Fatal("click or key not handled")
	}
	if l.fm == nil || stateOf(l) != stateOf(byKey) {
		t.Errorf("click %+v, key %+v", stateOf(l), stateOf(byKey))
	}
	if !byKey.Act(ActionLogFilter) {
		t.Error("Act does not handle the filter action")
	}
	// The key in force is named, with a filter on the button is primary and says what it shows.
	cases := []struct {
		f    LogFilter
		lang i18n.Lang
		want string
	}{
		{LogFilter{}, i18n.English, "[ T period: all ]"},
		{LogFilter{Kind: LogFilterLast, Count: 30, Unit: LogUnitMinutes}, i18n.English, "[ T period: last 30 min ]"},
		{LogFilter{Kind: LogFilterLast, Count: 2, Unit: LogUnitMonths}, i18n.English, "[ T period: last 2 mo ]"},
		{LogFilter{Kind: LogFilterLast, Count: 30, Unit: LogUnitMinutes}, i18n.German, "[ T Zeitraum: letzte 30 Min. ]"},
		{LogFilter{Kind: LogFilterRange, From: lt(2026, time.November, 3, 14, 0, 0), Until: lt(2026, time.November, 3, 15, 0, 0)}, i18n.English, "[ T period: Nov 3 14:00–15:00 ]"},
		{LogFilter{Kind: LogFilterRange, From: lt(2026, time.November, 3, 14, 0, 0), Until: lt(2026, time.November, 3, 15, 0, 0)}, i18n.German, "[ T Zeitraum: 03.11. 14:00–15:00 ]"},
		{LogFilter{Kind: LogFilterRange, From: lt(2026, time.November, 2, 22, 0, 0), Until: lt(2026, time.November, 3, 1, 30, 0)}, i18n.German, "[ T Zeitraum: 02.11. 22:00–03.11. 01:30 ]"},
		{LogFilter{Kind: LogFilterRange, From: lt(2025, time.December, 31, 22, 0, 0), Until: lt(2026, time.January, 1, 1, 0, 0)}, i18n.English, "[ T period: Dec 31 2025 22:00–Jan 1 2026 01:00 ]"},
	}
	for _, tt := range cases {
		v := clockLog(t, LogViewSpec{Filter: tt.f}, c, tt.lang)
		v.Append(numbered(3)...)
		if row, _ := statusTargets(t, v, 200, 4); !strings.Contains(row, tt.want) {
			t.Errorf("%q missing in %q", tt.want, row)
		}
	}
	// A remapped key shows on the button.
	actions := LogViewActions()
	for i := range actions {
		actions[i].Level, actions[i].Rebind = LevelGlobal, RebindReplace
		actions[i].DefaultKeys = []string{[]string{"f3", "f4", "f6", "f7", "f9", "f15"}[i]}
	}
	keys, err := NewKeymap(append(sampleActions(), actions...), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.Bind(map[ActionID]string{ActionLogFilter: "f17"}); err != nil {
		t.Fatal(err)
	}
	r, err := NewLogView(LogViewSpec{Keys: logKeys(), Now: c.now}, keys, newTranslator(t, i18n.English, nil, keys))
	if err != nil {
		t.Fatal(err)
	}
	if row, _ := statusTargets(t, r, 160, 4); !strings.Contains(row, "[ F17 period: all ]") {
		t.Errorf("remapped key: %q", row)
	}
}

func filterGolden(t *testing.T, f LogFilter) *LogView {
	t.Helper()
	c := &clock{time.Date(2026, time.March, 14, 9, 30, 0, 0, time.UTC)} // the lines of goldenLog are UTC
	l := goldenLog(t)
	l.now = c.now
	if err := l.SetFilter(f); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestGoldenLogViewFilter(t *testing.T) {
	last := LogFilter{Kind: LogFilterLast, Count: 3, Unit: LogUnitMinutes}
	ended := LogFilter{Kind: LogFilterRange, From: time.Date(2026, time.March, 14, 9, 26, 55, 0, time.UTC), Until: time.Date(2026, time.March, 14, 9, 27, 5, 0, time.UTC)}
	for _, tt := range []struct {
		name string
		f    LogFilter
		w, h int
	}{
		{"logview.filter.narrow.golden", last, 68, 16},
		{"logview.filter.wide.golden", ended, 120, 16},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := filterGolden(t, tt.f).View(looks(t)["noneascii"], tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

func TestLogViewReportsAChosenPeriodOnce(t *testing.T) {
	l := newLog(t, LogViewSpec{}, i18n.English)
	if _, ok := l.TakeFilterChange(); ok {
		t.Fatal("a change without a choice")
	}
	l.pending = true // what the choice of the period sets
	if _, ok := l.TakeFilterChange(); !ok {
		t.Fatal("the change is not reported")
	}
	if _, ok := l.TakeFilterChange(); ok {
		t.Fatal("the change is reported twice")
	}
}

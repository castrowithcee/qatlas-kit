package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func keyEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func keyEsc() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func keySave() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyF2} }
func keyTab() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyTab} }
func keyRight() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyRight} }
func keyDown() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyDown} }
func keyBack() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyBackspace} }

// modalClock is the time of the modal tests: a local time, so the tests do not depend on the zone.
func modalClock() *clock { return &clock{lt(2026, time.November, 3, 15, 0, 0)} }

func modalLog(t *testing.T, spec LogViewSpec, lang i18n.Lang) (*LogView, *clock) {
	t.Helper()
	c := modalClock()
	l := clockLog(t, spec, c, lang)
	for i := range 6 {
		l.Append(LogLine{Text: "line", Time: c.at.Add(-time.Duration(i) * time.Minute)})
	}
	return l, c
}

func send(l *LogView, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		l.Update(k)
	}
}

func typeModal(l *LogView, text string) {
	for _, r := range text {
		l.Update(typed(r))
	}
}

// modalDraw draws the view with the modal over it in width by height cells.
func modalDraw(t *testing.T, l *LogView, w, h int) (string, []Target) {
	t.Helper()
	st := plainLog(t)
	screen, _ := l.View(st, w, h)
	out, targets, ok := l.ModalOverlay(st, screen, w, h, Rect{Width: w, Height: h})
	if !ok {
		t.Fatalf("%dx%d: no room for the modal", w, h)
	}
	return out, targets
}

// clickItem clicks the target of the open modal whose item or action matches.
func clickItem(t *testing.T, l *LogView, w, h int, match func(Target) bool) bool {
	t.Helper()
	_, targets := modalDraw(t, l, w, h)
	var hit *Target
	for i := range targets {
		if match(targets[i]) && targets[i].Rect.Width > 0 {
			hit = &targets[i]
		}
	}
	if hit == nil {
		t.Fatalf("no target in %+v", targets)
	}
	r := NewMouseMap(targets, true).Handle(click(hit.Rect.X+hit.Rect.Width/2, hit.Rect.Y))
	return l.Click(r)
}

func item(ref string) func(Target) bool { return func(tg Target) bool { return tg.Item == ref } }

func mFieldRef(id FieldID) string { return formFieldRef + string(id) }

func mOptionRef(id FieldID, choice ChoiceID) string {
	return formOptionRef + string(id) + "\x00" + string(choice)
}

func clearText(l *LogView, n int) {
	for range n {
		l.Update(keyBack())
	}
}

func TestFilterModalOpensWithTheCurrentPeriod(t *testing.T) {
	l, _ := modalLog(t, LogViewSpec{Filter: LogFilter{Kind: LogFilterLast, Count: 45, Unit: LogUnitHours}}, i18n.English)
	if l.FilterOpen() {
		t.Fatal("open before the action")
	}
	if !l.Update(typed('T')) || !l.FilterOpen() {
		t.Fatal("the filter key does not open the modal")
	}
	out, _ := modalDraw(t, l, 80, 24)
	for _, want := range []string{"Time period", "last n units", "45", "hours", "[ F2 apply ]", "[ esc discard ]"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing in\n%s", want, out)
		}
	}
	if strings.Contains(out, "From") {
		t.Errorf("fields of another kind are shown:\n%s", out)
	}
	// Only the units on offer are listed.
	l2, _ := modalLog(t, LogViewSpec{Units: []LogFilterUnit{LogUnitDays, LogUnitMonths}}, i18n.English)
	l2.Update(typed('T'))
	send(l2, keyRight())
	clickItem(t, l2, 80, 24, item(mFieldRef(logFieldUnit)))
	out, _ = modalDraw(t, l2, 80, 24)
	if !strings.Contains(out, "days") || !strings.Contains(out, "months") || strings.Contains(out, "hours") || strings.Contains(out, "minutes") {
		t.Errorf("units:\n%s", out)
	}
	// A range is filled in from the period.
	from, until := lt(2026, time.November, 3, 9, 0, 0), lt(2026, time.November, 3, 10, 30, 0)
	l3, _ := modalLog(t, LogViewSpec{Filter: LogFilter{Kind: LogFilterRange, From: from, Until: until}}, i18n.German)
	l3.Update(typed('T'))
	out, _ = modalDraw(t, l3, 80, 24)
	for _, want := range []string{"Zeitraum", "von - bis", "2026-11-03 09:00", "2026-11-03 10:30", "übernehmen", "Verwerfen"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing in\n%s", want, out)
		}
	}
}

func TestFilterModalTabWraps(t *testing.T) {
	l, _ := modalLog(t, LogViewSpec{Filter: LogFilter{Kind: LogFilterLast, Count: 5, Unit: LogUnitHours}}, i18n.English)
	l.Update(typed('T'))
	if got := l.fm.form.Focused(); got != logFieldKind {
		t.Fatalf("focus starts on %q", got)
	}
	send(l, keyTab(), keyTab())
	if got := l.fm.form.Focused(); got != logFieldUnit {
		t.Errorf("tab twice gave %q", got)
	}
	send(l, keyTab())
	if a, ok := l.fm.form.FocusedButton(); !ok || a != l.spec.Keys.FilterForm.Save {
		t.Errorf("tab past the unit reaches %q, %v; it is apply", a, ok)
	}
	send(l, keyTab())
	if a, ok := l.fm.form.FocusedButton(); !ok || a != l.spec.Keys.FilterForm.Back {
		t.Errorf("tab on apply reaches %q, %v; it is discard", a, ok)
	}
	send(l, keyTab())
	if got := l.fm.form.Focused(); got != logFieldKind || l.fm.form.onButton() {
		t.Errorf("tab past discard gave %q; it wraps to the kind", got)
	}
	l.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if a, ok := l.fm.form.FocusedButton(); !ok || a != l.spec.Keys.FilterForm.Back {
		t.Errorf("shift+tab on the kind reaches %q, %v; it is discard", a, ok)
	}
	if !l.FilterOpen() {
		t.Error("the modal closed")
	}
}

func TestFilterModalEnterOnAButton(t *testing.T) {
	last := LogFilter{Kind: LogFilterLast, Count: 5, Unit: LogUnitHours}
	// Enter on apply applies; on discard it discards.
	l, _ := modalLog(t, LogViewSpec{Filter: last}, i18n.English)
	l.Update(typed('T'))
	l.fm.form.Focus(logFieldCount)
	clearText(l, 1)
	typeModal(l, "7")
	send(l, keyTab(), keyTab(), keyEnter())
	if l.FilterOpen() || l.filter.Count != 7 {
		t.Errorf("enter on apply: open %v, filter %+v", l.FilterOpen(), l.filter)
	}
	l, _ = modalLog(t, LogViewSpec{Filter: last}, i18n.English)
	l.Update(typed('T'))
	l.fm.form.Focus(logFieldCount)
	clearText(l, 1)
	typeModal(l, "9")
	send(l, keyTab(), keyTab(), keyTab(), keyEnter())
	if l.FilterOpen() || l.filter.Count != 5 {
		t.Errorf("enter on discard: open %v, filter %+v", l.FilterOpen(), l.filter)
	}
	// An invalid period stays open with its problem, and the focus goes to the field.
	l, _ = modalLog(t, LogViewSpec{Filter: last}, i18n.English)
	l.Update(typed('T'))
	l.fm.form.Focus(logFieldCount)
	clearText(l, 1)
	send(l, keyTab(), keyTab(), keyEnter())
	if !l.FilterOpen() || l.filter.Count != 5 {
		t.Errorf("enter on apply with no count: open %v, filter %+v", l.FilterOpen(), l.filter)
	}
	if got := l.fm.form.Focused(); got != logFieldCount || l.fm.form.onButton() {
		t.Errorf("the problem leaves the focus on %q", got)
	}
	// Typing on a button changes no field.
	l, _ = modalLog(t, LogViewSpec{Filter: last}, i18n.English)
	l.Update(typed('T'))
	l.fm.form.Focus(logFieldUnit)
	send(l, keyTab())
	typeModal(l, "x")
	send(l, keyRight(), keyDown())
	if !l.fm.form.onButton() || l.fm.form.Values()[logFieldCount].Text != "5" || l.fm.form.Values()[logFieldUnit].Choice != "hours" {
		t.Errorf("a button took typing: %+v", l.fm.form.Values())
	}
}

func TestFilterModalKindsByKeyAndByClick(t *testing.T) {
	const w, h = 80, 24
	start := LogFilter{Kind: LogFilterLast, Count: 5, Unit: LogUnitHours}
	for _, tt := range []struct {
		name string
		want LogFilter
		keys func(l *LogView)
		mice func(t *testing.T, l *LogView)
	}{
		{"all", LogFilter{}, func(l *LogView) { l.Update(typed('T')); send(l, keyRight(), keyRight(), keySave()) }, func(t *testing.T, l *LogView) {
			l.Update(typed('T'))
			clickItem(t, l, w, h, item(mFieldRef(logFieldKind)))
			clickItem(t, l, w, h, item(mOptionRef(logFieldKind, logKindAll)))
			clickItem(t, l, w, h, item(formButtonRef+"0"))
		}},
		{"last", LogFilter{Kind: LogFilterLast, Count: 45, Unit: LogUnitDays}, func(l *LogView) {
			l.Update(typed('T'))
			send(l, keyTab())
			clearText(l, 3)
			typeModal(l, "45")
			send(l, keyTab(), keyRight(), keySave())
		}, func(t *testing.T, l *LogView) {
			l.Update(typed('T'))
			clickItem(t, l, w, h, item(mFieldRef(logFieldCount)))
			clearText(l, 3)
			typeModal(l, "45")
			clickItem(t, l, w, h, item(mFieldRef(logFieldUnit)))
			clickItem(t, l, w, h, item(mOptionRef(logFieldUnit, "days")))
			clickItem(t, l, w, h, item(formButtonRef+"0"))
		}},
		{"range", LogFilter{Kind: LogFilterRange, From: lt(2026, time.November, 3, 14, 0, 0), Until: lt(2026, time.November, 3, 14, 45, 0)}, func(l *LogView) {
			l.Update(typed('T'))
			send(l, keyRight(), keyTab())
			typeModal(l, "2026-11-03 14:00")
			send(l, keyTab())
			typeModal(l, "2026-11-03 14:45")
			send(l, keySave())
		}, func(t *testing.T, l *LogView) {
			l.Update(typed('T'))
			clickItem(t, l, w, h, item(mFieldRef(logFieldKind)))
			clickItem(t, l, w, h, item(mOptionRef(logFieldKind, logKindRange)))
			clickItem(t, l, w, h, item(mFieldRef(logFieldFrom)))
			typeModal(l, "2026-11-03 14:00")
			clickItem(t, l, w, h, item(mFieldRef(logFieldUntil)))
			typeModal(l, "2026-11-03 14:45")
			clickItem(t, l, w, h, item(formButtonRef+"0"))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			byKey, _ := modalLog(t, LogViewSpec{Filter: start}, i18n.English)
			byClick, _ := modalLog(t, LogViewSpec{Filter: start}, i18n.English)
			tt.keys(byKey)
			tt.mice(t, byClick)
			for name, l := range map[string]*LogView{"keys": byKey, "clicks": byClick} {
				if l.FilterOpen() {
					t.Errorf("%s: the modal stays open", name)
				}
				if got := l.Filter(); !got.same(tt.want) {
					t.Errorf("%s: filter %+v, want %+v", name, got, tt.want)
				}
				if f, ok := l.TakeFilterChange(); !ok || !f.same(tt.want) {
					t.Errorf("%s: reported %+v %v", name, f, ok)
				}
				if _, ok := l.TakeFilterChange(); ok {
					t.Errorf("%s: reported twice", name)
				}
			}
		})
	}
}

func TestFilterModalRefusesWhatIsInvalid(t *testing.T) {
	for _, tt := range []struct {
		name string
		keys func(l *LogView)
		hint string
	}{
		{"count empty", func(l *LogView) { send(l, keyRight(), keyTab()); clearText(l, 3) }, "not a whole number from 1 to 9999"},
		{"count zero", func(l *LogView) { send(l, keyRight(), keyTab()); clearText(l, 3); typeModal(l, "0") }, "not a whole number from 1 to 9999"},
		{"count too high", func(l *LogView) { send(l, keyRight(), keyTab()); clearText(l, 3); typeModal(l, "10000") }, "not a whole number from 1 to 9999"},
		{"count not a number", func(l *LogView) { send(l, keyRight(), keyTab()); clearText(l, 3); typeModal(l, "2x") }, "not a whole number from 1 to 9999"},
		{"dates empty", func(l *LogView) { send(l, keyRight(), keyRight()) }, "no date or time"},
		{"end empty", func(l *LogView) {
			send(l, keyRight(), keyRight(), keyTab())
			typeModal(l, "2026-11-03 10:00")
		}, "no date or time"},
		{"end before start", func(l *LogView) {
			send(l, keyRight(), keyRight(), keyTab())
			typeModal(l, "2026-11-03 10:00")
			send(l, keyTab())
			typeModal(l, "2026-11-03 09:59")
		}, "the end is before the start"},
		{"date is no date", func(l *LogView) {
			send(l, keyRight(), keyRight(), keyTab())
			typeModal(l, "2026-13-45 10:00")
		}, "that date or time does not exist"},
	} {
		for _, byClick := range []bool{false, true} {
			l, _ := modalLog(t, LogViewSpec{}, i18n.English)
			l.Update(typed('T'))
			tt.keys(l)
			if byClick {
				clickItem(t, l, 80, 24, item(formButtonRef+"0"))
			} else {
				l.Update(keySave())
			}
			if !l.FilterOpen() {
				t.Errorf("%s (click %v): the modal closed", tt.name, byClick)
				continue
			}
			if l.Filter() != (LogFilter{}) {
				t.Errorf("%s: the filter changed: %+v", tt.name, l.Filter())
			}
			if _, ok := l.TakeFilterChange(); ok {
				t.Errorf("%s: a refusal is reported", tt.name)
			}
			out, _ := modalDraw(t, l, 80, 24)
			if !strings.Contains(out, "[!]") || !strings.Contains(out, tt.hint) {
				t.Errorf("%s (click %v): no hint %q in\n%s", tt.name, byClick, tt.hint, out)
			}
		}
	}
	// A refused period can be corrected and applied.
	l, _ := modalLog(t, LogViewSpec{}, i18n.English)
	l.Update(typed('T'))
	send(l, keyRight(), keyTab())
	clearText(l, 3)
	l.Update(keySave())
	typeModal(l, "3")
	l.Update(keySave())
	if l.FilterOpen() || l.Filter().Count != 3 {
		t.Errorf("corrected: open %v, %+v", l.FilterOpen(), l.Filter())
	}
}

func TestFilterModalEscDiscards(t *testing.T) {
	start := LogFilter{Kind: LogFilterLast, Count: 5, Unit: LogUnitHours}
	for _, byClick := range []bool{false, true} {
		l, _ := modalLog(t, LogViewSpec{Filter: start}, i18n.English)
		l.Update(typed('T'))
		send(l, keyRight(), keyTab())
		typeModal(l, "7")
		if byClick {
			clickItem(t, l, 80, 24, item(formButtonRef+"1"))
		} else {
			l.Update(keyEsc())
		}
		if l.FilterOpen() || l.Filter() != start {
			t.Errorf("click %v: open %v, filter %+v", byClick, l.FilterOpen(), l.Filter())
		}
		if _, ok := l.TakeFilterChange(); ok {
			t.Errorf("click %v: a discard is reported", byClick)
		}
		// Reopened, it shows the period again, not the draft.
		l.Update(typed('T'))
		if out, _ := modalDraw(t, l, 80, 24); !strings.Contains(out, "5") || strings.Contains(out, "57") {
			t.Errorf("reopened:\n%s", out)
		}
	}
	// Esc closes an open list or calendar first, then the modal.
	l, _ := modalLog(t, LogViewSpec{}, i18n.English)
	l.Update(typed('T'))
	clickItem(t, l, 80, 24, item(mFieldRef(logFieldKind)))
	if !l.fm.form.Open() {
		t.Fatal("a click on the choice does not open its list")
	}
	l.Update(keyEsc())
	if !l.FilterOpen() || l.fm.form.Open() {
		t.Error("esc in an open list closes more than the list")
	}
	l.Update(keyEsc())
	if l.FilterOpen() {
		t.Error("esc does not close the modal")
	}
}

func TestFilterModalEnterRule(t *testing.T) {
	// Enter applies from the count, and from a choice, which stays closed: it steps with left and right.
	l, _ := modalLog(t, LogViewSpec{Filter: LogFilter{Kind: LogFilterLast, Count: 5, Unit: LogUnitHours}}, i18n.English)
	l.Update(typed('T'))
	send(l, keyTab())
	clearText(l, 3)
	typeModal(l, "9")
	l.Update(keyEnter())
	if l.FilterOpen() || l.Filter().Count != 9 {
		t.Errorf("enter in the count: open %v, %+v", l.FilterOpen(), l.Filter())
	}
	l.Update(typed('T'))
	send(l, keyTab(), keyTab(), keyRight(), keyEnter())
	if l.FilterOpen() || l.Filter().Unit != LogUnitDays || l.Filter().Count != 9 {
		t.Errorf("enter in the unit: open %v, %+v", l.FilterOpen(), l.Filter())
	}
	// With the kind all, the modal holds only the choice and enter applies there.
	l.Update(typed('T'))
	send(l, keyRight(), keyRight())
	if l.fm.form.Open() {
		t.Fatal("stepping opened the list")
	}
	l.Update(keyEnter())
	if l.FilterOpen() || l.Filter() != (LogFilter{}) {
		t.Errorf("enter in the kind: open %v, %+v", l.FilterOpen(), l.Filter())
	}
	// A click opens the list of a choice, and enter in the list takes the value and keeps the modal open.
	l.Update(typed('T'))
	clickItem(t, l, 80, 24, item(mFieldRef(logFieldKind)))
	if !l.fm.form.Open() {
		t.Fatal("a click on the kind does not open its list")
	}
	send(l, keyDown(), keyEnter())
	if !l.FilterOpen() || l.fm.form.Open() || l.fm.kind != logKindLast {
		t.Errorf("enter in the list: open %v, kind %v", l.FilterOpen(), l.fm.kind)
	}
	// In a date enter opens the calendar, and in the calendar it takes the day.
	send(l, keyRight(), keyTab(), keyEnter())
	if !l.FilterOpen() || !l.fm.form.cal {
		t.Fatal("enter in a date does not open the calendar")
	}
	send(l, keyEnter())
	if l.fm.form.cal || l.fm.form.Values()[logFieldFrom].Time.IsZero() {
		t.Error("enter in the calendar does not take the day")
	}
	if !l.FilterOpen() {
		t.Error("enter in a date closed the modal")
	}
	// The save key applies from a date too, but not while the calendar is open.
	send(l, keyTab(), keyEnter())
	l.Update(keySave())
	if !l.FilterOpen() || !l.fm.form.cal {
		t.Errorf("save in an open calendar: open %v", l.FilterOpen())
	}
	l.Update(keyEsc())
	typeModal(l, "2026-11-03 16:00")
	l.Update(keySave())
	if l.FilterOpen() {
		t.Error("save does not apply from a date")
	}
}

func TestFilterModalAppliesOnceAndOnlyAChange(t *testing.T) {
	l, _ := modalLog(t, LogViewSpec{Filter: LogFilter{Kind: LogFilterLast, Count: 5, Unit: LogUnitHours}}, i18n.English)
	l.Update(typed('T'))
	l.Update(keySave()) // unchanged
	if l.FilterOpen() {
		t.Error("the modal stays open")
	}
	if _, ok := l.TakeFilterChange(); ok {
		t.Error("an unchanged period is reported")
	}
	l.Update(typed('T'))
	send(l, keyTab())
	clearText(l, 3)
	typeModal(l, "6")
	l.Update(keySave())
	if f, ok := l.TakeFilterChange(); !ok || f.Count != 6 {
		t.Errorf("reported %+v %v", f, ok)
	}
	if _, ok := l.TakeFilterChange(); ok {
		t.Error("reported twice")
	}
}

func TestFilterModalTakesEveryKeyAndClick(t *testing.T) {
	l, _ := modalLog(t, LogViewSpec{}, i18n.English)
	_, targets := l.View(plainLog(t), 160, 12)
	l.Update(typed('T'))
	before := stateOf(l)
	before.askOpen = true
	for _, k := range []tea.KeyPressMsg{typed('w'), typed('t'), typed('L'), typed('/'), typed('x'), {Code: tea.KeyUp}, {Code: tea.KeyPgUp}} {
		if !l.Update(k) {
			t.Errorf("key %v not taken", k)
		}
	}
	if l.Searching() || l.wrap || l.stamps {
		t.Error("a key reached the view behind the modal")
	}
	// A click on the view behind, with the targets of that drawing, and one on nothing do nothing.
	m := NewMouseMap(targets, true)
	for _, tg := range targets {
		if tg.Action == "" {
			continue
		}
		if !l.Click(m.Handle(click(tg.Rect.X+1, tg.Rect.Y))) {
			t.Errorf("click on %s not swallowed", tg.Action)
		}
	}
	if !l.Click(MouseResult{}) || !l.FilterOpen() || l.wrap || l.stamps || l.Searching() {
		t.Error("a click behind the modal acted")
	}
	l.Wheel(3)
	if !l.FilterOpen() {
		t.Error("the wheel closed the modal")
	}
}

func TestFilterModalControlsAreTargets(t *testing.T) {
	for _, kind := range []string{"all", "last", "range"} {
		l, _ := modalLog(t, LogViewSpec{}, i18n.English)
		l.Update(typed('T'))
		for range map[string]int{"all": 0, "last": 1, "range": 2}[kind] {
			l.Update(keyRight())
		}
		out, targets := modalDraw(t, l, 80, 24)
		want := []string{mFieldRef(logFieldKind), formButtonRef + "0", formButtonRef + "1"}
		switch kind {
		case "last":
			want = append(want, mFieldRef(logFieldCount), mFieldRef(logFieldUnit))
		case "range":
			want = append(want, mFieldRef(logFieldFrom), mFieldRef(logFieldUntil), formCalendarRef+string(logFieldFrom), formCalendarRef+string(logFieldUntil))
		}
		for _, ref := range want {
			found := false
			for _, tg := range targets {
				found = found || tg.Item == ref
			}
			if !found {
				t.Errorf("%s: no target %q in\n%s", kind, ref, out)
			}
		}
		rows := strings.Split(out, "\n")
		for _, tg := range targets {
			if tg.Item == formButtonRef+"0" || tg.Item == formButtonRef+"1" {
				text := string([]rune(rows[tg.Rect.Y])[tg.Rect.X : tg.Rect.X+tg.Rect.Width])
				if !strings.HasPrefix(text, "[ ") || !strings.HasSuffix(text, " ]") {
					t.Errorf("%s: target %q is no whole button", kind, text)
				}
			}
		}
	}
	// A button names the key in force.
	actions := LogViewActions()
	for i := range actions {
		actions[i].Level, actions[i].Rebind = LevelGlobal, RebindReplace
		actions[i].DefaultKeys = []string{[]string{"f3", "f4", "f6", "f7", "f9", "f15"}[i]}
	}
	keys, err := NewKeymap(append(sampleActions(), actions...), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.Bind(map[ActionID]string{"save": "f18"}); err != nil {
		t.Fatal(err)
	}
	c := modalClock()
	l, err := NewLogView(LogViewSpec{Keys: logKeys(), Now: c.now}, keys, newTranslator(t, i18n.English, nil, keys))
	if err != nil {
		t.Fatal(err)
	}
	l.Act(ActionLogFilter)
	if out, _ := modalDraw(t, l, 80, 24); !strings.Contains(out, "[ F18 apply ]") {
		t.Errorf("remapped save key:\n%s", out)
	}
}

func TestFilterModalClickIsItsKey(t *testing.T) {
	setup := func() *LogView {
		l, _ := modalLog(t, LogViewSpec{}, i18n.English)
		l.Update(typed('T'))
		send(l, keyRight(), keyTab())
		clearText(l, 3)
		typeModal(l, "4")
		return l
	}
	// The footer keys act as their keys: save applies, back discards.
	for _, tt := range []struct {
		key   tea.KeyPressMsg
		match func(Target) bool
	}{
		{keySave(), func(tg Target) bool { return tg.Item == "" && tg.Action == "save" }},
		{keyEsc(), func(tg Target) bool { return tg.Item == "" && tg.Action == "back" }},
		{keyEnter(), func(tg Target) bool { return tg.Item == "" && tg.Action == "enter" }},
	} {
		byKey, byClick := setup(), setup()
		byKey.Update(tt.key)
		clickItem(t, byClick, 80, 24, tt.match)
		if byKey.FilterOpen() != byClick.FilterOpen() || byKey.Filter() != byClick.Filter() {
			t.Errorf("key %v: key gives open %v %+v, click gives open %v %+v", tt.key, byKey.FilterOpen(), byKey.Filter(), byClick.FilterOpen(), byClick.Filter())
		}
	}
	// A click on the status button opens the modal as the key does.
	l, _ := modalLog(t, LogViewSpec{}, i18n.English)
	_, targets := l.View(plainLog(t), 160, 12)
	for _, tg := range targets {
		if tg.Action == ActionLogFilter {
			l.Click(NewMouseMap(targets, true).Handle(click(tg.Rect.X+1, tg.Rect.Y)))
		}
	}
	if !l.FilterOpen() {
		t.Error("the button does not open the modal")
	}
}

func TestLogViewClear(t *testing.T) {
	c := modalClock()
	l := clockLog(t, LogViewSpec{Wrap: true, Timestamps: true, Filter: LogFilter{Kind: LogFilterLast, Count: 1, Unit: LogUnitHours}}, c, i18n.English)
	for i := range 50 {
		l.Append(LogLine{Text: "needle line", Source: "src", Time: c.at.Add(-time.Duration(i) * time.Second)})
	}
	l.Update(typed('/'))
	typeText(l, "needle")
	l.Act("enter")
	l.Act("first")
	l.Append(LogLine{Text: "more", Time: c.at})
	l.Clear()
	if l.Len() != 0 || len(l.hits) != 0 || l.hasCur || l.unseen != 0 || l.srcWidth != 0 {
		t.Errorf("not empty: %d lines, hits %v, unseen %d", l.Len(), l.hits, l.unseen)
	}
	if !l.Following() {
		t.Error("following does not resume")
	}
	if l.Query() != "needle" || !l.wrap || !l.stamps || l.Filter().Count != 1 {
		t.Errorf("Clear dropped a setting: %q wrap %v stamps %v %+v", l.Query(), l.wrap, l.stamps, l.Filter())
	}
	// The kept search finds the lines of the reload, and they show.
	l.Append(LogLine{Text: "needle again", Time: c.at})
	if len(l.hits) != 1 || l.hits[0] != l.first() {
		t.Errorf("hits %v after the reload", l.hits)
	}
	if out := strings.Join(viewLines(t, l, 80, 6), "\n"); !strings.Contains(out, "needle again") || !strings.Contains(out, "1 of 1") && !strings.Contains(out, "1-1/1") {
		t.Errorf("view after Clear:\n%s", out)
	}
	// An ended range stays unfollowed.
	if err := l.SetFilter(LogFilter{Kind: LogFilterRange, From: c.at.Add(-2 * time.Hour), Until: c.at.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	l.Clear()
	if l.Following() {
		t.Error("follows an ended range after Clear")
	}
}

func TestFilterChangeResumesFollowing(t *testing.T) {
	for _, via := range []string{"SetFilter", "modal"} {
		for _, to := range []LogFilter{{}, {Kind: LogFilterLast, Count: 2, Unit: LogUnitDays}} {
			l, c := modalLog(t, LogViewSpec{}, i18n.English)
			l.Append(numbered(60)...)
			l.Act(ActionLogFollow)
			if l.Following() {
				t.Fatal("still following")
			}
			l.fm = nil
			switch via {
			case "SetFilter":
				if err := l.SetFilter(LogFilter{Kind: LogFilterLast, Count: 1, Unit: LogUnitDays}); err != nil {
					t.Fatal(err)
				}
				l.Act(ActionLogFollow) // pause again
				if err := l.SetFilter(to); err != nil {
					t.Fatal(err)
				}
			case "modal":
				l.Update(typed('T'))
				send(l, keyRight(), keyTab())
				clearText(l, 3)
				typeModal(l, "3")
				l.Update(keySave())
				l.Act(ActionLogFollow)
				if l.Following() {
					t.Fatal("still following")
				}
				l.Update(typed('T'))
				if to.Kind == LogFilterAll {
					send(l, keyRight(), keyRight())
				} else {
					send(l, keyTab())
					clearText(l, 3)
					typeModal(l, "2")
					send(l, keyTab(), keyRight(), keyRight())
				}
				l.Update(keySave())
			}
			if !l.Following() {
				t.Errorf("%s to %+v: following did not resume", via, to)
			}
			_ = c
		}
	}
	l, c := modalLog(t, LogViewSpec{}, i18n.English)
	l.Update(typed('T'))
	send(l, keyRight(), keyRight(), keyTab())
	typeModal(l, c.at.Add(-3*time.Hour).Format(dateTimeLayout))
	send(l, keyTab())
	typeModal(l, c.at.Add(-2*time.Hour).Format(dateTimeLayout))
	l.Update(keySave())
	if l.FilterOpen() || l.Filter().Kind != LogFilterRange || l.Following() {
		t.Errorf("an ended range: open %v, following %v", l.FilterOpen(), l.Following())
	}
}

func TestNewLogViewNeedsTheFilterFormKeys(t *testing.T) {
	keys := logKeymap(t)
	spec := LogViewSpec{Keys: logKeys()}
	spec.Keys.FilterForm = FormKeys{}
	if _, err := NewLogView(spec, keys, newTranslator(t, i18n.English, nil, keys)); err == nil {
		t.Error("a filter key without form keys is accepted")
	}
	spec.Keys.Filter = ""
	if _, err := NewLogView(spec, keys, newTranslator(t, i18n.English, nil, keys)); err != nil {
		t.Errorf("no filter key, no form keys needed: %v", err)
	}
}

func TestFilterModalHints(t *testing.T) {
	l, _ := modalLog(t, LogViewSpec{}, i18n.English)
	keys := logKeymap(t)
	text := newTranslator(t, i18n.English, nil, keys)
	plain := func() string {
		var parts []string
		for _, g := range keys.Hints(l.Hints(), l.Input(), text) {
			parts = append(parts, g.Text())
		}
		return strings.Join(parts, "|")
	}
	l.Update(typed('T'))
	if got := plain(); !strings.Contains(got, "F2/enter apply") || !strings.Contains(got, "esc discard") || strings.Contains(got, "choose") {
		t.Errorf("hints: %s", got)
	}
	send(l, keyRight(), keyTab())
	if got := plain(); !strings.Contains(got, "enter apply") || l.Input() != InputText {
		t.Errorf("hints in the count: %s (input %v)", got, l.Input())
	}
	send(l, keyTab())
	clickItem(t, l, 80, 24, item(mFieldRef(logFieldUnit)))
	if got := plain(); strings.Contains(got, "apply") || !strings.Contains(got, "esc") {
		t.Errorf("hints in an open list: %s", got)
	}
}

func TestFilterModalSweep(t *testing.T) {
	for _, look := range []string{"dark", "noneascii", "mono"} {
		st := looks(t)[look]
		for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
			l, _ := modalLog(t, LogViewSpec{}, lang)
			l.Update(typed('T'))
			for range 3 {
				for w := 12; w <= 120; w += 9 {
					for h := 4; h <= 30; h += 3 {
						screen, _ := l.View(st, w, h)
						if out, targets, ok := l.ModalOverlay(st, screen, w, h, Rect{Width: w, Height: h}); ok {
							rows := strings.Split(out, "\n")
							if len(rows) != h {
								t.Fatalf("%s %dx%d: %d rows", look, w, h, len(rows))
							}
							for _, tg := range targets {
								if tg.Rect.X < 0 || tg.Rect.X+tg.Rect.Width > w || tg.Rect.Y < 0 || tg.Rect.Y >= h {
									t.Fatalf("%dx%d: target %+v outside", w, h, tg)
								}
							}
						}
						view, _ := l.ModalView(st, w, h)
						if len(strings.Split(view, "\n")) != h {
							t.Fatalf("%s %dx%d: ModalView has the wrong height", look, w, h)
						}
					}
				}
				send(l, keyRight())
			}
		}
	}
}

func modalGolden(t *testing.T, kind string) *LogView {
	t.Helper()
	l, _ := modalLog(t, LogViewSpec{}, i18n.English)
	l.Update(typed('T'))
	switch kind {
	case "last":
		send(l, keyRight(), keyTab())
		clearText(l, 3)
		typeModal(l, "30")
	case "range":
		send(l, keyRight(), keyRight(), keyTab())
		typeModal(l, "2026-11-03 14:00")
	case "button":
		send(l, keyRight(), keyTab(), keyTab(), keyTab())
	}
	return l
}

func TestGoldenFilterModal(t *testing.T) {
	for _, tt := range []struct {
		name, kind string
		w, h       int
	}{
		{"logview.modal.last.narrow.golden", "last", 46, 20},
		{"logview.modal.last.wide.golden", "last", 100, 24},
		{"logview.modal.range.narrow.golden", "range", 46, 24},
		{"logview.modal.range.wide.golden", "range", 100, 24},
		{"logview.modal.button.narrow.golden", "button", 46, 20},
		{"logview.modal.button.wide.golden", "button", 100, 24},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := modalDraw(t, modalGolden(t, tt.kind), tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

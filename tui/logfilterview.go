package tui

import (
	"errors"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The fields and choices of the form that picks the period.
const (
	logFieldKind  FieldID = "kind"
	logFieldCount FieldID = "count"
	logFieldUnit  FieldID = "unit"
	logFieldFrom  FieldID = "from"
	logFieldUntil FieldID = "until"

	logKindAll   ChoiceID = "all"
	logKindLast  ChoiceID = "last"
	logKindRange ChoiceID = "range"
)

// logFilterModal is the open choice of the period: a form with the fields of the chosen kind, and what was
// typed into the fields of the other kinds, which comes back when the kind does.
type logFilterModal struct {
	form        *Form
	kind        ChoiceID
	count       string
	unit        ChoiceID
	from, until time.Time
}

// unitChoice is the ID and the full name of a unit in the choice field.
func unitChoice(u LogFilterUnit) (ChoiceID, TextID) {
	switch u {
	case LogUnitMinutes:
		return "minutes", TextLogUnitNameMin
	case LogUnitHours:
		return "hours", TextLogUnitNameHour
	case LogUnitDays:
		return "days", TextLogUnitNameDay
	case LogUnitMonths:
		return "months", TextLogUnitNameMonth
	}
	return "", ""
}

func firstUnit(u LogFilterUnit) ChoiceID {
	id, _ := unitChoice(u)
	return id
}

func unitOf(id ChoiceID) LogFilterUnit {
	for _, u := range allLogUnits() {
		if got, _ := unitChoice(u); got == id {
			return u
		}
	}
	return 0
}

// filterForm builds the form for the kind and the values of m: the kind always, then a count and a unit, or a
// start and an end with the time of day.
func (l *LogView) filterForm(m *logFilterModal) (*Form, error) {
	fields := []FormField{{ID: logFieldKind, Kind: FieldChoice, Label: TextLogFieldKind, Value: FormValue{Choice: m.kind},
		Choices: []Choice{{ID: logKindAll, Label: TextLogFilterAll}, {ID: logKindLast, Label: TextLogKindLast},
			{ID: logKindRange, Label: TextLogKindRange}}}}
	switch m.kind {
	case logKindLast:
		var units []Choice
		for _, u := range l.units {
			id, label := unitChoice(u)
			units = append(units, Choice{ID: id, Label: label})
		}
		fields = append(fields,
			FormField{ID: logFieldCount, Kind: FieldText, Label: TextLogFieldCount, Value: FormValue{Text: m.count}},
			FormField{ID: logFieldUnit, Kind: FieldChoice, Label: TextLogFieldUnit, Value: FormValue{Choice: m.unit}, Choices: units})
	case logKindRange:
		date := DateField{Time: true, Now: l.now}
		fields = append(fields,
			FormField{ID: logFieldFrom, Kind: FieldDate, Label: TextLogFieldFrom, Value: FormValue{Time: m.from}, Date: date},
			FormField{ID: logFieldUntil, Kind: FieldDate, Label: TextLogFieldUntil, Value: FormValue{Time: m.until}, Date: date})
	}
	fk := l.spec.Keys.FilterForm
	buttons := []FormButton{{Action: fk.Save, Label: TextFilterApply, Primary: true}, {Action: fk.Back, Label: TextKeyDiscard}}
	return NewForm(FormSpec{Fields: fields, Keys: fk, Buttons: buttons}, l.keys, l.text)
}

// openFilter opens the choice of the period with the current period filled in.
func (l *LogView) openFilter() {
	if l.fm != nil {
		return
	}
	m := &logFilterModal{kind: logKindAll, count: "1", unit: firstUnit(l.units[0])}
	switch f := l.filter; f.Kind {
	case LogFilterLast:
		m.kind, m.count, m.unit = logKindLast, strconv.Itoa(f.Count), firstUnit(f.Unit)
	case LogFilterRange:
		m.kind, m.from, m.until = logKindRange, f.From, f.Until
	}
	form, err := l.filterForm(m)
	if err != nil {
		return
	}
	m.form, l.fm = form, m
}

// FilterOpen reports whether the choice of the period is open. While it is, the view takes every key, click
// and wheel notch, and the application draws ModalOverlay, or ModalView and Hints where that does not fit.
func (l *LogView) FilterOpen() bool { return l.fm != nil }

// keep reads the values of the open form back into the modal.
func (m *logFilterModal) keep() {
	for id, v := range m.form.Values() {
		switch id {
		case logFieldKind:
			m.kind = v.Choice
		case logFieldCount:
			m.count = v.Text
		case logFieldUnit:
			m.unit = v.Choice
		case logFieldFrom:
			m.from = v.Time
		case logFieldUntil:
			m.until = v.Time
		}
	}
}

// syncKind builds the form again when the kind changed, keeping what was typed; the focus stays on the kind.
func (l *LogView) syncKind() {
	m := l.fm
	if m.form.Values()[logFieldKind].Choice == m.kind {
		return
	}
	m.keep()
	if form, err := l.filterForm(m); err == nil {
		m.form = form
	}
}

// dateFocused reports whether a date field has the focus.
func (m *logFilterModal) dateFocused() bool {
	return !m.form.onButton() && m.form.fields[m.form.focus].Kind == FieldDate
}

// inner reports whether the form has a list or a calendar open, which then takes every key.
func (m *logFilterModal) inner() bool { return m.form.open || m.form.cal || m.form.capture }

// updateFilter handles a key of the open modal; it takes every key.
func (l *LogView) updateFilter(msg tea.KeyPressMsg) bool {
	m, fk := l.fm, l.spec.Keys.FilterForm
	focus := m.form.Input()
	for _, id := range []ActionID{fk.Back, fk.Save, fk.Enter} {
		// Enter on a button is the button's: the form reports its action.
		if l.keys.Is(msg, id, focus) && !(id == fk.Enter && m.form.onButton()) {
			return l.filterAct(id)
		}
	}
	if act, _ := m.form.Update(msg); act == fk.Save || act == fk.Back {
		return l.filterAct(act)
	}
	l.syncKind()
	return true
}

// filterAct is a key of the open modal. Back discards, and the save key and enter apply, enter from every
// field but a date, where it opens the calendar as in every form. A choice list opens by a click on the field
// and its value changes with left and right. While a list or the calendar is open every key goes to it, enter
// chooses, and back closes only that.
func (l *LogView) filterAct(id ActionID) bool {
	m, fk := l.fm, l.spec.Keys.FilterForm
	switch {
	case m.inner():
		m.form.Act(id)
	case id == fk.Back:
		l.fm = nil
	case id == fk.Save, id == fk.Enter && !m.dateFocused():
		l.applyModal()
	default:
		m.form.Act(id)
	}
	if l.fm != nil {
		l.syncKind()
	}
	return true
}

// clickFilter handles a click while the modal is open; a click outside its targets does nothing.
func (l *LogView) clickFilter(r MouseResult) bool {
	m := l.fm
	if !r.Handled {
		return true
	}
	switch {
	case strings.HasPrefix(r.Item, formButtonRef) && r.Action != "":
		if !m.inner() {
			return l.filterAct(r.Action)
		}
	case r.Item == "" && r.Action != "":
		return l.filterAct(r.Action)
	default:
		m.form.Click(r)
	}
	if l.fm != nil {
		l.syncKind()
	}
	return true
}

// applyModal takes the period of the form and closes the modal; a period that is not valid stays open with its
// problems under the fields. The change is reported for TakeFilterChange only where the period differs.
func (l *LogView) applyModal() {
	m, fk := l.fm, l.spec.Keys.FilterForm
	if act, _ := m.form.Act(fk.Save); act != fk.Save {
		return // a date holds text that is no date; the form shows it
	}
	m.keep()
	f, problems := l.draftFilter(m)
	if len(problems) > 0 {
		m.form.SetProblems(problems)
		return
	}
	l.fm = nil
	if !f.same(l.filter) {
		l.applyFilter(f)
		l.pending = true
	}
}

// draftFilter is the period of m, or the problems that keep it from being one.
func (l *LogView) draftFilter(m *logFilterModal) (LogFilter, []FormProblem) {
	var f LogFilter
	switch m.kind {
	case logKindLast:
		n, err := strconv.Atoi(strings.TrimSpace(m.count))
		if err != nil {
			n = 0
		}
		f = LogFilter{Kind: LogFilterLast, Count: n, Unit: unitOf(m.unit)}
	case logKindRange:
		f = LogFilter{Kind: LogFilterRange, From: m.from, Until: m.until}
	}
	err := l.checkFilter(f)
	problem := func(field FieldID, cause, next TextID, args ...any) FormProblem {
		return FormProblem{Field: field, Cause: l.text.Lookup(cause, args...), NextStep: l.text.Lookup(next)}
	}
	var out []FormProblem
	switch {
	case err == nil:
	case errors.Is(err, ErrLogFilterCount):
		out = append(out, problem(logFieldCount, TextLogProblemCount, TextLogNextCount, LogFilterMaxCount))
	case errors.Is(err, ErrLogFilterRangeOpen):
		if f.From.IsZero() {
			out = append(out, problem(logFieldFrom, TextLogProblemOpen, TextLogNextOpen))
		}
		if f.Until.IsZero() {
			out = append(out, problem(logFieldUntil, TextLogProblemOpen, TextLogNextOpen))
		}
	case errors.Is(err, ErrLogFilterRangeOrder):
		out = append(out, problem(logFieldUntil, TextLogProblemOrder, TextLogNextOrder))
	default:
		out = append(out, FormProblem{Cause: Escape(err.Error())})
	}
	return f, out
}

// filterHints are the keys of the open modal for the key line: the answers with the form's own keys, which
// stand in the modal itself where it is drawn over the screen. While a list or the calendar is open they are
// the keys of that.
func (l *LogView) filterHints() []Hint {
	m, fk := l.fm, l.spec.Keys.FilterForm
	hints := m.form.Hints()
	if m.inner() {
		return hints
	}
	out := []Hint{{Action: fk.Save, Label: TextFilterApply, Kind: HintAnswer}, {Action: fk.Back, Label: TextKeyDiscard, Kind: HintAnswer}}
	// Enter applies from every field but a date; on a button the form's own hint says what it does.
	onField := !m.form.onButton()
	if !m.dateFocused() && onField {
		out = append(out, Hint{Action: fk.Enter, Label: TextFilterApply, Kind: HintAnswer})
	}
	for _, h := range hints {
		if h.Action != fk.Save && (h.Action != fk.Enter || m.dateFocused() || !onField) {
			out = append(out, h)
		}
	}
	return out
}

// formRows is the height the form needs at width with no list or calendar open.
func (m *logFilterModal) formRows(styles Styles, width int) int {
	c := *m.form
	// The buttons keep the bottom rows of the form, so they are measured apart from the fields, and with the
	// focus on a field, which may add the line of its hint.
	c.open, c.cal, c.btn = false, false, -1
	buttons, _ := c.buttonRows(width, styles)
	c.buttons = nil
	view, _ := c.View(styles, width, 60)
	rows := strings.Split(view, "\n")
	for len(rows) > 1 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	return len(rows) + len(buttons) + 1
}

// ModalOverlay draws the open choice of the period with styles over screen, the drawn screen of width by
// height cells, in the way of Dialog.Overlay: the screen dimmed, the form with the fields of the chosen kind
// in its frame, the apply and discard buttons of the form and the key line, centred in room. The targets are the modal's
// alone, so the view behind it takes no click. ok is false where room is too small, and while the choice is
// closed; the application then draws ModalView in the workspace and Hints in the footer.
func (l *LogView) ModalOverlay(styles Styles, screen string, width, height int, room Rect) (view string, targets []Target, ok bool) {
	m := l.fm
	if m == nil {
		return "", nil, false
	}
	inner := min(dialogMaxInner, room.Width-8)
	if inner < dialogMinInner {
		return "", nil, false
	}
	rows := m.formRows(styles, inner)
	extra := 0
	switch {
	case m.form.cal:
		box, _ := m.form.calendar(styles)
		extra = len(box)
	case m.form.open:
		extra = len(m.form.fields[m.form.focus].Choices) + 2
	}
	formH := rows + extra
	// The form with its buttons, a gap and the key line, in the frame.
	if formH+2+2 > room.Height {
		return "", nil, false
	}
	body, ts := m.form.View(styles, inner, formH)
	lines := strings.Split(body, "\n")
	lines = append(lines, "")
	keyLine, kt := styles.footer(l.keys.Hints(l.filterHints(), m.form.Input(), l.text), "", inner)
	ts = append(ts, OffsetTargets(kt, 0, len(lines))...)
	lines = append(lines, keyLine)
	for i, line := range lines {
		lines[i] = Pad(Clip(line, inner), inner)
	}
	box := styles.Panel(l.text.Lookup(TextLogFilterTitle), "", lines, inner+4, len(lines)+2, true)
	top := room.Y + (room.Height-len(box))/2
	left := room.X + (room.Width-inner-4)/2
	return overlay(screen, box, width, height, top, left, styles), OffsetTargets(ts, left+2, top+1), true
}

// ModalView draws the open choice of the period with styles as the content of a workspace of width by height
// cells, for a terminal too small to draw it over the screen: the form with its buttons where they fit, and the keys
// in the footer through Hints. Empty while the choice is closed.
func (l *LogView) ModalView(styles Styles, width, height int) (string, []Target) {
	if l.fm == nil {
		return "", nil
	}
	return l.fm.form.View(styles, width, height)
}

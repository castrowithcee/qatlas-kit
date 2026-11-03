package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// FieldKind is how a form field takes its value.
type FieldKind string

const (
	// FieldText is one line of text; it scrolls sideways and is never cut.
	FieldText FieldKind = "text"
	// FieldChoice is one of known values, never free text: left and right step through them, enter opens
	// their list.
	FieldChoice FieldKind = "choice"
	// FieldMultiple is any number of known values, each with a tick box; toggle ticks the one under the
	// cursor.
	FieldMultiple FieldKind = "multiple"
	// FieldMasked is a secret or a passphrase. What is typed is never shown, and an empty field keeps a
	// stored value.
	FieldMasked FieldKind = "masked"
	// FieldKey takes the next key pressed after enter.
	FieldKey FieldKind = "key"
	// FieldList shows the summary of a list the application edits in an editor of its own, opened by enter.
	FieldList FieldKind = "list"
	// FieldReadOnly shows a value with its hint and never takes the focus.
	FieldReadOnly FieldKind = "readonly"
	// FieldDate is a date, or a date with the time of day where FormField.Date says so, typed as
	// YYYY-MM-DD or YYYY-MM-DD HH:MM in the local time zone of the process. It may be empty.
	FieldDate FieldKind = "date"
)

func (k FieldKind) known() bool {
	switch k {
	case FieldText, FieldChoice, FieldMultiple, FieldMasked, FieldKey, FieldList, FieldReadOnly, FieldDate:
		return true
	}
	return false
}

func (k FieldKind) typed() bool { return k == FieldText || k == FieldMasked || k == FieldDate }

// FieldID is the stable, untranslated ID of a field of a form or a filter bar.
type FieldID string

// SectionID is the stable, untranslated ID of a section of a form.
type SectionID string

// ChoiceID is the stable, untranslated ID of a value of a choice field or of a field of a filter bar.
type ChoiceID string

// Choice is one value of a choice field or of a field of a filter bar.
type Choice struct {
	ID    ChoiceID
	Label TextID
}

// FormValue is the value of a field: Text for text, masked, key, list and read-only fields, Choice for a
// choice, Selected for a multiple choice, in the order of the choices, and Time for a date, in the local time
// zone of the process, the zero time for an empty date. The other members stay empty.
type FormValue struct {
	Text     string
	Choice   ChoiceID
	Selected []ChoiceID
	Time     time.Time
}

func (v FormValue) clone() FormValue {
	v.Selected = slices.Clone(v.Selected)
	return v
}

func (v FormValue) equal(o FormValue) bool {
	return v.Text == o.Text && v.Choice == o.Choice && slices.Equal(v.Selected, o.Selected) && v.Time.Equal(o.Time)
}

// DateField is what a date field adds to its FormField. Time adds the time of day, in minutes. Min and Max are
// the earliest and the latest value, both allowed; a zero one sets no bound, and a date without time compares
// by its day. Now is the clock of the field, time.Now where nil; the form never reads a clock but through it.
type DateField struct {
	Time     bool
	Min, Max time.Time
	Now      func() time.Time
}

func (d DateField) set() bool { return d.Time || !d.Min.IsZero() || !d.Max.IsZero() || d.Now != nil }

// FormField is one field of a form. Section names the section it stands in; Hint is the dimmed line under
// the focused field, and the note in the row of a read-only field. Stored says that a masked field has a
// value already, which an empty field keeps; the form is never handed that value. Date belongs to a date
// field only.
type FormField struct {
	ID      FieldID
	Section SectionID
	Kind    FieldKind
	Label   TextID
	Value   FormValue
	Choices []Choice
	Hint    TextID
	Stored  bool
	Date    DateField
}

// FormSection is a section of a form. Lead is the line under its title where the sections stand as a list.
// An advanced section stays closed and dimmed while none of its fields holds a value or a problem, the focus
// is not in it and the person has not opened it.
type FormSection struct {
	ID       SectionID
	Title    TextID
	Lead     TextID
	Advanced bool
}

// FormProblem is a problem of a refused save, with its cause and the next step; Field names the field it
// stands under, empty or unknown for a problem that stands above the form.
type FormProblem struct {
	Field    FieldID
	Cause    string
	NextStep string
}

// FormKeys are the actions of the key table a form reads. Up, Down, Next and Prev move between the fields,
// PageUp and PageDown between the sections, Left and Right through a value or a text, First and Last to
// the ends of a text, Enter opens a choice, a list or a key and otherwise goes on, Toggle ticks a value,
// Save saves from any field, and Back closes an open list of values or ends taking a key.
type FormKeys struct {
	Up, Down, Next, Prev, PageUp, PageDown, Left, Right, First, Last, Enter, Toggle, Save, Back ActionID
}

// FormButton is a button below the fields of a form. Action is the action of the key table it stands for: its
// key, its click and enter on the focused button all give it, as Update and Click report it. Label is its
// text, drawn behind the key now in force. Primary makes it the main button.
type FormButton struct {
	Action  ActionID
	Label   TextID
	Primary bool
}

// FormSpec is the content of a form. The application supplies every text and checks the values when it
// saves; the form knows no schema. Buttons stand below the fields, and tab reaches them after the last field.
type FormSpec struct {
	Fields   []FormField
	Sections []FormSection
	Keys     FormKeys
	Buttons  []FormButton
}

// The target items of a form start with a control character, which no ID holds.
const (
	formFieldRef   = "\x00F"
	formTickRef    = "\x00T"
	formOptionRef  = "\x00O"
	formSectionRef = "\x00S"
	formButtonRef  = "\x00B"
	// formCalendarRef is the symbol of a date field that opens its calendar.
	formCalendarRef = "\x00C"
)

// formField is a field with what the form holds for it while it is edited.
type formField struct {
	FormField
	line    textLine
	choice  int
	cursor  int
	ticked  []bool
	initial FormValue
	// date is the last valid value of a date field, and issue what is wrong with the text typed since.
	date  time.Time
	issue dateIssue
	// refused says that a save was refused over the text, so its problem shows in the focused field too.
	refused bool
}

// Form is an open form: its fields, the focus, the values typed and chosen, an open list of values and
// the problems of the last refused save.
type Form struct {
	keys     FormKeys
	table    *Keymap
	text     Translator
	fields   []formField
	sections []FormSection
	buttons  []FormButton

	// focus is the field with the focus, or the one the focus left for a button; btn is the focused button,
	// -1 while a field has the focus.
	focus    int
	btn      int
	pinned   map[SectionID]bool
	problems map[FieldID][]FormProblem
	other    []FormProblem
	open     bool
	pick     int
	// cal says that the calendar of the focused date field is open, on the day calDay.
	cal     bool
	calDay  time.Time
	capture bool
}

// NewForm checks spec and returns the form with the focus on its first field that takes input. It rejects a
// form without a field that takes input, a field or section without ID or title, an ID with a control
// character or listed twice, an unknown kind, a label-less field, a value that does not fit its kind, a text
// with a control character, a masked field handed a value, a choice field without choices or with a value
// that is none of them, choices on another kind, a section that holds no field or that a field names without
// it being declared, a date field with a text, a choice, DateField members on another kind, bounds in the
// wrong order or a value outside them, a form that mixes fields with and without section, and a key that
// names no action of the key table or that two form keys share, and a button without label, with an action that
// is no action of the key table or that another button has, or with a key that moves in the form (every form
// key but save and back). The fields stand in the order of their sections.
func NewForm(spec FormSpec, keys *Keymap, text Translator) (*Form, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the form needs a key table and a translator")
	}
	var errs []error
	k := spec.Keys
	named := []struct {
		name string
		id   ActionID
	}{{"up", k.Up}, {"down", k.Down}, {"next", k.Next}, {"prev", k.Prev}, {"page up", k.PageUp},
		{"page down", k.PageDown}, {"left", k.Left}, {"right", k.Right}, {"first", k.First}, {"last", k.Last},
		{"enter", k.Enter}, {"toggle", k.Toggle}, {"save", k.Save}, {"back", k.Back}}
	used := map[ActionID]string{}
	for _, n := range named {
		if !keys.known(n.id) {
			errs = append(errs, fmt.Errorf("form key %s %q is no action of the key table", n.name, n.id))
		}
		if other, dup := used[n.id]; dup && n.id != "" {
			errs = append(errs, fmt.Errorf("form keys %s and %s are both %q", other, n.name, n.id))
		}
		used[n.id] = n.name
	}
	badID := func(id string) bool { return id == "" || strings.IndexFunc(id, unicode.IsControl) >= 0 }
	order := map[SectionID]int{}
	for i, s := range spec.Sections {
		switch _, dup := order[s.ID]; {
		case badID(string(s.ID)):
			errs = append(errs, fmt.Errorf("section %q: an ID is not empty and holds no control character", s.ID))
		case dup:
			errs = append(errs, fmt.Errorf("section %q is listed twice", s.ID))
		}
		order[s.ID] = i
		if s.Title == "" {
			errs = append(errs, fmt.Errorf("section %q has no title", s.ID))
		}
	}
	moving := map[ActionID]bool{k.Up: true, k.Down: true, k.Next: true, k.Prev: true, k.PageUp: true, k.PageDown: true,
		k.Left: true, k.Right: true, k.First: true, k.Last: true, k.Enter: true, k.Toggle: true}
	pressed := map[ActionID]bool{}
	for i, b := range spec.Buttons {
		switch {
		case !keys.known(b.Action):
			errs = append(errs, fmt.Errorf("button %d: %q is no action of the key table", i, b.Action))
		case moving[b.Action]:
			errs = append(errs, fmt.Errorf("button %d: %q is a key that moves in the form", i, b.Action))
		case pressed[b.Action]:
			errs = append(errs, fmt.Errorf("button %d: action %q is on another button", i, b.Action))
		}
		pressed[b.Action] = true
		if b.Label == "" {
			errs = append(errs, fmt.Errorf("button %d has no label", i))
		}
	}
	if len(spec.Fields) == 0 {
		errs = append(errs, errors.New("the form has no field"))
	}
	seen := map[FieldID]bool{}
	filled := map[SectionID]bool{}
	takes := false
	for _, f := range spec.Fields {
		switch {
		case badID(string(f.ID)):
			errs = append(errs, fmt.Errorf("field %q: an ID is not empty and holds no control character", f.ID))
		case seen[f.ID]:
			errs = append(errs, fmt.Errorf("field %q is listed twice", f.ID))
		}
		seen[f.ID] = true
		if _, ok := order[f.Section]; f.Section != "" && !ok {
			errs = append(errs, fmt.Errorf("field %q: section %q is not declared", f.ID, f.Section))
		}
		if (f.Section == "") != (len(spec.Sections) == 0) {
			errs = append(errs, fmt.Errorf("field %q: either every field stands in a declared section or none does", f.ID))
		}
		filled[f.Section] = true
		if f.Label == "" {
			errs = append(errs, fmt.Errorf("field %q has no label", f.ID))
		}
		errs = append(errs, fieldErrors(f, f.Value)...)
		takes = takes || f.Kind != FieldReadOnly
	}
	for _, s := range spec.Sections {
		if !filled[s.ID] {
			errs = append(errs, fmt.Errorf("section %q holds no field", s.ID))
		}
	}
	if len(spec.Fields) > 0 && !takes {
		errs = append(errs, errors.New("no field of the form takes input"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	f := &Form{keys: k, table: keys, text: text, sections: slices.Clone(spec.Sections),
		buttons: slices.Clone(spec.Buttons), btn: -1, pinned: map[SectionID]bool{}, problems: map[FieldID][]FormProblem{}, focus: -1}
	fields := slices.Clone(spec.Fields)
	slices.SortStableFunc(fields, func(a, b FormField) int { return order[a.Section] - order[b.Section] })
	for _, field := range fields {
		field.Choices = slices.Clone(field.Choices)
		ff := formField{FormField: field}
		ff.set(field.Value)
		ff.initial = ff.value()
		f.fields = append(f.fields, ff)
	}
	f.focus = f.firstTaking()
	return f, nil
}

// fieldErrors are the reasons v cannot be the value of f.
func fieldErrors(f FormField, v FormValue) []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("field %q: "+format, append([]any{f.ID}, args...)...))
	}
	if !f.Kind.known() {
		fail("unknown kind %q", f.Kind)
		return errs
	}
	choices := f.Kind == FieldChoice || f.Kind == FieldMultiple
	switch {
	case choices && len(f.Choices) == 0:
		fail("a choice field has choices")
	case !choices && len(f.Choices) > 0:
		fail("only a choice field has choices")
	case choices && v.Text != "":
		fail("a choice field holds no text")
	case f.Kind != FieldChoice && v.Choice != "":
		fail("only a choice field holds one choice")
	case f.Kind != FieldMultiple && len(v.Selected) > 0:
		fail("only a multiple choice field holds selected choices")
	case f.Kind == FieldMasked && v.Text != "":
		// The form draws the field; a value it holds could reach the screen, so it never holds a stored one.
		fail("a masked field is handed no value; Stored says it has one")
	case f.Kind != FieldMasked && f.Stored:
		fail("only a masked field is stored")
	case f.Kind != FieldDate && !v.Time.IsZero():
		fail("only a date field holds a time")
	case f.Kind != FieldDate && f.Date.set():
		fail("only a date field has date settings")
	case f.Kind == FieldDate && v.Text != "":
		fail("a date field holds a time, no text")
	case strings.IndexFunc(v.Text, unicode.IsControl) >= 0:
		fail("a one-line value holds no control character")
	}
	if f.Kind == FieldDate {
		d := f.Date
		if !d.Min.IsZero() && !d.Max.IsZero() && d.norm(d.Min).After(d.norm(d.Max)) {
			fail("the earliest value is after the latest")
		}
		if !v.Time.IsZero() && d.bound(d.norm(v.Time)) != dateOK {
			fail("the value is outside the bounds of the field")
		}
	}
	ids := map[ChoiceID]bool{}
	for _, c := range f.Choices {
		switch {
		case c.ID == "" || strings.IndexFunc(string(c.ID), unicode.IsControl) >= 0:
			fail("choice %q: an ID is not empty and holds no control character", c.ID)
		case ids[c.ID]:
			fail("choice %q is listed twice", c.ID)
		}
		ids[c.ID] = true
		if c.Label == "" {
			fail("choice %q has no label", c.ID)
		}
	}
	if v.Choice != "" && !ids[v.Choice] {
		fail("value %q is no choice of the field", v.Choice)
	}
	picked := map[ChoiceID]bool{}
	for _, id := range v.Selected {
		switch {
		case !ids[id]:
			fail("selected %q is no choice of the field", id)
		case picked[id]:
			fail("selected %q is listed twice", id)
		}
		picked[id] = true
	}
	return errs
}

// set makes v the value of the field.
func (f *formField) set(v FormValue) {
	f.line = newTextLine(v.Text)
	if f.Kind == FieldDate {
		f.date, f.issue = time.Time{}, dateOK
		if !v.Time.IsZero() {
			f.date = f.Date.norm(v.Time)
			f.line = newTextLine(f.Date.format(f.date))
		}
	}
	f.choice = 0
	for i, c := range f.Choices {
		if c.ID == v.Choice {
			f.choice = i
		}
	}
	f.ticked = make([]bool, len(f.Choices))
	for i, c := range f.Choices {
		f.ticked[i] = slices.Contains(v.Selected, c.ID)
	}
}

func (f *formField) value() FormValue {
	switch f.Kind {
	case FieldChoice:
		return FormValue{Choice: f.Choices[f.choice].ID}
	case FieldMultiple:
		var out []ChoiceID
		for i, on := range f.ticked {
			if on {
				out = append(out, f.Choices[i].ID)
			}
		}
		return FormValue{Selected: out}
	}
	if f.Kind == FieldDate {
		return FormValue{Time: f.date}
	}
	return FormValue{Text: f.line.String()}
}

// hasValue reports whether the field holds more than a fresh field: text, a stored or typed secret, a choice
// other than the first or a ticked value.
func (f *formField) hasValue() bool {
	switch f.Kind {
	case FieldChoice:
		return f.choice > 0
	case FieldMultiple:
		return slices.Contains(f.ticked, true)
	case FieldMasked:
		return f.Stored || len(f.line.runes) > 0
	}
	return len(f.line.runes) > 0
}

func (f *Form) index(id FieldID) int {
	for i, field := range f.fields {
		if field.ID == id {
			return i
		}
	}
	return -1
}

func (f *Form) section(id SectionID) (FormSection, bool) {
	for _, s := range f.sections {
		if s.ID == id {
			return s, true
		}
	}
	return FormSection{}, false
}

// closed reports whether the fields of a section are folded away.
func (f *Form) closed(id SectionID) bool {
	s, ok := f.section(id)
	if !ok || !s.Advanced || f.pinned[id] {
		return false
	}
	for i := range f.fields {
		field := &f.fields[i]
		if field.Section == id && (i == f.focus || field.hasValue() || len(f.problems[field.ID]) > 0 || field.issue != dateOK) {
			return false
		}
	}
	return true
}

// takes reports whether the field at i can have the focus.
func (f *Form) takes(i int) bool {
	return f.fields[i].Kind != FieldReadOnly && !f.closed(f.fields[i].Section)
}

func (f *Form) firstTaking() int {
	for i := range f.fields {
		if f.takes(i) {
			return i
		}
	}
	for i := range f.fields {
		if f.fields[i].Kind != FieldReadOnly {
			return i
		}
	}
	return 0
}

// step moves the focus to the next field that takes input in direction by and reports whether there is one.
func (f *Form) step(by int) bool {
	for i := f.focus + by; i >= 0 && i < len(f.fields); i += by {
		if f.takes(i) {
			f.setFocus(i)
			return true
		}
	}
	return false
}

// wrap moves the focus like step but through the fields that take input and then the buttons, and continues at
// the other end, so tab and shift+tab cycle through all of them; a lone stop keeps the focus.
func (f *Form) wrap(by int) {
	// A stop is a field index, or -1-j for button j.
	var stops []int
	at := -1
	for i := range f.fields {
		if f.takes(i) {
			if i == f.focus && f.btn < 0 {
				at = len(stops)
			}
			stops = append(stops, i)
		}
	}
	for j := range f.buttons {
		if j == f.btn {
			at = len(stops)
		}
		stops = append(stops, -1-j)
	}
	if len(stops) < 2 || at < 0 {
		return
	}
	if next := stops[((at+by)%len(stops)+len(stops))%len(stops)]; next >= 0 {
		f.setFocus(next)
	} else {
		f.open, f.capture, f.cal, f.btn = false, false, false, -1-next
	}
}

func (f *Form) setFocus(i int) {
	if i != f.focus || f.btn >= 0 {
		f.open, f.capture, f.cal = false, false, false
	}
	f.focus, f.btn = i, -1
}

// onButton reports whether a button has the focus.
func (f *Form) onButton() bool { return f.btn >= 0 }

// fieldFocused reports whether field i has the focus, which no field has while a button does.
func (f *Form) fieldFocused(i int) bool { return i == f.focus && f.btn < 0 }

// FocusedButton is the action of the button with the focus; false while a field has it.
func (f *Form) FocusedButton() (ActionID, bool) {
	if f.btn < 0 {
		return "", false
	}
	return f.buttons[f.btn].Action, true
}

// Focused is the ID of the field with the focus.
func (f *Form) Focused() FieldID { return f.fields[f.focus].ID }

// Focus gives the focus to the field id and opens its section; false for a field that cannot take it.
func (f *Form) Focus(id FieldID) bool {
	i := f.index(id)
	if i < 0 || f.fields[i].Kind == FieldReadOnly {
		return false
	}
	if s := f.fields[i].Section; f.closed(s) {
		f.pinned[s] = true
	}
	f.setFocus(i)
	return true
}

// Input is the input focus the form reads keys in: key capture while a key is taken, text in a text or masked
// field, navigation otherwise. The application switches the mouse off while a key is taken.
func (f *Form) Input() InputFocus {
	switch {
	case f.capture:
		return InputKeyCapture
	case !f.open && !f.cal && f.btn < 0 && f.fields[f.focus].Kind.typed():
		return InputText
	}
	return InputNavigation
}

// Capturing reports whether the key field with the focus waits for a key.
func (f *Form) Capturing() bool { return f.capture }

// Open reports whether the list of values of the focused choice field is open.
func (f *Form) Open() bool { return f.open }

// Values are the values of every field by ID, each a copy. A masked field holds what was typed; empty keeps
// a stored value. A date holds its last valid value while its text is not one, which the save key refuses.
func (f *Form) Values() map[FieldID]FormValue {
	out := make(map[FieldID]FormValue, len(f.fields))
	for i := range f.fields {
		out[f.fields[i].ID] = f.fields[i].value()
	}
	return out
}

// SetValue gives the field id a new value, such as the summary of a list after its editor, under the rules of
// NewForm, and retires the problems of the field.
func (f *Form) SetValue(id FieldID, v FormValue) error {
	i := f.index(id)
	if i < 0 {
		return fmt.Errorf("field %q is no field of the form", id)
	}
	if err := errors.Join(fieldErrors(f.fields[i].FormField, v)...); err != nil {
		return err
	}
	f.fields[i].set(v.clone())
	f.changed(i)
	return nil
}

// Changed reports whether a value differs from the one the form opened with, a masked field was typed in or a
// date holds a text that is no value; leaving a changed form asks first.
func (f *Form) Changed() bool {
	for i := range f.fields {
		if !f.fields[i].value().equal(f.fields[i].initial) || f.fields[i].issue != dateOK {
			return true
		}
	}
	return false
}

// SetProblems shows the problems of a refused save: each under its field, one without a field above the
// form. A section with a problem opens, the focus goes to the first field with one, and a problem stays until
// its field changes. Every value stays as it was typed.
func (f *Form) SetProblems(problems []FormProblem) {
	f.problems, f.other = map[FieldID][]FormProblem{}, nil
	first := -1
	for _, p := range problems {
		i := f.index(p.Field)
		if i < 0 {
			f.other = append(f.other, p)
			continue
		}
		f.problems[p.Field] = append(f.problems[p.Field], p)
		if first < 0 && f.fields[i].Kind != FieldReadOnly {
			first = i
		}
	}
	if first >= 0 {
		f.setFocus(first)
	}
}

func (f *Form) changed(i int) {
	delete(f.problems, f.fields[i].ID)
	if f.fields[i].Kind == FieldDate {
		f.fields[i].recheck()
	}
}

// Update handles a key while the form has the focus. act is Keys.Save where the save key was pressed, from any
// field, Keys.Enter where enter was pressed on a list field, whose editor the application opens, and the action of
// a button where its key or enter on the focused button was pressed. handled is false for a key the form leaves
// to the application: back, which leads to the leave question where Changed says so and is reported as act
// Keys.Back, up above the first field, which leads to the zone above, and every key it does not read. Tab and
// shift+tab cycle through the fields and the buttons and are always handled. The key of a button gives its
// action; while a button has the focus, typing changes no field and up and down stay on it.
func (f *Form) Update(msg tea.KeyPressMsg) (act ActionID, handled bool) {
	if f.capture {
		f.capture = false
		if f.table.Is(msg, f.keys.Back, InputKeyCapture) {
			return "", true
		}
		if _, ok := f.table.Match(msg, InputKeyCapture); ok {
			return "", false
		}
		field := &f.fields[f.focus]
		field.line = newTextLine(KeyName(msg))
		f.changed(f.focus)
		return "", true
	}
	focus := f.Input()
	k := f.keys
	for _, id := range []ActionID{k.Save, k.Back, k.Enter, k.Toggle, k.Up, k.Down, k.Next, k.Prev, k.Left, k.Right,
		k.First, k.Last, k.PageUp, k.PageDown} {
		if f.table.Is(msg, id, focus) {
			return f.Act(id)
		}
	}
	for _, b := range f.buttons {
		if f.table.Is(msg, b.Action, focus) {
			return f.Act(b.Action)
		}
	}
	if focus == InputText && f.fields[f.focus].line.edit(msg) {
		f.changed(f.focus)
		return "", true
	}
	return "", false
}

// Paste puts pasted text into the focused text or masked field at its cursor; line breaks and other control
// characters are dropped.
func (f *Form) Paste(text string) bool {
	field := &f.fields[f.focus]
	if f.btn >= 0 || f.open || f.capture || !field.Kind.typed() || !field.line.insert(text) {
		return false
	}
	f.changed(f.focus)
	return true
}

// Act runs one of the form keys or the action of a button as its key does, for a click or a command that
// stands for the key, and reports as Update does. Enter on a focused button runs the action of that button and
// nothing more, back included: act is Keys.Back with handled false.
func (f *Form) Act(id ActionID) (act ActionID, handled bool) {
	k := f.keys
	if id == "" {
		return "", false
	}
	field := &f.fields[f.focus]
	if f.cal {
		return f.calendarAct(id)
	}
	if f.btn >= 0 {
		switch id {
		case k.Enter:
			id = f.buttons[f.btn].Action
		case k.Up, k.Down, k.PageUp, k.PageDown:
			return "", true
		case k.Left, k.Right, k.First, k.Last, k.Toggle:
			// These act on the value of a field, which a button has not got.
			return "", false
		}
	}
	if f.open {
		switch id {
		case k.Up:
			f.pick = max(f.pick-1, 0)
		case k.Down:
			f.pick = min(f.pick+1, len(field.Choices)-1)
		case k.Enter:
			f.choose(f.pick)
		case k.Back:
			f.open = false
		}
		return "", true
	}
	switch id {
	case k.Save:
		// A date with a text that is no value is not saved as the value before it.
		refused := false
		for i := range f.fields {
			if f.fields[i].issue != dateOK && f.takes(i) {
				if !refused {
					f.setFocus(i)
				}
				f.fields[i].refused, refused = true, true
			}
		}
		if refused {
			return "", true
		}
		return k.Save, true
	case k.Back:
		return k.Back, false
	case k.Enter:
		switch field.Kind {
		case FieldChoice:
			f.open, f.pick = true, field.choice
		case FieldKey:
			f.capture = true
		case FieldList:
			return k.Enter, true
		case FieldDate:
			f.openCalendar()
		default:
			f.step(1)
		}
	case k.Toggle:
		if field.Kind != FieldMultiple {
			return "", false
		}
		field.ticked[field.cursor] = !field.ticked[field.cursor]
		f.changed(f.focus)
	case k.Up:
		return "", f.step(-1)
	case k.Prev:
		f.wrap(-1)
	case k.Down:
		f.step(1)
	case k.Next:
		f.wrap(1)
	case k.Left, k.Right:
		by := 1
		if id == k.Left {
			by = -1
		}
		switch field.Kind {
		case FieldChoice:
			field.choice = wrap(field.choice+by, len(field.Choices))
			f.changed(f.focus)
		case FieldMultiple:
			field.cursor = wrap(field.cursor+by, len(field.Choices))
		case FieldText, FieldMasked, FieldDate:
			field.line.move(field.line.cursor + by)
		default:
			return "", false
		}
	case k.First, k.Last:
		if !field.Kind.typed() {
			return "", false
		}
		if id == k.First {
			field.line.move(0)
		} else {
			field.line.move(len(field.line.runes))
		}
	case k.PageUp, k.PageDown:
		by := 1
		if id == k.PageUp {
			by = -1
		}
		return "", f.jumpSection(by)
	default:
		for _, b := range f.buttons {
			if b.Action == id {
				return id, true
			}
		}
		return "", false
	}
	return "", true
}

// choose gives the focused choice field its value j and closes the list.
func (f *Form) choose(j int) {
	f.open = false
	field := &f.fields[f.focus]
	if field.choice != j {
		field.choice = j
		f.changed(f.focus)
	}
}

// jumpSection moves the focus to the first field of the section by places on, opening it.
func (f *Form) jumpSection(by int) bool {
	if len(f.sections) < 2 {
		return false
	}
	at := 0
	for i, s := range f.sections {
		if s.ID == f.fields[f.focus].Section {
			at = i
		}
	}
	next := at + by
	if next < 0 || next >= len(f.sections) {
		return true
	}
	return f.enterSection(f.sections[next].ID)
}

// enterSection opens the section id and gives the focus to its first field that takes input.
func (f *Form) enterSection(id SectionID) bool {
	f.pinned[id] = true
	for i := range f.fields {
		if f.fields[i].Section == id && f.fields[i].Kind != FieldReadOnly {
			f.setFocus(i)
			return true
		}
	}
	return true
}

// Click handles a click on a target the form drew, as the MouseMap of its targets reports it. A click on a
// field gives it the focus and types nothing; on a choice field it also opens the list of values, and on a
// list field it is enter. A key field takes a key from the second click on, as enter does. A click on a tick
// box ticks its value, one on a value of the open list takes it, and one on a section goes to its first field.
// The calendar symbol of a date field gives the field the focus and opens its calendar, as enter does; a click
// on a day or a button of the open calendar does what its key does, and one anywhere else does nothing. A
// click on a button of the form gives its action, as its key does, and leaves the focus where it is.
func (f *Form) Click(r MouseResult) (act ActionID, handled bool) {
	if !r.Handled || f.capture {
		return "", false
	}
	if f.cal {
		return f.calendarClick(r)
	}
	switch {
	case strings.HasPrefix(r.Item, formFieldRef):
		i := f.index(FieldID(strings.TrimPrefix(r.Item, formFieldRef)))
		if i < 0 || !f.takes(i) {
			return "", false
		}
		again := f.fieldFocused(i)
		wasOpen := f.open
		f.setFocus(i)
		switch kind := f.fields[i].Kind; {
		case kind == FieldChoice && again && wasOpen:
			f.open = false
		case kind == FieldChoice, kind == FieldList, kind == FieldKey && again:
			return f.Act(f.keys.Enter)
		}
		return "", true
	case strings.HasPrefix(r.Item, formTickRef):
		id, value, _ := strings.Cut(strings.TrimPrefix(r.Item, formTickRef), "\x00")
		i := f.index(FieldID(id))
		if i < 0 || !f.takes(i) {
			return "", false
		}
		for j, c := range f.fields[i].Choices {
			if string(c.ID) == value {
				f.setFocus(i)
				f.fields[i].cursor = j
				return f.Act(f.keys.Toggle)
			}
		}
	case strings.HasPrefix(r.Item, formOptionRef):
		id, value, _ := strings.Cut(strings.TrimPrefix(r.Item, formOptionRef), "\x00")
		if !f.open || f.index(FieldID(id)) != f.focus {
			return "", false
		}
		for j, c := range f.fields[f.focus].Choices {
			if string(c.ID) == value {
				f.choose(j)
				return "", true
			}
		}
	case strings.HasPrefix(r.Item, formCalendarRef):
		i := f.index(FieldID(strings.TrimPrefix(r.Item, formCalendarRef)))
		if i < 0 || !f.takes(i) || f.fields[i].Kind != FieldDate {
			return "", false
		}
		f.setFocus(i)
		f.openCalendar()
		return "", true
	case strings.HasPrefix(r.Item, formButtonRef):
		j, err := strconv.Atoi(strings.TrimPrefix(r.Item, formButtonRef))
		if err != nil || j < 0 || j >= len(f.buttons) {
			return "", false
		}
		return f.Act(f.buttons[j].Action)
	case strings.HasPrefix(r.Item, formSectionRef):
		id := SectionID(strings.TrimPrefix(r.Item, formSectionRef))
		if _, ok := f.section(id); ok {
			return "", f.enterSection(id)
		}
	}
	return "", false
}

// Wheel moves the focus, or the cursor of an open list of values, by the notches of the wheel and stops at
// the ends.
func (f *Form) Wheel(steps int) {
	if f.open {
		f.pick = WheelStep(f.pick, len(f.fields[f.focus].Choices), steps)
		return
	}
	if f.cal {
		// A notch moves the day by a week, as down and up do.
		for range max(steps, -steps) {
			if steps > 0 {
				f.calendarAct(f.keys.Down)
			} else {
				f.calendarAct(f.keys.Up)
			}
		}
		return
	}
	if f.btn >= 0 {
		return
	}
	by := 1
	if steps < 0 {
		by, steps = -1, -steps
	}
	for range steps {
		f.step(by)
	}
}

// Hints are the keys of the focused field for the footer: what enter does there, toggle on a multiple
// choice, save, and the moves between fields; in an open list of values its moves, enter and back; while a
// key is taken, back.
func (f *Form) Hints() []Hint {
	k := f.keys
	nav := []Hint{{Action: k.Next, Label: TextKeyNext, Kind: HintNavigation}, {Action: k.Prev, Label: TextKeyPrevious, Kind: HintNavigation}}
	switch {
	case f.capture:
		return []Hint{{Action: k.Back, Label: TextKeyBack, Kind: HintAlways}}
	case f.cal:
		return []Hint{{Action: k.Enter, Label: TextKeyChoose}, {Action: k.Back, Label: TextKeyBack},
			{Action: k.PageUp, Label: TextCalPrevMonth}, {Action: k.PageDown, Label: TextCalNextMonth},
			{Action: k.First, Label: TextCalToday},
			{Action: k.Up, Label: TextKeyUp, Kind: HintNavigation}, {Action: k.Down, Label: TextKeyDown, Kind: HintNavigation},
			{Action: k.Left, Label: TextKeyLeft, Kind: HintNavigation}, {Action: k.Right, Label: TextKeyRight, Kind: HintNavigation}}
	case f.open:
		return []Hint{{Action: k.Enter, Label: TextKeyChoose}, {Action: k.Back, Label: TextKeyBack},
			{Action: k.Up, Label: TextKeyUp, Kind: HintNavigation}, {Action: k.Down, Label: TextKeyDown, Kind: HintNavigation}}
	}
	var hints []Hint
	if f.btn >= 0 {
		hints = append(hints, Hint{Action: k.Enter, Label: f.buttons[f.btn].Label})
	} else {
		hints = append(hints, f.fieldHints()...)
	}
	hints = append(hints, Hint{Action: k.Save, Label: TextKeySave, Kind: HintAlways})
	return append(hints, nav...)
}

// fieldHints is what enter or toggle does in the focused field.
func (f *Form) fieldHints() []Hint {
	k := f.keys
	var hints []Hint
	switch f.fields[f.focus].Kind {
	case FieldChoice:
		hints = append(hints, Hint{Action: k.Enter, Label: TextKeyChoose})
	case FieldMultiple:
		hints = append(hints, Hint{Action: k.Toggle, Label: TextKeyToggle})
	case FieldKey, FieldList:
		hints = append(hints, Hint{Action: k.Enter, Label: TextKeyEdit})
	case FieldDate:
		hints = append(hints, Hint{Action: k.Enter, Label: TextKeyCalendar})
	}
	return hints
}

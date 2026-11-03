package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
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
)

func (k FieldKind) known() bool {
	switch k {
	case FieldText, FieldChoice, FieldMultiple, FieldMasked, FieldKey, FieldList, FieldReadOnly:
		return true
	}
	return false
}

func (k FieldKind) typed() bool { return k == FieldText || k == FieldMasked }

// ChoiceID is the stable, untranslated ID of a value of a choice field.
type ChoiceID string

// Choice is one value of a choice field.
type Choice struct {
	ID    ChoiceID
	Label TextID
}

// FormValue is the value of a field: Text for text, masked, key, list and read-only fields, Choice for a
// choice and Selected for a multiple choice, in the order of the choices. The other members stay empty.
type FormValue struct {
	Text     string
	Choice   ChoiceID
	Selected []ChoiceID
}

func (v FormValue) clone() FormValue {
	v.Selected = slices.Clone(v.Selected)
	return v
}

func (v FormValue) equal(o FormValue) bool {
	return v.Text == o.Text && v.Choice == o.Choice && slices.Equal(v.Selected, o.Selected)
}

// FormField is one field of a form. Section names the section it stands in; Hint is the dimmed line under
// the focused field, and the note in the row of a read-only field. Stored says that a masked field has a
// value already, which an empty field keeps; the form is never handed that value.
type FormField struct {
	ID      string
	Section string
	Kind    FieldKind
	Label   TextID
	Value   FormValue
	Choices []Choice
	Hint    TextID
	Stored  bool
}

// FormSection is a section of a form. Lead is the line under its title where the sections stand as a list.
// An advanced section stays closed and dimmed while none of its fields holds a value or a problem, the focus
// is not in it and the person has not opened it.
type FormSection struct {
	ID       string
	Title    TextID
	Lead     TextID
	Advanced bool
}

// FormProblem is a problem of a refused save, with its cause and the next step; FieldID names the field it
// stands under, empty or unknown for a problem that stands above the form.
type FormProblem struct {
	FieldID  string
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

// FormSpec is the content of a form. The application supplies every text and checks the values when it
// saves; the form knows no schema.
type FormSpec struct {
	Fields   []FormField
	Sections []FormSection
	Keys     FormKeys
}

// The target items of a form start with a control character, which no ID holds.
const (
	formFieldRef   = "\x00F"
	formTickRef    = "\x00T"
	formOptionRef  = "\x00O"
	formSectionRef = "\x00S"
)

// formField is a field with what the form holds for it while it is edited.
type formField struct {
	FormField
	line    textLine
	choice  int
	cursor  int
	ticked  []bool
	initial FormValue
}

// FormModel is an open form: its fields, the focus, the values typed and chosen, an open list of values and
// the problems of the last refused save.
type FormModel struct {
	keys     FormKeys
	table    *Keymap
	text     Translator
	fields   []formField
	sections []FormSection

	focus    int
	pinned   map[string]bool
	problems map[string][]FormProblem
	other    []FormProblem
	open     bool
	pick     int
	capture  bool
}

// NewForm checks spec and returns the form with the focus on its first field that takes input. It rejects a
// form without a field that takes input, a field or section without ID or title, an ID with a control
// character or listed twice, an unknown kind, a label-less field, a value that does not fit its kind, a text
// with a control character, a masked field handed a value, a choice field without choices or with a value
// that is none of them, choices on another kind, a section that holds no field or that a field names without
// it being declared, a form that mixes fields with and without section, and a key that names no action of
// the key table or that two form keys share. The fields stand in the order of their sections.
func NewForm(spec FormSpec, keys *Keymap, text Translator) (*FormModel, error) {
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
	order := map[string]int{}
	for i, s := range spec.Sections {
		switch _, dup := order[s.ID]; {
		case badID(s.ID):
			errs = append(errs, fmt.Errorf("section %q: an ID is not empty and holds no control character", s.ID))
		case dup:
			errs = append(errs, fmt.Errorf("section %q is listed twice", s.ID))
		}
		order[s.ID] = i
		if s.Title == "" {
			errs = append(errs, fmt.Errorf("section %q has no title", s.ID))
		}
	}
	if len(spec.Fields) == 0 {
		errs = append(errs, errors.New("the form has no field"))
	}
	seen := map[string]bool{}
	filled := map[string]bool{}
	takes := false
	for _, f := range spec.Fields {
		switch {
		case badID(f.ID):
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
	f := &FormModel{keys: k, table: keys, text: text, sections: slices.Clone(spec.Sections),
		pinned: map[string]bool{}, problems: map[string][]FormProblem{}, focus: -1}
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
	case strings.IndexFunc(v.Text, unicode.IsControl) >= 0:
		fail("a one-line value holds no control character")
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

func (f *FormModel) index(id string) int {
	for i, field := range f.fields {
		if field.ID == id {
			return i
		}
	}
	return -1
}

func (f *FormModel) section(id string) (FormSection, bool) {
	for _, s := range f.sections {
		if s.ID == id {
			return s, true
		}
	}
	return FormSection{}, false
}

// closed reports whether the fields of a section are folded away.
func (f *FormModel) closed(id string) bool {
	s, ok := f.section(id)
	if !ok || !s.Advanced || f.pinned[id] {
		return false
	}
	for i := range f.fields {
		field := &f.fields[i]
		if field.Section == id && (i == f.focus || field.hasValue() || len(f.problems[field.ID]) > 0) {
			return false
		}
	}
	return true
}

// takes reports whether the field at i can have the focus.
func (f *FormModel) takes(i int) bool {
	return f.fields[i].Kind != FieldReadOnly && !f.closed(f.fields[i].Section)
}

func (f *FormModel) firstTaking() int {
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
func (f *FormModel) step(by int) bool {
	for i := f.focus + by; i >= 0 && i < len(f.fields); i += by {
		if f.takes(i) {
			f.setFocus(i)
			return true
		}
	}
	return false
}

func (f *FormModel) setFocus(i int) {
	if i != f.focus {
		f.open, f.capture = false, false
	}
	f.focus = i
}

// Focused is the ID of the field with the focus.
func (f *FormModel) Focused() string { return f.fields[f.focus].ID }

// Focus gives the focus to the field id and opens its section; false for a field that cannot take it.
func (f *FormModel) Focus(id string) bool {
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
func (f *FormModel) Input() InputFocus {
	switch {
	case f.capture:
		return InputKeyCapture
	case !f.open && f.fields[f.focus].Kind.typed():
		return InputText
	}
	return InputNavigation
}

// Capturing reports whether the key field with the focus waits for a key.
func (f *FormModel) Capturing() bool { return f.capture }

// Open reports whether the list of values of the focused choice field is open.
func (f *FormModel) Open() bool { return f.open }

// Values are the values of every field by ID, each a copy. A masked field holds what was typed; empty keeps
// a stored value.
func (f *FormModel) Values() map[string]FormValue {
	out := make(map[string]FormValue, len(f.fields))
	for i := range f.fields {
		out[f.fields[i].ID] = f.fields[i].value()
	}
	return out
}

// SetValue gives the field id a new value, such as the summary of a list after its editor, under the rules of
// NewForm, and retires the problems of the field.
func (f *FormModel) SetValue(id string, v FormValue) error {
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

// Changed reports whether a value differs from the one the form opened with, or a masked field was typed in;
// leaving a changed form asks first.
func (f *FormModel) Changed() bool {
	for i := range f.fields {
		if !f.fields[i].value().equal(f.fields[i].initial) {
			return true
		}
	}
	return false
}

// SetProblems shows the problems of a refused save: each under its field, one without a field above the
// form. A section with a problem opens, the focus goes to the first field with one, and a problem stays until
// its field changes. Every value stays as it was typed.
func (f *FormModel) SetProblems(problems []FormProblem) {
	f.problems, f.other = map[string][]FormProblem{}, nil
	first := -1
	for _, p := range problems {
		i := f.index(p.FieldID)
		if i < 0 {
			f.other = append(f.other, p)
			continue
		}
		f.problems[p.FieldID] = append(f.problems[p.FieldID], p)
		if first < 0 && f.fields[i].Kind != FieldReadOnly {
			first = i
		}
	}
	if first >= 0 {
		f.setFocus(first)
	}
}

func (f *FormModel) changed(i int) { delete(f.problems, f.fields[i].ID) }

// Update handles a key while the form has the focus. act is Keys.Save where the save key was pressed, from any
// field, and Keys.Enter where enter was pressed on a list field, whose editor the application opens. handled is
// false for a key the form leaves to the application: back, which leads to the leave question where Changed
// says so, up above the first field and tab past the ends, which lead to the next zone, and every key it does
// not read.
func (f *FormModel) Update(msg tea.KeyPressMsg) (act ActionID, handled bool) {
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
	if focus == InputText && f.fields[f.focus].line.edit(msg) {
		f.changed(f.focus)
		return "", true
	}
	return "", false
}

// Paste puts pasted text into the focused text or masked field at its cursor; line breaks and other control
// characters are dropped.
func (f *FormModel) Paste(text string) bool {
	field := &f.fields[f.focus]
	if f.open || f.capture || !field.Kind.typed() || !field.line.insert(text) {
		return false
	}
	f.changed(f.focus)
	return true
}

// Act runs one of the form keys as its key does, for a click or a command that stands for the key.
func (f *FormModel) Act(id ActionID) (act ActionID, handled bool) {
	k := f.keys
	if id == "" {
		return "", false
	}
	field := &f.fields[f.focus]
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
		return k.Save, true
	case k.Back:
		return "", false
	case k.Enter:
		switch field.Kind {
		case FieldChoice:
			f.open, f.pick = true, field.choice
		case FieldKey:
			f.capture = true
		case FieldList:
			return k.Enter, true
		default:
			f.step(1)
		}
	case k.Toggle:
		if field.Kind != FieldMultiple {
			return "", false
		}
		field.ticked[field.cursor] = !field.ticked[field.cursor]
		f.changed(f.focus)
	case k.Up, k.Prev:
		return "", f.step(-1)
	case k.Down:
		f.step(1)
	case k.Next:
		return "", f.step(1)
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
		case FieldText, FieldMasked:
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
		return "", false
	}
	return "", true
}

// choose gives the focused choice field its value j and closes the list.
func (f *FormModel) choose(j int) {
	f.open = false
	field := &f.fields[f.focus]
	if field.choice != j {
		field.choice = j
		f.changed(f.focus)
	}
}

// jumpSection moves the focus to the first field of the section by places on, opening it.
func (f *FormModel) jumpSection(by int) bool {
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
func (f *FormModel) enterSection(id string) bool {
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
func (f *FormModel) Click(r MouseResult) (act ActionID, handled bool) {
	if !r.Handled || f.capture {
		return "", false
	}
	switch {
	case strings.HasPrefix(r.Item, formFieldRef):
		i := f.index(strings.TrimPrefix(r.Item, formFieldRef))
		if i < 0 || !f.takes(i) {
			return "", false
		}
		again := i == f.focus
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
		i := f.index(id)
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
		if !f.open || f.index(id) != f.focus {
			return "", false
		}
		for j, c := range f.fields[f.focus].Choices {
			if string(c.ID) == value {
				f.choose(j)
				return "", true
			}
		}
	case strings.HasPrefix(r.Item, formSectionRef):
		id := strings.TrimPrefix(r.Item, formSectionRef)
		if _, ok := f.section(id); ok {
			return "", f.enterSection(id)
		}
	}
	return "", false
}

// Wheel moves the focus, or the cursor of an open list of values, by the notches of the wheel and stops at
// the ends.
func (f *FormModel) Wheel(steps int) {
	if f.open {
		f.pick = WheelStep(f.pick, len(f.fields[f.focus].Choices), steps)
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
func (f *FormModel) Hints() []Hint {
	k := f.keys
	nav := []Hint{{Action: k.Next, Label: TextKeyNext, Kind: HintNavigation}, {Action: k.Prev, Label: TextKeyPrevious, Kind: HintNavigation}}
	switch {
	case f.capture:
		return []Hint{{Action: k.Back, Label: TextKeyBack, Kind: HintAlways}}
	case f.open:
		return []Hint{{Action: k.Enter, Label: TextKeyChoose}, {Action: k.Back, Label: TextKeyBack},
			{Action: k.Up, Label: TextKeyUp, Kind: HintNavigation}, {Action: k.Down, Label: TextKeyDown, Kind: HintNavigation}}
	}
	var hints []Hint
	switch f.fields[f.focus].Kind {
	case FieldChoice:
		hints = append(hints, Hint{Action: k.Enter, Label: TextKeyChoose})
	case FieldMultiple:
		hints = append(hints, Hint{Action: k.Toggle, Label: TextKeyToggle})
	case FieldKey, FieldList:
		hints = append(hints, Hint{Action: k.Enter, Label: TextKeyEdit})
	}
	hints = append(hints, Hint{Action: k.Save, Label: TextKeySave, Kind: HintAlways})
	return append(hints, nav...)
}

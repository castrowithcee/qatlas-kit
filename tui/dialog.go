package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Answer is one answer of a dialog: its action and the word its button and its key show.
type Answer struct {
	Action ActionID
	Label  TextID
}

// DialogInput is the input line of a dialog, such as a name or a passphrase. A masked input never shows what
// is typed and starts empty.
type DialogInput struct {
	Masked bool
	Value  string
	// Suggest returns the suggestions for the typed text in the order shown, such as the paths that complete
	// it; none shows nothing. The dialog asks it whenever the text changes. A masked input has none.
	Suggest func(typed string) []Suggestion
}

// Suggestion is one entry under a dialog input. Text is data of the application: the dialog escapes it to
// draw it and cleans it as pasted text when it becomes the value.
type Suggestion struct {
	Text string
	// Continues keeps the dialog open when enter takes the suggestion and shows the suggestions of the new
	// text, such as a directory in a dialog that asks for a file.
	Continues bool
}

// DialogKeys are the actions of the key table a dialog reads beside its answers. Enter is the action of the
// enter key; a dialog with a default needs it.
type DialogKeys struct {
	Enter ActionID
	// Up, Down, PageUp and PageDown scroll the details where they do not fit, and stop at the ends; First and
	// Last jump there. All are optional; without them only the wheel scrolls. In a dialog with an input, the
	// four that scroll need a key that acts while text is typed, and First and Last stay with the text line.
	Up, Down, PageUp, PageDown, First, Last ActionID
	// Complete takes a suggestion, typically the tab action; SuggestUp and SuggestDown are second ways of Up
	// and Down that act only while suggestions are shown. An input with suggestions needs Complete, Up and
	// Down; each needs a key that acts while text is typed.
	Complete, SuggestUp, SuggestDown ActionID
}

// DialogLine is one detail line of a dialog: plain text with a style, optionally behind a prefix such as a
// prompt sign. A wrapped line continues under the text, not under the prefix. The dialog takes no styled
// text, so it can escape every character before it draws.
type DialogLine struct {
	// Prefix stands in front of the first row, in the style of the line.
	Prefix string
	Text   string
	// Style is StyleText, StyleHint, StyleWarn or StyleFail; the zero value is StyleText. StyleFail is only
	// for a Notice, since red in a question belongs to the confirming button of a destructive one.
	Style TextStyle
}

// DialogSpec is what a dialog asks. Question, Consequence and Details are plain text the application has
// translated, since they name its data; the dialog escapes them.
type DialogSpec struct {
	// Title stands in the frame of the dialog.
	Title TextID
	// Question is the question, bold behind the warning glyph; in a dialog with an input it is the hint and
	// carries no glyph. In a Notice it is the bold heading.
	Question string
	// Consequence is one sentence on what follows from the answer. A question without an input has one.
	Consequence string
	// Details are further lines between the consequence and the answers, such as the commands a question
	// would run.
	Details []DialogLine
	// Irreversible draws the consequence in the warn colour instead of dimmed: the answer cannot be undone.
	Irreversible bool
	// Notice makes the dialog a calm message headed by Question: no warning glyph, the consequence optional,
	// and at least one detail line, since a dialog that interrupts has something to read. It cannot carry an
	// input or a danger.
	Notice  bool
	Answers []Answer
	// Default is the answer the key Enter gives too; its button shows focused. Empty for a question that
	// enter does not answer.
	Default ActionID
	// Danger is the answer of a destructive question that destroys; its button is drawn in the fail colour.
	Danger ActionID
	// CloseOutside is the answer a click outside the frame gives, as Esc gives it, and so names that answer;
	// empty, the default, leaves the click to do nothing. A dialog with a Danger ignores the option, so a
	// click beside the question never answers it.
	CloseOutside ActionID
	Input        *DialogInput
	Keys         DialogKeys
	// Extra are hints of the application for the key line, such as the command line and the help. They stand
	// behind the answers in the order given and give way before them where the line is short, by Priority as
	// the other normal hints do. Each is a click target, but the dialog does not act on it: Update and Click
	// report it as not handled, so the application runs the action. Each needs an action that is no answer,
	// no enter or scroll action and listed once, and is a normal hint. An extra whose keys would be typed text
	// in a dialog with an input is not shown.
	Extra []Hint
}

// The smallest and the widest text of a dialog drawn over the screen.
const (
	dialogMinInner = 36
	dialogMaxInner = 60
	// dialogMinDetails is the fewest detail lines a dialog over the screen keeps before it gives way to View.
	dialogMinDetails = 3
)

// buttonGap stands between two buttons of a row.
const buttonGap = "  "

// dialogInputRef is the target item of the input line of a dialog.
const dialogInputRef = "\x00I"

// Dialog is an open dialog: a question answered by keys or buttons, or a short input. The answers are
// the only thing it does; every other key goes back to the application, which keeps the screen behind the
// dialog from taking it.
type Dialog struct {
	spec  DialogSpec
	keys  *Keymap
	text  Translator
	input suggestLine
	msg   *Message
	// scroll is the first shown detail line and what the last drawing found.
	scroll textScroll
	// doc is the text of the question, the consequence and the details, for the selection with the mouse.
	doc selDoc
}

// NewDialog checks spec and returns the open dialog. It rejects a dialog without question, a question without
// consequence unless it is a notice, a notice without detail line, with an input or with a danger, a detail line
// in a style other than text, hint, warn or fail, a detail line in fail outside a notice, a dialog without
// answers, an answer or key that names no action of keys or a label-less answer, an answer listed twice, a
// default or danger that is no answer, a default without the enter action or with an enter key that is another
// answer, a danger that is the default, so enter never destroys, an answer of an input dialog that has no key
// outside the typed text, a scroll key that names no action, is listed twice or is an answer or the enter key,
// a scroll key of an input dialog that has no key outside the typed text, a masked input that starts with a
// value or has suggestions, an input with suggestions without the complete, up or down action, a complete or
// suggestion key that names no action, is listed twice, is an answer or the enter key or has no key outside the
// typed text in an input dialog, and an extra hint without action, of another kind than normal, that names an
// unknown action, lists one twice or names an answer, the enter action, a scroll or a suggestion action.
func NewDialog(spec DialogSpec, keys *Keymap, text Translator) (*Dialog, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the dialog needs a key table and a translator")
	}
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("dialog: "+format, args...)) }
	if strings.TrimSpace(spec.Question) == "" {
		if spec.Notice {
			fail("a notice needs a heading in Question")
		} else {
			fail("no question")
		}
	}
	if spec.Input == nil && !spec.Notice && strings.TrimSpace(spec.Consequence) == "" {
		fail("a question says what follows from its answer")
	}
	if spec.Notice {
		switch {
		case len(spec.Details) == 0:
			fail("a notice needs a detail line, else it belongs in the message line")
		case spec.Danger != "":
			fail("a notice destroys nothing and has no danger")
		case spec.Input != nil:
			fail("a notice has no input")
		}
	}
	for i, l := range spec.Details {
		switch l.Style {
		case StyleText, StyleHint, StyleWarn:
		case StyleFail:
			if !spec.Notice {
				fail("detail %d is red, which a question keeps for the button of a destructive answer", i)
			}
		default:
			fail("detail %d has a style other than text, hint, warn or fail", i)
		}
	}
	if len(spec.Answers) == 0 {
		fail("no answer")
	}
	focus := InputNavigation
	if spec.Input != nil {
		focus = InputText
	}
	var answers []ActionID
	for _, a := range spec.Answers {
		switch {
		case !keys.known(a.Action):
			fail("answer %q is no action of the key table", a.Action)
		case slices.Contains(answers, a.Action):
			fail("answer %q is listed twice", a.Action)
		case focus == InputText && len(keys.keysUnder(keys.index[a.Action], InputText)) == 0:
			fail("answer %q has no key that acts while text is typed", a.Action)
		}
		if a.Label == "" {
			fail("answer %q has no label", a.Action)
		}
		answers = append(answers, a.Action)
	}
	if spec.Default != "" {
		switch {
		case !slices.Contains(answers, spec.Default):
			fail("default %q is no answer", spec.Default)
		case !keys.known(spec.Keys.Enter):
			fail("a default needs the enter action, %q is no action of the key table", spec.Keys.Enter)
		case spec.Keys.Enter != spec.Default && slices.Contains(answers, spec.Keys.Enter):
			fail("enter %q answers the dialog besides the default %q", spec.Keys.Enter, spec.Default)
		}
	}
	if spec.Danger != "" {
		switch {
		case !slices.Contains(answers, spec.Danger):
			fail("danger %q is no answer", spec.Danger)
		case spec.Danger == spec.Default:
			fail("danger %q is the default; enter never destroys", spec.Danger)
		}
	}
	if spec.CloseOutside != "" && !slices.Contains(answers, spec.CloseOutside) {
		fail("close outside %q is no answer", spec.CloseOutside)
	}
	suggests := spec.Input != nil && spec.Input.Suggest != nil
	if spec.Input != nil && spec.Input.Masked {
		if spec.Input.Value != "" {
			fail("a masked input starts empty")
		}
		if suggests {
			fail("a masked input has no suggestions, which would show what it hides")
		}
	}
	k := spec.Keys
	scrollKeys := []namedKey{{"up", k.Up, suggests}, {"down", k.Down, suggests}, {"page up", k.PageUp, false},
		{"page down", k.PageDown, false}, {"first", k.First, false}, {"last", k.Last, false}}
	suggestKeys := []namedKey{{"complete", k.Complete, suggests}, {"suggestion up", k.SuggestUp, false},
		{"suggestion down", k.SuggestDown, false}}
	errs = append(errs, checkKeys("dialog", keys, append(slices.Clone(scrollKeys), suggestKeys...))...)
	for _, sk := range append(slices.Clone(scrollKeys), suggestKeys...) {
		// First and last stay with the text line, so only they need no key that acts while text is typed.
		lineEnd := sk.name == "first" || sk.name == "last"
		switch {
		case sk.id == "" || !keys.known(sk.id):
		case slices.Contains(answers, sk.id) || sk.id == k.Enter:
			fail("%s key %q is also an answer or the enter key", sk.name, sk.id)
		case focus == InputText && !lineEnd && len(keys.keysUnder(keys.index[sk.id], InputText)) == 0:
			fail("%s key %q has no key that acts while text is typed", sk.name, sk.id)
		}
	}
	var extras []ActionID
	for i, h := range spec.Extra {
		switch {
		case h.Action == "":
			fail("extra hint %d has no action; it is a click target", i)
		case !keys.known(h.Action):
			fail("extra hint %q is no action of the key table", h.Action)
		case slices.Contains(extras, h.Action):
			fail("extra hint %q is listed twice", h.Action)
		case slices.Contains(answers, h.Action):
			fail("extra hint %q is also an answer", h.Action)
		case h.Action == k.Enter || slices.ContainsFunc(scrollKeys, func(sk namedKey) bool { return sk.id == h.Action }):
			// Update would run these itself and report them handled.
			fail("extra hint %q is also the enter action or a scroll action", h.Action)
		case slices.ContainsFunc(suggestKeys, func(sk namedKey) bool { return sk.id == h.Action }):
			fail("extra hint %q is also a suggestion action", h.Action)
		case h.Kind != HintNormal || h.WhileTyping:
			fail("extra hint %q is a normal hint that names no text keys", h.Action)
		}
		extras = append(extras, h.Action)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	spec.Extra = slices.Clone(spec.Extra)
	spec.Answers = slices.Clone(spec.Answers)
	spec.Details = slices.Clone(spec.Details)
	d := &Dialog{spec: spec, keys: keys, text: text}
	if spec.Input != nil {
		in := *spec.Input
		d.spec.Input = &in
		d.input = newSuggestLine(in.Value, in.Suggest)
	}
	return d, nil
}

// known reports whether id is an action of the table this view sees.
func (k *Keymap) known(id ActionID) bool {
	i, ok := k.index[id]
	return ok && k.visible(i)
}

// otherPage reports whether id is an action of the table that only another page sees.
func (k *Keymap) otherPage(id ActionID) bool {
	i, ok := k.index[id]
	return ok && !k.visible(i)
}

func (d *Dialog) focus() InputFocus {
	if d.spec.Input != nil {
		return InputText
	}
	return InputNavigation
}

// Input is the input focus the dialog reads keys in: text in a dialog with an input, navigation otherwise.
func (d *Dialog) Input() InputFocus { return d.focus() }

// Update handles a key. answer is the answer the key gives, the default for enter; handled is false for a key
// that neither answers nor edits the input, which the application handles or drops, but never passes to the
// screen behind the dialog. While suggestions are shown, enter after a suggestion was marked takes it first:
// one that continues keeps the dialog open, any other gives the answer to enter with it as the value.
func (d *Dialog) Update(msg tea.KeyPressMsg) (answer ActionID, handled bool) {
	focus := d.focus()
	k := d.spec.Keys
	if d.input.marked && d.keys.Is(msg, k.Enter, focus) {
		if enter := d.enterAnswer(); enter != "" {
			chosen := d.input.take(d.input.mark)
			d.clearError()
			if chosen.Continues {
				return "", true
			}
			return enter, true
		}
	}
	for _, a := range d.spec.Answers {
		if d.keys.Is(msg, a.Action, focus) {
			return a.Action, true
		}
	}
	if d.spec.Default != "" && d.keys.Is(msg, k.Enter, focus) {
		return d.spec.Default, true
	}
	for _, id := range d.suggestKeys() {
		if d.keys.Is(msg, id, focus) {
			if !d.input.shown() {
				return "", false
			}
			d.suggestBy(id)
			return "", true
		}
	}
	for _, id := range d.scrollKeys() {
		if d.keys.Is(msg, id, focus) {
			d.scrollBy(id)
			return "", true
		}
	}
	if d.spec.Input != nil {
		handled, changed := d.input.edit(msg, d.spec.Input.Masked)
		if changed {
			d.clearError()
		}
		return "", handled
	}
	return "", false
}

// enterAnswer is the answer enter gives, or "" where enter answers nothing.
func (d *Dialog) enterAnswer() ActionID {
	k := d.spec.Keys
	switch {
	case d.spec.Default != "":
		return d.spec.Default
	case k.Enter != "" && slices.ContainsFunc(d.spec.Answers, func(a Answer) bool { return a.Action == k.Enter }):
		return k.Enter
	}
	return ""
}

// suggestKeys are the actions that act on the suggestions: up and down only while suggestions are shown, as
// they scroll otherwise; complete and the second ways always, so Update reports them as not handled then.
func (d *Dialog) suggestKeys() []ActionID {
	k := d.spec.Keys
	ids := []ActionID{k.Complete, k.SuggestUp, k.SuggestDown}
	if d.input.shown() {
		ids = append(ids, k.Up, k.Down)
	}
	return slices.DeleteFunc(ids, func(id ActionID) bool { return id == "" })
}

// suggestBy runs a suggestion action.
func (d *Dialog) suggestBy(id ActionID) {
	k := d.spec.Keys
	switch id {
	case k.Complete:
		d.input.complete()
		d.clearError()
	case k.Up, k.SuggestUp:
		d.input.move(-1)
	case k.Down, k.SuggestDown:
		d.input.move(1)
	}
}

// scrollKeys are the scroll actions the dialog acts on. A dialog with an input leaves first and last to the
// text line.
func (d *Dialog) scrollKeys() []ActionID {
	k := d.spec.Keys
	ids := []ActionID{k.Up, k.Down, k.PageUp, k.PageDown}
	if d.spec.Input == nil {
		ids = append(ids, k.First, k.Last)
	}
	return slices.DeleteFunc(ids, func(id ActionID) bool { return id == "" })
}

// scrollBy runs a scroll action on the details.
func (d *Dialog) scrollBy(id ActionID) {
	k := d.spec.Keys
	switch id {
	case k.Up:
		d.scroll.by(-1)
	case k.Down:
		d.scroll.by(1)
	case k.PageUp:
		d.scroll.by(-d.scroll.step())
	case k.PageDown:
		d.scroll.by(d.scroll.step())
	case k.First:
		d.scroll.offset = 0
	case k.Last:
		d.scroll.offset = d.scroll.last
	}
}

// Wheel scrolls the details by the notches of the wheel, WheelTextLines a notch, and stops at their ends. The
// application passes it a notch over the dialog, which reports ZoneDetail while the details scroll.
func (d *Dialog) Wheel(steps int) { d.scroll.by(steps * WheelTextLines) }

// Paste puts pasted text into the input at its cursor, cleaned as a text line cleans it.
func (d *Dialog) Paste(text string) bool {
	if d.spec.Input == nil || !d.input.insert(text) {
		return false
	}
	d.clearError()
	return true
}

func (d *Dialog) clearError() {
	if d.msg != nil && d.msg.Kind == MessageError {
		d.msg = nil
	}
}

// Click handles a click on a target the dialog drew, as the MouseMap of its targets reports it: a button or
// a key of the key line gives its answer, as its key does, and a click on the input does nothing more than
// keep it. A click on a suggestion marks it, and one on the marked suggestion takes it as the complete key
// does. A click on an extra hint is not handled, as its key is not: r.Action is the action for the
// application to run.
func (d *Dialog) Click(r MouseResult) (answer ActionID, handled bool) {
	if !r.Handled {
		return "", false
	}
	if r.Zone == ZoneOutside {
		return d.outside(), d.outside() != ""
	}
	if r.Item == dialogInputRef && d.spec.Input != nil {
		return "", true
	}
	if i, ok := d.input.suggestionOf(r.Item); ok {
		if d.input.marked && i == d.input.mark {
			d.input.take(i)
			d.clearError()
		} else {
			d.input.markAt(i)
		}
		return "", true
	}
	if r.Action != "" && d.input.shown() && slices.Contains(d.suggestKeys(), r.Action) {
		d.suggestBy(r.Action)
		return "", true
	}
	for _, a := range d.spec.Answers {
		if r.Action == a.Action {
			return a.Action, true
		}
	}
	if d.spec.Default != "" && r.Action == d.spec.Keys.Enter {
		return d.spec.Default, true
	}
	if r.Action != "" && slices.Contains(d.scrollKeys(), r.Action) {
		d.scrollBy(r.Action)
		return "", true
	}
	return "", false
}

// outside is the answer of a click outside the frame, empty where the dialog gives none.
func (d *Dialog) outside() ActionID {
	if d.spec.Danger != "" {
		return ""
	}
	return d.spec.CloseOutside
}

// Value is the text of the input.
func (d *Dialog) Value() string { return d.input.String() }

// ClearInput empties the input, such as after a wrong passphrase.
func (d *Dialog) ClearInput() { d.input.clear() }

// SetMessage shows what is still running or the last error inside the dialog; nil removes it. Typing removes
// an error.
func (d *Dialog) SetMessage(msg *Message) {
	if msg == nil {
		d.msg = nil
		return
	}
	m := *msg
	d.msg = &m
}

// Hints are the answers as hints of the key line, the enter key beside the default, the extra hints of the
// application and the scroll keys while the last drawing scrolled the details. While suggestions are shown,
// the complete key and up and down, which mark then instead of scrolling a line, stand before the answers.
// The extra and the scroll hints give way by their priority where the line is short; an answer is shortened
// and never dropped. The extra hints stand before the scroll hints, so they keep their place while the scroll
// hints come and go. A dialog drawn over the screen shows them itself; where it does not fit, the application
// puts them into the footer, whose targets then answer it.
func (d *Dialog) Hints() []Hint {
	var hints []Hint
	suggests := d.input.shown()
	if suggests {
		k := d.spec.Keys
		hints = append(hints, Hint{Action: k.Complete, Label: TextKeyComplete},
			Hint{Action: k.Up, Label: TextKeyMark}, Hint{Action: k.Down, Label: TextKeyMark})
	}
	for _, a := range d.spec.Answers {
		hints = append(hints, Hint{Action: a.Action, Label: a.Label, Kind: HintAnswer})
		if a.Action == d.spec.Default && d.spec.Keys.Enter != d.spec.Default {
			hints = append(hints, Hint{Action: d.spec.Keys.Enter, Label: a.Label, Kind: HintAnswer})
		}
	}
	hints = append(hints, d.spec.Extra...)
	if d.scroll.last > 0 {
		k := d.spec.Keys
		for _, h := range []struct {
			id       ActionID
			label    TextID
			priority int
		}{{k.Up, TextHelpScroll, 1}, {k.Down, TextHelpScroll, 1}, {k.PageUp, TextHelpPage, 2}, {k.PageDown, TextHelpPage, 2}} {
			if h.id != "" && !(suggests && h.label == TextHelpScroll) {
				hints = append(hints, Hint{Action: h.id, Label: h.label, Priority: h.priority})
			}
		}
		if d.spec.Input == nil {
			if k.First != "" {
				hints = append(hints, Hint{Action: k.First, Label: TextKeyFirst, Priority: 3})
			}
			if k.Last != "" {
				hints = append(hints, Hint{Action: k.Last, Label: TextKeyLast, Priority: 3})
			}
		}
	}
	return hints
}

// redact replaces the text of a masked input wherever a text of the dialog repeats it.
func (d *Dialog) redact(text string) string {
	if d.spec.Input == nil || !d.spec.Input.Masked {
		return text
	}
	return redactSecret(text, d.input.String())
}

// minRedacted is the shortest secret replaced in other texts: replacing a shorter one would garble the text
// and show where the secret's characters stand.
const minRedacted = 4

// redactSecret replaces secret in text by a mask of fixed length.
func redactSecret(text, secret string) string {
	if len([]rune(secret)) < minRedacted {
		return text
	}
	return strings.ReplaceAll(text, secret, "***")
}

// dialogParts is the content above the buttons in width cells: what stands above the details, the details
// and what stands below them, with the targets of the input line and its suggestions counted from the top left
// of the tail.
type dialogParts struct {
	head, details, tail []string
	tailTargets         []Target
	// headSel and detailSel are the selection rows of head and details.
	headSel, detailSel []selRow
}

// parts draws the content above the buttons in width cells.
func (d *Dialog) parts(width int, styles Styles) dialogParts {
	var lines, details []string
	var targets []Target
	var sels []selRow
	d.doc.reset("")
	blank := func() {
		lines = append(lines, "")
		sels = append(sels, selRow{line: holeLine})
	}
	wrapped := func(style TextStyle, text, lead string) {
		indent := strings.Repeat(" ", lipgloss.Width(lead))
		escaped := Escape(d.redact(text))
		pieces := Wrap(escaped, max(width-lipgloss.Width(lead), 1))
		rows := d.doc.para(escaped, pieces, lipgloss.Width(lead), false)
		for i, l := range pieces {
			prefix := indent
			if i == 0 {
				prefix = lead
			}
			lines = append(lines, d.doc.mark(prefix+styles.Render(style, l), rows[i], styles))
		}
		sels = append(sels, rows...)
	}
	consequence := StyleHint
	if d.spec.Irreversible {
		consequence = StyleWarn
	}
	switch {
	case d.spec.Notice:
		wrapped(StyleTitle, d.spec.Question, "")
		blank()
		if strings.TrimSpace(d.spec.Consequence) != "" {
			wrapped(consequence, d.spec.Consequence, "")
		}
	case d.spec.Input == nil:
		wrapped(StyleTitle, d.spec.Question, styles.Render(StyleWarn, styles.Glyph(GlyphWarning))+" ")
		blank()
		wrapped(consequence, d.spec.Consequence, "")
	default:
		wrapped(StyleText, d.spec.Question, "")
		if d.spec.Consequence != "" {
			wrapped(consequence, d.spec.Consequence, "")
		}
	}
	head, headSel := lines, sels
	lines, sels = nil, nil
	for _, detail := range d.spec.Details {
		// Escape and measure the prefix before wrapping, so the continuation stands under the text as drawn.
		lead := Escape(d.redact(detail.Prefix))
		indent := strings.Repeat(" ", lipgloss.Width(lead))
		escaped := Escape(d.redact(detail.Text))
		pieces := wrapWords(escaped, max(width-lipgloss.Width(lead), 1))
		rows := d.doc.para(escaped, pieces, lipgloss.Width(lead), false)
		for i, l := range pieces {
			row := indent
			if i == 0 {
				row = styles.Render(detail.Style, lead)
			}
			lines = append(lines, d.doc.mark(row+styles.Render(detail.Style, l), rows[i], styles))
		}
		sels = append(sels, rows...)
	}
	details, lines = lines, nil
	detailSel := sels
	if d.spec.Input != nil {
		mask := ""
		if d.spec.Input.Masked {
			mask = styles.Glyph(GlyphMask)
		}
		lines = append(lines, "")
		targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 1}, Zone: ZoneForm, Item: dialogInputRef})
		lines = append(lines, fieldStyle(styles, false).Render(d.input.view(width, styles, true, mask)))
		rows, rt := d.input.rows(width, styles)
		targets = append(targets, OffsetTargets(rt, 0, len(lines))...)
		lines = append(lines, rows...)
	}
	if d.msg != nil && d.msg.Text != "" {
		lines = append(lines, "")
		lines = append(lines, strings.Split(styles.Message(d.msg.Kind, d.redact(d.msg.Text), width), "\n")...)
	}
	return dialogParts{head: head, details: details, tail: lines, tailTargets: targets, headSel: headSel, detailSel: detailSel}
}

// compose joins the parts with the details cut to room rows: where they do not fit, they scroll and a position
// line stands under them, which takes one of the rows. A room below zero shows every detail. scrolls reports
// that the details scroll. sel is the selection target over the question, the consequence and the shown details;
// the caller places it after its other targets.
func (d *Dialog) compose(p dialogParts, room, width int, styles Styles) (lines []string, targets []Target, scrolls bool, sel Target) {
	details, detailSel := p.details, p.detailSel
	if room >= 0 {
		start, end, position := d.scroll.window(len(details), room, d.text)
		details, detailSel = slices.Clone(details[start:end]), p.detailSel[start:end]
		if position != "" {
			details = append(details, styles.Render(StyleHint, styles.Truncate(Escape(position), width)))
		}
		scrolls = d.scroll.last > 0
	}
	lines = append(append(slices.Clone(p.head), details...), p.tail...)
	rows := append(slices.Clone(p.headSel), detailSel...)
	sel = d.doc.target(rows, Rect{Width: width, Height: len(rows)}, "")
	return lines, OffsetTargets(p.tailTargets, 0, len(p.head)+len(details)), scrolls, sel
}

// body is the content above the buttons in width cells with every detail, and the target of the input line,
// counted from its top left.
func (d *Dialog) body(width int, styles Styles) ([]string, []Target) {
	lines, targets, _, _ := d.compose(d.parts(width, styles), -1, width, styles)
	return lines, targets
}

// wrapWords breaks text into lines of at most width cells at spaces only, unlike Wrap, which also breaks at
// hyphens and would alter a command. A word wider than width is split by cell width, so nothing is cut away.
func wrapWords(text string, width int) []string {
	width = max(width, 1)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) <= width {
			out = append(out, line)
			continue
		}
		cur := ""
		for _, word := range strings.Fields(line) {
			if cur != "" && lipgloss.Width(cur)+1+lipgloss.Width(word) <= width {
				cur += " " + word
				continue
			}
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			if lipgloss.Width(word) <= width {
				cur = word
				continue
			}
			parts := strings.Split(ansi.Hardwrap(word, width, true), "\n")
			out = append(out, parts[:len(parts)-1]...)
			cur = parts[len(parts)-1]
		}
		out = append(out, cur)
	}
	return out
}

// buttonCell is a drawn button with the target that stands for it; flowButtons sets the place of the target.
type buttonCell struct {
	drawn  string
	target Target
}

// flowButtons lays the buttons out in rows of at most width cells and returns a target per button, counted
// from the top left of the rows.
func flowButtons(cells []buttonCell, width int) ([]string, []Target) {
	var rows []string
	var targets []Target
	line, x := "", 0
	for _, c := range cells {
		w := lipgloss.Width(c.drawn)
		if x > 0 && x+len(buttonGap)+w > width {
			rows, line, x = append(rows, line), "", 0
		}
		if x > 0 {
			line += buttonGap
			x += len(buttonGap)
		}
		if r, ok := clipRect(Rect{X: x, Y: len(rows), Width: w, Height: 1}, width); ok {
			t := c.target
			t.Rect = r
			targets = append(targets, t)
		}
		line += c.drawn
		x += w
	}
	if line != "" {
		rows = append(rows, line)
	}
	return rows, targets
}

// buttons draws one button per answer label in rows of width cells, the default behind the enter key and the
// danger in the fail colour, none focused since a dialog has no focus to move, and returns a target per button,
// counted from the top left of the rows.
func (d *Dialog) buttons(width int, styles Styles) ([]string, []Target) {
	var cells []buttonCell
	seen := map[string]bool{}
	for _, a := range d.spec.Answers {
		label := d.text.Lookup(a.Label)
		if seen[label] {
			// Answers that read the same, such as n and esc that both keep, share the button of the first.
			continue
		}
		seen[label] = true
		kind := ButtonNormal
		switch a.Action {
		case d.spec.Danger:
			kind = ButtonDanger
		case d.spec.Default:
			kind = ButtonPrimary
			// The key stands in the text so the default reads without colour and apart from a focus.
			if key := d.keys.Label(d.spec.Keys.Enter); key != "" {
				label = key + " " + label
			}
		}
		cells = append(cells, buttonCell{styles.Button(kind, label, false), Target{Action: a.Action}})
	}
	return flowButtons(cells, width)
}

// content is the whole dialog in width cells: the body with the details cut to room rows, the buttons and the
// key line. A scrolling dialog also has a target over all of it, which the wheel reports as ZoneDetail.
func (d *Dialog) content(width int, styles Styles, p dialogParts, room int) ([]string, []Target) {
	lines, targets, scrolls, sel := d.compose(p, room, width, styles)
	lines = append(lines, "")
	rows, bt := d.buttons(width, styles)
	targets = append(targets, OffsetTargets(bt, 0, len(lines))...)
	lines = append(lines, rows...)
	keyLine, kt := styles.footer(d.keys.Hints(d.Hints(), d.focus(), d.text), "", width)
	targets = append(targets, OffsetTargets(kt, 0, len(lines))...)
	lines = append(lines, keyLine)
	for i, l := range lines {
		lines[i] = Pad(Clip(l, width), width)
	}
	if scrolls {
		targets = append(targets, Target{Rect: Rect{Width: width, Height: len(lines)}, Zone: ZoneDetail})
	}
	return lines, append(targets, sel)
}

// Overlay draws the dialog with styles over screen, the drawn screen of width by height cells: the screen
// dimmed, without its colours, and the dialog in its frame centred in room, the part of the screen it may
// cover, such as the workspace of the frame, so the header and the tab line stay in view. The targets are the
// dialog's alone, so the screen behind it takes no click. ok is false where room is too small for the dialog;
// the application then draws View in the workspace and Hints in the footer. Where the details do not fit,
// the question, the consequence, the input, the buttons and the key line stay and the details scroll in the
// rest, which holds at least dialogMinDetails lines and the position line under them; ok is false only where
// even that does not fit. The suggestions under the input stay as the input does, so the box grows and
// shrinks as they come and go.
func (d *Dialog) Overlay(styles Styles, screen string, width, height int, room Rect) (view string, targets []Target, ok bool) {
	inner := min(dialogMaxInner, room.Width-8)
	if inner < dialogMinInner {
		return "", nil, false
	}
	p := d.parts(inner, styles)
	rows, _ := d.buttons(inner, styles)
	avail := room.Height - 2 - len(p.head) - len(p.tail) - 1 - len(rows) - 1
	if len(p.details) > avail && avail < dialogMinDetails+1 {
		return "", nil, false
	}
	lines, ts := d.content(inner, styles, p, avail)
	box := styles.Panel(d.text.Lookup(d.spec.Title), "", lines, inner+4, len(lines)+2, true)
	if len(box) > room.Height {
		return "", nil, false
	}
	top := room.Y + (room.Height-len(box))/2
	left := room.X + (room.Width-inner-4)/2
	targets = withOutside(d.outside() != "", OffsetTargets(ts, left+2, top+1), width, height, Rect{X: left, Y: top, Width: inner + 4, Height: len(box)})
	return overlay(screen, box, width, height, top, left, styles), targets, true
}

// View draws the dialog with styles as the content of a workspace of width by height cells, for a terminal
// too small to draw it over the screen: the body without buttons and key line, which stand in the footer.
// Details that do not fit scroll between the question and what follows them, as in Overlay.
func (d *Dialog) View(styles Styles, width, height int) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if height == 0 {
		return "", nil
	}
	inner := max(width-2, 1)
	p := d.parts(inner, styles)
	room := height - len(p.head) - len(p.tail)
	if room < 2 {
		room = -1
	}
	lines, targets, scrolls, sel := d.compose(p, room, inner, styles)
	out := make([]string, height)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = "  " + lines[i]
		}
		out[i] = Pad(Clip(line, width), width)
	}
	var kept []Target
	for _, t := range OffsetTargets(targets, 2, 0) {
		if t.Rect.Y < height {
			kept = append(kept, t)
		}
	}
	if scrolls {
		kept = append(kept, Target{Rect: Rect{Width: width, Height: height}, Zone: ZoneDetail})
	}
	if t := OffsetTargets([]Target{sel}, 2, 0)[0]; t.Rect.Y < height {
		t.Rect.Height = min(t.Rect.Height, height-t.Rect.Y)
		kept = append(kept, t)
	}
	return strings.Join(out, "\n"), kept
}

// overlay draws box at top, left over screen, cut or padded to width by height cells, with the rest of the
// screen dimmed and stripped of its colours, so it stays readable and only the box stands out.
func overlay(screen string, box []string, width, height, top, left int, styles Styles) string {
	rows := strings.Split(screen, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	rows = rows[:height]
	boxWidth := 0
	for _, line := range box {
		boxWidth = max(boxWidth, ansi.StringWidth(line))
	}
	dim := func(text string) string {
		if text == "" {
			return ""
		}
		return styles.Render(StyleHint, text)
	}
	for i, row := range rows {
		plain := Pad(ansi.Strip(row), width)
		if i < top || i >= top+len(box) {
			rows[i] = dim(ansi.Cut(plain, 0, width))
			continue
		}
		rows[i] = dim(ansi.Cut(plain, 0, left)) + box[i-top] + dim(ansi.Cut(plain, left+boxWidth, width))
	}
	return strings.Join(rows, "\n")
}

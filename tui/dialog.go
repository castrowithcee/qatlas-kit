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
}

// DialogSpec is what a dialog asks. Question, Consequence and Details are plain text the application has
// translated, since they name its data; the dialog escapes them.
type DialogSpec struct {
	// Title stands in the frame of the dialog.
	Title TextID
	// Question is the question, bold behind the warning glyph; in a dialog with an input it is the hint and
	// carries no glyph.
	Question string
	// Consequence is one sentence on what follows from the answer. A question without an input has one.
	Consequence string
	// Details are further lines between the consequence and the answers.
	Details []string
	Answers []Answer
	// Default is the answer the key Enter gives too; its button shows focused. Empty for a question that
	// enter does not answer.
	Default ActionID
	// Danger is the answer of a destructive question that destroys; its button is drawn in the fail colour.
	Danger ActionID
	// Enter is the action of the enter key; a dialog with a default needs it.
	Enter ActionID
	Input *DialogInput
}

// The smallest and the widest text of a dialog drawn over the screen.
const (
	dialogMinInner = 36
	dialogMaxInner = 60
)

// buttonGap stands between two buttons of a row.
const buttonGap = "  "

// dialogInputRef is the target item of the input line of a dialog.
const dialogInputRef = "\x00I"

// DialogModel is an open dialog: a question answered by keys or buttons, or a short input. The answers are
// the only thing it does; every other key goes back to the application, which keeps the screen behind the
// dialog from taking it.
type DialogModel struct {
	spec  DialogSpec
	keys  *Keymap
	text  Translator
	input textLine
	msg   *Message
}

// NewDialog checks spec and returns the open dialog. It rejects a dialog without question, a question without
// consequence, a dialog without answers, an answer or key that names no action of keys or a label-less answer,
// an answer listed twice, a default or danger that is no answer, a default without the enter action or with an
// enter key that is another answer, a danger that is the default, so enter never destroys, an answer of an
// input dialog that has no key outside the typed text, and a masked input that starts with a value.
func NewDialog(spec DialogSpec, keys *Keymap, text Translator) (*DialogModel, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the dialog needs a key table and a translator")
	}
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("dialog: "+format, args...)) }
	if strings.TrimSpace(spec.Question) == "" {
		fail("no question")
	}
	if spec.Input == nil && strings.TrimSpace(spec.Consequence) == "" {
		fail("a question says what follows from its answer")
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
		case !keys.known(spec.Enter):
			fail("a default needs the enter action, %q is no action of the key table", spec.Enter)
		case spec.Enter != spec.Default && slices.Contains(answers, spec.Enter):
			fail("enter %q answers the dialog besides the default %q", spec.Enter, spec.Default)
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
	if spec.Input != nil && spec.Input.Masked && spec.Input.Value != "" {
		fail("a masked input starts empty")
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	spec.Answers = slices.Clone(spec.Answers)
	spec.Details = slices.Clone(spec.Details)
	d := &DialogModel{spec: spec, keys: keys, text: text}
	if spec.Input != nil {
		in := *spec.Input
		d.spec.Input = &in
		d.input = newTextLine(in.Value)
	}
	return d, nil
}

// known reports whether id is an action of the table.
func (k *Keymap) known(id ActionID) bool {
	_, ok := k.index[id]
	return ok
}

func (d *DialogModel) focus() InputFocus {
	if d.spec.Input != nil {
		return InputText
	}
	return InputNavigation
}

// Input is the input focus the dialog reads keys in: text in a dialog with an input, navigation otherwise.
func (d *DialogModel) Input() InputFocus { return d.focus() }

// Update handles a key. answer is the answer the key gives, the default for enter; handled is false for a key
// that neither answers nor edits the input, which the application handles or drops, but never passes to the
// screen behind the dialog.
func (d *DialogModel) Update(msg tea.KeyPressMsg) (answer ActionID, handled bool) {
	focus := d.focus()
	for _, a := range d.spec.Answers {
		if d.keys.Is(msg, a.Action, focus) {
			return a.Action, true
		}
	}
	if d.spec.Default != "" && d.keys.Is(msg, d.spec.Enter, focus) {
		return d.spec.Default, true
	}
	if d.spec.Input != nil && d.input.edit(msg) {
		d.clearError()
		return "", true
	}
	return "", false
}

// Paste puts pasted text into the input at its cursor; line breaks and other control characters are dropped.
func (d *DialogModel) Paste(text string) bool {
	if d.spec.Input == nil || !d.input.insert(text) {
		return false
	}
	d.clearError()
	return true
}

func (d *DialogModel) clearError() {
	if d.msg != nil && d.msg.Kind == MessageError {
		d.msg = nil
	}
}

// Click handles a click on a target the dialog drew, as the MouseMap of its targets reports it: a button or
// a key of the key line gives its answer, as its key does, and a click on the input does nothing more than
// keep it.
func (d *DialogModel) Click(r MouseResult) (answer ActionID, handled bool) {
	if !r.Handled {
		return "", false
	}
	if r.Item == dialogInputRef && d.spec.Input != nil {
		return "", true
	}
	for _, a := range d.spec.Answers {
		if r.Action == a.Action {
			return a.Action, true
		}
	}
	if d.spec.Default != "" && r.Action == d.spec.Enter {
		return d.spec.Default, true
	}
	return "", false
}

// Value is the text of the input.
func (d *DialogModel) Value() string { return d.input.String() }

// ClearInput empties the input, such as after a wrong passphrase.
func (d *DialogModel) ClearInput() { d.input.clear() }

// SetMessage shows what is still running or the last error inside the dialog; nil removes it. Typing removes
// an error.
func (d *DialogModel) SetMessage(msg *Message) {
	if msg == nil {
		d.msg = nil
		return
	}
	m := *msg
	d.msg = &m
}

// Hints are the answers as hints of the key line, the enter key beside the default. A dialog drawn over the
// screen shows them itself; where it does not fit, the application puts them into the footer, whose targets
// then answer it.
func (d *DialogModel) Hints() []Hint {
	var hints []Hint
	for _, a := range d.spec.Answers {
		hints = append(hints, Hint{Action: a.Action, Label: a.Label, Kind: HintAnswer})
		if a.Action == d.spec.Default && d.spec.Enter != d.spec.Default {
			hints = append(hints, Hint{Action: d.spec.Enter, Label: a.Label, Kind: HintAnswer})
		}
	}
	return hints
}

// redact replaces the text of a masked input wherever a text of the dialog repeats it.
func (d *DialogModel) redact(text string) string {
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

// body is the content above the buttons in width cells, and the target of the input line, counted from its
// top left.
func (d *DialogModel) body(width int, styles Styles) ([]string, []Target) {
	var lines []string
	var targets []Target
	wrapped := func(style TextStyle, text, lead string) {
		indent := strings.Repeat(" ", lipgloss.Width(lead))
		for i, l := range Wrap(Escape(d.redact(text)), max(width-lipgloss.Width(lead), 1)) {
			prefix := indent
			if i == 0 {
				prefix = lead
			}
			lines = append(lines, prefix+styles.Render(style, l))
		}
	}
	if d.spec.Input == nil {
		wrapped(StyleTitle, d.spec.Question, styles.Render(StyleWarn, styles.Glyph(GlyphWarning))+" ")
		lines = append(lines, "")
		wrapped(StyleHint, d.spec.Consequence, "")
	} else {
		wrapped(StyleText, d.spec.Question, "")
		if d.spec.Consequence != "" {
			wrapped(StyleHint, d.spec.Consequence, "")
		}
	}
	for _, detail := range d.spec.Details {
		wrapped(StyleText, detail, "")
	}
	if d.spec.Input != nil {
		mask := ""
		if d.spec.Input.Masked {
			mask = styles.Glyph(GlyphMask)
		}
		lines = append(lines, "")
		targets = append(targets, Target{Rect: Rect{Y: len(lines), Width: width, Height: 1}, Zone: ZoneForm, Item: dialogInputRef})
		lines = append(lines, fieldStyle(styles, false).Render(d.input.view(width, styles, true, mask)))
	}
	if d.msg != nil && d.msg.Text != "" {
		lines = append(lines, "")
		lines = append(lines, strings.Split(styles.Message(d.msg.Kind, d.redact(d.msg.Text), width), "\n")...)
	}
	return lines, targets
}

// buttons draws one button per answer label in rows of width cells, the default focused and the danger in the fail
// colour, and returns a target per button, counted from the top left of the rows.
func (d *DialogModel) buttons(width int, styles Styles) ([]string, []Target) {
	var rows []string
	var targets []Target
	line, x := "", 0
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
		}
		drawn := styles.Button(kind, label, a.Action == d.spec.Default)
		w := lipgloss.Width(drawn)
		if x > 0 && x+len(buttonGap)+w > width {
			rows, line, x = append(rows, line), "", 0
		}
		if x > 0 {
			line += buttonGap
			x += len(buttonGap)
		}
		if r, ok := clipRect(Rect{X: x, Y: len(rows), Width: w, Height: 1}, width); ok {
			targets = append(targets, Target{Rect: r, Action: a.Action})
		}
		line += drawn
		x += w
	}
	if line != "" {
		rows = append(rows, line)
	}
	return rows, targets
}

// content is the whole dialog in width cells: the body, the buttons and the key line.
func (d *DialogModel) content(width int, styles Styles) ([]string, []Target) {
	lines, targets := d.body(width, styles)
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
	return lines, targets
}

// View draws the dialog over screen, the drawn screen of width by height cells: the screen dimmed, without
// its colours, and the dialog in its frame centred in room, the part of the screen it may cover, such as the
// workspace of the frame, so the header and the tab line stay in view. The targets are the dialog's alone, so
// the screen behind it takes no click. ok is false where room is too small for the dialog; the application
// then draws Page in the workspace and Hints in the footer.
func (d *DialogModel) View(screen string, width, height int, room Rect, styles Styles) (view string, targets []Target, ok bool) {
	inner := min(dialogMaxInner, room.Width-8)
	if inner < dialogMinInner {
		return "", nil, false
	}
	lines, ts := d.content(inner, styles)
	box := styles.Panel(d.text.Lookup(d.spec.Title), "", lines, inner+4, len(lines)+2, true)
	if len(box) > room.Height {
		return "", nil, false
	}
	top := room.Y + (room.Height-len(box))/2
	left := room.X + (room.Width-inner-4)/2
	return overlay(screen, box, width, height, top, left, styles), OffsetTargets(ts, left+2, top+1), true
}

// Page draws the dialog as the content of a workspace of width by height cells, for a terminal too small to
// draw it over the screen: the body without buttons and key line, which stand in the footer.
func (d *DialogModel) Page(width, height int, styles Styles) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if height == 0 {
		return "", nil
	}
	lines, targets := d.body(max(width-2, 1), styles)
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

package tui

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/helptopics"
)

// Explanation is the long explanation of the focused element. Title, Body and LinkText are plain text the
// application has translated, since they name its data; the explanation escapes them.
type Explanation struct {
	// Title names what is explained, such as a field, an entry or an area; empty takes TextInfoTitle.
	Title string
	// Body is the long explanation, one line per paragraph.
	Body string
	// Term explains the technical word the element is about, from the same terms the command line and the
	// manpage show; the zero Term is none.
	Term helptopics.Term
	// URL is the address of the documentation, an absolute http or https address; empty is none.
	URL string
	// LinkText stands above the address; empty takes TextInfoLink.
	LinkText string
}

// ExplanationKeys are the actions of the key table an explanation reads.
type ExplanationKeys struct {
	// Back and Info close the explanation.
	Back, Info ActionID
	// Copy hands the text of the explanation to the application, which puts it on the clipboard.
	Copy ActionID
	// Up and Down scroll a text longer than the room.
	Up, Down ActionID
	// Link, where the table has it, hands the address to the application, which opens it; a click on the
	// address does the same.
	Link ActionID
}

// ExplanationSpec is what an explanation shows and the keys it reads.
type ExplanationSpec struct {
	Item Explanation
	Keys ExplanationKeys
}

// The smallest text of an explanation drawn over the screen, and the fewest rows of its text.
const (
	explanationMinInner = 20
	explanationMinText  = 1
)

// explanationRef is the target item of the area of an explanation over the screen; a click there closes it.
const explanationRef = "\x00e"

// ExplanationModel is an open explanation: over the screen, in the workspace, or in a pane beside a form or a
// list. It holds the explanation and its scroll position.
type ExplanationModel struct {
	spec   ExplanationSpec
	keys   *Keymap
	text   Translator
	scroll textScroll
}

// NewExplanation checks spec and returns the explanation from its top. It rejects a missing key of Back,
// Info, Copy, Up or Down, a key that names no action of keys or that two keys share, and an explanation that
// SetItem rejects.
func NewExplanation(spec ExplanationSpec, keys *Keymap, text Translator) (*ExplanationModel, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the explanation needs a key table and a translator")
	}
	k := spec.Keys
	errs := checkKeys("explanation", keys, []namedKey{
		{"back", k.Back, true}, {"info", k.Info, true}, {"copy", k.Copy, true}, {"up", k.Up, true},
		{"down", k.Down, true}, {"link", k.Link, false},
	})
	errs = append(errs, checkExplanation(spec.Item))
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &ExplanationModel{spec: spec, keys: keys, text: text}, nil
}

// checkExplanation reports an explanation with nothing to explain, a term that breaks the contract of a
// term, and an address that is not an absolute http or https address of printable ASCII characters, which
// could otherwise end the hyperlink sequence and write to the terminal.
func checkExplanation(e Explanation) error {
	var errs []error
	if strings.TrimSpace(e.Body) == "" && e.Term == (helptopics.Term{}) && e.URL == "" {
		errs = append(errs, errors.New("explanation: nothing to explain; it needs a body, a term or an address"))
	}
	if e.Term != (helptopics.Term{}) {
		if err := helptopics.CheckTerms([]helptopics.Term{e.Term}); err != nil {
			errs = append(errs, fmt.Errorf("explanation: %w", err))
		}
	}
	if e.URL != "" {
		if err := checkLink(e.URL); err != nil {
			errs = append(errs, fmt.Errorf("explanation: address %q: %w", e.URL, err))
		}
	}
	return errors.Join(errs...)
}

func checkLink(address string) error {
	for _, r := range address {
		if r <= ' ' || r > '~' {
			return errors.New("holds a blank, a control or a non-ASCII character")
		}
	}
	u, err := url.Parse(address)
	switch {
	case err != nil:
		return err
	case u.Scheme != "https" && u.Scheme != "http":
		return errors.New("is no http or https address")
	case u.Host == "" || u.User != nil:
		return errors.New("names no host or carries user information")
	}
	return nil
}

// SetItem shows another explanation, such as the one of the element that took the focus; a different one
// starts at its top. It keeps the shown one and reports why for an explanation NewExplanation would reject.
func (e *ExplanationModel) SetItem(item Explanation) error {
	if err := checkExplanation(item); err != nil {
		return err
	}
	if item != e.spec.Item {
		e.spec.Item, e.scroll.offset = item, 0
	}
	return nil
}

// Item is the explanation shown.
func (e *ExplanationModel) Item() Explanation { return e.spec.Item }

// Update handles a key. act is the action the application runs: Back or Info, which close the explanation,
// Copy, for which it puts Text on the clipboard, and Link, for which it opens the address. The scroll keys are
// handled inside; handled is false for every other key, which the application handles or drops.
func (e *ExplanationModel) Update(msg tea.KeyPressMsg) (act ActionID, handled bool) {
	for _, id := range e.navKeys() {
		if e.keys.Is(msg, id, InputNavigation) {
			return e.Act(id)
		}
	}
	return "", false
}

func (e *ExplanationModel) navKeys() []ActionID {
	k := e.spec.Keys
	ids := []ActionID{k.Back, k.Info, k.Copy, k.Up, k.Down}
	if k.Link != "" && e.spec.Item.URL != "" {
		ids = append(ids, k.Link)
	}
	return ids
}

// Act runs one of the explanation keys as its key does, for a click or a command that stands for the key.
func (e *ExplanationModel) Act(id ActionID) (act ActionID, handled bool) {
	k := e.spec.Keys
	switch {
	case id == "":
		return "", false
	case id == k.Up:
		e.scroll.by(-1)
	case id == k.Down:
		e.scroll.by(1)
	case slices.Contains(e.navKeys(), id):
		return id, true
	default:
		return "", false
	}
	return "", true
}

// Click handles a click on a target the explanation drew, as the MouseMap of its targets reports it: a key of
// the key line or the address acts as its key, and a click anywhere else on the explanation over the screen
// closes it as Back does.
func (e *ExplanationModel) Click(r MouseResult) (act ActionID, handled bool) {
	switch {
	case !r.Handled:
		return "", false
	case r.Item == explanationRef:
		return e.spec.Keys.Back, true
	}
	return e.Act(r.Action)
}

// Wheel scrolls the text by the notches of the wheel, WheelTextLines a notch, and stops at its ends.
func (e *ExplanationModel) Wheel(steps int) { e.scroll.by(steps * WheelTextLines) }

// Hints are the explanation keys as hints of a key line: scrolling only where the last drawing did not show
// the whole text. The explanation drawn over the screen shows them itself; in the workspace, the application
// puts them into the footer.
func (e *ExplanationModel) Hints() []Hint {
	k := e.spec.Keys
	var hints []Hint
	if e.scroll.last > 0 {
		hints = append(hints, Hint{Action: k.Up, Label: TextHelpScroll}, Hint{Action: k.Down, Label: TextHelpScroll})
	}
	if k.Link != "" && e.spec.Item.URL != "" {
		hints = append(hints, Hint{Action: k.Link, Label: TextInfoOpenLink, Priority: 1})
	}
	return append(hints, Hint{Action: k.Copy, Label: TextKeyCopy, Priority: 2},
		Hint{Action: k.Back, Label: TextHelpClose, Kind: HintAlways}, Hint{Action: k.Info, Label: TextHelpClose, Kind: HintAlways})
}

// Text is the explanation as plain text for the clipboard: what it shows and nothing else, without control
// characters.
func (e *ExplanationModel) Text() string {
	item := e.spec.Item
	var parts []string
	for _, p := range []string{item.Title, strings.TrimSpace(item.Body), e.termText(), item.URL} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	lines := strings.Split(strings.Join(parts, "\n\n"), "\n")
	for i, l := range lines {
		lines[i] = Escape(l)
	}
	return strings.Join(lines, "\n")
}

func (e *ExplanationModel) termText() string {
	if e.spec.Item.Term == (helptopics.Term{}) {
		return ""
	}
	return e.spec.Item.Term.Title + ": " + e.spec.Item.Term.Text
}

// rows is the text in width cells: the body, the term with its title in bold, and the address under its
// label, every piece of it linked to the whole address and a target of the link key; a blank line stands
// between them.
func (e *ExplanationModel) rows(width int, styles Styles, body TextStyle) []textRow {
	item := e.spec.Item
	var rows []textRow
	gap := func() {
		if len(rows) > 0 {
			rows = append(rows, textRow{})
		}
	}
	if b := strings.TrimSpace(item.Body); b != "" {
		for _, line := range strings.Split(b, "\n") {
			for _, piece := range strings.Split(helptopics.Wrap(Escape(line), width), "\n") {
				rows = append(rows, textRow{text: styles.Render(body, Clip(piece, width))})
			}
		}
	}
	if term := e.termText(); term != "" {
		gap()
		head := Escape(item.Term.Title) + ":"
		for i, piece := range strings.Split(helptopics.Wrap(Escape(term), width), "\n") {
			piece = Clip(piece, width)
			if rest, ok := strings.CutPrefix(piece, head); ok && i == 0 {
				piece = styles.Render(StyleTitle, head) + styles.Render(body, rest)
			} else {
				piece = styles.Render(body, piece)
			}
			rows = append(rows, textRow{text: piece})
		}
	}
	if item.URL != "" {
		gap()
		label := item.LinkText
		if label == "" {
			label = e.text.Lookup(TextInfoLink)
		}
		for _, piece := range strings.Split(helptopics.Wrap(Escape(label), width), "\n") {
			rows = append(rows, textRow{text: styles.Render(StyleHint, Clip(piece, width))})
		}
		linked := styles.profile != ProfileNone && styles.profile <= ProfileTrueColor
		for _, piece := range strings.Split(ansi.Hardwrap(item.URL, max(width, 1), true), "\n") {
			text := piece
			if linked {
				// Each piece links to the whole address; the address also stands as text for a terminal
				// without hyperlinks.
				text = ansi.SetHyperlink(item.URL) + piece + ansi.ResetHyperlink()
			}
			var targets []Target
			if e.spec.Keys.Link != "" {
				targets = []Target{{Rect: Rect{Width: len(piece), Height: 1}, Action: e.spec.Keys.Link}}
			}
			rows = append(rows, textRow{text: text, targets: targets})
		}
	}
	return rows
}

// window is rows cut to room lines from the scroll position, with the position line where they do not fit.
func (e *ExplanationModel) window(rows []textRow, room, width int, styles Styles) []textRow {
	start, end, position := e.scroll.window(len(rows), room, e.text)
	out := slices.Clone(rows[start:end])
	if position != "" {
		out = append(out, textRow{text: styles.Render(StyleHint, styles.Truncate(Escape(position), width))})
	}
	return out
}

// View draws the explanation over screen, the drawn screen of width by height cells: the screen dimmed and
// the explanation in its frame, titled with the title, centred in room, the part of the screen it may cover,
// as high as its text needs and room allows, with the position where it scrolls and the key line. The targets
// are the explanation's alone; a click on it outside its keys and address closes it. ok is false where room
// is too small; the application then draws Page in the workspace and Hints in the footer.
func (e *ExplanationModel) View(screen string, width, height int, room Rect, styles Styles) (view string, targets []Target, ok bool) {
	inner := min(dialogMaxInner, room.Width-8)
	textRoom := room.Height - 2 - 2 - 2
	if inner < explanationMinInner || textRoom < explanationMinText {
		return "", nil, false
	}
	lines, ts := place(nil, nil, e.window(e.rows(inner, styles, StyleText), textRoom, inner, styles), 0)
	keyLine, kt := styles.footer(e.keys.Hints(e.Hints(), InputNavigation, e.text), "", inner)
	lines = append(lines, "")
	ts = append(ts, OffsetTargets(kt, 0, len(lines))...)
	lines = append(lines, keyLine)
	box := styles.Panel(e.title(), "", lines, inner+4, len(lines)+2, true)
	top := room.Y + (room.Height-len(box))/2
	left := room.X + (room.Width-inner-4)/2
	all := append([]Target{{Rect: Rect{X: left, Y: top, Width: inner + 4, Height: len(box)}, Item: explanationRef}},
		OffsetTargets(ts, left+2, top+1)...)
	return overlay(screen, box, width, height, top, left, styles), all, true
}

// Page draws the explanation as the content of a workspace of width by height cells, for a terminal too small
// to draw it over the screen: the title, then the text; the keys stand in the footer.
func (e *ExplanationModel) Page(width, height int, styles Styles) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if width == 0 || height == 0 {
		return finishPage(nil, nil, width, height)
	}
	lines := []string{styles.TitleLine(e.title(), "", width), ""}
	var targets []Target
	if height > len(lines) {
		lines, targets = place(lines, targets, e.window(e.rows(width, styles, StyleText), height-len(lines), width, styles), 0)
	}
	return finishPage(lines, targets, width, height)
}

// Pane draws the explanation dimmed in a frame of width by height cells, for the pane beside a form or a list
// on a wide terminal. Text that does not fit scrolls with the wheel over the pane; its target names the detail
// zone, so the wheel reaches it under the pointer without the pane taking the focus by a key.
func (e *ExplanationModel) Pane(width, height int, styles Styles) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	x0, y0, inner, room := 2, 1, width-4, height-2
	if width < 4 || height < 2 {
		x0, y0, inner, room = 0, 0, width, height
	}
	var lines []string
	var targets []Target
	if inner > 0 && room > 0 {
		lines, targets = place(nil, nil, e.window(e.rows(inner, styles, StyleHint), room, inner, styles), 0)
	}
	all := []Target{{Rect: Rect{Width: width, Height: height}, Zone: ZoneDetail}}
	for _, t := range OffsetTargets(targets, x0, y0) {
		if r, ok := clipRect(t.Rect, x0+max(inner, 0)); ok && t.Rect.Y < y0+max(room, 0) {
			t.Rect = r
			all = append(all, t)
		}
	}
	return strings.Join(styles.Panel(e.title(), "", lines, width, height, false), "\n"), all
}

func (e *ExplanationModel) title() string {
	if e.spec.Item.Title == "" {
		return e.text.Lookup(TextInfoTitle)
	}
	return e.spec.Item.Title
}

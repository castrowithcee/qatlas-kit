package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// BarAction is one action of the action bar of a page: the button shows the key of Action in force, then
// the text Label.
type BarAction struct {
	Action ActionID
	Label  TextID
	// Kind is the look of the button; it never changes the place in the bar.
	Kind ButtonKind
}

// barGap stands between two buttons of the bar.
const barGap = " "

// barButton is a button of the bar with the key and label it shows.
type barButton struct {
	action     ActionID
	kind       ButtonKind
	key, label string
}

func (b barButton) width() int { return lipgloss.Width("[ " + b.key + " " + b.label + " ]") }

// KeyButton draws a button as "[ key label ]": the key in the footer key style, the label in the style of a
// button of kind. Without colour the place tells key and label apart, as in the footer. A primary or danger
// button is not filled, so the key keeps its colour: primary is bold, danger takes the colour of failure.
func (s Styles) KeyButton(kind ButtonKind, key, label string) string {
	key, label = Escape(key), Escape(label)
	style, ok := s.buttonStyle(kind)
	if !ok {
		return "[ " + key + " " + label + " ]"
	}
	if kind != ButtonNormal {
		style = style.UnsetBackground().UnsetForeground()
		if kind == ButtonDanger && !s.profile.Colorless() && s.palette.Fail != "" {
			style = style.Foreground(lipgloss.Color(string(s.palette.Fail)))
		}
	}
	return style.Render("[ ") + s.Render(StyleFooterKey, key) + style.Render(" "+label+" ]")
}

// actionBar resolves the actions of the bar of spec: an action without a key that acts in the input focus
// of the screen, a repeated action and a label that is empty leave out; of the rest as many stand, in the
// order given, as fit the width, and the first that does not fit ends the list, so the actions that give way
// are the last ones, which draw leftmost.
func (f *frameRun) actionBar(spec FrameSpec) []barButton {
	var out []barButton
	width, seen := 0, map[ActionID]bool{}
	for _, a := range spec.Bar {
		key, ok := f.keys.barKey(a.Action, spec.Input)
		label := Escape(lookup(f.text, a.Label))
		if !ok || label == "" || seen[a.Action] {
			continue
		}
		b := barButton{a.Action, a.Kind, Escape(key), label}
		w := b.width()
		if len(out) > 0 {
			w += len(barGap)
		}
		if width+w > spec.Width {
			break
		}
		width += w
		seen[a.Action] = true
		out = append(out, b)
	}
	return out
}

// barKey is the first key of id that acts under focus, spelled as the footer spells it; false where there
// is none or the action is unknown or belongs to another page.
func (k *Keymap) barKey(id ActionID, focus InputFocus) (string, bool) {
	if k == nil {
		return "", false
	}
	i, ok := k.index[id]
	if !ok || !k.visible(i) {
		return "", false
	}
	keys := k.keysUnder(i, focus)
	if len(keys) == 0 {
		return "", false
	}
	return k.KeyNameLabel(keys[0]), true
}

// barLine draws the buttons as one line of the given width and returns one click target for each. The
// first button, the most important, stands at the right end and the next ones left of it.
func (f *frameRun) barLine(buttons []barButton, width int) (string, []Target) {
	total := 0
	for i, btn := range buttons {
		if i > 0 {
			total += len(barGap)
		}
		total += btn.width()
	}
	var b strings.Builder
	var targets []Target
	x := max(width-total, 0)
	b.WriteString(strings.Repeat(" ", x))
	for i := len(buttons) - 1; i >= 0; i-- {
		btn := buttons[i]
		b.WriteString(f.styles.KeyButton(btn.kind, btn.key, btn.label))
		targets = append(targets, Target{Rect: Rect{X: x, Width: btn.width(), Height: 1}, Action: btn.action})
		x += btn.width()
		if i > 0 {
			b.WriteString(barGap)
			x += len(barGap)
		}
	}
	return b.String(), targets
}

// footerHints are the hints of the footer without those whose action stands in the bar as a button.
func (f *frameRun) footerHints(spec FrameSpec) []HintGroup {
	hints := spec.Footer
	if bar := f.actionBar(spec); len(bar) > 0 {
		shown := map[ActionID]bool{}
		for _, b := range bar {
			shown[b.action] = true
		}
		hints = nil
		for _, h := range spec.Footer {
			if h.Action == "" || !shown[h.Action] {
				hints = append(hints, h)
			}
		}
	}
	return f.keys.Hints(hints, spec.Input, f.text)
}

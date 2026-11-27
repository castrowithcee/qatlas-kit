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

// reachContrast is c moved towards the colour toward, in steps of a twentieth, until it reads against other
// at textContrast; a colour that already does, or that is no hex value, stays. At toward itself it reads,
// since the text does on every ground of the palette.
func reachContrast(c, other, toward Color) Color {
	a, okA := rgbOfColor(c)
	o, okO := rgbOfColor(other)
	t, okT := rgbOfColor(toward)
	if !c.isHex() || !okA || !okO || !okT {
		return c
	}
	for step := 0; step <= 20; step++ {
		if v := mix(a, t, float64(step)/20); contrast(v, o) >= textContrast {
			return v.hex()
		}
	}
	return t.hex()
}

// width is the cells of the button as the styles draw it: the plain form "[ key label ]" is wider than the
// filled one.
func (b barButton) width(s Styles) int { return lipgloss.Width(s.KeyButton(b.kind, b.key, b.label)) }

// KeyButton draws a button as a filled block: the key in its own pad, the same in every kind, in the footer
// key style, then the label on the fill of kind: a neutral fill with the text colour, the accent fill for
// primary and the failure fill for danger, each of those bold on the base colour. Without colour the label
// is reversed and the key stands bold before it, not reversed; primary and danger add bold. A palette that
// lacks a fill draws as without colour, and ProfileNone draws "[ key label ]".
func (s Styles) KeyButton(kind ButtonKind, key, label string) string {
	key, label = Escape(key), Escape(label)
	if s.profile == ProfileNone || s.profile > ProfileTrueColor {
		return "[ " + key + " " + label + " ]"
	}
	p := s.palette
	fill, text := p.Surface, p.Text
	switch kind {
	case ButtonPrimary:
		fill, text = p.Accent, p.Base
	case ButtonDanger:
		fill, text = p.Fail, p.Base
	}
	bold := kind != ButtonNormal
	if s.profile.Colorless() || fill == "" || p.Panel == "" || text == "" {
		return s.Render(StyleFooterKey, " "+key+" ") + lipgloss.NewStyle().Reverse(true).Bold(bold).Render(" "+label+" ")
	}
	fill = reachContrast(fill, text, p.Text)
	keyColor := reachContrast(p.Accent, p.Panel, p.Text)
	pad := s.Style(StyleFooterKey).Background(lipgloss.Color(string(p.Panel))).Foreground(lipgloss.Color(string(keyColor)))
	body := lipgloss.NewStyle().Bold(bold).Background(lipgloss.Color(string(fill))).Foreground(lipgloss.Color(string(text)))
	return pad.Render(" "+key+" ") + body.Render(" "+label+" ")
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
		w := b.width(f.styles)
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
		total += btn.width(f.styles)
	}
	var b strings.Builder
	var targets []Target
	x := max(width-total, 0)
	b.WriteString(strings.Repeat(" ", x))
	for i := len(buttons) - 1; i >= 0; i-- {
		btn := buttons[i]
		b.WriteString(f.styles.KeyButton(btn.kind, btn.key, btn.label))
		w := btn.width(f.styles)
		targets = append(targets, Target{Rect: Rect{X: x, Width: w, Height: 1}, Action: btn.action})
		x += w
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

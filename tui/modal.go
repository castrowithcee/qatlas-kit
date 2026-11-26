package tui

import tea "charm.land/bubbletea/v2"

// Modal is the shape Dialog, Help, Explanation and Form share: they answer with an ActionID and stand over
// the screen with Overlay.
type Modal interface {
	Update(msg tea.KeyPressMsg) (ActionID, bool)
	Click(r MouseResult) (ActionID, bool)
	Wheel(steps int)
	Overlay(styles Styles, screen string, width, height int, room Rect) (view string, targets []Target, ok bool)
	View(styles Styles, width, height int) (string, []Target)
	Hints() []Hint
}

var (
	_ Modal = (*Dialog)(nil)
	_ Modal = (*Help)(nil)
	_ Modal = (*Explanation)(nil)
	_ Modal = (*Form)(nil)
)

// withOutside returns targets and, where on, the targets of ZoneOutside that cover the screen of width by
// height cells around frame, the box of the modal. They lie beside the box, so a click inside it, on a target
// or not, never reports ZoneOutside.
func withOutside(on bool, targets []Target, width, height int, frame Rect) []Target {
	if !on {
		return targets
	}
	right, bottom := frame.X+frame.Width, frame.Y+frame.Height
	var out []Target
	for _, r := range []Rect{
		{X: 0, Y: 0, Width: width, Height: frame.Y},
		{X: 0, Y: bottom, Width: width, Height: height - bottom},
		{X: 0, Y: frame.Y, Width: frame.X, Height: frame.Height},
		{X: right, Y: frame.Y, Width: width - right, Height: frame.Height},
	} {
		if r.Width > 0 && r.Height > 0 {
			out = append(out, Target{Rect: r, Zone: ZoneOutside})
		}
	}
	return append(out, targets...)
}

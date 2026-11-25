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

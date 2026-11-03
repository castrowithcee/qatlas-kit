package tui

import tea "charm.land/bubbletea/v2"

// WheelTextLines is how many lines one notch of the wheel moves in a text area; in lists and forms a notch
// is one step.
const WheelTextLines = 3

// MouseResult is what one mouse event asks for. A click names the target it hit: an action to run as its key
// runs it, an area or tab to open as a tab click opens it, or a bare zone to focus as tab focuses it. A notch of
// the wheel names the zone under the pointer, empty where none is, and WheelSteps is -1 up or 1 down. Handled
// is false where the event does nothing.
type MouseResult struct {
	Action     ActionID
	Zone       FocusZone
	Area       AreaID
	Tab        string
	WheelSteps int
	Handled    bool
}

// MouseMap holds the click targets of one drawn frame. A frame drawn later replaces it, so a target that is
// no longer on screen cannot act.
type MouseMap struct {
	targets []Target
	enabled bool
}

// NewMouseMap returns the map of targets, the targets of one drawing run in the order they were drawn.
// enabled is false where the mouse is switched off, while a key is captured and while the terminal is too
// small for the screen; such a map handles no event.
func NewMouseMap(targets []Target, enabled bool) *MouseMap {
	return &MouseMap{targets: append([]Target(nil), targets...), enabled: enabled}
}

// Handle reads one mouse event. Only a press of the left button and the wheel act; motion, release and the
// other buttons do nothing. A target drawn later lies on top of one drawn earlier, and a bare zone lies below
// every target that names an action, area or tab, so a click on an element inside a zone reaches the element.
func (m *MouseMap) Handle(msg tea.MouseMsg) MouseResult {
	if m == nil || !m.enabled {
		return MouseResult{}
	}
	event := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft {
			return MouseResult{}
		}
		t, ok := m.at(event.X, event.Y, Target.element)
		if !ok {
			t, ok = m.at(event.X, event.Y, func(t Target) bool { return t.Zone != "" })
		}
		if !ok {
			return MouseResult{}
		}
		return MouseResult{Action: t.Action, Zone: t.Zone, Area: t.Area, Tab: t.Tab, Handled: true}
	case tea.MouseWheelMsg:
		steps := 0
		switch event.Button {
		case tea.MouseWheelUp:
			steps = -1
		case tea.MouseWheelDown:
			steps = 1
		default:
			return MouseResult{}
		}
		t, _ := m.at(event.X, event.Y, func(t Target) bool { return t.Zone != "" })
		return MouseResult{Zone: t.Zone, WheelSteps: steps, Handled: true}
	}
	return MouseResult{}
}

// at is the target drawn last that covers the cell and meets match.
func (m *MouseMap) at(x, y int, match func(Target) bool) (Target, bool) {
	for i := len(m.targets) - 1; i >= 0; i-- {
		t := m.targets[i]
		r := t.Rect
		if match(t) && x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
			return t, true
		}
	}
	return Target{}, false
}

// element reports whether a click on t does more than focus its zone.
func (t Target) element() bool { return t.Action != "" || t.Area != "" || t.Tab != "" }

// OffsetTargets returns targets moved by x columns and y rows, for a part drawn at that place of the screen.
func OffsetTargets(targets []Target, x, y int) []Target {
	out := make([]Target, len(targets))
	for i, t := range targets {
		t.Rect.X += x
		t.Rect.Y += y
		out[i] = t
	}
	return out
}

// WheelStep is the place steps notches of the wheel lead to from at among n places. The wheel stops at both
// ends, where keys may wrap round; without places it is 0.
func WheelStep(at, n, steps int) int {
	if n <= 0 {
		return 0
	}
	return max(min(at+steps, n-1), 0)
}

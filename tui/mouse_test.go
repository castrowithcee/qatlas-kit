package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func click(x, y int) tea.MouseClickMsg { return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft} }

// sampleTargets are a zone over the rows, a row inside it, a later row over the first, and a tab.
func sampleTargets() []Target {
	return []Target{
		{Rect: Rect{X: 0, Y: 2, Width: 40, Height: 10}, Zone: ZoneRows},
		{Rect: Rect{X: 0, Y: 3, Width: 40, Height: 1}, Zone: ZoneRows, Action: "enter"},
		{Rect: Rect{X: 30, Y: 3, Width: 10, Height: 1}, Action: "info"},
		{Rect: Rect{X: 2, Y: 0, Width: 8, Height: 1}, Zone: ZoneTabs, Area: "items"},
	}
}

// Only a press of the left button and the wheel act; release, motion, the other buttons and the sideways
// wheel do nothing, and neither does a map with the mouse switched off.
func TestOnlyALeftClickAndTheWheelAct(t *testing.T) {
	m := NewMouseMap(sampleTargets(), true)
	for _, msg := range []tea.MouseMsg{
		tea.MouseReleaseMsg{X: 5, Y: 3, Button: tea.MouseLeft},
		tea.MouseMotionMsg{X: 5, Y: 3, Button: tea.MouseLeft},
		tea.MouseClickMsg{X: 5, Y: 3, Button: tea.MouseRight},
		tea.MouseClickMsg{X: 5, Y: 3, Button: tea.MouseMiddle},
		tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelLeft},
		tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelRight},
	} {
		if r := m.Handle(msg); r != (MouseResult{}) {
			t.Errorf("%T %v: %+v", msg, msg.Mouse().Button, r)
		}
	}
	off := NewMouseMap(sampleTargets(), false)
	for _, msg := range []tea.MouseMsg{click(5, 3), tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelDown}} {
		if r := off.Handle(msg); r != (MouseResult{}) {
			t.Errorf("switched off: %+v", r)
		}
	}
	var none *MouseMap
	if r := none.Handle(click(5, 3)); r.Handled {
		t.Errorf("a nil map handled a click: %+v", r)
	}
}

// A click reaches the element under it before the zone around it, and a target drawn later before one drawn
// earlier; the free area of a zone focuses the zone, and a cell without target does nothing.
func TestAClickReachesTheElementBeforeItsZone(t *testing.T) {
	m := NewMouseMap(sampleTargets(), true)
	for _, c := range []struct {
		x, y int
		want MouseResult
	}{
		{5, 3, MouseResult{Action: "enter", Zone: ZoneRows, Handled: true}},
		{35, 3, MouseResult{Action: "info", Handled: true}},
		{5, 6, MouseResult{Zone: ZoneRows, Handled: true}},
		{39, 11, MouseResult{Zone: ZoneRows, Handled: true}},
		{40, 3, MouseResult{}},
		{5, 12, MouseResult{}},
		{5, 1, MouseResult{}},
		{2, 0, MouseResult{Zone: ZoneTabs, Area: "items", Handled: true}},
		{10, 0, MouseResult{}},
	} {
		if got := m.Handle(click(c.x, c.y)); got != c.want {
			t.Errorf("click at %d,%d = %+v, want %+v", c.x, c.y, got, c.want)
		}
	}
}

// The wheel names the zone under the pointer, so a side pane scrolls without taking the focus, and acts
// beside every zone as well.
func TestTheWheelNamesTheZoneUnderThePointer(t *testing.T) {
	targets := append(sampleTargets(), Target{Rect: Rect{X: 41, Y: 2, Width: 20, Height: 10}, Zone: ZoneDetail})
	m := NewMouseMap(targets, true)
	for _, c := range []struct {
		x, y   int
		button tea.MouseButton
		want   MouseResult
	}{
		{5, 3, tea.MouseWheelDown, MouseResult{Zone: ZoneRows, WheelSteps: 1, Handled: true}},
		{50, 3, tea.MouseWheelUp, MouseResult{Zone: ZoneDetail, WheelSteps: -1, Handled: true}},
		{70, 20, tea.MouseWheelDown, MouseResult{WheelSteps: 1, Handled: true}},
	} {
		if got := m.Handle(tea.MouseWheelMsg{X: c.x, Y: c.y, Button: c.button}); got != c.want {
			t.Errorf("wheel at %d,%d = %+v, want %+v", c.x, c.y, got, c.want)
		}
	}
}

// The map keeps its own copy, so a target changed or added after the frame was drawn has no effect.
func TestAMapKeepsTheTargetsOfItsFrame(t *testing.T) {
	targets := sampleTargets()
	m := NewMouseMap(targets, true)
	targets[1].Action = "delete"
	_ = append(targets[:1], Target{Rect: Rect{Width: 80, Height: 24}, Action: "quit"})
	if got := m.Handle(click(5, 3)); got.Action != "enter" {
		t.Errorf("click = %+v, want the action of the drawn frame", got)
	}
}

// A click on a key the frame drew is that key: the action it reports is the action the key triggers in the
// input focus of the screen. A click on a tab opens the area of the digit the tab shows.
func TestAClickOnAFrameTargetIsItsKey(t *testing.T) {
	f, keys := sampleFrame(t, looks(t)["noneascii"], i18n.English)
	spec := sampleSpec(100, 9)
	out, targets := f.Render(spec)
	rows := plainRows(out)
	m := NewMouseMap(targets, true)
	actions := map[ActionID]Action{}
	for _, a := range keys.Actions() {
		actions[a.ID] = a
	}
	areas := map[AreaID]int{}
	for i, a := range spec.Areas {
		areas[a.ID] = i
	}
	clicked := 0
	for _, tg := range targets {
		for x := tg.Rect.X; x < tg.Rect.X+tg.Rect.Width; x++ {
			got := m.Handle(click(x, tg.Rect.Y))
			if !got.Handled || got.Action != tg.Action || got.Area != tg.Area || got.Tab != tg.Tab {
				t.Fatalf("click at %d,%d = %+v, want target %+v", x, tg.Rect.Y, got, tg)
			}
			clicked++
		}
		switch {
		case tg.Action != "":
			pressed := false
			for _, key := range actions[tg.Action].DefaultKeys {
				if id, ok := keys.Match(press(key), spec.Input); ok && id == tg.Action {
					pressed = true
				}
			}
			if !pressed {
				t.Errorf("no key of %q triggers it in the focus of the screen", tg.Action)
			}
		case tg.Tab == "":
			cells := string([]rune(rows[tg.Rect.Y])[tg.Rect.X : tg.Rect.X+tg.Rect.Width])
			digit := string(rune('1' + areas[tg.Area]))
			if !strings.Contains(cells, digit) {
				t.Errorf("tab %q covers %q, want its digit %s", tg.Area, cells, digit)
			}
		}
	}
	if clicked == 0 {
		t.Fatal("the frame drew no target")
	}
}

// A target placed with its part lands where the part stands, and the input is left as it was.
func TestOffsetTargetsMovesThePlaceOnly(t *testing.T) {
	in := sampleTargets()
	out := OffsetTargets(in, 3, 4)
	for i := range in {
		want := in[i]
		want.Rect.X += 3
		want.Rect.Y += 4
		if out[i] != want {
			t.Errorf("target %d = %+v, want %+v", i, out[i], want)
		}
	}
	if in[0].Rect.X != 0 || in[0].Rect.Y != 2 {
		t.Errorf("the input changed: %+v", in[0])
	}
}

// The wheel stops at both ends and never wraps round.
func TestTheWheelStopsAtTheEnds(t *testing.T) {
	for _, c := range []struct{ at, n, steps, want int }{
		{0, 5, -1, 0}, {4, 5, 1, 4}, {2, 5, 1, 3}, {2, 5, -WheelTextLines, 0}, {1, 10, WheelTextLines, 4},
		{0, 0, 1, 0}, {3, 0, -1, 0}, {7, 5, -1, 4},
	} {
		if got := WheelStep(c.at, c.n, c.steps); got != c.want {
			t.Errorf("WheelStep(%d, %d, %d) = %d, want %d", c.at, c.n, c.steps, got, c.want)
		}
	}
}

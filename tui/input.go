package tui

import (
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// fragmentWindow is the longest gap between the pieces of one split mouse report. Typing is slower than this.
const fragmentWindow = 200 * time.Millisecond

// bracketHold is how long a "[" that may open the rest of a report, alt+[ or a "[" after an escape, waits for
// it before it counts as the key it is. A text field types it, so it cannot be let through first and taken
// back. It is as long as fragmentWindow: a head the filter would still take as the rest must find the "["
// still held, or the "[" is typed and the rest dropped.
const bracketHold = fragmentWindow

// wheelMerge is the longest gap between a wheel event and an equal one that is its release half. A terminal
// that reports a notch as a press and a release delivers both as presses, back to back.
const wheelMerge = 15 * time.Millisecond

var (
	// fragmentTail matches what a mouse report of the SGR form can still lack: the rest of its parameters and
	// the final M or m.
	fragmentTail = regexp.MustCompile(`^[0-9;]*[Mm]?`)
	// fragmentHead matches the start of such a report once its escape and "[" went into keys of their own.
	fragmentHead = regexp.MustCompile(`^<[0-9;]*[Mm]?`)
	// reportStart matches the start of such a report the input reader gave up on, its rest still to come.
	reportStart = regexp.MustCompile(`^\x1b\[<[0-9;]*$`)
)

// fragmentState is what the input filter met last.
type fragmentState uint8

const (
	stateNone fragmentState = iota
	stateEsc
	stateBracket
	// stateDouble follows the "\x1b[[" with which the Linux console opens F1 to F5.
	stateDouble
	stateTail
)

// InputBatch is messages the InputFilter delivers in order as one, such as a held key and the key after it.
// An application handles each of them in turn as if it had come alone.
type InputBatch []tea.Msg

// heldKeyMsg ends the hold of the "[" number id.
type heldKeyMsg struct{ id int }

// InputFilter keeps mouse reports from becoming text. A terminal behind SSH or tmux may deliver a report in
// more than one read with a pause between them longer than the input reader waits; the reader then reports
// the start as an unknown sequence, an escape or alt+[, and the rest as typed characters like "<65;12;5M",
// which a text field would take. The filter drops such pieces. A piece counts only when it follows the one
// before it within fragmentWindow and fits the report form, so the same characters typed by hand, pasted or
// standing alone pass unchanged. Only a "[" that may open a piece is held back, briefly; a lone escape is a
// real key and passes at once. The filter also drops the second half of a wheel notch a terminal reports
// twice, and names F1 to F5 of the Linux console, which the reader reports in two parts.
//
// The filter is the function of tea.WithFilter and runs in the event loop, one message at a time.
type InputFilter struct {
	now  func() time.Time
	send func(tea.Msg)

	state fragmentState
	at    time.Time
	held  *tea.KeyPressMsg
	id    int

	wheel     tea.Mouse
	wheelAt   time.Time
	wheelSeen bool
}

// NewInputFilter returns a filter on the clock now; nil is the system clock. Attach must give it the Send of
// the program before the program runs, or a held "[" waits for the next key instead of its hold.
func NewInputFilter(now func() time.Time) *InputFilter {
	if now == nil {
		now = time.Now
	}
	return &InputFilter{now: now}
}

// Attach gives the filter the function that delivers a message to the event loop, the Send of the program.
func (f *InputFilter) Attach(send func(tea.Msg)) { f.send = send }

// Filter is the function to pass to tea.WithFilter.
func (f *InputFilter) Filter(_ tea.Model, msg tea.Msg) tea.Msg {
	switch m := msg.(type) {
	case heldKeyMsg:
		return f.release(m)
	case uv.UnknownEvent:
		if out, ok := f.reportStart(string(m)); ok {
			return out
		}
	case uv.UnknownCsiEvent:
		if out, ok := f.reportStart(string(m)); ok {
			return out
		}
	case tea.MouseWheelMsg:
		return f.wheelNotch(m)
	case tea.PasteMsg:
		f.state = stateNone
		if held := f.takeHeld(); held != nil {
			return InputBatch{*held, m}
		}
		return m
	case tea.KeyPressMsg:
		return f.key(m)
	}
	return msg
}

func (f *InputFilter) release(m heldKeyMsg) tea.Msg {
	if f.held == nil || m.id != f.id {
		return nil
	}
	key := f.takeHeld()
	// A head arriving after the hold is text, whatever the window says, so the timer and the window cannot
	// race.
	if f.state == stateBracket {
		f.state = stateNone
	}
	return *key
}

func (f *InputFilter) takeHeld() *tea.KeyPressMsg {
	held := f.held
	f.held = nil
	return held
}

// reportStart takes the start of a report the reader gave up on; the rest that follows is dropped.
func (f *InputFilter) reportStart(seq string) (tea.Msg, bool) {
	switch {
	case seq == "\x1b[[":
		f.state = stateDouble
	case reportStart.MatchString(seq):
		f.state = stateTail
	default:
		return nil, false
	}
	f.at = f.now()
	if held := f.takeHeld(); held != nil {
		return InputBatch{*held}, true
	}
	return nil, true
}

// wheelNotch drops the release half of the notch seen last. It claims at most one event per notch: after it
// dropped one, the next equal event is a notch of its own however close it is.
func (f *InputFilter) wheelNotch(m tea.MouseWheelMsg) tea.Msg {
	event, now := m.Mouse(), f.now()
	if f.wheelSeen && event == f.wheel && now.Sub(f.wheelAt) < wheelMerge {
		f.wheelSeen = false
		return nil
	}
	f.wheel, f.wheelAt, f.wheelSeen = event, now, true
	return m
}

func (f *InputFilter) key(key tea.KeyPressMsg) tea.Msg {
	at, last := f.now(), f.state
	f.state = stateNone
	if last != stateNone && at.Sub(f.at) > fragmentWindow {
		last = stateNone
	}
	if last == stateDouble {
		if fn, ok := linuxFunctionKey(key); ok {
			return fn
		}
	}
	held := f.takeHeld()
	text := ""
	if key.Text != "" && !key.Mod.Contains(tea.ModAlt) {
		text = key.Text
	}
	n := 0
	switch last {
	case stateBracket:
		if strings.HasPrefix(text, "<") {
			n = len(fragmentHead.FindString(text))
		}
	case stateTail:
		n = len(fragmentTail.FindString(text))
	}
	if n > 0 {
		if piece := text[:n]; !strings.HasSuffix(piece, "M") && !strings.HasSuffix(piece, "m") {
			f.state, f.at = stateTail, at
		}
		if n == len(text) {
			return nil
		}
		return textKey(text[n:])
	}
	// Not a piece: a held "[" was a key after all and goes first.
	switch {
	case key.Code == tea.KeyEscape && key.Mod == 0:
		f.state, f.at = stateEsc, at
	case key.Code == '[' && key.Mod == tea.ModAlt || last == stateEsc && text == "[":
		f.state, f.at = stateBracket, at
		f.held = &key
		f.id++
		if send, id := f.send, f.id; send != nil {
			time.AfterFunc(bracketHold, func() { send(heldKeyMsg{id}) })
		}
		if held != nil {
			return InputBatch{*held}
		}
		return nil
	}
	if held != nil {
		return InputBatch{*held, key}
	}
	return key
}

// linuxFunctionKey is F1 to F5 of the Linux console, which sends them as "\x1b[[" and a letter from A to E.
func linuxFunctionKey(key tea.KeyPressMsg) (tea.KeyPressMsg, bool) {
	if len(key.Text) != 1 || key.Text[0] < 'A' || key.Text[0] > 'E' {
		return key, false
	}
	return tea.KeyPressMsg{Code: tea.KeyF1 + rune(key.Text[0]-'A')}, true
}

// textKey is the key press of typed text.
func textKey(text string) tea.KeyPressMsg {
	code := tea.KeyExtended
	if runes := []rune(text); len(runes) == 1 {
		code = runes[0]
	}
	return tea.KeyPressMsg{Code: code, Text: text}
}

package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ChatMaxDraftBytes is the longest draft a Chat holds; typing or a paste that would pass it is refused.
const ChatMaxDraftBytes = 64 << 10

const (
	chatInputRows  = 5  // the most rows the draft takes before it scrolls
	chatMinField   = 12 // the narrowest field beside the send button
	chatMineShift  = 4  // the indent of the user's own messages, so they stand apart without colour
	chatMineWidth  = 40 // the narrowest chat that indents them
	chatTimeLayout = "15:04"
)

// The target items of a chat. Message IDs hold no control character, so none of them names a message.
const (
	chatMsgRef    = "\x00m"
	chatDetailRef = "\x00d"
	chatReplyRef  = "\x00r"
	chatFieldRef  = "\x00f"
	chatSendRef   = "\x00s"
)

// The stops of the focus besides the quick replies, which are their index in ChatSpec.Actions.
const (
	chatAtRows  = -1
	chatAtField = -2
	chatAtSend  = -3
)

// ChatMessage is one message of a chat. Every text in it is untrusted plain text: control characters are
// escaped when it is drawn.
type ChatMessage struct {
	// ID is stable and holds no control character; the selection follows it.
	ID   ItemID
	From string
	// At is the time of the message; the zero Time shows none.
	At time.Time
	// Status is the word of the state of the message, such as "delivered"; Kind gives it the glyph of its
	// state in the colour of that state. Either may be empty.
	Status string
	Kind   StatusKind
	// Text may hold line breaks.
	Text string
	// Details are further lines the message folds away; enter or a click unfolds them.
	Details []string
	// Mine marks a message of the user, which stands indented where the width allows.
	Mine bool
}

// ChatKeys are the actions of the key table a chat reads.
type ChatKeys struct {
	// Up and Down select the previous and next message and, in the field, move the cursor by a row; PageUp and
	// PageDown scroll the messages by a page, also from the field; First and Last select the first and last
	// message and, in the field, go to the start and end of the line.
	Up, Down, PageUp, PageDown, First, Last ActionID
	// Left and Right move the cursor in the field.
	Left, Right ActionID
	// Enter unfolds or folds the details of the selected message, adds a line break in the field and presses a
	// focused button.
	Enter ActionID
	// Next and Prev step the focus through the messages, the quick replies, the field and the send button,
	// with wrap-around.
	Next, Prev ActionID
	// Back leaves the field or a button for the messages; on the messages it is left to the application.
	Back ActionID
}

// ChatSpec is what a chat shows and the keys it reads. The application supplies every text.
type ChatSpec struct {
	Keys ChatKeys
	// Empty is the dimmed sentence of a chat without messages, with the next step.
	Empty TextID
	// Actions are quick replies drawn as buttons under the selected message, each an action of the key table
	// and a label behind the key in force.
	Actions []FormButton
	// Now is the clock that decides how much of a time is shown; nil takes time.Now.
	Now func() time.Time
}

// ChatResult is what a key press or a click asks the application to run.
type ChatResult struct {
	// Send is true when the draft was sent: Text holds it, and the chat has emptied the draft.
	Send bool
	Text string
	// Action is the quick reply chosen, and Message the message it answers.
	Action  ActionID
	Message ItemID
}

// Chat shows messages with sender, time, status and folded details above a multi-line field to write the next
// one. The messages, their state and what a sent text or a quick reply does are the application's.
type Chat struct {
	spec ChatSpec
	keys *Keymap
	text Translator

	msgs   []ChatMessage
	sel    int
	open   map[ItemID]bool
	offset int  // first shown line of the messages
	reveal bool // the next drawing brings the selected message into view

	at     int
	active bool

	draft textArea

	// What the last drawing found, for the page keys and the wheel.
	area         int
	starts, ends []int
}

// NewChat checks spec and returns an empty chat with the focus on the messages. It rejects a chat without the
// text of its empty state, a missing key, a key that names no action of keys or that two keys share, and a
// quick reply without label, whose action is no action of keys, is a chat key or is on another quick reply.
func NewChat(spec ChatSpec, keys *Keymap, text Translator) (*Chat, error) {
	if keys == nil || text == nil {
		return nil, errors.New("the chat needs a key table and a translator")
	}
	var errs []error
	if spec.Empty == "" {
		errs = append(errs, errors.New("chat: no text for the empty state"))
	}
	k := spec.Keys
	named := []namedKey{{"up", k.Up, true}, {"down", k.Down, true}, {"page up", k.PageUp, true},
		{"page down", k.PageDown, true}, {"first", k.First, true}, {"last", k.Last, true}, {"left", k.Left, true},
		{"right", k.Right, true}, {"enter", k.Enter, true}, {"next", k.Next, true}, {"previous", k.Prev, true},
		{"back", k.Back, true}}
	errs = append(errs, checkKeys("chat", keys, named)...)
	for i, b := range spec.Actions {
		switch {
		case !keys.known(b.Action):
			errs = append(errs, fmt.Errorf("chat: quick reply %d: %q is no action of the key table", i, b.Action))
		case slices.ContainsFunc(named, func(n namedKey) bool { return n.id == b.Action }):
			errs = append(errs, fmt.Errorf("chat: quick reply %d: %q is a key of the chat", i, b.Action))
		case slices.ContainsFunc(spec.Actions[:i], func(o FormButton) bool { return o.Action == b.Action }):
			errs = append(errs, fmt.Errorf("chat: quick reply %d: action %q is on another quick reply", i, b.Action))
		}
		if b.Label == "" {
			errs = append(errs, fmt.Errorf("chat: quick reply %d has no label", i))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	spec.Actions = slices.Clone(spec.Actions)
	if spec.Now == nil {
		spec.Now = time.Now
	}
	return &Chat{spec: spec, keys: keys, text: text, sel: -1, open: map[ItemID]bool{}, at: chatAtRows, active: true,
		draft: textArea{limit: ChatMaxDraftBytes, width: 40}}, nil
}

// checkMessages reports a message without ID or with a control character in it, an ID listed twice or already
// held, and an unknown status kind.
func checkMessages(msgs, held []ChatMessage) error {
	var errs []error
	seen := map[ItemID]bool{}
	for _, m := range held {
		seen[m.ID] = true
	}
	for i, m := range msgs {
		switch {
		case m.ID == "":
			errs = append(errs, fmt.Errorf("chat: message %d has no ID", i))
		case strings.ContainsFunc(string(m.ID), unicode.IsControl):
			errs = append(errs, fmt.Errorf("chat: message %q holds a control character in its ID", m.ID))
		case seen[m.ID]:
			errs = append(errs, fmt.Errorf("chat: message %q is listed twice", m.ID))
		}
		seen[m.ID] = true
		if m.Kind != "" && !slices.Contains([]StatusKind{StatusOK, StatusWarn, StatusFail, StatusBusy}, m.Kind) {
			errs = append(errs, fmt.Errorf("chat: message %q has the unknown status kind %q", m.ID, m.Kind))
		}
	}
	return errors.Join(errs...)
}

func cloneMessages(msgs []ChatMessage) []ChatMessage {
	out := slices.Clone(msgs)
	for i := range out {
		out[i].Details = slices.Clone(out[i].Details)
	}
	return out
}

// following reports whether new messages take the selection: the newest is selected, or none, and no quick
// reply has the focus, so a reply always answers the message it stands under.
func (c *Chat) following() bool { return (c.sel < 0 || c.sel == len(c.msgs)-1) && c.at < 0 }

// SetMessages replaces the messages, in the order they are shown, oldest first. The selection stays on its
// message where that remains and follows the newest message where it stood there; the details of the
// messages that remain stay unfolded. It rejects a message without ID or with a control character in it, an ID
// listed twice and an unknown status kind, and then changes nothing.
func (c *Chat) SetMessages(msgs []ChatMessage) error {
	if err := checkMessages(msgs, nil); err != nil {
		return err
	}
	follow := c.following()
	var id ItemID
	if c.sel >= 0 {
		id = c.msgs[c.sel].ID
	}
	c.msgs = cloneMessages(msgs)
	open := map[ItemID]bool{}
	c.sel = -1
	for i, m := range c.msgs {
		if c.open[m.ID] {
			open[m.ID] = true
		}
		if !follow && m.ID == id {
			c.sel = i
		}
	}
	c.open = open
	if c.sel < 0 {
		c.sel = len(c.msgs) - 1
	}
	if c.at >= 0 && (c.sel < 0 || c.msgs[c.sel].ID != id) {
		c.at = chatAtRows
	}
	c.reveal = true
	return nil
}

// Append adds messages at the end. Where the newest message was selected, the selection follows to the new
// newest one. It rejects what SetMessages rejects and an ID the chat already holds, and then changes nothing.
func (c *Chat) Append(msgs ...ChatMessage) error {
	if err := checkMessages(msgs, c.msgs); err != nil {
		return err
	}
	follow := c.following()
	c.msgs = append(c.msgs, cloneMessages(msgs)...)
	if follow && len(c.msgs) > 0 {
		c.sel = len(c.msgs) - 1
		c.reveal = true
	}
	return nil
}

// Selected is the ID of the selected message; false where the chat has none.
func (c *Chat) Selected() (ItemID, bool) {
	if c.sel < 0 {
		return "", false
	}
	return c.msgs[c.sel].ID, true
}

// Select selects the message id and brings it into view; it reports false for an ID the chat does not hold.
func (c *Chat) Select(id ItemID) bool {
	for i, m := range c.msgs {
		if m.ID == id {
			c.choose(i)
			return true
		}
	}
	return false
}

// Draft is the text in the field, with "\n" line breaks.
func (c *Chat) Draft() string { return c.draft.String() }

// SetDraft replaces the text in the field and puts the cursor at its end, for a draft kept elsewhere or a
// sent text that could not be delivered. Control characters other than line breaks are dropped, a tab becomes
// a space, and text past ChatMaxDraftBytes is cut.
func (c *Chat) SetDraft(text string) { c.draft.set(text) }

// Paste puts pasted text into the field at its cursor while the field has the focus, with the rules of
// SetDraft; a paste that would pass ChatMaxDraftBytes is refused whole. It reports whether the field took it.
func (c *Chat) Paste(text string) bool {
	return c.active && c.at == chatAtField && c.draft.insert(text)
}

// Focus gives the focus to the messages for ZoneRows and to the field for ZoneForm; any other zone takes the
// focus from the chat, which then draws no focus and no cursor.
func (c *Chat) Focus(zone FocusZone) {
	switch zone {
	case ZoneRows:
		c.at, c.active = chatAtRows, true
	case ZoneForm:
		c.at, c.active = chatAtField, true
	default:
		c.active = false
	}
}

// Focused is the zone that holds the focus: ZoneRows for the messages and their quick replies, ZoneForm for
// the field and the send button, empty where the chat has none.
func (c *Chat) Focused() FocusZone {
	switch {
	case !c.active:
		return ""
	case c.at == chatAtField || c.at == chatAtSend:
		return ZoneForm
	}
	return ZoneRows
}

// Input is the input focus the chat reads keys in: text in the field, navigation otherwise.
func (c *Chat) Input() InputFocus {
	if c.at == chatAtField {
		return InputText
	}
	return InputNavigation
}

// Update handles a key while the chat has the focus. In the field every key that types a character is text,
// enter adds a line break and ctrl+enter sends, where the terminal tells it from enter; the send button,
// reached with Next, sends with enter in every terminal. A quick reply answers the selected message by its key
// outside the field. handled is false for a key the chat leaves to the application, such as back on the
// messages.
func (c *Chat) Update(msg tea.KeyPressMsg) (ChatResult, bool) {
	if c.at == chatAtField && sendsText(msg) {
		return c.send()
	}
	focus := c.Input()
	k := c.spec.Keys
	for _, id := range []ActionID{k.Back, k.Enter, k.Next, k.Prev, k.Up, k.Down, k.Left, k.Right, k.PageUp,
		k.PageDown, k.First, k.Last} {
		if c.keys.Is(msg, id, focus) {
			return c.Act(id)
		}
	}
	if c.at == chatAtField {
		return ChatResult{}, c.draft.edit(msg)
	}
	for _, b := range c.spec.Actions {
		if c.keys.Is(msg, b.Action, focus) {
			return c.Act(b.Action)
		}
	}
	return ChatResult{}, false
}

// Act runs one of the chat keys or a quick reply as its key does, for a click or a command that stands for the
// key, and reports as Update does.
func (c *Chat) Act(id ActionID) (ChatResult, bool) {
	k := c.spec.Keys
	if id == "" {
		return ChatResult{}, false
	}
	for j, b := range c.spec.Actions {
		if b.Action == id {
			return c.reply(j)
		}
	}
	switch id {
	case k.Next:
		c.step(1)
		return ChatResult{}, true
	case k.Prev:
		c.step(-1)
		return ChatResult{}, true
	case k.PageUp:
		c.scroll(-max(c.area-1, 1))
		return ChatResult{}, true
	case k.PageDown:
		c.scroll(max(c.area-1, 1))
		return ChatResult{}, true
	}
	if c.at == chatAtField {
		return c.fieldAct(id)
	}
	switch id {
	case k.Back:
		if c.at == chatAtRows {
			return ChatResult{}, false
		}
		c.at = chatAtRows
	case k.Enter:
		switch {
		case c.at == chatAtSend:
			return c.send()
		case c.at >= 0:
			return c.reply(c.at)
		case c.sel < 0 || len(c.msgs[c.sel].Details) == 0:
			return ChatResult{}, false
		}
		mid := c.msgs[c.sel].ID
		c.open[mid] = !c.open[mid]
		c.reveal = true
	case k.Up:
		c.choose(c.sel - 1)
	case k.Down:
		c.choose(c.sel + 1)
	case k.First:
		c.choose(0)
	case k.Last:
		c.choose(len(c.msgs) - 1)
	default:
		return ChatResult{}, false
	}
	return ChatResult{}, true
}

// fieldAct runs a chat key in the field.
func (c *Chat) fieldAct(id ActionID) (ChatResult, bool) {
	k := c.spec.Keys
	switch id {
	case k.Back:
		c.at = chatAtRows
	case k.Enter:
		c.draft.insert("\n")
	case k.Left:
		c.draft.move(c.draft.cursor - 1)
	case k.Right:
		c.draft.move(c.draft.cursor + 1)
	case k.Up:
		c.draft.vertical(-1)
	case k.Down:
		c.draft.vertical(1)
	case k.First:
		c.draft.home()
	case k.Last:
		c.draft.end()
	default:
		return ChatResult{}, false
	}
	return ChatResult{}, true
}

// stops are the places the focus steps through in their order: the messages, the quick replies under the
// selected message, the field and the send button.
func (c *Chat) stops() []int {
	out := []int{chatAtRows}
	if c.sel >= 0 {
		for j := range c.spec.Actions {
			out = append(out, j)
		}
	}
	return append(out, chatAtField, chatAtSend)
}

func (c *Chat) step(by int) {
	stops := c.stops()
	i := slices.Index(stops, c.at)
	c.at = stops[wrap(max(i, 0)+by, len(stops))]
	c.active = true
}

// choose selects the message at i, held within the messages, and brings it into view. A quick reply with the
// focus gives it back to the messages, so it never stands under another message than before.
func (c *Chat) choose(i int) {
	if len(c.msgs) == 0 {
		return
	}
	c.sel = max(min(i, len(c.msgs)-1), 0)
	c.reveal = true
	if c.at >= 0 {
		c.at = chatAtRows
	}
}

// Wheel scrolls the messages by the notches of the wheel, WheelTextLines a notch, and stops at the ends.
func (c *Chat) Wheel(steps int) { c.scroll(steps * WheelTextLines) }

// scroll moves the first shown line by n lines. Where the selected message leaves the view, the message
// nearest to it in the view takes the selection.
func (c *Chat) scroll(n int) {
	if len(c.starts) != len(c.msgs) || len(c.msgs) == 0 {
		return
	}
	total := c.ends[len(c.ends)-1]
	c.offset = max(min(c.offset+n, total-c.area), 0)
	c.reveal = false
	top, bottom := c.offset, c.offset+c.area
	if c.sel >= 0 && c.ends[c.sel] > top && c.starts[c.sel] < bottom {
		return
	}
	pick := -1
	for i := range c.msgs {
		if c.ends[i] > top && c.starts[i] < bottom {
			if pick < 0 || n < 0 {
				pick = i
			}
			if n > 0 {
				break
			}
		}
	}
	if pick >= 0 {
		c.sel = pick
		if c.at >= 0 {
			c.at = chatAtRows
		}
	}
}

func (c *Chat) reply(j int) (ChatResult, bool) {
	if c.sel < 0 {
		return ChatResult{}, false
	}
	return ChatResult{Action: c.spec.Actions[j].Action, Message: c.msgs[c.sel].ID}, true
}

// send hands over the draft and empties it; a draft of blanks only is not sent. The field takes the focus for
// the next message.
func (c *Chat) send() (ChatResult, bool) {
	c.at, c.active = chatAtField, true
	text := c.draft.String()
	if strings.TrimSpace(text) == "" {
		return ChatResult{}, true
	}
	c.draft.set("")
	return ChatResult{Send: true, Text: text}, true
}

// Click handles a click on a target the chat drew, as the MouseMap of its targets reports it. A click on a
// message selects it, and only a click on the selected message while the messages have the focus unfolds or
// folds its details, as enter does; a click on the details line does so at once. A click on a quick reply
// answers the selected message, one on the field gives it the focus and types nothing, and one on the send
// button sends.
func (c *Chat) Click(r MouseResult) (ChatResult, bool) {
	if !r.Handled {
		return ChatResult{}, false
	}
	index := func(id string) int {
		return slices.IndexFunc(c.msgs, func(m ChatMessage) bool { return string(m.ID) == id })
	}
	switch {
	case r.Item == chatSendRef:
		return c.send()
	case r.Item == chatFieldRef || r.Item == "" && r.Zone == ZoneForm:
		c.at, c.active = chatAtField, true
	case strings.HasPrefix(r.Item, chatReplyRef):
		j, err := strconv.Atoi(strings.TrimPrefix(r.Item, chatReplyRef))
		if err != nil || j < 0 || j >= len(c.spec.Actions) {
			return ChatResult{}, false
		}
		return c.reply(j)
	case strings.HasPrefix(r.Item, chatDetailRef):
		i := index(strings.TrimPrefix(r.Item, chatDetailRef))
		if i < 0 {
			return ChatResult{}, false
		}
		c.at, c.active = chatAtRows, true
		c.choose(i)
		c.open[c.msgs[i].ID] = !c.open[c.msgs[i].ID]
	case strings.HasPrefix(r.Item, chatMsgRef):
		i := index(strings.TrimPrefix(r.Item, chatMsgRef))
		if i < 0 {
			return ChatResult{}, false
		}
		again := i == c.sel && c.active && c.at == chatAtRows
		c.at, c.active = chatAtRows, true
		c.choose(i)
		if again && len(c.msgs[i].Details) > 0 {
			c.open[c.msgs[i].ID] = !c.open[c.msgs[i].ID]
		}
	case r.Item == "" && r.Zone == ZoneRows:
		c.at, c.active = chatAtRows, true
	default:
		return ChatResult{}, false
	}
	return ChatResult{}, true
}

// Hints are the keys of the focused part for the footer. In the field they name ctrl+enter for sending as
// text alone, since not every terminal tells it from enter, and the next key, which leads to the send button.
func (c *Chat) Hints() []Hint {
	k := c.spec.Keys
	nav := []Hint{{Action: k.Next, Label: TextKeyNext, Kind: HintNavigation},
		{Action: k.Prev, Label: TextKeyPrevious, Kind: HintNavigation}}
	var hints []Hint
	switch {
	case c.at == chatAtField:
		hints = []Hint{{Action: k.Enter, Label: TextKeyNewLine}, {Label: TextChatSendKey},
			{Action: k.Back, Label: TextKeyBack}}
	case c.at == chatAtSend:
		hints = []Hint{{Action: k.Enter, Label: TextChatSend}, {Action: k.Back, Label: TextKeyBack}}
	case c.at >= 0:
		hints = []Hint{{Action: k.Enter, Label: c.spec.Actions[c.at].Label}, {Action: k.Back, Label: TextKeyBack}}
	default:
		if c.sel >= 0 {
			for _, b := range c.spec.Actions {
				hints = append(hints, Hint{Action: b.Action, Label: b.Label})
			}
			if len(c.msgs[c.sel].Details) > 0 {
				hints = append(hints, Hint{Action: k.Enter, Label: TextChatDetailsKey})
			}
		}
		hints = append(hints, Hint{Action: k.Up, Label: TextKeyUp, Kind: HintNavigation},
			Hint{Action: k.Down, Label: TextKeyDown, Kind: HintNavigation})
	}
	return append(hints, nav...)
}

// chatLine is one drawn line of the messages with the item its target carries and the targets of the buttons
// on it, counted from the start of the line.
type chatLine struct {
	text    string
	item    string
	targets []Target
}

// View draws the chat with styles in width by height cells: the messages that fit, the selected one always in
// view and the newest at the bottom while it is selected, a rule, and the field with the send button beside
// it or, where the width runs short, below it. It returns a target for the free area of the messages and of
// the field zone, one per line of a message, one per details line and per button, counted from the top left.
func (c *Chat) View(styles Styles, width, height int) (string, []Target) {
	width, height = max(width, 0), max(height, 0)
	if width == 0 || height == 0 {
		return finishPage(nil, nil, width, height)
	}
	input, inTargets := c.input(styles, width, height)
	rule := 0
	if height > len(input) {
		rule = 1
	}
	c.area = max(height-len(input)-rule, 0)
	lines, targets := c.messages(styles, width)
	if rule == 1 {
		h, _, _ := styles.frameChars(c.Focused() == ZoneForm)
		style := StyleBorder
		if c.Focused() == ZoneForm {
			style = StyleBorderFocus
		}
		lines = append(lines, styles.Render(style, strings.Repeat(h, width)))
	}
	targets = append(targets, OffsetTargets(inTargets, 0, len(lines))...)
	return finishPage(append(lines, input...), targets, width, height)
}

// messages draws the area of the messages, c.area rows, and records where each message starts and ends.
func (c *Chat) messages(styles Styles, width int) ([]string, []Target) {
	if c.area == 0 {
		return nil, nil
	}
	lines := make([]string, 0, c.area)
	targets := []Target{{Rect: Rect{Width: width, Height: c.area}, Zone: ZoneRows}}
	if len(c.msgs) == 0 {
		c.starts, c.ends = nil, nil
		for _, l := range Wrap(Escape(c.text.Lookup(c.spec.Empty)), width) {
			lines = append(lines, styles.Render(StyleHint, l))
		}
		return padLines(lines[:min(len(lines), c.area)], c.area), targets
	}
	var all []chatLine
	c.starts, c.ends = make([]int, len(c.msgs)), make([]int, len(c.msgs))
	for i := range c.msgs {
		if i > 0 {
			all = append(all, chatLine{})
		}
		c.starts[i] = len(all)
		all = append(all, c.block(i, styles, width)...)
		c.ends[i] = len(all)
	}
	if c.reveal && c.sel >= 0 {
		start, end := c.starts[c.sel], c.ends[c.sel]
		switch {
		case end-start > c.area || start < c.offset:
			c.offset = start
		case end > c.offset+c.area:
			c.offset = end - c.area
		}
		c.reveal = false
	}
	c.offset = max(min(c.offset, len(all)-c.area), 0)
	// Messages that do not fill the area stand at its bottom, next to the field, as the newest does.
	lines = padLines(lines, c.area-len(all))
	for i := c.offset; i < len(all) && len(lines) < c.area; i++ {
		ln, y := all[i], len(lines)
		lines = append(lines, ln.text)
		if ln.item != "" {
			targets = append(targets, Target{Rect: Rect{Y: y, Width: width, Height: 1}, Zone: ZoneRows, Item: ln.item})
		}
		targets = append(targets, OffsetTargets(ln.targets, 0, y)...)
	}
	return lines, targets
}

func padLines(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

// block draws the message at i: the head with sender, time and status, the text, the details line with the
// details where they are unfolded and, under the selected message, the quick replies. Every line of the
// selected message carries the selection mark.
func (c *Chat) block(i int, styles Styles, width int) []chatLine {
	m := c.msgs[i]
	gutter := 0
	if width >= 6 {
		gutter = 2
	}
	shift := 0
	if m.Mine && width >= chatMineWidth {
		shift = chatMineShift
	}
	room := max(width-gutter-shift, 1)
	ref := chatMsgRef + string(m.ID)
	lines := []chatLine{{text: c.head(m, styles, room), item: ref}}
	if m.Text != "" {
		for _, part := range strings.Split(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n") {
			for _, w := range Wrap(Escape(part), room) {
				lines = append(lines, chatLine{text: styles.Render(StyleText, w), item: ref})
			}
		}
	}
	if len(m.Details) > 0 {
		glyph := GlyphCollapsed
		if c.open[m.ID] {
			glyph = GlyphExpanded
		}
		label := styles.Glyph(glyph) + " " + Escape(c.text.Lookup(TextChatDetails, len(m.Details)))
		lines = append(lines, chatLine{text: styles.Render(StyleHint, styles.Truncate(label, room)),
			item: chatDetailRef + string(m.ID)})
		if c.open[m.ID] {
			for _, d := range m.Details {
				for _, part := range strings.Split(strings.ReplaceAll(d, "\r\n", "\n"), "\n") {
					for _, w := range Wrap(Escape(part), max(room-2, 1)) {
						lines = append(lines, chatLine{text: "  " + styles.Render(StyleHint, w), item: ref})
					}
				}
			}
		}
	}
	if i == c.sel && len(c.spec.Actions) > 0 {
		rows, targets := c.replies(styles, room)
		for r, row := range rows {
			ln := chatLine{text: row, item: ref}
			for _, t := range targets {
				if t.Rect.Y == r {
					t.Rect.Y = 0
					ln.targets = append(ln.targets, t)
				}
			}
			lines = append(lines, ln)
		}
	}
	lead := strings.Repeat(" ", gutter)
	if i == c.sel && gutter > 0 {
		lead = styles.Render(StyleSelectionMark, styles.SelectionMark()) + " "
	}
	lead += strings.Repeat(" ", shift)
	for j := range lines {
		lines[j].text = lead + lines[j].text
		lines[j].targets = OffsetTargets(lines[j].targets, gutter+shift, 0)
	}
	return lines
}

// statusGlyph is the glyph of a status kind, so the state reads without colour.
func statusGlyph(kind StatusKind) Glyph {
	switch kind {
	case StatusOK:
		return GlyphOK
	case StatusWarn:
		return GlyphWarning
	case StatusFail:
		return GlyphFail
	}
	return GlyphStatus
}

// head is the first line of a message in room cells: the sender, the time and the status, cut at the end.
func (c *Chat) head(m ChatMessage, styles Styles, room int) string {
	style := StyleTitle
	if m.Mine {
		style = StyleActive
	}
	parts := []string{styles.Render(style, Escape(m.From))}
	if stamp := c.stamp(m.At); stamp != "" {
		parts = append(parts, styles.Render(StyleHint, stamp))
	}
	var status []string
	if m.Kind != "" {
		status = append(status, styles.Render(statusStyle(m.Kind), styles.Glyph(statusGlyph(m.Kind))))
	}
	if m.Status != "" {
		status = append(status, styles.Render(StyleHint, Escape(m.Status)))
	}
	if len(status) > 0 {
		parts = append(parts, strings.Join(status, " "))
	}
	return styles.Truncate(strings.Join(parts, "  "), room)
}

// stamp is the time of a message: the time of day for today on the clock of the chat, else with the date, and
// with the year where it is not the current one.
func (c *Chat) stamp(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	now := c.spec.Now().In(at.Location())
	switch {
	case sameDay(now, at.Year(), at.Month(), at.Day()):
		return at.Format(chatTimeLayout)
	case now.Year() == at.Year():
		return at.Format(c.text.Lookup(TextLogDateTime))
	}
	return at.Format(c.text.Lookup(TextLogDateTimeYear))
}

// replies draws the quick replies in rows of at most width cells, each behind the key in force, the focused
// one as focused, with a target per button counted from the top left of the rows.
func (c *Chat) replies(styles Styles, width int) ([]string, []Target) {
	var cells []buttonCell
	for j, b := range c.spec.Actions {
		kind := ButtonNormal
		if b.Primary {
			kind = ButtonPrimary
		}
		label := c.text.Lookup(b.Label)
		if key := c.keys.Label(b.Action); key != "" {
			label = key + " " + label
		}
		cells = append(cells, buttonCell{styles.Button(kind, label, c.active && c.at == j),
			Target{Zone: ZoneRows, Action: b.Action, Item: chatReplyRef + strconv.Itoa(j)}})
	}
	return flowButtons(cells, width)
}

// input draws the field and the send button for a chat of width by height cells: the button beside the first
// row of the field where both fit, else on a row below it. The field takes up to chatInputRows rows and leaves
// at least one row to the messages and one to the rule where the height allows; a chat of one row has no room
// for the button below the field, and the next key still reaches it.
func (c *Chat) input(styles Styles, width, height int) ([]string, []Target) {
	focused := c.active && c.at == chatAtField
	btn := styles.Button(ButtonPrimary, c.text.Lookup(TextChatSend), c.active && c.at == chatAtSend)
	bw := lipgloss.Width(btn)
	gutter := 0
	if width >= 6 {
		gutter = 2
	}
	room := width - gutter
	beside := room-bw-1 >= chatMinField
	below := 0
	if beside {
		room -= bw + 1
	} else {
		below = 1
	}
	rows := len(c.draft.rows(room))
	n := max(min(rows, chatInputRows, height-2-below), 1)
	field := c.draft.view(room, n, styles, focused, c.text.Lookup(TextChatPlaceholder))
	lead := strings.Repeat(" ", gutter)
	var lines []string
	for _, f := range field {
		lines = append(lines, lead+f)
	}
	targets := []Target{{Rect: Rect{Width: gutter + room, Height: len(lines)}, Zone: ZoneForm, Item: chatFieldRef}}
	switch {
	case beside:
		lines[0] += " " + btn
		targets = append(targets, Target{Rect: Rect{X: width - bw, Width: bw, Height: 1}, Zone: ZoneForm, Item: chatSendRef})
	case height > len(lines):
		targets = append(targets, Target{Rect: Rect{X: gutter, Y: len(lines), Width: min(bw, room), Height: 1}, Zone: ZoneForm, Item: chatSendRef})
		lines = append(lines, lead+btn)
	}
	return lines, targets
}

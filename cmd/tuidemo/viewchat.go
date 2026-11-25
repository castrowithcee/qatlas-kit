package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// chatBlock is a Chat of invented messages. A sent text appears as a message of the user, followed by an
// invented answer; the quick replies answer the selected message the same way.
type chatBlock struct {
	chat *tui.Chat
	next int // the number of the next message, for its ID
	at   time.Time
}

func newChatBlock(m *model) (*chatBlock, error) {
	chat, err := tui.NewChat(tui.ChatSpec{
		Keys: tui.ChatKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst, Last: actLast,
			Left: actLeft, Right: actRight, Enter: actEnter, Next: actZoneNext, Prev: actZonePrev, Back: actBack},
		Empty: textChatEmpty, Now: sampleToday,
		Actions: []tui.FormButton{{Action: actChatThanks, Label: textChatThanks, Primary: true}, {Action: actChatLater, Label: textChatLater}},
	}, m.keys, m.text)
	if err != nil {
		return nil, err
	}
	msgs := sampleChat()
	if err := chat.SetMessages(msgs); err != nil {
		return nil, err
	}
	return &chatBlock{chat: chat, next: len(msgs) + 1, at: msgs[len(msgs)-1].At}, nil
}

// said adds a message of the user and the invented answer to it.
func (b *chatBlock) said(m *model, text string) {
	b.at = b.at.Add(time.Minute)
	mine := tui.ChatMessage{ID: tui.ItemID(fmt.Sprintf("m%d", b.next)), From: m.text.Lookup(textChatYou), At: b.at, Mine: true,
		Status: m.text.Lookup(textChatDelivered), Kind: tui.StatusOK, Text: text}
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	b.at = b.at.Add(time.Minute)
	answer := tui.ChatMessage{ID: tui.ItemID(fmt.Sprintf("m%d", b.next+1)), From: "Rosa", At: b.at,
		Text: m.text.Lookup(textChatAnswer, first)}
	b.next += 2
	_ = b.chat.Append(mine, answer) // the IDs are new and hold no control character
}

// took runs what a key or a click asked: a sent text or a quick reply. The focus of the zone follows the chat,
// which moves it between the messages, the replies, the field and the send button.
func (b *chatBlock) took(m *model, res tui.ChatResult) {
	switch {
	case res.Send:
		b.said(m, res.Text)
		m.message = &tui.Message{Kind: tui.MessageResult, Text: m.text.Lookup(textChatSent)}
	case res.Action == actChatThanks:
		b.said(m, m.text.Lookup(textChatThanksText))
	case res.Action == actChatLater:
		b.said(m, m.text.Lookup(textChatLaterText))
	}
	if z := b.chat.Focused(); z != "" {
		m.zone = z
	}
}

func (b *chatBlock) zones() []tui.FocusZone             { return []tui.FocusZone{tui.ZoneRows, tui.ZoneForm} }
func (b *chatBlock) input(tui.FocusZone) tui.InputFocus { return b.chat.Input() }
func (b *chatBlock) wheel(res tui.MouseResult)          { b.chat.Wheel(res.WheelSteps) }
func (b *chatBlock) paste(text string)                  { b.chat.Paste(text) }
func (b *chatBlock) hints(tui.FocusZone) []tui.Hint     { return b.chat.Hints() }

// focus gives the chat the zone, unless it already holds it with a stop of its own, such as a reply.
func (b *chatBlock) focus(zone tui.FocusZone) {
	if b.chat.Focused() != zone {
		b.chat.Focus(zone)
	}
}

func (b *chatBlock) key(m *model, msg tea.KeyPressMsg) bool {
	res, handled := b.chat.Update(msg)
	b.took(m, res)
	return handled
}

func (b *chatBlock) click(m *model, res tui.MouseResult) {
	r, _ := b.chat.Click(res)
	b.took(m, r)
}

// act takes the keys of the chat that a footer hint names, while the focus lies in it.
func (b *chatBlock) act(m *model, action tui.ActionID) bool {
	if m.zone != tui.ZoneRows && m.zone != tui.ZoneForm {
		return false
	}
	res, handled := b.chat.Act(action)
	b.took(m, res)
	return handled
}

func (b *chatBlock) draw(m *model, room tui.Rect) ([]string, []tui.Target) {
	lines := m.note(textNoteViewChat, room)
	view, ts := b.chat.View(m.styles, room.Width, max(room.Height-len(lines), 0))
	return underNote(lines, view, ts)
}

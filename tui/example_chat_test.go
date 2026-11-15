package tui_test

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// A chat between the user and an invented garden helper: the application supplies the messages, the quick
// replies and what a sent text does; the chat draws them and reports what the user asked for.
func ExampleChat() {
	act := func(id tui.ActionID, level tui.KeyLevel, key string) tui.Action {
		return tui.Action{ID: id, Level: level, DefaultKeys: []string{key}, Help: tui.TextID("garden.key." + id)}
	}
	keys, err := tui.NewKeymap([]tui.Action{
		act("back", tui.LevelGlobal, "esc"),
		act("up", tui.LevelMove, "up"), act("down", tui.LevelMove, "down"),
		act("left", tui.LevelMove, "left"), act("right", tui.LevelMove, "right"),
		act("first", tui.LevelMove, "home"), act("last", tui.LevelMove, "end"),
		act("pageup", tui.LevelMove, "pgup"), act("pagedown", tui.LevelMove, "pgdown"),
		act("next", tui.LevelMove, "tab"), act("prev", tui.LevelMove, "shift+tab"),
		act("enter", tui.LevelMove, "enter"),
		act("approve", tui.LevelLetter, "a"),
	}, tui.CharsetASCII)
	if err != nil {
		panic(err)
	}
	text, err := tui.NewTranslator(i18n.English, i18n.Catalogs[tui.TextID]{
		i18n.English: {"garden.empty": "No messages yet.", "garden.approve": "approve"},
		i18n.German:  {"garden.empty": "Noch keine Nachrichten.", "garden.approve": "freigeben"},
	}, keys)
	if err != nil {
		panic(err)
	}
	now := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	chat, err := tui.NewChat(tui.ChatSpec{
		Keys: tui.ChatKeys{Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown", First: "first",
			Last: "last", Left: "left", Right: "right", Enter: "enter", Next: "next", Prev: "prev", Back: "back"},
		Empty:   "garden.empty",
		Actions: []tui.FormButton{{Action: "approve", Label: "garden.approve"}},
		Now:     func() time.Time { return now },
	}, keys, text)
	if err != nil {
		panic(err)
	}
	err = chat.SetMessages([]tui.ChatMessage{
		{ID: "1", From: "you", At: now.Add(-9 * time.Minute), Mine: true, Status: "read", Kind: tui.StatusOK,
			Text: "Order compost for the east beds."},
		{ID: "2", From: "helper", At: now.Add(-2 * time.Minute), Status: "waiting", Kind: tui.StatusWarn,
			Text: "Two bags cost 24.80. Approve?", Details: []string{"supplier: Green Acre"}},
	})
	if err != nil {
		panic(err)
	}

	// The key of a quick reply answers the selected message.
	res, _ := chat.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	fmt.Printf("reply %s to %s\n", res.Action, res.Message)

	// In the field every letter is text and enter adds a line break; tab leads to the send button, where enter
	// sends in every terminal, as ctrl+enter does where the terminal reports it.
	chat.Focus(tui.ZoneForm)
	for _, r := range "Thanks" {
		chat.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	chat.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	chat.Paste("see you")
	view, _ := chat.View(tui.Styles{}, 44, 10)
	for _, line := range strings.Split(view, "\n") {
		fmt.Println(strings.TrimRight(line, " "))
	}
	chat.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	res, _ = chat.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	fmt.Printf("sent %q, draft now %q\n", res.Text, chat.Draft())
	// Output:
	// reply approve to 2
	//       you  09:51  + read
	//       Order compost for the east beds.
	//
	// > helper  09:58  ! waiting
	// > Two bags cost 24.80. Approve?
	// > > details (1)
	// > [ a approve ]
	// ============================================
	//   Thanks                            [ send ]
	//   see you_
	// sent "Thanks\nsee you", draft now ""
}

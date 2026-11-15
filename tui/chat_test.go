package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func chatKeys() ChatKeys {
	return ChatKeys{Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown", First: "first", Last: "last",
		Left: "left", Right: "right", Enter: "enter", Next: "next", Prev: "prev", Back: "back"}
}

func chatActions() []Action {
	return append(sampleActions(),
		Action{ID: "approve", Level: LevelLetter, DefaultKeys: []string{"a"}, Help: "app.approve"},
		Action{ID: "reject", Level: LevelLetter, DefaultKeys: []string{"r"}, Help: "app.reject"})
}

func chatCatalog() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {"app.empty": "No messages yet; write the first one below.", "app.approve": "approve", "app.reject": "reject"},
		i18n.German:  {"app.empty": "Noch keine Nachrichten; schreiben Sie unten die erste.", "app.approve": "freigeben", "app.reject": "ablehnen"},
	}
}

var chatNow = time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)

func newChat(t *testing.T, lang i18n.Lang) *Chat {
	t.Helper()
	keys, err := NewKeymap(chatActions(), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewChat(ChatSpec{Keys: chatKeys(), Empty: "app.empty", Now: func() time.Time { return chatNow },
		Actions: []FormButton{{Action: "approve", Label: "app.approve", Primary: true}, {Action: "reject", Label: "app.reject"}}},
		keys, newTranslator(t, lang, chatCatalog(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func chatMessages() []ChatMessage {
	at := func(d, h, m int) time.Time { return time.Date(2026, 3, d, h, m, 0, 0, time.UTC) }
	return []ChatMessage{
		{ID: "m1", From: "planner", At: at(13, 18, 40), Status: "read", Kind: StatusOK,
			Text:    "The beds on the east side need compost before Friday. Shall I order two bags?",
			Details: []string{"supplier: Green Acre", "price: 12.40 each"}},
		{ID: "m2", From: "you", At: at(14, 9, 12), Status: "sent", Kind: StatusBusy, Mine: true,
			Text: "Yes, order two bags.\nAnd check the hose."},
		{ID: "m3", From: "you", At: at(14, 9, 18), Status: "not delivered", Kind: StatusFail, Mine: true,
			Text: "Also water the seedlings."},
		{ID: "m4", From: "planner", At: at(14, 9, 20), Status: "needs approval", Kind: StatusWarn,
			Text: "Order total 24.80. Approve?"},
	}
}

func goldenChat(t *testing.T, lang i18n.Lang) *Chat {
	t.Helper()
	c := newChat(t, lang)
	if err := c.SetMessages(chatMessages()); err != nil {
		t.Fatal(err)
	}
	c.open["m1"] = true
	c.SetDraft("Thanks,\nsee you")
	return c
}

func chatView(t *testing.T, c *Chat, look string, w, h int) (string, []Target) {
	t.Helper()
	return c.View(looks(t)[look], w, h)
}

func shiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

func ctrlEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl} }

func typeChat(c *Chat, text string) {
	for _, r := range text {
		c.Update(typed(r))
	}
}

func TestNewChatRejectsWhatCannotWork(t *testing.T) {
	keys, err := NewKeymap(chatActions(), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	text := newTranslator(t, i18n.English, chatCatalog(), keys)
	cases := []struct {
		name string
		edit func(*ChatSpec)
		want string
	}{
		{"no empty text", func(s *ChatSpec) { s.Empty = "" }, "empty state"},
		{"missing key", func(s *ChatSpec) { s.Keys.Back = "" }, "back key is missing"},
		{"unknown key", func(s *ChatSpec) { s.Keys.Enter = "open" }, "no action of the key table"},
		{"shared key", func(s *ChatSpec) { s.Keys.Next = "down" }, "also the down key"},
		{"unknown reply", func(s *ChatSpec) { s.Actions = []FormButton{{Action: "nope", Label: "x"}} }, "no action of the key table"},
		{"reply is a chat key", func(s *ChatSpec) { s.Actions = []FormButton{{Action: "enter", Label: "x"}} }, "a key of the chat"},
		{"reply twice", func(s *ChatSpec) {
			s.Actions = []FormButton{{Action: "approve", Label: "x"}, {Action: "approve", Label: "y"}}
		}, "on another quick reply"},
		{"reply without label", func(s *ChatSpec) { s.Actions = []FormButton{{Action: "approve"}} }, "has no label"},
	}
	for _, tt := range cases {
		spec := ChatSpec{Keys: chatKeys(), Empty: "app.empty"}
		tt.edit(&spec)
		if _, err := NewChat(spec, keys, text); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := NewChat(ChatSpec{Keys: chatKeys(), Empty: "app.empty"}, nil, text); err == nil {
		t.Error("a missing key table is accepted")
	}
}

func TestChatRejectsBadMessages(t *testing.T) {
	c := newChat(t, i18n.English)
	for name, msgs := range map[string][]ChatMessage{
		"no ID":           {{From: "a"}},
		"control in ID":   {{ID: "a\x1bb"}},
		"ID twice":        {{ID: "a"}, {ID: "a"}},
		"unknown kind":    {{ID: "a", Kind: "great"}},
		"unknown and dup": {{ID: "a", Kind: "great"}, {ID: "a"}},
	} {
		if err := c.SetMessages(msgs); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := c.SetMessages(chatMessages()); err != nil {
		t.Fatal(err)
	}
	if err := c.Append(ChatMessage{ID: "m2"}); err == nil {
		t.Error("Append accepts an ID the chat holds")
	}
	if len(c.msgs) != 4 {
		t.Errorf("a refused Append changed the messages: %d", len(c.msgs))
	}
}

func TestGoldenChat(t *testing.T) {
	for _, tt := range []struct {
		name, look string
		lang       i18n.Lang
		w, h       int
	}{
		{"chat.narrow.golden", "noneascii", i18n.English, 40, 24},
		{"chat.wide.golden", "noneascii", i18n.English, 100, 22},
		{"chat.wide.de.golden", "noneascii", i18n.German, 100, 22},
		{"chat.wide.dark.golden", "dark", i18n.English, 100, 22},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := goldenChat(t, tt.lang)
			if tt.look == "dark" {
				c.Focus(ZoneForm)
			}
			out, _ := chatView(t, c, tt.look, tt.w, tt.h)
			checkGolden(t, tt.name, out+"\n")
		})
	}
}

// The draft is sent without ctrl+enter and without the mouse: next leads from the field to the send button,
// and enter there sends. Enter in the field adds a line break.
func TestChatSendsByKeyboardAlone(t *testing.T) {
	c := newChat(t, i18n.English)
	c.Focus(ZoneForm)
	typeChat(c, "hello")
	c.Update(press("enter"))
	typeChat(c, "world")
	if got := c.Draft(); got != "hello\nworld" {
		t.Fatalf("draft = %q, want a line break from enter", got)
	}
	if _, ok := c.Update(press("tab")); !ok || c.at != chatAtSend || c.Focused() != ZoneForm {
		t.Fatalf("tab from the field does not reach the send button: at %d", c.at)
	}
	res, ok := c.Update(press("enter"))
	if !ok || !res.Send || res.Text != "hello\nworld" {
		t.Fatalf("enter on send = %+v, %v", res, ok)
	}
	if c.Draft() != "" || c.at != chatAtField {
		t.Errorf("after sending the draft is %q and the focus %d; want empty and in the field", c.Draft(), c.at)
	}
	// A draft of blanks is not sent.
	typeChat(c, "  ")
	if res, ok := c.Update(ctrlEnter()); !ok || res.Send {
		t.Errorf("a blank draft is sent: %+v", res)
	}
}

func TestChatSendsWithCtrlEnter(t *testing.T) {
	if got := KeyName(ctrlEnter()); got != textAreaSend {
		t.Fatalf("ctrl+enter is reported as %q", got)
	}
	c := newChat(t, i18n.English)
	c.Focus(ZoneForm)
	typeChat(c, "ok")
	if res, _ := c.Update(ctrlEnter()); !res.Send || res.Text != "ok" {
		t.Errorf("ctrl+enter = %+v, want the draft sent", res)
	}
	// Outside the field it sends nothing.
	c.Focus(ZoneRows)
	c.SetDraft("kept")
	if res, _ := c.Update(ctrlEnter()); res.Send || c.Draft() != "kept" {
		t.Errorf("ctrl+enter on the messages sends: %+v", res)
	}
}

// ctrl+enter is the field's own key: no action of a key table may take it except as a second way, so it never
// collides with an application key.
func TestCtrlEnterIsNoActionKey(t *testing.T) {
	table := append(chatActions(), Action{ID: "probe", Level: LevelCtrl, DefaultKeys: []string{textAreaSend}, Help: "x"})
	if _, err := NewKeymap(table, CharsetUnicode); err == nil {
		t.Fatal("an action takes ctrl+enter")
	}
	k, err := NewKeymap(chatActions(), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.CheckBinding("save", textAreaSend, nil); err == nil {
		t.Error("ctrl+enter can be bound")
	}
	if id, ok := k.Match(ctrlEnter(), InputText); ok {
		t.Errorf("ctrl+enter matches %s", id)
	}
}

// Letters are text in the field and never answer; back leaves the field, where the key of a quick reply
// answers the selected message.
func TestChatLettersAreTextInTheField(t *testing.T) {
	c := newChat(t, i18n.English)
	if err := c.SetMessages(chatMessages()); err != nil {
		t.Fatal(err)
	}
	c.Focus(ZoneForm)
	if c.Input() != InputText {
		t.Fatal("the field does not read text")
	}
	for _, r := range "ar?q" {
		if res, ok := c.Update(typed(r)); !ok || res != (ChatResult{}) {
			t.Errorf("%c in the field = %+v, %v", r, res, ok)
		}
	}
	if c.Draft() != "ar?q" {
		t.Fatalf("draft = %q", c.Draft())
	}
	if _, ok := c.Update(press("esc")); !ok || c.Focused() != ZoneRows || c.Input() != InputNavigation {
		t.Fatalf("esc does not leave the field: %q", c.Focused())
	}
	res, ok := c.Update(typed('a'))
	if !ok || res.Action != "approve" || res.Message != "m4" {
		t.Errorf("a on the messages = %+v, want approve of m4", res)
	}
	if _, ok := c.Update(press("esc")); ok {
		t.Error("esc on the messages is not left to the application")
	}
}

func TestChatEnterFoldsDetails(t *testing.T) {
	c := newChat(t, i18n.English)
	if err := c.SetMessages(chatMessages()); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Update(press("enter")); ok {
		t.Error("enter on a message without details is handled")
	}
	c.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if id, _ := c.Selected(); id != "m1" {
		t.Fatalf("home selects %s", id)
	}
	c.Update(press("enter"))
	out, _ := chatView(t, c, "noneascii", 60, 30)
	if !strings.Contains(out, "v details (2)") || !strings.Contains(out, "supplier: Green Acre") {
		t.Fatalf("enter does not unfold the details:\n%s", out)
	}
	c.Update(press("enter"))
	if out, _ = chatView(t, c, "noneascii", 60, 30); strings.Contains(out, "supplier") || !strings.Contains(out, "> details (2)") {
		t.Errorf("enter does not fold the details:\n%s", out)
	}
}

// Tab steps through the messages, each quick reply, the field and the send button and wraps round; enter on a
// focused quick reply answers the message it stands under.
func TestChatTabOrder(t *testing.T) {
	c := newChat(t, i18n.English)
	if err := c.SetMessages(chatMessages()); err != nil {
		t.Fatal(err)
	}
	want := []int{0, 1, chatAtField, chatAtSend, chatAtRows}
	for i, at := range want {
		c.Update(press("tab"))
		if c.at != at {
			t.Fatalf("tab %d: at %d, want %d", i+1, c.at, at)
		}
	}
	c.Update(shiftTab())
	if c.at != chatAtSend {
		t.Fatalf("shift+tab from the messages: at %d", c.at)
	}
	c.Focus(ZoneRows)
	c.Update(press("tab"))
	c.Update(press("tab"))
	if res, _ := c.Update(press("enter")); res.Action != "reject" || res.Message != "m4" {
		t.Errorf("enter on the second reply = %+v", res)
	}
	// A new message does not move the selection away from the focused reply.
	if err := c.Append(ChatMessage{ID: "m5", From: "planner", Text: "Anything else?"}); err != nil {
		t.Fatal(err)
	}
	if res, _ := c.Update(press("enter")); res.Message != "m4" {
		t.Errorf("after a new message the reply answers %s", res.Message)
	}
	h := c.keys.Hints(c.Hints(), c.Input(), c.text)
	if len(h) == 0 || h[0].Text() != "enter reject" {
		t.Errorf("hints on a reply: %+v", h)
	}
	// Where its message goes, the focused reply gives the focus back to the messages.
	if err := c.SetMessages(chatMessages()[:2]); err != nil {
		t.Fatal(err)
	}
	if c.at != chatAtRows {
		t.Errorf("the reply keeps the focus after its message went: at %d", c.at)
	}
}

func TestChatFollowsTheNewestMessage(t *testing.T) {
	c := newChat(t, i18n.English)
	if err := c.SetMessages(chatMessages()); err != nil {
		t.Fatal(err)
	}
	chatView(t, c, "noneascii", 60, 12)
	if err := c.Append(ChatMessage{ID: "m5", From: "planner", Text: "Ordered."}); err != nil {
		t.Fatal(err)
	}
	out, _ := chatView(t, c, "noneascii", 60, 12)
	if id, _ := c.Selected(); id != "m5" || !strings.Contains(out, "Ordered.") {
		t.Fatalf("the newest message is not followed (%s):\n%s", id, out)
	}
	c.Select("m2")
	if err := c.Append(ChatMessage{ID: "m6", From: "planner", Text: "Delivered."}); err != nil {
		t.Fatal(err)
	}
	if id, _ := c.Selected(); id != "m2" {
		t.Errorf("a new message took the selection from an older one: %s", id)
	}
	// SetMessages keeps the selection on its message.
	msgs := append(chatMessages()[:1], chatMessages()[1:]...)
	msgs[1].Status = "delivered"
	if err := c.SetMessages(msgs); err != nil {
		t.Fatal(err)
	}
	if id, _ := c.Selected(); id != "m2" {
		t.Errorf("SetMessages moved the selection to %s", id)
	}
}

func TestChatWheelAndPageKeepTheSelectionInView(t *testing.T) {
	c := newChat(t, i18n.English)
	var msgs []ChatMessage
	for i := 0; i < 30; i++ {
		msgs = append(msgs, ChatMessage{ID: ItemID(rune('A' + i)), From: "bot", Text: "line"})
	}
	if err := c.SetMessages(msgs); err != nil {
		t.Fatal(err)
	}
	chatView(t, c, "noneascii", 40, 12)
	bottom := c.offset
	c.Wheel(-1)
	if c.offset != bottom-WheelTextLines {
		t.Fatalf("a notch up scrolls to %d, want %d", c.offset, bottom-WheelTextLines)
	}
	for i := 0; i < 40; i++ {
		c.Wheel(-1)
	}
	if c.offset != 0 {
		t.Errorf("the wheel does not stop at the top: %d", c.offset)
	}
	out, targets := chatView(t, c, "noneascii", 40, 12)
	id, _ := c.Selected()
	if !inView(targets, chatMsgRef+string(id)) {
		t.Errorf("the selection %s left the view:\n%s", id, out)
	}
	c.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	_, targets = chatView(t, c, "noneascii", 40, 12)
	if id, _ = c.Selected(); !inView(targets, chatMsgRef+string(id)) || c.offset == 0 {
		t.Errorf("page down: offset %d, selection %s out of view", c.offset, id)
	}
}

func inView(targets []Target, item string) bool {
	for _, tg := range targets {
		if tg.Item == item {
			return true
		}
	}
	return false
}

func clickAt(m *MouseMap, x, y int) MouseResult {
	return m.Handle(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func findTarget(t *testing.T, targets []Target, match func(Target) bool) Target {
	t.Helper()
	for _, tg := range targets {
		if match(tg) {
			return tg
		}
	}
	t.Fatalf("no such target in %+v", targets)
	return Target{}
}

// Every button the chat draws is a target, and a click does what its key does.
func TestChatMouseTargets(t *testing.T) {
	c := goldenChat(t, i18n.English)
	_, targets := chatView(t, c, "noneascii", 100, 30)
	m := NewMouseMap(targets, true)
	click := func(tg Target) (ChatResult, bool) {
		return c.Click(clickAt(m, tg.Rect.X, tg.Rect.Y))
	}
	approve := findTarget(t, targets, func(tg Target) bool { return tg.Action == "approve" })
	if res, ok := click(approve); !ok || res.Action != "approve" || res.Message != "m4" {
		t.Errorf("click on approve = %+v", res)
	}
	reject := findTarget(t, targets, func(tg Target) bool { return tg.Action == "reject" })
	if res, _ := click(reject); res.Action != "reject" {
		t.Errorf("click on reject = %+v", res)
	}
	field := findTarget(t, targets, func(tg Target) bool { return tg.Item == chatFieldRef })
	if _, ok := click(field); !ok || c.Focused() != ZoneForm || c.Draft() != "Thanks,\nsee you" {
		t.Errorf("click on the field: focus %q, draft %q", c.Focused(), c.Draft())
	}
	// A click on a message selects it; a second one unfolds nothing without details, and the details line folds.
	m2 := findTarget(t, targets, func(tg Target) bool { return tg.Item == chatMsgRef+"m2" })
	click(m2)
	if id, _ := c.Selected(); id != "m2" || c.Focused() != ZoneRows {
		t.Errorf("click on m2 selects %s with focus %q", id, c.Focused())
	}
	details := findTarget(t, targets, func(tg Target) bool { return tg.Item == chatDetailRef+"m1" })
	click(details)
	if c.open["m1"] {
		t.Error("click on the details line does not fold them")
	}
	_, targets = chatView(t, c, "noneascii", 100, 30)
	m = NewMouseMap(targets, true)
	m1 := findTarget(t, targets, func(tg Target) bool { return tg.Item == chatMsgRef+"m1" })
	click(m1)
	if !c.open["m1"] {
		t.Error("a click on the selected message does not unfold its details")
	}
	// The free area of the messages takes the focus, and the send button sends.
	c.Focus(ZoneForm)
	free := findTarget(t, targets, func(tg Target) bool { return tg.Zone == ZoneRows && tg.Item == "" && tg.Action == "" })
	if _, ok := c.Click(MouseResult{Zone: free.Zone, Handled: true}); !ok || c.Focused() != ZoneRows {
		t.Error("the free area does not take the focus")
	}
	send := findTarget(t, targets, func(tg Target) bool { return tg.Item == chatSendRef })
	if res, _ := click(send); !res.Send || res.Text != "Thanks,\nsee you" {
		t.Errorf("click on send = %+v", res)
	}
	if res, ok := c.Click(MouseResult{}); ok || res != (ChatResult{}) {
		t.Error("an event that did nothing is handled")
	}
}

// The send button stays a target where the width puts it below the field.
func TestChatNarrowSendButton(t *testing.T) {
	c := goldenChat(t, i18n.English)
	for _, w := range []int{20, 12, 6} {
		out, targets := chatView(t, c, "noneascii", w, 10)
		send := findTarget(t, targets, func(tg Target) bool { return tg.Item == chatSendRef })
		row := strings.Split(out, "\n")[send.Rect.Y]
		if !strings.Contains(row, "[ ") {
			t.Errorf("width %d: the send target is on %q", w, row)
		}
	}
	for _, size := range [][2]int{{0, 0}, {1, 1}, {3, 2}, {-1, 5}} {
		c.View(plainLog(t), size[0], size[1])
	}
}

// Sender, text, status and details are untrusted: no control character reaches the terminal.
func TestChatEscapesUntrustedText(t *testing.T) {
	c := newChat(t, i18n.English)
	evil := "\x1b[31mred\x1b]0;title\x07\r\b"
	if err := c.SetMessages([]ChatMessage{{ID: "x", From: evil, Status: evil, Kind: StatusFail, Text: evil,
		Details: []string{evil}}}); err != nil {
		t.Fatal(err)
	}
	c.open["x"] = true
	c.SetDraft("draft\x1b[2J")
	for name, st := range looks(t) {
		out, _ := c.View(st, 120, 20)
		plain := stripSGR(out)
		if strings.ContainsAny(plain, "\x07\r\b") || strings.Contains(plain, "\x1b") {
			t.Errorf("%s: a control character reaches the terminal: %q", name, plain)
		}
		if !strings.Contains(plain, `\x1b[31mred`) {
			t.Errorf("%s: the escaped text is not shown: %q", name, plain)
		}
	}
	if strings.Contains(c.Draft(), "\x1b") {
		t.Errorf("the draft holds an escape: %q", c.Draft())
	}
}

// stripSGR drops the style sequences the styles themselves write.
func stripSGR(s string) string { return sgrSequence.ReplaceAllString(s, "") }

func TestChatStatusMarksReadWithoutColour(t *testing.T) {
	for look, want := range map[string][]string{
		"noneascii": {"+ read", "* sent", "x not delivered", "! needs approval"},
		"dark":      {"✓", "●", "✗", "⚠"},
	} {
		c := goldenChat(t, i18n.English)
		out, _ := chatView(t, c, look, 100, 30)
		plain := stripSGR(out)
		for _, w := range want {
			if !strings.Contains(plain, w) {
				t.Errorf("%s: no %q in\n%s", look, w, plain)
			}
		}
	}
}

func TestChatPaste(t *testing.T) {
	c := newChat(t, i18n.English)
	if c.Paste("text") {
		t.Error("a paste on the messages is taken")
	}
	c.Focus(ZoneForm)
	c.Focus(ZoneTabs)
	if c.Paste("text") || c.Focused() != "" {
		t.Error("a chat without the focus takes a paste")
	}
	c.Focus(ZoneForm)
	if !c.Paste("one\r\ntwo\tthree\x00") || c.Draft() != "one\ntwo three" {
		t.Errorf("paste = %q", c.Draft())
	}
	if c.Paste(strings.Repeat("x", ChatMaxDraftBytes)) || c.Draft() != "one\ntwo three" {
		t.Error("a paste over the limit is taken")
	}
	c.SetDraft(strings.Repeat("é", ChatMaxDraftBytes))
	if n := len(c.Draft()); n > ChatMaxDraftBytes {
		t.Errorf("SetDraft holds %d bytes", n)
	}
}

func TestChatFieldEditing(t *testing.T) {
	c := newChat(t, i18n.English)
	c.Focus(ZoneForm)
	c.SetDraft("abc\ndefgh")
	chatView(t, c, "noneascii", 40, 10)
	c.Update(press("up"))
	c.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if c.Draft() != "ab\ndefgh" {
		t.Fatalf("up then backspace: %q", c.Draft())
	}
	c.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	c.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if c.Draft() != "abdefgh" {
		t.Fatalf("end then delete: %q", c.Draft())
	}
	c.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	typeChat(c, ">")
	if c.Draft() != ">abdefgh" {
		t.Errorf("home then typing: %q", c.Draft())
	}
	h := c.keys.Hints(c.Hints(), c.Input(), c.text)
	var texts []string
	for _, g := range h {
		texts = append(texts, g.Text())
	}
	if joined := strings.Join(texts, " | "); !strings.Contains(joined, "enter new line") || !strings.Contains(joined, "ctrl+enter send") ||
		!strings.Contains(joined, "esc back") || !strings.Contains(joined, "tab next field") {
		t.Errorf("field hints: %s", joined)
	}
}

func TestChatCatalogTexts(t *testing.T) {
	if err := CheckCatalogs(ModuleCatalog()); err != nil {
		t.Fatal(err)
	}
	cat := ModuleCatalog()
	for _, id := range []TextID{TextChatSend, TextChatSendKey, TextChatPlaceholder, TextChatDetails, TextChatDetailsKey} {
		if cat[i18n.English][id] == "" || cat[i18n.German][id] == "" {
			t.Errorf("%s misses a text", id)
		}
	}
	c := newChat(t, i18n.German)
	out, _ := chatView(t, c, "noneascii", 60, 6)
	if !strings.Contains(out, "Noch keine Nachrichten") || !strings.Contains(out, "Nachricht schreiben") || !strings.Contains(out, "Senden") {
		t.Errorf("German empty chat:\n%s", out)
	}
}

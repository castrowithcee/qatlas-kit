package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

func TestViewsTextNumbersSearchAndRevealByKeyAndClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "9", "Sample configuration")
	act(t, term, "\t\t", "line numbers off") // the sub-tab line, then the view
	act(t, term, "#", "line numbers on")
	expect(t, term, "  3 ")
	act(t, term, "/not-a-sec", "/ not-a-sec")
	expect(t, term, "no matches") // the masked section holds no match while hidden
	send(t, term, "\x1b")
	gone(t, term, "no matches")
	act(t, term, "/east", "/ east")
	act(t, term, "\r", "match 1/1")
	send(t, term, "\x1b")
	gone(t, term, "match 1/1")
	gone(t, term, "example-not-a-secret")
	// By key: the workbench asks first, and only yes shows the secret.
	act(t, term, "V", "Show the masked secret?")
	act(t, term, "n", "Cancelled")
	gone(t, term, "example-not-a-secret")
	act(t, term, "V", "Show the masked secret?")
	act(t, term, "y", "example-not-a-secret")
	act(t, term, "V", "reveal") // hiding needs no question
	gone(t, term, "example-not-a-secret")
	// By click: the button of the status row, then the answer.
	clickOn(t, term, "V reveal", "Show the masked secret?")
	clickOn(t, term, "yes", "example-not-a-secret")
	clickOn(t, term, "V hide", "reveal")
	gone(t, term, "example-not-a-secret")
	clickOn(t, term, "# line numbers on", "line numbers off")
}

func TestViewsLogFilterPeriodByKeyAndClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "9", "Text")
	clickOn(t, term, "Log", "period: all")
	act(t, term, "\t", "lines 41-60")
	act(t, term, "P", "Time period")
	// Last 1 hour: the count is the first text field, its unit the choice behind it.
	act(t, term, "\x1b[C", "last n units")
	act(t, term, "\t", "Count")
	act(t, term, "\x1b[3~", "Count") // delete the default count
	act(t, term, "\b", "Count")
	act(t, term, "3", "3")
	act(t, term, keyF2, "Period applied")
	expect(t, term, "period: last 3")
	gone(t, term, "Time period")
	// By click: the button of the status row, then the buttons of the dialog.
	clickOn(t, term, "P period", "Time period")
	clickOn(t, term, "discard", "period: last 3")
	clickOn(t, term, "P period", "Time period")
	act(t, term, "\x1b[D", "all")
	clickOn(t, term, "apply", "Period applied")
	expect(t, term, "period: all")
}

func TestViewsLogSwitchByKeyAndClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "9", "Text")
	clickOn(t, term, "Log", "L following")
	act(t, term, "\t", "lines 41-60")
	act(t, term, "L", "L paused")
	gone(t, term, "L following")
	act(t, term, "w", "w wrap lines on")
	act(t, term, "h", "h off")
	gone(t, term, "21:07:00")
	clickOn(t, term, "L paused", "L following")
	clickOn(t, term, "w wrap lines on", "w wrap lines off")
	clickOn(t, term, "h off", "h on")
	expect(t, term, "21:07:00")
}

func TestViewsChatWritesLinesSendsAndAnswersByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "9", "Text")
	clickOn(t, term, "Chat", "Good morning")
	act(t, term, "\t\t", "Write a message")
	act(t, term, "\t", "> ") // the messages first
	clickOn(t, term, "Write a message", "Write a message")
	act(t, term, "first line\rsecond line", "second line")
	expect(t, term, "first line")
	act(t, term, "\t\r", "Message sent") // tab leads to the send button, enter sends
	expect(t, term, "Noted: first line")
	clickOn(t, term, "k thanks", "Thanks, that helps.")
	act(t, term, "\x1b", "Noted:")
}

func TestViewsLettersTypeIntoChatAndSearchNotIntoSwitches(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "9")
	press := func(msg tea.KeyPressMsg) { m.Update(msg) }
	typeIn := func(s string) {
		for _, r := range s {
			press(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	unchanged := func(what string) {
		t.Helper()
		if m.prefs.Theme != tui.ThemeTerminal || m.prefs.Language != uiprefs.LanguageEnglish || m.prefs.Icons != uiprefs.IconsNone || !m.prefs.MouseEnabled {
			t.Fatalf("a letter typed into %s switched something: %+v", what, m.prefs)
		}
	}
	// The search of the text view and of the log view.
	for _, tab := range []tui.TabID{"text", "log"} {
		m.tabs["views"] = tab
		m.zone = tui.ZoneRows
		press(tea.KeyPressMsg{Code: '/', Text: "/"})
		if m.input() != tui.InputText {
			t.Fatalf("%s: the search key did not start typing", tab)
		}
		typeIn("tlIMqwLh#V")
		unchanged(string(tab) + " search")
		press(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.input() != tui.InputNavigation || m.zone != tui.ZoneRows {
			t.Fatalf("%s: back did not end the search", tab)
		}
	}
	// The field of the chat, which also takes the keys of the quick replies.
	m.tabs["views"] = "chat"
	m.zone = tui.ZoneForm
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	typeIn("tlIMqkz+")
	unchanged("the chat field")
	if got := m.live().(*chatBlock).chat.Draft(); got != "tlIMqkz+" {
		t.Fatalf("draft %q, want the typed letters", got)
	}
	if n := len(m.live().(*chatBlock).chat.Draft()); n == 0 || strings.Contains(m.view, "Thanks") {
		t.Fatalf("a typed reply key answered a message")
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.zone != tui.ZoneRows {
		t.Fatalf("zone %q after back from the field, want the messages", m.zone)
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+c did not quit")
	}
}

// drag is the press at the 1-based cell from, motion with the button held and the release at to.
func drag(fromX, fromY, toX, toY int) string {
	return fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<32;%d;%dM\x1b[<0;%d;%dm", fromX, fromY, toX, toY, toX, toY)
}

// clipboard returns the texts handed to the terminal by OSC 52 sequences in the output.
func clipboard(t *testing.T, output string) []string {
	t.Helper()
	var got []string
	for _, part := range strings.Split(output, "\x1b]52;")[1:] {
		payload, _, _ := strings.Cut(part, "\x07")
		payload, _, _ = strings.Cut(payload, "\x1b\\")
		_, data, _ := strings.Cut(payload, ";")
		text, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			t.Fatalf("clipboard payload %q: %v", data, err)
		}
		got = append(got, string(text))
	}
	return got
}

func TestViewsTextDragCopiesContentOnly(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "9", "Sample configuration")
	act(t, term, "\t\t", "line numbers off")
	act(t, term, "#", "line numbers on")
	x, y := find(t, term, "notify:")
	endX, endY := find(t, term, "retries: 3")
	// A click without motion copies nothing.
	clickOnly(t, term, "retries: 3")
	if got := clipboard(t, term.Output()); len(got) != 0 {
		t.Fatalf("a click handed %q to the clipboard", got)
	}
	send(t, term, drag(x, y, endX+len("retries: 3")-1, endY))
	expect(t, term, "Handed to the terminal for the clipboard")
	got := clipboard(t, term.Output())
	if len(got) != 1 {
		t.Fatalf("clipboard texts = %q, want one", got)
	}
	if !strings.HasPrefix(got[0], "notify:\n  token: ") || !strings.HasSuffix(got[0], "\n  retries: 3") || strings.Count(got[0], "\n") != 2 {
		t.Errorf("copied %q, want the three lines of content", got[0])
	}
	if strings.Contains(got[0], sampleSecret) {
		t.Errorf("copied %q holds the masked secret", got[0])
	}
	if strings.Contains(term.Output(), base64.StdEncoding.EncodeToString([]byte(sampleSecret))) {
		t.Error("the output holds the secret in base64")
	}
}

func TestViewsLogDragCopiesContentOnly(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "9", "Text")
	clickOn(t, term, "Log", "period: all")
	x, y := find(t, term, "pump started")
	send(t, term, drag(x, y, x+len("pump started")-1, y))
	expect(t, term, "Handed to the terminal for the clipboard")
	if got := clipboard(t, term.Output()); len(got) != 1 || got[0] != "pump started" {
		t.Errorf("clipboard texts = %q, want the dragged part of the line only", got)
	}
}

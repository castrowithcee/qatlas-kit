package main

import (
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

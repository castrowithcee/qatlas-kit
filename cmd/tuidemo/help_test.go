package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

func TestHelpShowsThePageOfTheScreenInTheLanguageOfTheDemo(t *testing.T) {
	cases := []struct {
		area, tab, language, topic, page string
	}{
		{"overview", "about", uiprefs.LanguageEnglish, "start", "index"},
		{"frame", "mouse", uiprefs.LanguageEnglish, "controls", "mouse"},
		{"help", "commands", uiprefs.LanguageEnglish, "controls", "keys"},
		{"help", "help", uiprefs.LanguageEnglish, "controls", "index"},
		{"lists", "filter", uiprefs.LanguageEnglish, "start", "index"}, // no page: the first one
	}
	for _, c := range cases {
		m := testModel(t, uiprefs.IconsNone)
		m.openArea(tui.AreaID(c.area))
		m.tabs[tui.AreaID(c.area)] = tui.TabID(c.tab)
		m.openHelp()
		h, ok := m.modal.(*tui.Help)
		if !ok {
			t.Fatalf("%s/%s: no help opened", c.area, c.tab)
		}
		if topic, page := h.Page(); topic != c.topic || page != c.page {
			t.Errorf("%s/%s: page %s/%s, want %s/%s", c.area, c.tab, topic, page, c.topic, c.page)
		}
	}
}

func TestHelpFollowsTheLanguageOfTheDemo(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.setLanguage(uiprefs.LanguageGerman)
	m.openHelp()
	view, _ := m.modal.(*tui.Help).View(m.styles, 80, 24)
	if !strings.Contains(ansi.Strip(view), "Über die Werkbank") {
		t.Errorf("the help is not German:\n%s", ansi.Strip(view))
	}
}

func TestHelpAnswersToF1AndOpensFromAnywhere(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.kind != kindHelp {
		t.Fatalf("f1 opens kind %d, want the help", m.kind)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.modal != nil {
		t.Error("f1 does not close the open help")
	}
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.modal != nil {
		t.Error("? does not close the open help")
	}
}

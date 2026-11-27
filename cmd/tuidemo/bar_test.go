package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// barRow is the row of the screen below the rule that holds the action bar, and its footer.
func barRow(m *model) (bar, footer string) {
	rows := strings.Split(strings.TrimRight(m.view, "\n"), "\n")
	for i, r := range rows {
		if strings.HasPrefix(r, "---") || strings.HasPrefix(r, "───") {
			return strings.TrimSpace(rows[i+1]), strings.Join(rows[len(rows)-2:], "\n")
		}
	}
	return "", ""
}

// The files page puts the actions of the browser into the bar, and the browser draws none below its list.
func TestActionBarOfTheFilesPageRunsLikeItsKey(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "8")
	m.tabs["pickers"] = "files"
	m.zone = tui.ZoneRows
	m.refresh()
	bar, _ := barRow(m)
	if bar != "[ x clear chosen file ] [ g choose file ]" {
		t.Fatalf("bar = %q\n%s", bar, m.view)
	}
	if strings.Count(m.view, "choose file") != 1 {
		t.Errorf("the action stands more than once:\n%s", m.view)
	}
	// The first row is a folder: the click warns as the key does.
	if !clickWhere(m, func(r tui.MouseResult) bool { return r.Action == actTake }) {
		t.Fatal("no button of the bar is a click target")
	}
	if m.message == nil || m.message.Kind != tui.MessageWarning {
		t.Errorf("click on the button: message %+v", m.message)
	}
	m.message = nil
	m.handle(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if m.message == nil || m.message.Kind != tui.MessageWarning {
		t.Errorf("key: message %+v", m.message)
	}
	// The second button acts as its key: it forgets the file taken.
	b := m.live().(*filesBlock)
	b.chosen = "notes.txt"
	if !clickWhere(m, func(r tui.MouseResult) bool { return r.Action == actClear }) || b.chosen != "" {
		t.Errorf("click on clear: chosen %q", b.chosen)
	}
}

// At a narrow width the bar keeps the first button at the right end and cuts from the left, and the cut action keeps its hint
// in the footer.
func TestActionBarCutsAtANarrowWidth(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "8")
	m.tabs["pickers"] = "files"
	m.zone = tui.ZoneRows
	m.Update(tea.WindowSizeMsg{Width: minWidth, Height: 30})
	bar, footer := barRow(m)
	if bar != "[ g choose file ]" {
		t.Errorf("narrow: %q\n%s", bar, m.view)
	}
	if strings.Contains(footer, "choose file") || !strings.Contains(footer, "x clear chosen") {
		t.Errorf("footer of a cut bar:\n%s", footer)
	}
}

// A button of a page that has no block is a click target and opens the part as its key does.
func TestActionBarOfTheDialogsPageClicksRun(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "4")
	m.tabs["dialogs"] = "question"
	m.refresh()
	if bar, _ := barRow(m); bar != "[ a ask question ]" {
		t.Errorf("bar = %q", bar)
	}
	if !clickWhere(m, func(r tui.MouseResult) bool { return r.Action == actAsk }) || m.modal == nil {
		t.Errorf("click on ask did not open the question: modal %v", m.modal)
	}
}

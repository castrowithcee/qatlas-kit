package main

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

func testModel(t *testing.T, icons string) *model {
	t.Helper()
	prefs := uiprefs.Default(uiprefs.Schema{DefaultArea: "overview"})
	prefs.Icons = icons
	m, err := newModel(prefs, tui.ProfileNone, tui.CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestCatalogIsCompleteInBothLanguages(t *testing.T) {
	if err := tui.CheckCatalogs(catalog()); err != nil {
		t.Fatal(err)
	}
}

func TestEveryIconSetPassesTheFrameChecks(t *testing.T) {
	for _, icons := range []string{uiprefs.IconsUnicode, uiprefs.IconsNerdFont, uiprefs.IconsNone} {
		m := testModel(t, icons)
		spec := m.frameSpec()
		if err := errors.Join(tui.CheckFrame(spec), tui.CheckAreas(spec.Areas)); err != nil {
			t.Errorf("%s: %v", icons, err)
		}
	}
}

func TestEveryKeyOfTheSwitchesActs(t *testing.T) {
	m := testModel(t, uiprefs.IconsUnicode)
	for _, key := range []string{"t", "l", "i", "m"} {
		m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	}
	p := m.prefs
	if p.Theme == tui.ThemeTerminal || p.Language != uiprefs.LanguageGerman || p.Icons != uiprefs.IconsNerdFont || p.MouseEnabled {
		t.Fatalf("a switch did not act: %+v", p)
	}
}

func TestSwitchesStepThroughEveryValueAndWrap(t *testing.T) {
	m := testModel(t, uiprefs.IconsUnicode)
	for _, want := range []string{uiprefs.IconsNerdFont, uiprefs.IconsNone, uiprefs.IconsUnicode} {
		m.run(actIcons, "")
		if m.prefs.Icons != want {
			t.Fatalf("icons %q, want %q", m.prefs.Icons, want)
		}
	}
	names := m.themes.Names()
	for range names {
		m.run(actTheme, "")
	}
	if m.prefs.Theme != tui.ThemeTerminal {
		t.Fatalf("theme %q after a full round of %d, want the first", m.prefs.Theme, len(names))
	}
}

// clickable reports whether a click somewhere on the screen runs action.
func clickable(m *model, action tui.ActionID) bool {
	for y := range m.height {
		for x := range m.width {
			if m.mouse.Handle(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}).Action == action {
				return true
			}
		}
	}
	return false
}

func TestTheSwitchesAreReachableByClickWhileTheMouseIsOn(t *testing.T) {
	m := testModel(t, uiprefs.IconsUnicode)
	for _, action := range []tui.ActionID{actTheme, actLanguage, actIcons} {
		if !clickable(m, action) {
			t.Errorf("no click target runs %s", action)
		}
	}
}

func TestNoClickActsWhileTheMouseIsOff(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actMouse, "")
	m.refresh()
	for y := range 30 {
		for x := range 100 {
			if m.mouse.Handle(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}).Handled {
				t.Fatalf("a click at %d,%d acted with the mouse off", x, y)
			}
		}
	}
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("the terminal still reports the mouse")
	}
}

func TestSmallTerminalShowsOnlyTheHintAndTakesNoClick(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.Update(tea.WindowSizeMsg{Width: minWidth - 1, Height: minHeight})
	if !strings.Contains(m.view, "Terminal too small") {
		t.Fatalf("view %q", m.view)
	}
	if m.mouse.Handle(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft}).Handled {
		t.Fatal("a click acted on a screen that is not drawn")
	}
}

func TestEveryGroupDrawsInEveryTabAtEverySize(t *testing.T) {
	m := testModel(t, uiprefs.IconsUnicode)
	for _, size := range [][2]int{{minWidth, minHeight}, {60, 16}, {100, 30}, {200, 60}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, g := range m.groups {
			for _, tab := range g.area.Tabs {
				m.area, m.tabs[g.area.ID] = g.area.ID, tab.ID
				m.refresh()
				if lines := strings.Split(m.view, "\n"); len(lines) != size[1] {
					t.Errorf("%s/%s at %v: %d lines", g.area.ID, tab.ID, size, len(lines))
				}
			}
		}
	}
}

func TestArgumentsAreChecked(t *testing.T) {
	for _, args := range [][]string{{"extra"}, {"-icons", "emoji"}, {"-language", "fr"}, {"-theme", "nope"}, {"-bogus"}} {
		err := run(args, io.Discard, nil)
		var usage usageError
		if !errors.As(err, &usage) {
			t.Errorf("%v: error %v, want a usage error", args, err)
		}
	}
}

func modalOpeners() map[string]func(*model) {
	return map[string]func(*model){
		"question": (*model).openQuestion, "input": (*model).openInput, "notice": (*model).openNotice,
		"help": (*model).openHelp, "explanation": (*model).openExplanation,
	}
}

func TestAPartOverTheScreenDrawsAtEverySizeAndTakesAllClicks(t *testing.T) {
	screenActions := []tui.ActionID{actTheme, actLanguage, actIcons, actMouse, actCount, actAsk, actRename, actNotice, actHelp, actInfo, actCommands}
	for name, open := range modalOpeners() {
		for _, size := range [][2]int{{minWidth, minHeight}, {60, 16}, {100, 30}} {
			m := testModel(t, uiprefs.IconsNone)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			open(m)
			m.refresh()
			if m.modal == nil {
				t.Fatalf("%s did not open: %v", name, m.message)
			}
			if lines := strings.Split(m.view, "\n"); len(lines) != size[1] {
				t.Errorf("%s at %v: %d lines", name, size, len(lines))
			}
			for y := range size[1] {
				for x := range size[0] {
					res := m.mouse.Handle(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
					// The key list of the help is part of the help: a click on it runs the action of its key.
					own := m.kind == kindHelp || slices.ContainsFunc(m.modal.Hints(), func(h tui.Hint) bool { return h.Action == res.Action })
					if res.Area != "" || (slices.Contains(screenActions, res.Action) && !own) {
						t.Fatalf("%s at %v: a click at %d,%d reaches the screen behind: %+v", name, size, x, y, res)
					}
				}
			}
		}
	}
}

func TestKeysStayInThePartOverTheScreen(t *testing.T) {
	for name, open := range modalOpeners() {
		m := testModel(t, uiprefs.IconsNone)
		open(m)
		before := m.prefs
		for _, key := range []string{"t", "l", "i", "m", "2", "q"} {
			if cmd := m.handle(tea.KeyPressMsg{Code: rune(key[0]), Text: key}); cmd != nil && key == "q" && name != "input" {
				t.Errorf("%s: q quit the program", name)
			}
		}
		if m.prefs.Theme != before.Theme || m.prefs.Language != before.Language || m.prefs.Icons != before.Icons || m.prefs.MouseEnabled != before.MouseEnabled || m.area != "overview" {
			t.Errorf("%s: a key reached the screen behind: %+v, area %s", name, m.prefs, m.area)
		}
	}
}

func TestBackClosesEveryPartAndCtrlCQuits(t *testing.T) {
	for name, open := range modalOpeners() {
		m := testModel(t, uiprefs.IconsNone)
		open(m)
		m.handle(tea.KeyPressMsg{Code: tea.KeyEscape})
		if m.modal != nil {
			t.Errorf("%s: esc did not close it", name)
		}
		open(m)
		if cmd := m.handle(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
			t.Errorf("%s: ctrl+c did not quit", name)
		}
	}
}

func TestCommandLineTakesNoBackgroundClick(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actCommands, "")
	m.refresh()
	for x := range m.width {
		if res := m.mouse.Handle(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft}); res.Handled {
			t.Fatalf("a click on the tab line at %d acted: %+v", x, res)
		}
	}
	m.handle(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.cmdOpen {
		t.Fatal("esc did not close the command line")
	}
}

func TestEveryCommandRunsLikeItsKey(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	for _, c := range commands(m.groups, m.text) {
		m.cmdOpen = true
		m.commandDone(c, true)
		if m.cmdOpen {
			t.Errorf("%s left the line open", c.Name)
		}
		m.closeModal()
	}
}

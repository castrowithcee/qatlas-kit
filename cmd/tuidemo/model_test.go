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
	for _, key := range []string{"t", "l", "I", "M"} {
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

func TestThemeStepsBackByKeyAndWrapsAtBothEnds(t *testing.T) {
	m := testModel(t, uiprefs.IconsUnicode)
	names := m.themes.Names()
	m.Update(tea.KeyPressMsg{Code: 'T', Text: "T", Mod: tea.ModShift})
	if m.prefs.Theme != names[len(names)-1] {
		t.Fatalf("theme %q after T from the first, want the last %q", m.prefs.Theme, names[len(names)-1])
	}
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.prefs.Theme != names[0] {
		t.Fatalf("theme %q after t from the last, want the first", m.prefs.Theme)
	}
	m.run(actThemeBack, "")
	m.run(actThemeBack, "")
	if m.prefs.Theme != names[len(names)-2] {
		t.Fatalf("theme %q after two steps back", m.prefs.Theme)
	}
}

func TestThemeStepsBackByClickAndCommand(t *testing.T) {
	m := testModel(t, uiprefs.IconsUnicode)
	m.openArea("settings")
	m.refresh()
	for _, action := range []tui.ActionID{actTheme, actThemeBack} {
		if !clickable(m, action) {
			t.Errorf("no click target runs %s", action)
		}
	}
	names := m.themes.Names()
	if !clickWhere(m, func(r tui.MouseResult) bool { return r.Action == actThemeBack }) {
		t.Fatal("no click target runs back")
	}
	if m.prefs.Theme != names[len(names)-1] {
		t.Fatalf("theme %q after a click on back, want %q", m.prefs.Theme, names[len(names)-1])
	}
	m.run(actThemeBack, "")
	if i := slices.IndexFunc(commands(m.groups, m.text), func(c tui.Command) bool { return c.Action == actThemeBack }); i < 0 {
		t.Fatal("no command steps the theme back")
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

func TestFlagValuesAreChecked(t *testing.T) {
	for _, args := range [][]string{{"-theme", "nope"}, {"-language", "fr"}, {"-icons", "emoji"}} {
		err := run(args, io.Discard, nil)
		var usage usageError
		if !errors.As(err, &usage) || !strings.Contains(err.Error(), args[1]) {
			t.Errorf("%v: error %v, want a usage error naming the value", args, err)
		}
	}
	for _, args := range []string{"-theme demo-light", "-language system", "-language deutsch", "-icons none"} {
		prefs := uiprefs.Default(uiprefs.Schema{DefaultArea: "overview"})
		f := strings.Fields(args)
		switch f[0] {
		case "-theme":
			prefs.Theme = f[1]
		case "-language":
			prefs.Language = f[1]
		case "-icons":
			prefs.Icons = f[1]
		}
		if _, err := newModel(prefs, tui.ProfileNone, tui.CharsetUnicode); err != nil {
			t.Errorf("%s: %v", args, err)
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
		for _, key := range []string{"t", "l", "I", "M", "+", "2", "q"} {
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

func TestCommandLineStandsOverTheScreenAndTakesOnlyItsOwnClicks(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actCommands, "")
	m.refresh()
	if !strings.Contains(m.view, "go overview") || !strings.Contains(m.view, "Kit workbench") {
		t.Fatalf("the line does not stand over the screen:\n%s", m.view)
	}
	for y := range m.height {
		for x := range m.width {
			res := m.mouse.Handle(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if res.Area != "" && res.Zone != tui.ZoneRows || res.Handled && res.Zone != tui.ZoneRows && slices.Contains([]tui.ActionID{actTheme, actCommands, actHelp, actQuit}, res.Action) {
				t.Fatalf("a click at %d,%d reaches the screen behind: %+v", x, y, res)
			}
		}
	}
}

// clickWhere clicks the first cell that carries action and reports whether there was one.
func clickWhere(m *model, match func(tui.MouseResult) bool) bool {
	for y := range m.height {
		for x := range m.width {
			if match(m.mouse.Handle(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})) {
				m.handle(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				return true
			}
		}
	}
	return false
}

func TestCommandLineClicksRunAMatchAndCloseOnBack(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {minWidth, minHeight}} {
		m := testModel(t, uiprefs.IconsNone)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.run(actCommands, "")
		m.refresh()
		if !clickWhere(m, func(r tui.MouseResult) bool { return r.Zone == tui.ZoneRows && r.Action == actTheme }) && size[0] == 100 {
			t.Fatal("no match to click at 100 by 30")
		}
		if size[0] == 100 && (m.cmdOpen || m.prefs.Theme == tui.ThemeTerminal) {
			t.Fatalf("a click on a match did not run it: open %v, theme %q", m.cmdOpen, m.prefs.Theme)
		}
		m.run(actCommands, "")
		m.refresh()
		if !clickWhere(m, func(r tui.MouseResult) bool { return r.Action == actBack }) {
			t.Fatalf("%v: no close key to click", size)
		}
		if m.cmdOpen {
			t.Fatalf("%v: a click on the close key left the line open", size)
		}
	}
}

func TestCommandLineFillsTheWorkspaceInASmallTerminal(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.Update(tea.WindowSizeMsg{Width: minWidth, Height: minHeight})
	m.run(actCommands, "")
	m.refresh()
	lines := strings.Split(m.view, "\n")
	if len(lines) != minHeight || !strings.Contains(m.view, "go overview") {
		t.Fatalf("%d lines:\n%s", len(lines), m.view)
	}
	if strings.Contains(lines[len(lines)-1], "╚") || !strings.Contains(strings.Join(lines[len(lines)-2:], "\n"), "esc") {
		t.Errorf("the keys are not in the footer:\n%s", m.view)
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

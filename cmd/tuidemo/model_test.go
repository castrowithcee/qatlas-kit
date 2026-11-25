package main

import (
	"errors"
	"io"
	"os"
	"regexp"
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
	root, err := newSampleRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	m, err := newModel(prefs, tui.ProfileNone, tui.CharsetUnicode, root)
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

func TestThemeStepsBackByLeftInTheFormAndCommand(t *testing.T) {
	m := testModel(t, uiprefs.IconsUnicode)
	m.openArea("settings")
	m.refresh()
	if !clickable(m, actTheme) {
		t.Errorf("no click target runs %s", actTheme)
	}
	names := m.themes.Names()
	m.zone = tui.ZoneForm
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.prefs.Theme != names[len(names)-1] {
		t.Fatalf("theme %q after left in the theme field, want %q", m.prefs.Theme, names[len(names)-1])
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
		if _, err := newModel(prefs, tui.ProfileNone, tui.CharsetUnicode, ""); err != nil {
			t.Errorf("%s: %v", args, err)
		}
	}
}

func modalOpeners() map[string]func(*model) {
	return map[string]func(*model){
		"question": (*model).openQuestion, "input": (*model).openInput, "notice": (*model).openNotice,
		"help": (*model).openHelp, "explanation": (*model).openExplanation, "picker": (*model).openPicker, "suggest": (*model).openSuggest,
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

func TestNoKeyTakesAReservedKeyWithAnotherMeaning(t *testing.T) {
	owners := map[string]tui.ActionID{}
	for _, a := range tui.StandardActions() {
		for _, key := range a.DefaultKeys {
			owners[key] = a.ID
		}
	}
	table := keyTable(len(groups()))
	if _, err := tui.NewKeymap(table, tui.CharsetUnicode); err != nil {
		t.Fatal(err)
	}
	same := 0
	for _, a := range table {
		if a.Level == tui.LevelConfirm || a.Level == tui.LevelHelp {
			continue // overlay levels may repeat a key of the main levels
		}
		for _, key := range a.DefaultKeys {
			switch owner, ok := owners[key]; {
			case !ok:
			case owner != a.ID:
				t.Errorf("key %q of %q is reserved for %q", key, a.ID, owner)
			default:
				same++
			}
		}
	}
	if same == 0 {
		t.Error("no key of the table is a standard key; the check found nothing to compare")
	}
}

func TestEachTabHintsItsOwnActionsOnly(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	own := map[tui.ActionID]tui.TabID{}
	for _, g := range m.groups {
		for tab, hints := range g.hints {
			for _, h := range hints {
				own[h.Action] = tab
			}
		}
	}
	for _, g := range m.groups {
		for _, tab := range g.area.Tabs {
			m.area, m.tabs[g.area.ID] = g.area.ID, tab.ID
			for _, h := range m.footerHints() {
				if want, ok := own[h.Action]; ok && (want != tab.ID || len(g.hints[tab.ID]) == 0) {
					t.Errorf("%s/%s shows the hint of %s", g.area.ID, tab.ID, h.Action)
				}
			}
		}
	}
	if len(own) != 6 {
		t.Errorf("%d tab actions, want ask, rename, notice, count, pick and suggest", len(own))
	}
}

// press sends the key as the terminal would.
func press(m *model, code rune) {
	m.Update(tea.KeyPressMsg{Code: code})
}

var focusMark = regexp.MustCompile(`> [^<>\n]+ <`)

func TestOnlyTabsWithButtonsOrABlockHaveAWorkZone(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	for _, g := range m.groups {
		for _, tab := range g.area.Tabs {
			m.area, m.tabs[g.area.ID] = g.area.ID, tab.ID
			work := slices.DeleteFunc(m.zones(), func(z tui.FocusZone) bool { return z == tui.ZoneTabs || z == tui.ZoneSubtabs })
			if b := m.live(); b != nil {
				if len(m.actions()) > 0 || !slices.Equal(work, b.zones()) {
					t.Errorf("%s/%s: work zones %v, buttons %d, want the block's %v only", g.area.ID, tab.ID, work, len(m.actions()), b.zones())
				}
				continue
			}
			if got, want := slices.Contains(work, tui.ZoneRows), len(m.actions()) > 0; got != want || len(work) > 1 {
				t.Errorf("%s/%s: work zones %v, buttons %d", g.area.ID, tab.ID, work, len(m.actions()))
			}
		}
	}
	m.area, m.tabs["frame"] = "frame", "tabs"
	if slices.Contains(m.zones(), tui.ZoneRows) {
		t.Error("the tab without buttons reports a work zone")
	}
}

func TestEveryButtonIsReachableByKeyAndFocusedAndTriggersLikeItsClick(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	for _, g := range m.groups {
		for _, tab := range g.area.Tabs {
			m.openArea(g.area.ID)
			m.tabs[g.area.ID], m.zone, m.item = tab.ID, tui.ZoneTabs, 0
			m.refresh()
			buttons := m.actions()
			if len(buttons) == 0 {
				continue
			}
			if focusMark.MatchString(m.view) {
				t.Errorf("%s/%s: a button shows the focus before it is in the work area", g.area.ID, tab.ID)
			}
			press(m, tea.KeyDown) // tab line to sub-tab line
			press(m, tea.KeyDown) // into the work area
			for i, action := range buttons {
				if m.zone != tui.ZoneRows || m.item != i {
					t.Fatalf("%s/%s: focus at %s %d, want rows %d", g.area.ID, tab.ID, m.zone, m.item, i)
				}
				if n := len(focusMark.FindAllString(m.view, -1)); n != 1 {
					t.Errorf("%s/%s button %d: %d focus marks on the screen", g.area.ID, tab.ID, i, n)
				}
				if !clickable(m, action) {
					t.Errorf("%s: no click runs the action of the button", action)
				}
				press(m, tea.KeyDown)
			}
			if m.item != len(buttons)-1 {
				t.Errorf("%s/%s: down went past the last button", g.area.ID, tab.ID)
			}
			for range buttons {
				press(m, tea.KeyUp)
			}
			if m.zone != tui.ZoneSubtabs {
				t.Errorf("%s/%s: up from the first button leads to %s", g.area.ID, tab.ID, m.zone)
			}
			press(m, tea.KeyDown)
			press(m, tea.KeyEscape)
			if m.zone != tui.ZoneSubtabs {
				t.Errorf("%s/%s: esc from the buttons leads to %s", g.area.ID, tab.ID, m.zone)
			}
		}
	}
}

func TestEnterTriggersTheFocusedButtonAndLeadsIntoTheWorkArea(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.openArea("frame")
	m.tabs["frame"] = "mouse"
	press(m, tea.KeyEnter)
	if m.zone != tui.ZoneRows {
		t.Fatalf("enter on the tab line leads to %s", m.zone)
	}
	press(m, tea.KeyEnter)
	if m.count != 1 {
		t.Errorf("enter on the button counted %d", m.count)
	}
	m.openArea("dialogs")
	press(m, tea.KeyEnter)
	if m.modal == nil {
		t.Error("enter on the opener did not open the part")
	}
	m.zone = tui.ZoneTabs
	m.openArea("overview")
	press(m, tea.KeyEnter)
	if m.zone != tui.ZoneTabs {
		t.Errorf("enter on a tab without buttons moved the focus to %s", m.zone)
	}
}

func TestSideKeysSwitchTabsOnlyOnTheTabLines(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	for _, area := range []tui.AreaID{"dialogs", "settings"} { // buttons, and the form of a block
		m.zone = tui.ZoneTabs
		m.openArea(area)
		press(m, tea.KeyTab)
		press(m, tea.KeyTab) // tab line, sub-tab line, work area
		if m.zone != m.workZone() || m.zone == "" {
			t.Fatalf("%s: tab leads to %s", area, m.zone)
		}
		tab := m.tab()
		press(m, tea.KeyRight)
		press(m, tea.KeyLeft)
		if m.area != area || m.tab() != tab {
			t.Errorf("a side key in the work area of %s switched to %s/%s", area, m.area, m.tab())
		}
	}
}

func TestFooterNamesDownUpEnterWhereTheyAct(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	has := func(a tui.ActionID) bool {
		return slices.ContainsFunc(m.footerHints(), func(h tui.Hint) bool { return h.Action == a })
	}
	m.openArea("dialogs")
	if !has(actDown) || has(actUp) || !has(actEnter) {
		t.Error("tab line of a tab with buttons: want down and enter, no up")
	}
	m.zone = tui.ZoneRows
	if has(actDown) || !has(actUp) || !has(actEnter) {
		t.Error("its only button: want up and enter, no down")
	}
	m.zone = tui.ZoneTabs
	m.openArea("settings")
	if !has(actDown) || has(actUp) || !has(actEnter) {
		t.Error("tab line of a tab with a block: want down and enter, no up")
	}
	m.zone = tui.ZoneForm
	if hints := m.navHints(); len(hints) != 0 {
		t.Errorf("in the block: %d hints of the work area's own moves, want the block's only", len(hints))
	}
	m.openArea("overview")
	m.zone = tui.ZoneTabs
	if has(actEnter) || !has(actDown) || has(actUp) {
		t.Error("tab without buttons: want down only")
	}
}

// keepsUp are the blocks that take up at their top themselves, since up scrolls or selects there; esc leads
// out of them.
var keepsUp = map[blockKey]bool{{"views", "text"}: true, {"views", "log"}: true, {"views", "chat"}: true}

func TestEveryBlockIsReachedByKeyAndLeftByUpOrEsc(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	for _, g := range m.groups {
		for _, tab := range g.area.Tabs {
			m.openArea(g.area.ID)
			m.tabs[g.area.ID], m.zone = tab.ID, tui.ZoneTabs
			m.refresh()
			b := m.live()
			if b == nil {
				continue
			}
			name := string(g.area.ID) + "/" + string(tab.ID)
			into := func(how string, keys ...rune) {
				t.Helper()
				for _, k := range keys {
					press(m, k)
				}
				if m.inBlock(m.zone) == nil {
					t.Errorf("%s: %s leads to %s, not into the block", name, how, m.zone)
				}
			}
			into("down from the tab lines", tea.KeyDown, tea.KeyDown)
			for i := 0; i < 50 && m.zone != tui.ZoneSubtabs; i++ {
				press(m, tea.KeyUp)
			}
			if up := m.zone == tui.ZoneSubtabs; up == keepsUp[blockKey{g.area.ID, tab.ID}] {
				t.Errorf("%s: up from the top leads to %s", name, m.zone)
			}
			m.zone = tui.ZoneSubtabs
			into("enter on the sub-tab line", tea.KeyEnter)
			press(m, tea.KeyEscape)
			if m.zone != tui.ZoneSubtabs {
				t.Errorf("%s: esc leads to %s", name, m.zone)
			}
			into("tab on the sub-tab line", tea.KeyTab)
		}
	}
}

func TestAFooterHintOfABlockMovesTheBlock(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.openArea("lists")
	m.zone = tui.ZoneRows
	m.refresh()
	b := m.live().(*listBlock)
	before, _ := b.list.Current()
	if !clickWhere(m, func(r tui.MouseResult) bool { return r.Action == actDown && m.inBlock(r.Zone) == nil }) {
		t.Fatal("no footer hint for down in the list")
	}
	if after, _ := b.list.Current(); after.ID == before.ID || m.zone != tui.ZoneRows {
		t.Errorf("a click on the down hint left the list on %s, focus at %s", after.ID, m.zone)
	}
}

package main

import (
	"fmt"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// minWidth and minHeight are the smallest terminal the frame is drawn in.
const (
	minWidth  = 40
	minHeight = 12
)

// model is the state of the workbench. Every change of state ends in refresh, which draws the frame once and
// keeps its click targets, so a click always meets the screen the person sees.
type model struct {
	prefs   uiprefs.Prefs
	groups  []group
	themes  *tui.ThemeSet
	keys    *tui.Keymap
	text    tui.Translator
	frame   *tui.Frame
	styles  tui.Styles
	profile tui.ColorProfile
	charset tui.CharsetMode
	// fg and bg are what the terminal reported; both are needed for the terminal theme.
	fg, bg tui.Color
	term   *tui.TerminalColors

	width, height int
	area          tui.AreaID
	tabs          map[tui.AreaID]tui.TabID
	zone          tui.FocusZone
	message       *tui.Message

	// The mouse probe of the frame tab.
	clickX, clickY int
	hit            string
	wheel, count   int

	// The part standing over the screen, if any: a dialog, the help or an explanation. dialog and explanation
	// hold the same part as modal where it is one of them, for what only they offer.
	modal       tui.Modal
	kind        modalKind
	focus       tui.InputFocus
	dialog      *tui.Dialog
	explanation *tui.Explanation
	journal     string

	cmdline *tui.CommandLine
	cmdOpen bool

	mouse *tui.MouseMap
	view  string
}

func newModel(prefs uiprefs.Prefs, profile tui.ColorProfile, charset tui.CharsetMode) (*model, error) {
	gs := groups()
	areas := make([]tui.Area, len(gs))
	ids := make([]string, len(gs))
	for i, g := range gs {
		areas[i], ids[i] = g.area, string(g.area.ID)
	}
	if err := tui.CheckAreas(areas); err != nil {
		return nil, err
	}
	themes, err := tui.NewThemeSet([]tui.Theme{tui.DarkTheme("demo-dark"), tui.LightTheme("demo-light")})
	if err != nil {
		return nil, err
	}
	if err := uiprefs.Validate(prefs, uiprefs.Schema{Themes: themes.Names(), Areas: ids, DefaultArea: ids[0]}); err != nil {
		return nil, usageError{err}
	}
	keys, err := tui.NewKeymap(keyTable(len(gs)), charset)
	if err != nil {
		return nil, err
	}
	m := &model{prefs: prefs, groups: gs, themes: themes, keys: keys, profile: profile, charset: charset,
		area: gs[0].area.ID, tabs: map[tui.AreaID]tui.TabID{}, zone: tui.ZoneTabs}
	if err := m.rebuildText(); err != nil {
		return nil, err
	}
	m.restyle()
	return m, nil
}

// rebuildText builds the translator and the frame again for the language in force.
func (m *model) rebuildText() error {
	text, err := tui.NewTranslator(uiprefs.Language(m.prefs.Language), catalog(), m.keys)
	if err != nil {
		return err
	}
	line, err := tui.NewCommandLine(tui.CommandLineSpec{Keys: tui.CommandLineKeys{Up: actUp, Down: actDown, Next: actZoneNext,
		Prev: actZonePrev, Enter: actEnter, Back: actBack}, Commands: commands(m.groups, text)}, m.keys, text)
	if err != nil {
		return err
	}
	m.text, m.frame, m.cmdline = text, tui.NewFrame(m.keys, text), line
	return nil
}

// restyle resolves the theme in force; a theme the set does not know falls back to the terminal theme.
func (m *model) restyle() {
	var known bool
	m.styles, known = m.themes.Resolve(m.prefs.Theme, m.profile, m.charset, m.term)
	if !known {
		m.prefs.Theme = tui.ThemeTerminal
		m.message = &tui.Message{Kind: tui.MessageWarning, Text: m.text.Lookup(tui.TextThemeUnknown, m.prefs.Theme, tui.ThemeTerminal)}
	}
}

func (m *model) Init() tea.Cmd {
	if m.themes.AsksTerminalColors(m.prefs.Theme, m.profile) {
		return tea.Batch(tea.RequestForegroundColor, tea.RequestBackgroundColor)
	}
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.handle(msg)
	m.refresh()
	return m, cmd
}

func (m *model) handle(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tui.InputBatch:
		var cmds []tea.Cmd
		for _, one := range msg {
			cmds = append(cmds, m.handle(one))
		}
		return tea.Batch(cmds...)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.ForegroundColorMsg:
		m.fg = tui.ColorOf(msg.Color)
		m.takeTerminalColors()
	case tea.BackgroundColorMsg:
		m.bg = tui.ColorOf(msg.Color)
		m.takeTerminalColors()
	case tea.PasteMsg:
		switch {
		case m.cmdOpen:
			m.cmdline.Paste(msg.Content)
		case m.dialog != nil:
			m.dialog.Paste(msg.Content)
		}
	case tea.KeyPressMsg:
		switch {
		case m.cmdOpen:
			return m.commandKey(msg)
		case m.modal != nil:
			return m.modalKey(msg)
		}
		if action, ok := m.keys.Match(msg, tui.InputNavigation); ok {
			return m.run(action, tui.KeyName(msg))
		}
	case tea.MouseMsg:
		switch {
		case m.cmdOpen:
			return m.commandClick(msg)
		case m.modal != nil:
			return m.modalClick(msg)
		}
		return m.click(msg)
	}
	return nil
}

func (m *model) takeTerminalColors() {
	if m.fg != "" && m.bg != "" {
		m.term = &tui.TerminalColors{Foreground: m.fg, Background: m.bg}
		m.restyle()
	}
}

// click runs what the mouse hit exactly as the key or the tab click would.
func (m *model) click(msg tea.MouseMsg) tea.Cmd {
	res := m.mouse.Handle(msg)
	if !res.Handled {
		return nil
	}
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		m.wheel += res.WheelSteps
		return nil
	}
	event := msg.Mouse()
	m.clickX, m.clickY, m.hit = event.X, event.Y, hitName(res)
	switch {
	case res.Action != "":
		return m.run(res.Action, "")
	case res.Area != "":
		m.openArea(res.Area)
		if res.Tab != "" {
			m.tabs[res.Area] = res.Tab
		}
		m.zone = res.Zone
	case slices.Contains(m.zones(), res.Zone):
		m.zone = res.Zone
	}
	return nil
}

func hitName(res tui.MouseResult) string {
	switch {
	case res.Action != "":
		return string(res.Action)
	case res.Tab != "":
		return fmt.Sprintf("%s/%s", res.Area, res.Tab)
	case res.Area != "":
		return string(res.Area)
	}
	return string(res.Zone)
}

// run does one action; key is the key that triggered it, empty for a click.
func (m *model) run(action tui.ActionID, key string) tea.Cmd {
	switch action {
	case actQuit:
		return tea.Quit
	case actArea:
		if n, err := strconv.Atoi(key); err == nil && n >= 1 && n <= len(m.groups) {
			m.openArea(m.groups[n-1].area.ID)
		}
	case actLeft:
		m.step(-1)
	case actRight:
		m.step(1)
	case actZoneNext:
		m.zone = tui.CycleZone(m.zones(), m.zone, 1)
	case actZonePrev:
		m.zone = tui.CycleZone(m.zones(), m.zone, -1)
	case actTheme:
		m.stepTheme(1)
	case actThemeBack:
		m.stepTheme(-1)
	case actLanguage:
		if m.prefs.Language == uiprefs.LanguageGerman {
			m.prefs.Language = uiprefs.LanguageEnglish
		} else {
			m.prefs.Language = uiprefs.LanguageGerman
		}
		if err := m.rebuildText(); err != nil {
			m.message = &tui.Message{Kind: tui.MessageError, Text: err.Error()}
			break
		}
		m.changed(tui.TextSettingLanguage, m.prefs.Language)
	case actIcons:
		sets := []string{uiprefs.IconsUnicode, uiprefs.IconsNerdFont, uiprefs.IconsNone}
		m.prefs.Icons = sets[(slices.Index(sets, m.prefs.Icons)+1)%len(sets)]
		m.changed(tui.TextSettingIcons, m.prefs.Icons)
	case actMouse:
		m.prefs.MouseEnabled = !m.prefs.MouseEnabled
		m.changed(tui.TextSettingMouse, m.onOff(m.prefs.MouseEnabled))
	case actCount:
		m.count++
	case actHelp:
		m.openHelp()
	case actInfo:
		m.openExplanation()
	case actCommands:
		m.openCommandLine()
	case actAsk:
		m.openQuestion()
	case actRename:
		m.openInput()
	case actNotice:
		m.openNotice()
	}
	return nil
}

// stepTheme switches the theme by one place, with wrap-around at both ends.
func (m *model) stepTheme(by int) {
	m.prefs.Theme = m.themes.Step(m.prefs.Theme, by)
	m.restyle()
	m.changed(tui.TextSettingTheme, m.prefs.Theme)
}

func (m *model) changed(label tui.TextID, value string) {
	m.message = &tui.Message{Kind: tui.MessageStatus, Text: m.text.Lookup(textChanged, m.text.Lookup(label), value)}
}

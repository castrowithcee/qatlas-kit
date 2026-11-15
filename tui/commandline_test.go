package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

var sampleCommandKeys = CommandLineKeys{Up: "up", Down: "down", Next: "next", Prev: "prev", Enter: "enter", Back: "back"}

// outsideForm is the availability of a command whose key does not act while a form has the focus.
func outsideForm(ctx Context) (bool, string) {
	if ctx.Zone == ZoneForm {
		return false, "not in a form; leave it with esc first"
	}
	return true, ""
}

// sampleCommands are invented commands over the sample key table: one per area on the digit of its tab, a
// hidden one for a sub-tab, commands with and without a key of their own.
func sampleCommands() []Command {
	return []Command{
		{Name: "go overview", Aliases: []string{"overview"}, Description: "cmd.overview", Action: "area", Key: 1, Area: "overview", Available: Always},
		{Name: "go items", Aliases: []string{"items"}, Description: "cmd.items", Action: "area", Key: 2, Area: "items", Available: Always},
		{Name: "go settings", Aliases: []string{"settings"}, Description: "cmd.settings", Action: "area", Key: 3, Area: "settings", Available: Always},
		{Name: "go settings advanced", Aliases: []string{"advanced"}, Description: "cmd.advanced", Area: "settings", Tab: "advanced", Hidden: true, Available: Always},
		{Name: "reload", Aliases: []string{"refresh"}, Description: "cmd.reload", Action: "reload", Available: Always},
		{Name: "upgrade", Aliases: []string{"update"}, Description: "cmd.upgrade", Action: "upgrade", Available: outsideForm},
		{Name: "export config", Aliases: []string{"backup"}, Description: "cmd.export", Action: "export", Available: outsideForm},
		{Name: "help", Description: "cmd.help", Action: "help", Available: Always},
		{Name: "quit", Aliases: []string{"exit"}, Description: "cmd.quit", Action: "quit", Available: outsideForm},
	}
}

func commandCatalogs() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {
			"cmd.overview": "Open Overview", "cmd.items": "Open Items", "cmd.settings": "Open Settings",
			"cmd.advanced": "Open the advanced settings", "cmd.reload": "Read the data again",
			"cmd.upgrade": "Install the newer version", "cmd.export": "Write the configuration to a file",
			"cmd.help": "Show the help", "cmd.quit": "End the program", "cmd.area": "Open %s",
		},
		i18n.German: {
			"cmd.overview": "Übersicht öffnen", "cmd.items": "Einträge öffnen", "cmd.settings": "Einstellungen öffnen",
			"cmd.advanced": "Erweiterte Einstellungen öffnen", "cmd.reload": "Daten neu einlesen",
			"cmd.upgrade": "Neuere Version installieren", "cmd.export": "Konfiguration in eine Datei schreiben",
			"cmd.help": "Hilfe anzeigen", "cmd.quit": "Programm beenden", "cmd.area": "%s öffnen",
		},
	}
}

func sampleCommandLine(t *testing.T, lang i18n.Lang, commands []Command) (*CommandLine, *Keymap) {
	t.Helper()
	keys := sampleKeymap(t, CharsetUnicode)
	c, err := NewCommandLine(CommandLineSpec{Commands: commands, Keys: sampleCommandKeys}, keys, newTranslator(t, lang, commandCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	return c, keys
}

func typeInto(c *CommandLine, text string) {
	for _, r := range text {
		updateIn(c, tea.KeyPressMsg{Code: r, Text: string(r)}, Context{})
	}
}

func names(cmds []Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}

var (
	enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}
	escKey   = tea.KeyPressMsg{Code: tea.KeyEscape}
	downKey  = tea.KeyPressMsg{Code: tea.KeyDown}
	upKey    = tea.KeyPressMsg{Code: tea.KeyUp}
)

func TestNewCommandLineRejectsWhatCannotWork(t *testing.T) {
	keys := sampleKeymap(t, CharsetUnicode)
	text := newTranslator(t, i18n.English, commandCatalogs(), keys)
	ok := Command{Name: "help", Description: "cmd.help", Action: "help", Available: Always}
	with := func(change func(*Command)) []Command {
		c := ok
		change(&c)
		return []Command{c}
	}
	for _, c := range []struct {
		name     string
		commands []Command
		nav      CommandLineKeys
		keys     *Keymap
		text     Translator
		want     string
	}{
		{"no key table", []Command{ok}, sampleCommandKeys, nil, text, "key table"},
		{"no translator", []Command{ok}, sampleCommandKeys, keys, nil, "translator"},
		{"unknown move key", []Command{ok}, CommandLineKeys{Up: "up", Down: "down", Next: "next", Prev: "prev", Enter: "run", Back: "back"}, keys, text, `"run" is no action`},
		{"empty move key", []Command{ok}, CommandLineKeys{}, keys, text, `"" is no action`},
		{"no command", nil, sampleCommandKeys, keys, text, "no command"},
		{"upper case", with(func(c *Command) { c.Name = "Help" }), sampleCommandKeys, keys, text, "lower-case"},
		{"two blanks", with(func(c *Command) { c.Name = "show  help" }), sampleCommandKeys, keys, text, "lower-case"},
		{"leading blank", with(func(c *Command) { c.Name = " help" }), sampleCommandKeys, keys, text, "lower-case"},
		{"empty name", with(func(c *Command) { c.Name = "" }), sampleCommandKeys, keys, text, "lower-case"},
		{"control character", with(func(c *Command) { c.Aliases = []string{"he\x1blp"} }), sampleCommandKeys, keys, text, "lower-case"},
		{"alias twice", with(func(c *Command) { c.Aliases = []string{"help"} }), sampleCommandKeys, keys, text, "also a name"},
		{"no description", with(func(c *Command) { c.Description = "" }), sampleCommandKeys, keys, text, "no description"},
		{"no availability", with(func(c *Command) { c.Available = nil }), sampleCommandKeys, keys, text, "no availability"},
		{"nothing to do", with(func(c *Command) { c.Action = "" }), sampleCommandKeys, keys, text, "neither an action nor an area"},
		{"bad action", with(func(c *Command) { c.Action = "Help" }), sampleCommandKeys, keys, text, "no valid action ID"},
		{"tab alone", with(func(c *Command) { c.Tab = "advanced" }), sampleCommandKeys, keys, text, "without its area"},
		{"key past the keys", with(func(c *Command) { c.Action, c.Key = "area", 7 }), sampleCommandKeys, keys, text, "no key of action"},
		{"negative key", with(func(c *Command) { c.Action, c.Key = "area", -1 }), sampleCommandKeys, keys, text, "no key of action"},
		{"key of an unknown action", with(func(c *Command) { c.Action, c.Key = "export", 1 }), sampleCommandKeys, keys, text, "no key of action"},
		{"same effect", []Command{ok, {Name: "show help", Description: "cmd.help", Action: "help", Available: Always}}, sampleCommandKeys, keys, text, "make it an alias"},
		{"same name in two commands", []Command{ok, {Name: "quit", Aliases: []string{"help"}, Description: "cmd.quit", Action: "quit", Available: Always}}, sampleCommandKeys, keys, text, "also a name"},
		{"icon with variation selector", with(func(c *Command) { c.Icon = "\u26a0\ufe0f" }), sampleCommandKeys, keys, text, "variation selector"},
		{"icon with joiner", with(func(c *Command) { c.Icon = "\U0001F468\u200d\U0001F4BB" }), sampleCommandKeys, keys, text, "joiner"},
		{"icon as flag", with(func(c *Command) { c.Icon = "\U0001F1E9\U0001F1EA" }), sampleCommandKeys, keys, text, "regional indicator"},
		{"icon of two code points", with(func(c *Command) { c.Icon = "ab" }), sampleCommandKeys, keys, text, "2 code points"},
		{"icon with control character", with(func(c *Command) { c.Icon = "\x1b" }), sampleCommandKeys, keys, text, "control character"},
	} {
		_, err := NewCommandLine(CommandLineSpec{Commands: c.commands, Keys: c.nav}, c.keys, c.text)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want %q", c.name, err, c.want)
		}
	}
	if _, err := NewCommandLine(CommandLineSpec{Commands: sampleCommands(), Keys: sampleCommandKeys}, keys, text); err != nil {
		t.Errorf("the sample commands: %v", err)
	}
}

// Typed text finds commands by name and alias whatever the case and the blanks: an exact name first, then
// names that begin with it, then a word that does, then the text inside a word.
func TestMatchesRankExactPrefixWordAndInsideAWord(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"QU", []string{"quit"}},
		{"exit", []string{"quit"}},
		{"  GO   it ", []string{"go items"}},
		{"set", []string{"go settings", "go settings advanced"}},
		{"advanced", []string{"go settings advanced"}},
		{"up", []string{"upgrade", "export config"}},
		{"e", []string{"export config", "quit", "go overview", "go items", "go settings", "go settings advanced", "reload", "upgrade", "help"}},
		{"update", []string{"upgrade"}},
		{"zzz", nil},
	} {
		c.Reset()
		typeInto(c, tc.query)
		if got := names(c.Matches()); strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%q matches %q, want %q", tc.query, got, tc.want)
		}
	}
	exact := []Command{
		{Name: "lock all", Description: "cmd.help", Action: "lock", Available: Always},
		{Name: "lock", Description: "cmd.quit", Action: "quit", Available: Always},
	}
	c, _ = sampleCommandLine(t, i18n.English, exact)
	typeInto(c, "lock")
	if got := names(c.Matches()); strings.Join(got, "|") != "lock|lock all" {
		t.Errorf("an exact name does not come first: %q", got)
	}
}

// The empty line lists every command that is not hidden, in order; typed text still finds a hidden one.
func TestTheEmptyLineListsTheVisibleCommands(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	got := names(c.Matches())
	if len(got) != len(sampleCommands())-1 || strings.Contains(strings.Join(got, "|"), "advanced") || got[0] != "go overview" {
		t.Errorf("the empty line lists %q", got)
	}
}

// A command yields the same action as its key in the key table, and a command that opens an area names the
// area of the digit its key column shows.
func TestACommandDoesWhatItsKeyDoes(t *testing.T) {
	c, keys := sampleCommandLine(t, i18n.English, sampleCommands())
	actions := map[ActionID]Action{}
	for _, a := range keys.Actions() {
		actions[a.ID] = a
	}
	areas := map[AreaID]int{}
	for i, a := range sampleAreas() {
		areas[a.ID] = i
	}
	for _, cmd := range sampleCommands() {
		c.Reset()
		typeInto(c, cmd.Name)
		got, done := updateIn(c, enterKey, Context{Zone: ZoneRows})
		if !done || got.Name != cmd.Name || got.Action != cmd.Action || got.Area != cmd.Area || got.Tab != cmd.Tab {
			t.Errorf("%q runs %+v, %v", cmd.Name, got, done)
		}
		a, known := actions[cmd.Action]
		if !known {
			continue
		}
		pressKeys := a.DefaultKeys
		if cmd.Key > 0 {
			pressKeys = pressKeys[cmd.Key-1 : cmd.Key]
		}
		for _, key := range pressKeys {
			if id, ok := keys.Match(press(key), InputNavigation); !ok || id != cmd.Action {
				t.Errorf("%q: key %q triggers %q, the command %q", cmd.Name, key, id, cmd.Action)
			}
			if cmd.Area != "" && key != fmt.Sprint(areas[cmd.Area]+1) {
				t.Errorf("%q names area %q, its key is %q", cmd.Name, cmd.Area, key)
			}
		}
	}
	if c.Query() != "" {
		t.Errorf("a command that ran leaves %q in the line", c.Query())
	}
}

// The close key closes without effect, and the next opening starts empty.
func TestTheCloseKeyClosesWithoutEffect(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	typeInto(c, "quit")
	updateIn(c, downKey, Context{})
	if got, done := updateIn(c, escKey, Context{}); !done || got.Action != "" || got.Area != "" {
		t.Errorf("esc = %+v, %v", got, done)
	}
	if c.Query() != "" || len(c.Matches()) != len(sampleCommands())-1 {
		t.Errorf("after esc the line holds %q", c.Query())
	}
}

// An unknown command keeps the line open with what was typed and names it with the next step.
func TestAnUnknownCommandSaysWhy(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	typeInto(c, "frobnicate")
	if got, done := updateIn(c, enterKey, Context{}); done || got.Name != "" {
		t.Fatalf("an unknown command ran: %+v, %v", got, done)
	}
	view, _ := viewIn(c, looks(t)["noneascii"], 70, 12, Context{})
	plain := ansi.Strip(view)
	if c.Query() != "frobnicate" || !strings.Contains(plain, `unknown command "frobnicate"`) || !strings.Contains(plain, "clear the line") {
		t.Errorf("input %q, view:\n%s", c.Query(), plain)
	}
	if strings.Contains(plain, "no command matches") {
		t.Errorf("the refusal and the empty list both show:\n%s", plain)
	}
	typeInto(c, "x")
	if view, _ := viewIn(c, looks(t)["noneascii"], 70, 12, Context{}); strings.Contains(ansi.Strip(view), "unknown command") {
		t.Errorf("typing does not clear the refusal:\n%s", ansi.Strip(view))
	}
}

// A command acts only where its key acts: elsewhere the line stays open and names the reason.
func TestACommandActsOnlyWhereItsKeyActs(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	typeInto(c, "quit")
	if got, done := updateIn(c, enterKey, Context{Zone: ZoneForm}); done || got.Name != "" {
		t.Fatalf("quit ran in a form: %+v", got)
	}
	view, _ := viewIn(c, looks(t)["noneascii"], 80, 12, Context{Zone: ZoneForm})
	if plain := ansi.Strip(view); !strings.Contains(plain, "quit: not in a form; leave it with esc first") || c.Query() != "quit" {
		t.Errorf("refusal:\n%s", plain)
	}
	if got, done := updateIn(c, enterKey, Context{Zone: ZoneRows}); !done || got.Action != "quit" {
		t.Errorf("quit outside a form = %+v, %v", got, done)
	}
}

// The move keys choose with wrap-around, and editing the text starts the choice over.
func TestTheChoiceWrapsAndEditingStartsItOver(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	n := len(c.Matches())
	updateIn(c, upKey, Context{})
	if got, _ := updateIn(c, enterKey, Context{}); got.Name != "quit" {
		t.Errorf("up from the first runs %q, want the last", got.Name)
	}
	for range n {
		updateIn(c, tea.KeyPressMsg{Code: tea.KeyTab}, Context{})
	}
	updateIn(c, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, Context{})
	if got, _ := updateIn(c, enterKey, Context{}); got.Name != "quit" {
		t.Errorf("tab round and shift+tab run %q, want the last", got.Name)
	}
	typeInto(c, "go")
	updateIn(c, downKey, Context{})
	typeInto(c, " ")
	updateIn(c, tea.KeyPressMsg{Code: tea.KeyBackspace}, Context{})
	updateIn(c, tea.KeyPressMsg{Code: tea.KeyBackspace}, Context{})
	if c.Query() != "g" {
		t.Errorf("after two backspaces the line holds %q", c.Query())
	}
	typeInto(c, "o")
	if got, _ := updateIn(c, enterKey, Context{}); got.Name != "go overview" {
		t.Errorf("editing keeps the choice: %q", got.Name)
	}
}

// Where the line reads text, the keys of letters and signs are text, never their actions, and control
// characters never reach the line.
func TestTheLineTypesWhatOtherwiseActs(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	for _, key := range []tea.KeyPressMsg{press("q"), press("?"), press(":"), press("d"), press("space"), press("U")} {
		if got, done := updateIn(c, key, Context{}); done || got.Name != "" {
			t.Errorf("%q acted: %+v", key.String(), got)
		}
	}
	updateIn(c, tea.KeyPressMsg{Code: tea.KeyExtended, Text: "\x1b[31m\x07"}, Context{})
	updateIn(c, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}, Context{})
	updateIn(c, tea.KeyPressMsg{Code: 'y', Mod: tea.ModAlt, Text: "y"}, Context{})
	if want := "q?:d U[31m"; c.Query() != want {
		t.Errorf("the line holds %q, want %q", c.Query(), want)
	}
}

// Each row shows the key in force at its right, one digit for a command on one key of an action, and
// follows a rebinding.
func TestRowsShowTheKeyInForce(t *testing.T) {
	c, keys := sampleCommandLine(t, i18n.English, sampleCommands())
	st := looks(t)["noneascii"]
	row := func(name string) string {
		view, _ := viewIn(c, st, 80, 16, Context{})
		for _, l := range plainRows(view) {
			if strings.Contains(l, " "+name+" ") {
				return strings.TrimRight(strings.Trim(l, "| "), " ")
			}
		}
		t.Fatalf("no row %q:\n%s", name, view)
		return ""
	}
	for name, key := range map[string]string{"go overview": "1", "go items": "2", "go settings": "3", "help": "?", "reload": "ctrl+r", "quit": "q"} {
		if r := row(name); !strings.HasSuffix(r, " "+key) || !strings.Contains(r, name+"  ") {
			t.Errorf("row %q, want key %q at its right", r, key)
		}
	}
	if r := row("export config"); !strings.HasSuffix(r, "Write the configuration to a file") {
		t.Errorf("a command without key: %q", r)
	}
	if err := keys.Bind(map[ActionID]string{"help": "f4"}); err != nil {
		t.Fatal(err)
	}
	if r := row("help"); !strings.HasSuffix(r, " F4") {
		t.Errorf("the rebinding does not show: %q", r)
	}
}

// At most twelve matches show; the list scrolls with the choice, which stays visible, and says where it is.
func TestTheListScrollsWithTheChoice(t *testing.T) {
	var many []Command
	for i := range 20 {
		many = append(many, Command{Name: fmt.Sprintf("entry %02d", i), Description: "cmd.help", Area: AreaID(fmt.Sprint("a", i)), Available: Always})
	}
	c, _ := sampleCommandLine(t, i18n.English, many)
	st := looks(t)["noneascii"]
	for _, height := range []int{40, 10, 6} {
		c.Reset()
		for i := range 20 {
			view, targets := viewIn(c, st, 60, height, Context{})
			rows := plainRows(view)
			if len(rows) != height {
				t.Fatalf("height %d: %d rows", height, len(rows))
			}
			chosen := ""
			for _, r := range rows {
				if strings.HasPrefix(strings.TrimLeft(r, "| "), "> ") {
					chosen = r
				}
			}
			if !strings.Contains(chosen, fmt.Sprintf("entry %02d", i)) {
				t.Fatalf("height %d, choice %d not visible:\n%s", height, i, strings.Join(rows, "\n"))
			}
			if len(targets) > maxCommandRows || height == 40 && len(targets) != maxCommandRows {
				t.Errorf("height %d: %d rows shown", height, len(targets))
			}
			if !strings.Contains(view, " of 20") {
				t.Errorf("height %d: no position:\n%s", height, view)
			}
			updateIn(c, downKey, Context{})
		}
	}
}

// A click on a drawn row runs it as enter does, through the availability of the command.
func TestAClickRunsTheRow(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	st := looks(t)["noneascii"]
	clickRow := func(name string, ctx Context) (Command, bool) {
		view, targets := viewIn(c, st, 80, 16, ctx)
		rows := plainRows(view)
		for _, tg := range targets {
			if strings.Contains(rows[tg.Rect.Y], " "+name+" ") {
				// The panel stands at column 5, row 2 of the screen.
				m := NewMouseMap(OffsetTargets(targets, 5, 2), true)
				return clickIn(c, m.Handle(click(tg.Rect.X+5+3, tg.Rect.Y+2)), ctx)
			}
		}
		t.Fatalf("no row %q", name)
		return Command{}, false
	}
	if got, done := clickRow("go items", Context{}); !done || got.Area != "items" {
		t.Errorf("click on go items = %+v, %v", got, done)
	}
	if got, done := clickRow("quit", Context{Zone: ZoneForm}); done || got.Name != "" {
		t.Errorf("click on quit in a form ran it: %+v", got)
	}
	view, _ := viewIn(c, st, 80, 16, Context{Zone: ZoneForm})
	if !strings.Contains(view, "quit: not in a form") {
		t.Errorf("the click names no reason:\n%s", view)
	}
	if _, done := clickIn(c, MouseResult{Zone: ZoneTabs, Area: "items", Handled: true}, Context{}); done {
		t.Errorf("a click on a tab ran a command")
	}
	if _, done := clickIn(c, MouseResult{}, Context{}); done {
		t.Errorf("an unhandled event ran a command")
	}
}

// The line reads without colour, keeps the choice and the prompt, and is exactly its size at every size and in
// German; its targets stay inside it.
func TestTheLineKeepsItsSizeAndReadsWithoutColour(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		c, _ := sampleCommandLine(t, lang, sampleCommands())
		for look, st := range looks(t) {
			for width := 0; width <= 64; width += 4 {
				for height := 0; height <= 18; height += 3 {
					view, targets := viewIn(c, st, width, height, Context{Zone: ZoneForm})
					rows := strings.Split(view, "\n")
					if height == 0 {
						rows = nil
					}
					if len(rows) != height && !(height == 0 && view == "") {
						t.Fatalf("%s %s %dx%d: %d rows", lang, look, width, height, len(rows))
					}
					for i, r := range rows {
						if w := ansi.StringWidth(r); w != width {
							t.Errorf("%s %s %dx%d: row %d is %d cells: %q", lang, look, width, height, i, w, r)
						}
					}
					for _, tg := range targets {
						if r := tg.Rect; r.X < 0 || r.Y < 0 || r.X+r.Width > width || r.Y+r.Height > height {
							t.Errorf("%s %s %dx%d: target %+v outside", lang, look, width, height, tg)
						}
					}
				}
			}
		}
		view, _ := viewIn(c, looks(t)["noneascii"], 64, 18, Context{})
		noColorSequence(t, "noneascii", view)
		if plain := ansi.Strip(view); !strings.Contains(plain, "> ") || !strings.Contains(plain, ": ") {
			t.Errorf("%s: choice or prompt lost:\n%s", lang, plain)
		}
	}
	c, _ := sampleCommandLine(t, i18n.German, sampleCommands())
	view, _ := viewIn(c, looks(t)["noneascii"], 64, 18, Context{})
	if plain := ansi.Strip(view); !strings.Contains(plain, "Befehle") || !strings.Contains(plain, "Befehl suchen oder ausführen") || !strings.Contains(plain, "Übersicht öffnen") {
		t.Errorf("German line:\n%s", plain)
	}
}

// updateIn, viewIn and clickIn set the place the line was opened from before they hand it the key, draw it or
// hand it the click.
func updateIn(c *CommandLine, msg tea.KeyPressMsg, ctx Context) (Command, bool) {
	c.SetContext(ctx)
	return c.Update(msg)
}

func viewIn(c *CommandLine, styles Styles, width, height int, ctx Context) (string, []Target) {
	c.SetContext(ctx)
	return c.View(styles, width, height)
}

func clickIn(c *CommandLine, r MouseResult, ctx Context) (Command, bool) {
	c.SetContext(ctx)
	return c.Click(r)
}

// The line edits its text as a text line does: the cursor moves, typing and deletion work in the middle, and
// the matches follow.
func TestTheLineEditsInTheMiddle(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	in := func(msg tea.KeyPressMsg) { updateIn(c, msg, Context{}) }
	typeInto(c, "rload")
	for range 4 {
		in(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	typeInto(c, "e")
	if c.Query() != "reload" || names(c.Matches())[0] != "reload" {
		t.Fatalf("typing in the middle: %q, matches %v", c.Query(), names(c.Matches()))
	}
	in(tea.KeyPressMsg{Code: tea.KeyBackspace})
	in(tea.KeyPressMsg{Code: tea.KeyDelete})
	if c.Query() != "road" {
		t.Errorf("backspace and delete in the middle: %q", c.Query())
	}
	in(ctrlKey('a'))
	typeInto(c, "re")
	in(ctrlKey('e'))
	typeInto(c, " now")
	in(ctrlKey('w'))
	if c.Query() != "reroad " {
		t.Errorf("ctrl+w: %q", c.Query())
	}
	in(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	in(altKey('d'))
	if c.Query() != " " {
		t.Errorf("a word jump to the start and alt+d: %q", c.Query())
	}
	in(ctrlKey('e'))
	in(ctrlKey('u'))
	if c.Query() != "" || len(c.Matches()) != len(sampleCommands())-1 {
		t.Errorf("ctrl+u: %q with %d matches", c.Query(), len(c.Matches()))
	}
}

// Up, down, tab and shift+tab choose the match and leave the cursor; moving the cursor keeps the choice.
func TestTheChoiceKeysDoNotMoveTheCursor(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	typeInto(c, "go")
	for _, key := range []tea.KeyPressMsg{downKey, upKey, {Code: tea.KeyTab}, {Code: tea.KeyTab, Mod: tea.ModShift}} {
		updateIn(c, key, Context{})
		if c.line.cursor != 2 {
			t.Errorf("%s moved the cursor to %d", key.String(), c.line.cursor)
		}
	}
	updateIn(c, downKey, Context{})
	updateIn(c, tea.KeyPressMsg{Code: tea.KeyLeft}, Context{})
	updateIn(c, ctrlKey('a'), Context{})
	if got, _ := updateIn(c, enterKey, Context{}); got.Name != "go items" {
		t.Errorf("moving the cursor changed the choice: %q", got.Name)
	}
}

// Pasted text goes in at the cursor by the rule of the text line, filters again and starts the choice over.
func TestPasteIntoTheLine(t *testing.T) {
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	if c.Paste("\x1b\x07\n") || c.Query() != "" {
		t.Fatal("control characters were taken")
	}
	typeInto(c, "goitems")
	updateIn(c, downKey, Context{})
	for range 5 {
		updateIn(c, tea.KeyPressMsg{Code: tea.KeyLeft}, Context{})
	}
	if !c.Paste(" \r\n") || c.Query() != "go items" {
		t.Fatalf("paste in the middle: %q", c.Query())
	}
	if got := names(c.Matches()); len(got) == 0 || got[0] != "go items" {
		t.Errorf("matches after the paste: %v", got)
	}
	c.Reset()
	if !c.Paste("go\nsettings\n") || c.Query() != "go settings" {
		t.Fatalf("a paste with line breaks: %q", c.Query())
	}
	if got, _ := updateIn(c, enterKey, Context{}); got.Name != "go settings" {
		t.Errorf("enter runs %q", got.Name)
	}
}

// A long input scrolls sideways, the panel keeps its size and the cursor stays in view.
func TestALongInputScrollsAndKeepsTheCursorInView(t *testing.T) {
	const text = "abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	c, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	typeInto(c, text)
	st := looks(t)["noneascii"]
	plain := func() string {
		view, _ := viewIn(c, st, 24, 8, Context{})
		return ansi.Strip(view)
	}
	if got := plain(); !strings.Contains(got, text[len(text)-17:]+"_") || strings.Contains(got, "abcdef") {
		t.Errorf("end of the text:\n%s", got)
	}
	updateIn(c, ctrlKey('a'), Context{})
	if got := plain(); !strings.Contains(got, text[:12]) || strings.Contains(got, "ABCD") {
		t.Errorf("start of the text:\n%s", got)
	}
	for look, st := range looks(t) {
		for width := 0; width <= 30; width++ {
			view, _ := viewIn(c, st, width, 6, Context{})
			for i, row := range strings.Split(view, "\n") {
				if w := ansi.StringWidth(row); w != width {
					t.Errorf("%s: row %d is %d cells at width %d", look, i, w, width)
				}
			}
		}
	}
}

const (
	narrowIcon = "\u2605"     // one cell
	wideIcon   = "\U0001F680" // two cells
)

// iconCommands are the sample commands with a narrow and a wide icon on two of them and none on the rest.
func iconCommands() []Command {
	cmds := sampleCommands()
	cmds[0].Icon = narrowIcon
	cmds[1].Icon = wideIcon
	return cmds
}

// nameColumn is the cell at which the name of a command starts in its row of the plain view, or -1. The
// frame, the input line and the gap below it are skipped.
func nameColumn(view, name string) int {
	for _, l := range plainRows(view)[3:] {
		if i := strings.Index(l, " "+name+" "); i >= 0 {
			return ansi.StringWidth(l[:i]) + 1
		}
	}
	return -1
}

// An icon stands before the name, and the names stay flush for rows with a narrow icon, a wide icon and none.
func TestAnIconStandsBeforeTheNameAndTheNamesStayFlush(t *testing.T) {
	st := looks(t)["mono"]
	plain, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	withIcons, _ := sampleCommandLine(t, i18n.English, iconCommands())
	base, _ := viewIn(plain, st, 80, 16, Context{})
	view, _ := viewIn(withIcons, st, 80, 16, Context{})
	col := nameColumn(view, "go overview")
	for _, name := range []string{"go items", "go settings", "reload", "help", "quit"} {
		if got := nameColumn(view, name); got != col {
			t.Errorf("%q starts at cell %d, want %d:\n%s", name, got, col, view)
		}
	}
	// The column is the width of the widest icon and one blank.
	if want := nameColumn(base, "go overview") + 3; col != want {
		t.Errorf("names start at cell %d, want %d", col, want)
	}
	for glyph, name := range map[string]string{narrowIcon: "go overview", wideIcon: "go items"} {
		for _, l := range plainRows(view) {
			if strings.Contains(l, " "+name+" ") && !strings.Contains(l, glyph+" ") && !strings.Contains(l, glyph+"  ") {
				t.Errorf("no icon %q in the row of %q: %q", glyph, name, l)
			}
			if strings.Contains(l, " "+name+" ") && strings.Index(l, glyph) > strings.Index(l, name) {
				t.Errorf("icon %q stands behind the name: %q", glyph, l)
			}
		}
	}
	// A narrow icon alone makes a one cell column.
	narrow := sampleCommands()
	narrow[0].Icon = narrowIcon
	one, _ := sampleCommandLine(t, i18n.English, narrow)
	if got, _ := viewIn(one, st, 80, 16, Context{}); nameColumn(got, "help") != nameColumn(base, "help")+2 {
		t.Errorf("a one cell icon column:\n%s", got)
	}
}

// The column stays while the filter shows commands without icon only, and is absent without any icon and in
// an ASCII terminal.
func TestTheIconColumnFollowsTheCommandsAndTheTerminal(t *testing.T) {
	looksByName := looks(t)
	withIcons, _ := sampleCommandLine(t, i18n.English, iconCommands())
	plain, _ := sampleCommandLine(t, i18n.English, sampleCommands())
	full, _ := viewIn(withIcons, looksByName["mono"], 80, 16, Context{})
	typeInto(withIcons, "help")
	if got := withIcons.Matches(); len(got) != 1 || got[0].Icon != "" {
		t.Fatalf("filter shows %v", names(got))
	}
	filtered, _ := viewIn(withIcons, looksByName["mono"], 80, 16, Context{})
	if a, b := nameColumn(full, "help"), nameColumn(filtered, "help"); a != b || a < 0 {
		t.Errorf("the column moved with the filter: %d, %d", a, b)
	}
	base, _ := viewIn(plain, looksByName["mono"], 80, 16, Context{})
	if a, b := nameColumn(base, "help"), nameColumn(full, "help"); b != a+3 {
		t.Errorf("without icon at %d, with icons at %d", a, b)
	}
	for _, look := range []string{"noneascii", "asciidark"} {
		withIcons.Reset()
		view, _ := viewIn(withIcons, looksByName[look], 80, 16, Context{})
		if strings.Contains(view, narrowIcon) || strings.Contains(view, wideIcon) {
			t.Errorf("%s draws an icon:\n%s", look, view)
		}
		ref, _ := viewIn(plain, looksByName[look], 80, 16, Context{})
		if nameColumn(view, "help") != nameColumn(ref, "help") {
			t.Errorf("%s keeps the icon column", look)
		}
	}
}

// The line is exactly its size with icons at every size and in German, shortens its rows at a narrow width,
// and its targets are those of the same commands without icons.
func TestIconsKeepTheSizeTheCutAndTheTargets(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		withIcons, _ := sampleCommandLine(t, lang, iconCommands())
		plain, _ := sampleCommandLine(t, lang, sampleCommands())
		for look, st := range looks(t) {
			for width := 0; width <= 64; width += 4 {
				for height := 0; height <= 18; height += 3 {
					view, targets := viewIn(withIcons, st, width, height, Context{})
					_, want := viewIn(plain, st, width, height, Context{})
					for i, r := range strings.Split(view, "\n") {
						if w := ansi.StringWidth(r); height > 0 && w != width {
							t.Errorf("%s %s %dx%d: row %d is %d cells: %q", lang, look, width, height, i, w, r)
						}
					}
					if len(targets) != len(want) {
						t.Fatalf("%s %s %dx%d: %d targets, want %d", lang, look, width, height, len(targets), len(want))
					}
					for i := range targets {
						if targets[i] != want[i] {
							t.Errorf("%s %s %dx%d: target %+v, want %+v", lang, look, width, height, targets[i], want[i])
						}
					}
				}
			}
		}
		view, _ := viewIn(withIcons, looks(t)["mono"], 30, 16, Context{})
		if !strings.Contains(view, "\u2026") {
			t.Errorf("%s: no cut at a narrow width:\n%s", lang, view)
		}
	}
}

func argCommand(arg any) []Command {
	return []Command{{Name: "go items", Description: "cmd.area", DescriptionArgs: []any{arg}, Action: "area", Key: 2, Area: "items", Available: Always}}
}

// The arguments are formatted into the description in both languages; what they hold is escaped, and braces
// that look like a key placeholder stay as they are.
func TestDescriptionArgumentsAreFormattedAndEscaped(t *testing.T) {
	st := looks(t)["noneascii"]
	for lang, want := range map[i18n.Lang]string{i18n.English: "Open Items", i18n.German: "Items öffnen"} {
		c, _ := sampleCommandLine(t, lang, argCommand("Items"))
		if view, _ := viewIn(c, st, 80, 8, Context{}); !strings.Contains(view, want) {
			t.Errorf("%s: want %q:\n%s", lang, want, view)
		}
	}
	c, _ := sampleCommandLine(t, i18n.English, argCommand("A\x1b[31mB"))
	view, _ := viewIn(c, st, 80, 8, Context{})
	if strings.Contains(view, "\x1b") || !strings.Contains(view, `A\x1b[31mB`) {
		t.Errorf("a control character reaches the line:\n%q", view)
	}
	c, _ = sampleCommandLine(t, i18n.English, argCommand("{help}"))
	if view, _ := viewIn(c, st, 80, 8, Context{}); !strings.Contains(view, "Open {help}") {
		t.Errorf("a placeholder in an argument was replaced:\n%s", view)
	}
}

// The line holds its own copy of the arguments, and so does the command that Update yields.
func TestDescriptionArgumentsAreCopied(t *testing.T) {
	st := looks(t)["noneascii"]
	args := []any{"Items"}
	cmds := argCommand("")
	cmds[0].DescriptionArgs = args
	c, _ := sampleCommandLine(t, i18n.English, cmds)
	args[0] = "Changed"
	cmds[0].DescriptionArgs[0] = "Changed"
	if view, _ := viewIn(c, st, 80, 8, Context{}); !strings.Contains(view, "Open Items") {
		t.Errorf("the line follows the caller's slice:\n%s", view)
	}
	got, done := updateIn(c, enterKey, Context{})
	if !done || len(got.DescriptionArgs) != 1 {
		t.Fatalf("enter yields %+v, %v", got, done)
	}
	got.DescriptionArgs[0] = "Changed"
	if view, _ := viewIn(c, st, 80, 8, Context{}); !strings.Contains(view, "Open Items") {
		t.Errorf("the yielded command shares the line's slice:\n%s", view)
	}
	again, _ := updateIn(c, enterKey, Context{})
	if again.DescriptionArgs[0] != "Items" {
		t.Errorf("the next command holds %v", again.DescriptionArgs)
	}
}

// A command without a key shows an empty key column; a click and enter yield it.
func TestACommandWithoutKeyShowsNoKeyAndRuns(t *testing.T) {
	cmds := []Command{
		{Name: "reload", Description: "cmd.reload", Action: "reload", Available: Always},
		{Name: "export config", Description: "cmd.export", Action: "export", Available: Always},
	}
	c, _ := sampleCommandLine(t, i18n.English, cmds)
	st := looks(t)["noneascii"]
	view, targets := viewIn(c, st, 80, 8, Context{})
	for _, l := range plainRows(view) {
		if strings.Contains(l, " export config ") && !strings.HasSuffix(strings.TrimRight(strings.Trim(l, "| "), " "), "Write the configuration to a file") {
			t.Errorf("a key stands in the row: %q", l)
		}
	}
	m := NewMouseMap(OffsetTargets(targets, 5, 2), true)
	for i, tg := range targets {
		if !strings.Contains(plainRows(view)[tg.Rect.Y], "export config") {
			continue
		}
		got, done := clickIn(c, m.Handle(click(tg.Rect.X+5+3, tg.Rect.Y+2)), Context{})
		if !done || got.Name != "export config" {
			t.Errorf("click on target %d yields %+v, %v", i, got, done)
		}
	}
	c.Reset()
	typeInto(c, "export")
	if got, done := updateIn(c, enterKey, Context{}); !done || got.Name != "export config" {
		t.Errorf("enter yields %+v, %v", got, done)
	}
}

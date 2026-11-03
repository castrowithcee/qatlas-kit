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
			"cmd.help": "Show the help", "cmd.quit": "End the program",
		},
		i18n.German: {
			"cmd.overview": "Übersicht öffnen", "cmd.items": "Einträge öffnen", "cmd.settings": "Einstellungen öffnen",
			"cmd.advanced": "Erweiterte Einstellungen öffnen", "cmd.reload": "Daten neu einlesen",
			"cmd.upgrade": "Neuere Version installieren", "cmd.export": "Konfiguration in eine Datei schreiben",
			"cmd.help": "Hilfe anzeigen", "cmd.quit": "Programm beenden",
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

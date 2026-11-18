package tui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/projectpath"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// A dialog that asks for a file of an invented garden journal: the application reads the file system with a
// projectpath.Suggester and hands the dialog its suggestions, a directory as one that continues.
func ExampleDialog_suggestions() {
	dir, err := os.MkdirTemp("", "garden")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	for _, d := range []string{"beds", filepath.Join("beds", "east"), "pots"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			panic(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "beds", "basil.txt"), nil, 0o644); err != nil {
		panic(err)
	}

	act := func(id tui.ActionID, level tui.KeyLevel, key string) tui.Action {
		return tui.Action{ID: id, Level: level, DefaultKeys: []string{key}, Help: tui.TextID("garden.key." + id)}
	}
	second := func(a tui.Action, of tui.ActionID) tui.Action { a.SecondWayOf = of; return a }
	keys, err := tui.NewKeymap([]tui.Action{
		act("back", tui.LevelGlobal, "esc"),
		act("up", tui.LevelMove, "up"), act("down", tui.LevelMove, "down"),
		second(act("suggest.up", tui.LevelCtrl, "ctrl+p"), "up"), second(act("suggest.down", tui.LevelCtrl, "ctrl+n"), "down"),
		act("next", tui.LevelMove, "tab"), act("enter", tui.LevelMove, "enter"),
	}, tui.CharsetASCII)
	if err != nil {
		panic(err)
	}
	text, err := tui.NewTranslator(i18n.English, i18n.Catalogs[tui.TextID]{
		i18n.English: {"garden.open": "Open note", "garden.ok": "Open", "garden.cancel": "Cancel"},
		i18n.German:  {"garden.open": "Notiz öffnen", "garden.ok": "Öffnen", "garden.cancel": "Abbrechen"},
	}, keys)
	if err != nil {
		panic(err)
	}

	paths := projectpath.NewSuggester(projectpath.Files, "", dir)
	suggest := func(typed string) []tui.Suggestion {
		var out []tui.Suggestion
		for _, p := range paths.Suggest(typed) {
			out = append(out, tui.Suggestion{Text: p, Continues: strings.HasSuffix(p, string(filepath.Separator))})
		}
		return out
	}
	dialog, err := tui.NewDialog(tui.DialogSpec{
		Title: "garden.open", Question: "Type the path of the note.", Input: &tui.DialogInput{Suggest: suggest},
		Answers: []tui.Answer{{Action: "enter", Label: "garden.ok"}, {Action: "back", Label: "garden.cancel"}},
		Default: "enter",
		Keys:    tui.DialogKeys{Enter: "enter", Up: "up", Down: "down", Complete: "next", SuggestUp: "suggest.up", SuggestDown: "suggest.down"},
	}, keys, text)
	if err != nil {
		panic(err)
	}

	show := func() {
		view, _ := dialog.View(tui.Styles{}, 40, 9)
		for _, line := range strings.Split(view, "\n") {
			if line = strings.TrimRight(line, " "); line != "" {
				fmt.Println(filepath.ToSlash(line))
			}
		}
	}
	// tab takes the first suggestion; down marks one, and enter takes it: a directory keeps the dialog open,
	// a file answers with it.
	dialog.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	dialog.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	show()
	answer, _ := dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	fmt.Printf("%s %s\n", answer, filepath.ToSlash(dialog.Value()))
	// Output:
	//   Type the path of the note.
	//   beds/_
	//     beds/east/
	//   > beds/basil.txt
	// enter beds/basil.txt
}

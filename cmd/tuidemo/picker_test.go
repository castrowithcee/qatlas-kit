package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

const (
	keyDown  = "\x1b[B"
	keyEsc   = "\x1b"
	pickBody = "opens the list of beds"
)

func TestPickerOpensChoosesAndClosesByKey(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "8", pickBody)
	expect(t, term, "Bed: none yet")
	act(t, term, "p", "Choose a bed")
	expect(t, term, "South bed")
	act(t, term, keyDown+"\r", "Bed: Pot 3") // the marked row is taken by enter
	gone(t, term, "Choose a bed")
	act(t, term, "p", "Choose a bed")
	act(t, term, keyEsc, "Cancelled")
	gone(t, term, "Choose a bed")
	expect(t, term, "Bed: Pot 3") // a closed picker keeps the value
}

func TestPickerOpensAndChoosesByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	clickOn(t, term, "8 Pickers", pickBody)
	clickOn(t, term, "[ choose bed ]", "Choose a bed")
	clickOn(t, term, "South bed", "> South bed") // a click marks the row, the next one on it takes it
	clickOn(t, term, "South bed", "Bed: South bed")
	gone(t, term, "Choose a bed")
}

func TestPickerFilterTypesLettersNotSwitches(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "8", pickBody)
	act(t, term, "p", "Choose a bed")
	act(t, term, "po", "Pot 3")
	gone(t, term, "East bed")
	expect(t, term, "terminal  english  none  on")
}

func TestFilesOpenFolderAndChooseFileByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "8", pickBody)
	clickOn(t, term, "Files", "sample folder")
	expect(t, term, "notes.txt")
	act(t, term, "\t", "> beds/") // the sub-tab line took the click, tab gives the rows the focus
	act(t, term, "\r", "east.txt")
	gone(t, term, "notes.txt")
	act(t, term, keyDown+"\r", "File: beds/east.txt") // folders stand first: pots/ then east.txt
	expect(t, term, "File beds/east.txt chosen")
	act(t, term, keyEsc, "notes.txt") // back goes up one folder
	clickOn(t, term, "seeds/", "> seeds/")
	clickOn(t, term, "seeds/", "thyme.txt")
	clickOn(t, term, "thyme.txt", "> thyme.txt")
	clickOn(t, term, "thyme.txt", "File: seeds/thyme.txt")
}

func TestFilesTakeButtonAndKey(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "8", pickBody)
	clickOn(t, term, "Files", "sample folder")
	act(t, term, "g", "Mark a file, not a folder.") // the first row is a folder
	clickOn(t, term, "notes.txt", "> notes.txt")
	clickOn(t, term, "choose file", "File: notes.txt")
}

func TestSuggestionsTakenByKey(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "8", pickBody)
	clickOn(t, term, "Suggestions", "opens an input")
	act(t, term, "s", "Path in the sample folder:")
	expect(t, term, "seeds/")
	act(t, term, "se"+keyDown+"\r", "seeds/basil.txt") // a folder keeps the input open on its content
	expect(t, term, "seeds/thyme.txt")
	act(t, term, "\t\r", "Path seeds/basil.txt taken") // tab completes the first, enter takes it
	gone(t, term, "Path in the sample folder:")
	expect(t, term, "Path: seeds/basil.txt")
}

func TestSuggestionsTakenByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "8", pickBody)
	clickOn(t, term, "Suggestions", "opens an input")
	clickOn(t, term, "[ enter path ]", "Path in the sample folder:")
	act(t, term, "b", "beds/")
	gone(t, term, "notes.txt") // the rows move once the filter has been drawn
	clickOn(t, term, "beds/", "beds/")
	clickOn(t, term, "beds/", "beds/east.txt") // the second click takes it into the input and shows its content
	clickOn(t, term, "beds/south.txt", "beds/south.txt")
	clickOn(t, term, "beds/south.txt", "beds/south.txt")
	send(t, term, "\r")
	expect(t, term, "Path: beds/south.txt")
}

func TestTemporaryFolderLivesWithTheProgramAndIsRemovedAtTheEnd(t *testing.T) {
	tmp := t.TempDir()
	term := startDemoIn(t, "-icons none", tmp)
	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "tuidemo-") {
		t.Fatalf("while running: %v, %v", entries, err)
	}
	send(t, term, "q")
	if code, err := term.Wait(wait); err != nil || code != 0 {
		t.Fatalf("the program ended with %d, %v", code, err)
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestTemporaryFolderIsRemovedOnAnErrorPath(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, tmp)
	}
	if err := run([]string{"-theme", "no-such-theme"}, os.Stdout, nil); err == nil {
		t.Fatal("an unknown theme did not end the program")
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestBelowRefusesWhatLeavesTheRoot(t *testing.T) {
	for _, parts := range [][]string{{".."}, {"seeds", "..", ".."}, {"/etc"}, {""}} {
		if p, err := below("/root", parts); err == nil {
			t.Errorf("%q gave %q", parts, p)
		}
	}
	if p, err := below("/root", nil); err != nil || p != "/root" {
		t.Errorf("the root itself: %q, %v", p, err)
	}
}

func TestSuggestPathsFollowTheTypedFolder(t *testing.T) {
	texts := func(typed string) []string {
		var out []string
		for _, s := range suggestPaths(typed) {
			out = append(out, s.Text)
		}
		return out
	}
	if got, want := texts(""), []string{"notes.txt", "seeds/", "beds/"}; !slices.Equal(got, want) {
		t.Errorf("root: %v, want %v", got, want)
	}
	if got, want := texts("beds/"), []string{"beds/east.txt", "beds/south.txt", "beds/pots/"}; !slices.Equal(got, want) {
		t.Errorf("beds/: %v, want %v", got, want)
	}
	if got := texts("beds/s"); !slices.Equal(got, []string{"beds/south.txt"}) {
		t.Errorf("beds/s: %v", got)
	}
	if got := texts("beds/south.txt"); len(got) != 0 {
		t.Errorf("a full name suggests itself: %v", got)
	}
	if root := suggestPaths(""); root[0].Continues || !root[1].Continues {
		t.Errorf("only a folder continues: %+v", root)
	}
}

func TestPickerModelChoosesByEnterAndByTheButton(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "8")
	m.handle(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if m.kind != kindPick {
		t.Fatalf("p did not open the picker: kind %d", m.kind)
	}
	m.handle(tea.KeyPressMsg{Code: tea.KeyDown})
	m.handle(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.bed != "Pot 3" || m.modal != nil {
		t.Fatalf("bed %q, modal %v", m.bed, m.modal)
	}
	m.run(actPick, "")
	m.refresh()
	if !clickWhere(m, func(r tui.MouseResult) bool {
		return r.Action == actEnter && r.Item != "" && strings.HasPrefix(r.Item, "\x00B")
	}) {
		t.Fatal("the choose button is no click target")
	}
	if m.modal != nil || m.bed != "Pot 3" {
		t.Fatalf("the button kept the picker open or lost the bed: %q", m.bed)
	}
}

func TestPickerClosesOnAClickOutsideAsBackDoes(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "8")
	m.handle(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m.refresh()
	if !clickWhere(m, func(r tui.MouseResult) bool { return r.Zone == tui.ZoneOutside }) {
		t.Fatal("the picker has no target outside its frame")
	}
	if m.modal != nil || m.bed == "Pot 3" {
		t.Fatalf("a click outside left the picker open or chose: modal %v bed %q", m.modal, m.bed)
	}
}

func TestFilesTakeNeverLeavesTheRoot(t *testing.T) {
	m := testModel(t, uiprefs.IconsNone)
	m.run(actArea, "8")
	m.tabs["pickers"] = "files"
	b := m.live().(*filesBlock)
	m.zone = tui.ZoneRows
	b.run(m, tui.FileResult{Action: &tui.FileAction{Action: actEnter, Dir: []string{"seeds"},
		Entry: tui.FileEntry{Name: "basil.txt"}, HasEntry: true}})
	if b.chosen != "seeds/basil.txt" {
		t.Fatalf("chosen %q", b.chosen)
	}
	b.run(m, tui.FileResult{Action: &tui.FileAction{Action: actEnter, Dir: []string{".."},
		Entry: tui.FileEntry{Name: "x.txt"}, HasEntry: true}})
	if b.chosen != "seeds/basil.txt" || m.message.Kind != tui.MessageError {
		t.Fatalf("a folder outside the root was taken: %q, %v", b.chosen, m.message)
	}
}

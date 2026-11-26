package main

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/ptytest"
)

// copiedBy drags from the first cell of from to the last of to on one row, waits for the notice and returns
// the text handed to the clipboard by that drag.
func copiedBy(t *testing.T, term *ptytest.Terminal, from, to string) string {
	t.Helper()
	before := len(clipboard(t, term.Output()))
	x, y := find(t, term, from)
	ex, _ := find(t, term, to)
	send(t, term, drag(x, y, ex+len([]rune(to))-1, y))
	if err := term.WaitFor(wait, func(out string) bool { return len(clipboard(t, out)) > before }); err != nil {
		t.Fatalf("nothing copied from %q to %q: %v", from, to, err)
	}
	all := clipboard(t, term.Output())
	return all[len(all)-1]
}

// A drag on a row of a list copies the cell value and opens and marks nothing, whether the row is marked or
// the list opens at one click; a click without motion acts as before.
func TestListsDragNeitherMarksNorOpens(t *testing.T) {
	term := startDemo(t, "-icons none")
	clickOn(t, term, "6 Lists", "types a filter")
	clickOn(t, term, "Groups", "East bed (2)")
	clickOn(t, term, "Fennel", ">     Fennel")
	if got := copiedBy(t, term, "Mint", "Mint"); got != "Mint" {
		t.Errorf("copied %q, want the cell", got)
	}
	if onScreen(term.Output(), "Opened") || onScreen(term.Output(), ">     Mint") {
		t.Error("a drag over another row opened or marked it")
	}
	if got := copiedBy(t, term, "Fennel", "Fennel"); got != "Fennel" {
		t.Errorf("copied %q, want the cell", got)
	}
	if onScreen(term.Output(), "Opened") {
		t.Error("a drag over the marked row opened it")
	}
	clickOn(t, term, "Mint", "Opened Mint") // a focused row opens at once
}

func TestFormDragCopiesShownValueNeverTheMaskedInput(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "7", "A bed to add")
	act(t, term, "\t\t", "> Name")
	act(t, term, "Fern bed", "Fern bed")
	act(t, term, "\t\t\t\t", "> Shed code")
	act(t, term, "abc", "Shed code")
	// While it is typed into, the field is no selection target; moved on, the name is a value to copy.
	act(t, term, "\t", "Fern bed")
	if got := copiedBy(t, term, "Fern bed", "Fern bed"); got != "Fern bed" {
		t.Errorf("copied %q", got)
	}
	x, y := find(t, term, "Shed code")
	send(t, term, drag(x+14, y, x+40, y))
	if strings.Contains(term.Output(), "abc") && onScreen(term.Output(), "abc") {
		t.Error("the typed code is on the screen")
	}
	for _, c := range clipboard(t, term.Output()) {
		if strings.Contains(c, "abc") {
			t.Errorf("the clipboard holds %q", c)
		}
	}
}

func TestDialogHelpAndExplanationDragCopyText(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "a", "Reset the counter?")
	if got := copiedBy(t, term, "Reset the counter?", "Reset the counter?"); got != "Reset the counter?" {
		t.Errorf("dialog: %q", got)
	}
	// A click on the text answers nothing.
	clickOnly(t, term, "Reset the counter?")
	expect(t, term, "Reset the counter?")
	act(t, term, "n", "Cancelled")
	gone(t, term, "Reset the counter?")
	act(t, term, "?", "Tabs and switches")
	if got := copiedBy(t, term, "Tabs and switches", "switches"); got != "Tabs and switches" {
		t.Errorf("help: %q", got)
	}
	send(t, term, "?")
	gone(t, term, "Tabs and switches")
	act(t, term, "i", "Raised bed:")
	if got := copiedBy(t, term, "Raised bed:", "bed:"); got != "Raised bed:" {
		t.Errorf("explanation: %q", got)
	}
	// A click on its text closes it as before.
	clickOnly(t, term, "invented journal")
	gone(t, term, "Raised bed:")
}

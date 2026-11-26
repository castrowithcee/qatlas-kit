package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/ptytest"
)

const helperEnv = "TUIDEMO_HELPER"

// TestHelperProcess is the program the terminal tests start; it does nothing in a normal run.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) == "" {
		return
	}
	if err := run(strings.Fields(os.Getenv(helperEnv+"_ARGS")), os.Stdout, os.Environ()); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

const wait = 15 * time.Second

// startDemo runs the program at a pseudo-terminal of 100 by 30 cells and waits for its first screen.
func startDemo(t *testing.T, args string) *ptytest.Terminal {
	t.Helper()
	return startDemoIn(t, args, t.TempDir())
}

// startDemoIn is startDemo with tmp as the temporary directory of the program, so a test sees what it leaves.
func startDemoIn(t *testing.T, args, tmp string) *ptytest.Terminal {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the pseudo console of Windows renders the screen itself, so its output differs")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	term, err := ptytest.Start(ptytest.Command{Path: self, Args: []string{"-test.run=^TestHelperProcess$"},
		Env:  []string{helperEnv + "=1", helperEnv + "_ARGS=" + args, "TERM=xterm-256color", "TMPDIR=" + tmp, "NO_COLOR=1", "LANG=C.UTF-8"},
		Size: ptytest.Size{Columns: 100, Rows: 30}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	expect(t, term, "Terminal building blocks at work")
	return term
}

// onScreen reports whether text stands on the screen the output draws.
func onScreen(output, text string) bool {
	return strings.Contains(strings.Join(screen(output, 100, 30), "\n"), text)
}

func expect(t *testing.T, term *ptytest.Terminal, text string) {
	t.Helper()
	if err := term.WaitFor(wait, func(out string) bool { return onScreen(out, text) }); err != nil {
		t.Fatalf("waiting for %q: %v\n%s\n%q", text, err, strings.Join(screen(term.Output(), 100, 30), "\n"), term.Output()[max(len(term.Output())-900, 0):])
	}
}

func send(t *testing.T, term *ptytest.Terminal, input string) {
	t.Helper()
	if _, err := term.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
}

func TestStartShowsFrameAndQuitsCleanly(t *testing.T) {
	term := startDemo(t, "-icons none")
	for _, want := range []string{"Kit workbench", "1 Overview", "2 Frame", "3 Settings", "About", "Sample data", "quit"} {
		expect(t, term, want)
	}
	send(t, term, "q")
	if code, err := term.Wait(wait); err != nil || code != 0 {
		t.Fatalf("the program ended with %d, %v: %q", code, err, term.Output())
	}
}

// act types input and waits until text stands on the screen.
func act(t *testing.T, term *ptytest.Terminal, input, text string) {
	t.Helper()
	send(t, term, input)
	expect(t, term, text)
}

// clickOnly clicks the text on the screen, one cell inside it.
func clickOnly(t *testing.T, term *ptytest.Terminal, text string) {
	t.Helper()
	column, row := find(t, term, text)
	send(t, term, click(column+1, row))
}

// click is the press and release of the left mouse button at the 1-based column and row.
func click(column, row int) string {
	return fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", column, row, column, row)
}

// find waits until text stands on the screen and returns its 1-based column and row.
func find(t *testing.T, term *ptytest.Terminal, text string) (column, row int) {
	t.Helper()
	err := term.WaitFor(wait, func(out string) bool {
		for r, line := range screen(out, 100, 30) {
			if c := strings.Index(line, text); c >= 0 {
				column, row = len([]rune(line[:c]))+1, r+1
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("%q is not on the screen: %v\n%s", text, err, strings.Join(screen(term.Output(), 100, 30), "\n"))
	}
	return column, row
}

// clickOn clicks the text on the screen, one cell inside it, and waits for want.
func clickOn(t *testing.T, term *ptytest.Terminal, text, want string) {
	t.Helper()
	column, row := find(t, term, text)
	act(t, term, click(column+1, row), want)
}

const (
	tabsBody  = "Tabs and sub-tabs"
	probeBody = "Mouse probe"
	lookBody  = "Theme, icons and navigation change at once"
	aboutBody = "Terminal building blocks at work"
)

func TestTabsSwitchByKey(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "2", tabsBody)
	act(t, term, "\t", "[Tabs]") // the focus moves to the sub-tab line
	act(t, term, "\x1b[C", probeBody)
	act(t, term, "\x1b[D", tabsBody)
	act(t, term, "\x1b[Z", ">  1 Overview") // shift+tab gives the focus back to the tab line
	act(t, term, "\x1b[C", lookBody)
	act(t, term, "\x1b[D", tabsBody)
	act(t, term, "1", aboutBody)
}

func TestTabsSwitchByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	clickOn(t, term, "2 Frame", tabsBody)
	clickOn(t, term, "3 Settings", lookBody)
	clickOn(t, term, "Input", "Language and mouse change at once")
	clickOn(t, term, "1 Overview", aboutBody)
	clickOn(t, term, "Sample data", "PLANT    BED")
}

func TestMouseProbeCountsClicks(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "2", tabsBody)
	clickOn(t, term, "Mouse", probeBody)
	clickOn(t, term, "[ Count ]", "Count: 1")
	act(t, term, "+", "Count: 2")
}

func TestThemeSwitchByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "t", "theme: demo-dark")
	clickOn(t, term, "switch theme", "theme: demo-light")
	act(t, term, "T", "theme: demo-dark")
	clickOn(t, term, "switch theme back", "theme: terminal")
}

func TestLanguageSwitchByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "l", "1 Übersicht")
	expect(t, term, "Sprache: deutsch")
	expect(t, term, "Terminal-Bausteine im Betrieb")
	clickOn(t, term, "Sprache wechseln", "language: english")
	expect(t, term, "1 Overview")
}

func TestCtrlCQuitsCleanly(t *testing.T) {
	term := startDemo(t, "")
	send(t, term, "\x03")
	if code, err := term.Wait(wait); err != nil || code != 0 {
		t.Fatalf("the program ended with %d, %v: %q", code, err, term.Output())
	}
	if out := term.Output(); !strings.Contains(out, "\x1b[?1049l") {
		t.Errorf("the alternate screen was not left: %q", out[max(len(out)-200, 0):])
	}
}

// gone waits until text no longer stands on the screen.
func gone(t *testing.T, term *ptytest.Terminal, text string) {
	t.Helper()
	if err := term.WaitFor(wait, func(out string) bool { return !onScreen(out, text) }); err != nil {
		t.Fatalf("%q is still on the screen: %v", text, err)
	}
}

func TestQuestionAnsweredByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "a", "Reset the counter?")
	act(t, term, "n", "Cancelled")
	gone(t, term, "Reset the counter?")
	act(t, term, "a", "Reset the counter?")
	act(t, term, "\r", "Cancelled") // enter gives the default, which is no and never the destroying answer
	gone(t, term, "Reset the counter?")
	act(t, term, "\x1b[C\x1b[C\x1b[C", "4 Dialogs")
	act(t, term, "4", "A question over the screen")
	clickOn(t, term, "[ ask question ]", "Reset the counter?")
	clickOn(t, term, "[ yes ]", "Counter reset")
	gone(t, term, "Reset the counter?")
}

func TestQuestionBlocksTheBackground(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "a", "Reset the counter?")
	send(t, term, "t") // a key of the screen behind goes nowhere
	column, row := find(t, term, "3 Settings")
	send(t, term, click(column+1, row)) // nor does a click on a tab
	act(t, term, "n", "Cancelled")
	gone(t, term, "Reset the counter?")
	expect(t, term, "[1 Overview]")
	expect(t, term, "terminal  english  none  on")
}

func TestInputDialogKeepsAnEmptyNameAndTakesANewOne(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "\x1b[C\x1b[C\x1b[C", "4 Dialogs")
	act(t, term, "r", "Name of the garden journal:")
	act(t, term, "\x15\r", "The name must not be empty.") // ctrl+u empties the field, enter is refused
	act(t, term, "Rose bed\r", "Journal renamed to Rose bed")
	gone(t, term, "Name of the garden journal:")
	act(t, term, "1", "[1 Overview]")
	clickOn(t, term, "Sample data", "Rose bed")
	act(t, term, "r", "Name of the garden journal:")
	act(t, term, "\x1b", "Cancelled")
}

func TestNoticeClosesByKey(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "o", "This is a calm message without a question.")
	send(t, term, "\r")
	gone(t, term, "This is a calm message")
}

func TestHelpOpensAndClosesByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "?", "About the workbench:")
	act(t, term, "\x1b[C", "The key table has one place for every key")
	clickOn(t, term, "Mouse", "A click does what its key does")
	send(t, term, "?") // the help key closes it
	gone(t, term, "A click does what")
}

func TestHelpClosesByClickOnItsKeyLine(t *testing.T) {
	term := startDemo(t, "-icons none")
	clickOn(t, term, "[?] help", "About the workbench:")
	clickOnly(t, term, "close")
	gone(t, term, "About the workbench:")
}

func TestExplanationOpensCopiesAndCloses(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "i", "Raised bed:")
	act(t, term, "c", "Handed to the terminal for the clipboard")
	send(t, term, "i")
	gone(t, term, "Raised bed:")
	clickOn(t, term, "[i] info", "Raised bed:")
	clickOnly(t, term, "invented journal")
	gone(t, term, "Raised bed:")
}

func TestCommandLineRunsByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, ":", "go overview")
	// The line stands over the dimmed screen: the text of the screen shows beside its frame.
	row := screenRow(term, "go overview")
	if before, _, _ := strings.Cut(row, "║"); strings.TrimSpace(before) == "" {
		t.Errorf("the command line does not stand over the screen: %q", row)
	}
	act(t, term, "theme", "Open the tab") // typing filters; the theme command stays
	act(t, term, "\r", "theme: demo-dark")
	gone(t, term, "search or run a command")
	act(t, term, ":", "go overview")
	clickOn(t, term, "go frame", "Tabs and sub-tabs")
	gone(t, term, "search or run a command")
	act(t, term, ":", "go overview")
	act(t, term, "\x1b", "[2 Frame]")
	gone(t, term, "search or run a command")
	clickOn(t, term, "[:] commands", "go overview")
	clickOnly(t, term, "esc back")
	gone(t, term, "search or run a command")
}

// screenRow returns the line of the screen that holds text.
func screenRow(term *ptytest.Terminal, text string) string {
	for _, line := range screen(term.Output(), 100, 30) {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

// command runs a command of the command line by its name.
func command(t *testing.T, term *ptytest.Terminal, name, want string) {
	t.Helper()
	act(t, term, ":", "go overview")
	send(t, term, name)
	act(t, term, "\r", want)
	gone(t, term, "search or run a command")
}

func TestSwitchesRunFromTheCommandLine(t *testing.T) {
	term := startDemo(t, "")
	command(t, term, "theme", "theme: demo-dark")
	command(t, term, "icons", "icons: nerdfont")
	command(t, term, "mouse", "mouse: off")
	command(t, term, "mouse", "mouse: on")
	command(t, term, "language", "Sprache: deutsch")
	expect(t, term, "Terminal-Bausteine im Betrieb") // the icons are on, so the tab line has a glyph
}

// footerHint reports whether the footer of the screen shows text; the hints stand as key and label.
func footerHint(term *ptytest.Terminal, text string) bool {
	return onScreen(term.Output(), text)
}

// hintsOf checks that the open tab shows the footer hints in want and none of the others.
func hintsOf(t *testing.T, term *ptytest.Terminal, want string, all ...string) {
	t.Helper()
	expect(t, term, want)
	for _, h := range all {
		if h != want && footerHint(term, h) {
			t.Errorf("the footer shows %q beside %q:\n%s", h, want, strings.Join(screen(term.Output(), 100, 30), "\n"))
		}
	}
}

func TestFooterShowsTheKeysOfTheOpenTabOnly(t *testing.T) {
	term := startDemo(t, "-icons none")
	dialogs := []string{"a ask question", "r rename", "o show notice", "+ count"}
	act(t, term, "4", "A question over the screen")
	hintsOf(t, term, "a ask question", dialogs...)
	act(t, term, "\t", "[Question]")
	act(t, term, "\x1b[C", "A short input")
	hintsOf(t, term, "r rename", dialogs...)
	act(t, term, "\x1b[C", "A calm message")
	hintsOf(t, term, "o show notice", dialogs...)
	act(t, term, "2", tabsBody)
	hintsOf(t, term, "q quit", dialogs...)
	act(t, term, "\x1b[C", probeBody) // the focus stays on the sub-tab line
	hintsOf(t, term, "+ count", dialogs...)
}

func TestWorkAreaButtonByKeyboardOnly(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "2", tabsBody)
	act(t, term, "\x1b[B", "[Tabs]")    // down: tab line to sub-tab line
	act(t, term, "\x1b[C", probeBody)   // right: the Mouse sub-tab
	act(t, term, "\x1b[B", "> Count <") // down: into the work area, the button shows the focus
	act(t, term, "\r", "Count: 1")      // enter triggers it like a click
	act(t, term, "\x1b[A", "[Mouse]")   // up: back to the sub-tab line
	act(t, term, "\r", "> Count <")     // enter: into the work area again
	act(t, term, "\x1b[C", "> Count <") // right does not leave the work area
	act(t, term, "\x1b[Z", "[Mouse]")   // shift+tab: back to the sub-tab line
}

func TestBlocksAndOpenersByKeyboardOnly(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "6", "types a filter")
	send(t, term, "\x1b[B")           // down: tab line to sub-tab line
	act(t, term, "\x1b[B", "> Basil") // down: into the list, its first row
	send(t, term, "\x1b[A")           // up from the first row: back to the sub-tab line
	gone(t, term, "> Basil")
	act(t, term, "\x1b[C", "Groups") // right on the sub-tab line steps the sub-tab
	act(t, term, "\x1b[D", "types a filter")
	act(t, term, "\r", "> Basil") // enter: into the list again
	send(t, term, "\x1b")         // esc: back to the sub-tab line
	gone(t, term, "> Basil")
	send(t, term, "\x1b[A")                // up: the tab line
	act(t, term, "\x1b[C", "A bed to add") // right on the tab line opens the next tab, the Forms
	act(t, term, "\x1b[C", pickBody)
	act(t, term, "\x1b[B\x1b[B", "> choose bed <") // the opener of the tab shows the focus
	act(t, term, "\r", "Choose a bed")             // enter opens it as a click would
}

// pathForms are the navigation forms that name the place as a path.
var pathForms = []string{"pathline", "header"}

func TestNavigationSwitchesByCommandAndKey(t *testing.T) {
	term := startDemo(t, "-icons none")
	command(t, term, "navigation", "navigation: pathline")
	expect(t, term, "Overview ▾")
	gone(t, term, "1 Overview")
	command(t, term, "navigation", "navigation: header")
	command(t, term, "navigation", "navigation: tabs")
	expect(t, term, "1 Overview")
	act(t, term, "N", "navigation: pathline")
	gone(t, term, "1 Overview")
	act(t, term, "N", "navigation: header")
	act(t, term, "N", "navigation: tabs")
	expect(t, term, "1 Overview")
}

func TestNavigationSwitchesInTheSettingsForm(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "3", lookBody)
	act(t, term, "\x1b[B\x1b[B", "> theme") // down to the sub-tab line, then into the form
	act(t, term, "\x1b[B\x1b[B", "> navigation")
	act(t, term, "\x1b[C", "navigation: pathline")
	expect(t, term, "Settings ▾")
	expect(t, term, "> navigation ║ path line") // the form keeps the focus and shows the value
	act(t, term, "N", "║ path in header")
	act(t, term, "\x1b[D", "navigation: pathline")
	act(t, term, "\x1b[D", "navigation: tabs")
	expect(t, term, "[3 Settings]")
}

func TestTabFormIgnoresThePathKeys(t *testing.T) {
	term := startDemo(t, "-icons none")
	send(t, term, "m")
	send(t, term, "\r")
	act(t, term, "2", tabsBody)
	gone(t, term, "All areas")
	if footerHint(term, "m menu") {
		t.Error("the footer names the menu key on the tab form")
	}
}

func TestPathOverlaysOpenChooseAndCloseByKey(t *testing.T) {
	for _, form := range pathForms {
		t.Run(form, func(t *testing.T) {
			term := startDemo(t, "-icons none -navigation "+form)
			hintsOf(t, term, "m menu")
			act(t, term, "\r", "PLACE") // enter opens the choice among the tabs of the area
			gone(t, term, "All areas")
			act(t, term, "\x1b[B\r", "Garden journal")
			gone(t, term, "PLACE")
			if form == "pathline" {
				// Only the line names both sections; the header keeps the current one at this width.
				act(t, term, "\x1b[D", "[Overview ▾]") // the focus moves to the area section
				act(t, term, "\r", "All areas")
				act(t, term, "\x1b", "Garden journal")
				gone(t, term, "All areas")
			}
			act(t, term, "m", "All areas") // m opens the overview from anywhere
			act(t, term, "\x1b[B\x1b[B\x1b[B\r", probeBody)
			gone(t, term, "PLACE")
			act(t, term, "m", "All areas")
			act(t, term, "\x1b", probeBody)
			gone(t, term, "PLACE")
		})
	}
}

func TestPathOverlaysOpenAndChooseByClick(t *testing.T) {
	for _, form := range pathForms {
		t.Run(form, func(t *testing.T) {
			term := startDemo(t, "-icons none -navigation "+form)
			open, section := "Overview ▾", "Settings ▾"
			if form == "header" {
				open, section = "About ▾", "Look ▾"
			}
			clickOn(t, term, open, "PLACE")
			if form == "pathline" {
				clickOn(t, term, "Look", lookBody) // the area section opened the overview
			} else {
				clickOn(t, term, "Sample data", "Garden journal") // the only section opened the choice
				clickOn(t, term, "Sample data ▾", "PLACE")
				clickOn(t, term, "About", aboutBody)
				act(t, term, "m", "All areas")
				clickOn(t, term, "Look", lookBody)
			}
			gone(t, term, "PLACE")
			expect(t, term, section)
			// A click behind the overlay does nothing, and a click on the overlay's key line closes it.
			act(t, term, "m", "All areas")
			clickOnly(t, term, "[i] info")
			expect(t, term, "All areas")
			clickOn(t, term, "esc back", lookBody)
			gone(t, term, "PLACE")
			gone(t, term, "Raised bed:")
		})
	}
}

func TestPathLeadsDownIntoTheWorkAreaAndBackByKeyboardOnly(t *testing.T) {
	for _, form := range pathForms {
		t.Run(form, func(t *testing.T) {
			term := startDemo(t, "-icons none -navigation "+form)
			send(t, term, "\x1b[B") // the overview has no work area: down stays on the path
			act(t, term, "2", tabsBody)
			act(t, term, "\r", "PLACE")
			act(t, term, "\x1b[B\r", probeBody)
			act(t, term, "\x1b[B", "> Count <") // down: into the buttons
			act(t, term, "\r", "Count: 1")
			send(t, term, "\x1b[A") // up: back on the path
			gone(t, term, "> Count <")
			act(t, term, "\r", "PLACE") // enter on the path opens the choice again
			act(t, term, "\x1b", probeBody)
			gone(t, term, "PLACE")
			act(t, term, "6", "types a filter")
			act(t, term, "\x1b[B", "> Basil") // down: into the list block
			send(t, term, "\x1b[A")           // up from its first row: back on the path
			gone(t, term, "> Basil")
			act(t, term, "\t", "> Basil") // tab leads in as well
			send(t, term, "\x1b")         // esc leads out
			gone(t, term, "> Basil")
			act(t, term, "m", "All areas")
		})
	}
}

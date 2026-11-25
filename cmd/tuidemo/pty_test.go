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
	if runtime.GOOS == "windows" {
		t.Skip("the pseudo console of Windows renders the screen itself, so its output differs")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	term, err := ptytest.Start(ptytest.Command{Path: self, Args: []string{"-test.run=^TestHelperProcess$"},
		Env:  []string{helperEnv + "=1", helperEnv + "_ARGS=" + args, "TERM=xterm-256color", "NO_COLOR=1", "LANG=C.UTF-8"},
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

// find returns the 1-based column and row of text on the screen.
func find(t *testing.T, term *ptytest.Terminal, text string) (column, row int) {
	t.Helper()
	for r, line := range screen(term.Output(), 100, 30) {
		if c := strings.Index(line, text); c >= 0 {
			return len([]rune(line[:c])) + 1, r + 1
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", text, strings.Join(screen(term.Output(), 100, 30), "\n"))
	return 0, 0
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
	lookBody  = "Theme and icons change at once"
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
	clickOn(t, term, "Input", "Press the key or click the row")
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
}

func TestLanguageSwitchByKeyAndByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "l", "1 Übersicht")
	expect(t, term, "Sprache: deutsch")
	expect(t, term, "Terminal-Bausteine im Betrieb")
	clickOn(t, term, "Sprache wechseln", "language: english")
	expect(t, term, "1 Overview")
}

func TestIconsAndMouseSwitch(t *testing.T) {
	term := startDemo(t, "")
	act(t, term, "3", "[ unicode ]")
	act(t, term, "I", "[ nerdfont ]")
	clickOn(t, term, "[ nerdfont ]", "[ none ]")
	act(t, term, "\t", "[Look]")
	act(t, term, "\x1b[C", "Language and mouse change at once")
	act(t, term, "M", "[ off ]")
	act(t, term, "M", "[ on ]")
}

func TestSettingsRowsSwitchByClick(t *testing.T) {
	term := startDemo(t, "-icons none")
	act(t, term, "3", lookBody)
	clickOn(t, term, "[ terminal ]", "[ demo-dark ]")
	act(t, term, "\t", "[Look]")
	act(t, term, "\x1b[C", "Language and mouse change at once")
	clickOn(t, term, "[ english ]", "[ deutsch ]")
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
	clickOnly(t, term, "close")
	gone(t, term, "search or run a command")
}

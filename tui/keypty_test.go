package tui

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/ptytest"
)

const keyHelperEnv = "TUI_KEY_HELPER"

// keyReporter prints the name of every key it receives.
type keyReporter struct{}

func (keyReporter) Init() tea.Cmd { return nil }

func (keyReporter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return keyReporter{}, nil
	}
	if key.String() == "ctrl+c" {
		return keyReporter{}, tea.Quit
	}
	return keyReporter{}, tea.Println(fmt.Sprintf("key<%s>", KeyName(key)))
}

func (keyReporter) View() tea.View { return tea.NewView("ready") }

// TestKeyHelperProcess is the program the terminal test starts; it does nothing in a normal run.
func TestKeyHelperProcess(t *testing.T) {
	if os.Getenv(keyHelperEnv) == "" {
		return
	}
	if _, err := tea.NewProgram(keyReporter{}).Run(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// A terminal sends ctrl+] as the byte 0x1d and ctrl+\ as 0x1c, and the key rules name them as the program
// reads them from a real pseudo-terminal.
func TestControlBytesReachTheProgramAsCtrlPunctuation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports these keys as ] and \\ events, not as control bytes")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	term, err := ptytest.Start(ptytest.Command{Path: self, Args: []string{"-test.run=^TestKeyHelperProcess$"},
		Env: []string{keyHelperEnv + "=1", "TERM=xterm-256color"}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if err := term.WaitForText(15*time.Second, "ready"); err != nil {
		t.Fatalf("%v: %q", err, term.Output())
	}
	for _, c := range []struct {
		bytes []byte
		want  string
	}{{[]byte{0x1d}, "key<ctrl+]>"}, {[]byte{0x1c}, `key<ctrl+\>`}} {
		if _, err := term.Write(c.bytes); err != nil {
			t.Fatal(err)
		}
		if err := term.WaitForText(10*time.Second, c.want); err != nil {
			t.Fatalf("byte %#x: want %s: %v: %q", c.bytes[0], c.want, err, term.Output())
		}
		if !checkKeyRulesAccept(c.want[4 : len(c.want)-1]) {
			t.Errorf("the key rules refuse %s", c.want)
		}
	}
	if _, err := term.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	if code, err := term.Wait(15 * time.Second); err != nil || code != 0 {
		t.Fatalf("the program ended with %d, %v: %q", code, err, strings.TrimSpace(term.Output()))
	}
}

func checkKeyRulesAccept(key string) bool {
	_, err := checkKeyRules(Action{ID: "probe", Rebind: RebindReplace, DefaultKeys: []string{key}}, key)
	return err == nil
}

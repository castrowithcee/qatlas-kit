package tui_test

import (
	"os"
	"testing"

	"github.com/castrowithcee/qatlas-kit/archtest"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// Key names live in the rules of a binding: no other file of the TUI packages switches on a key or
// compares one with a key name.
func TestKeyNamesStayInTheKeyRules(t *testing.T) {
	archtest.Check(t, os.DirFS(".."), []archtest.Rule{{
		Name:  "key literals",
		Files: archtest.Files{Under: []string{"tui", "uiprefs"}, Except: []string{"tui/keyrules.go"}},
		Check: archtest.ForbidKeyLiterals(tui.RuleKeys(), "String"),
		Hint:  "ask the Keymap for the action instead of naming its key",
	}}, nil)
}

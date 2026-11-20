package highlight

import (
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// Actions of a key table for a text view, built from the exported parts of tui.
func viewActions() []tui.Action {
	act := func(id tui.ActionID, level tui.KeyLevel, key string) tui.Action {
		return tui.Action{ID: id, Level: level, DefaultKeys: []string{key}, Help: tui.TextID("example." + id)}
	}
	actions := []tui.Action{
		act("up", tui.LevelMove, "up"), act("down", tui.LevelMove, "down"),
		act("pageup", tui.LevelMove, "pgup"), act("pagedown", tui.LevelMove, "pgdown"),
		act("search", tui.LevelMove, "/"), act("enter", tui.LevelMove, "enter"), act("back", tui.LevelGlobal, "esc"),
	}
	return append(append(actions, tui.LogViewActions()...), tui.TextViewActions()...)
}

func newView(t *testing.T) *tui.TextView {
	t.Helper()
	keys, err := tui.NewKeymap(viewActions(), tui.CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	text, err := tui.NewTranslator(i18n.English, nil, keys)
	if err != nil {
		t.Fatal(err)
	}
	spec := tui.TextViewSpec{Keys: tui.TextViewKeys{Up: "up", Down: "down", PageUp: "pageup", PageDown: "pagedown",
		Search: "search", Enter: "enter", Back: "back", NextMatch: tui.ActionLogNextMatch,
		PrevMatch: tui.ActionLogPrevMatch, Wrap: tui.ActionLogWrap, Numbers: tui.ActionTextNumbers}}
	v, err := tui.NewTextView(spec, keys, text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func styles(t *testing.T) tui.Styles {
	t.Helper()
	set, err := tui.NewThemeSet([]tui.Theme{tui.DarkTheme("example dark")})
	if err != nil {
		t.Fatal(err)
	}
	st, known := set.Resolve("example dark", tui.ProfileTrueColor, tui.CharsetUnicode, nil)
	if !known {
		t.Fatal("the example theme is unknown")
	}
	return st
}

package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func f9Keymap(t *testing.T) *Keymap {
	t.Helper()
	var f9 Action
	for _, a := range StandardActions() {
		if a.ID == ActionHelpF9 {
			f9 = a
		}
	}
	keys, err := NewKeymap(append(sampleActions(), f9), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

var pressF9 = tea.KeyPressMsg{Code: tea.KeyF9}

func TestF9IsTheSecondWayOfHelpAndClosesTheHelp(t *testing.T) {
	keys := f9Keymap(t)
	if got := keys.SecondWays("help"); len(got) != 1 || got[0] != ActionHelpF9 || keys.SecondWays("") != nil {
		t.Fatalf("SecondWays(help) = %v", got)
	}
	if keys.Label("help") != "?" || keys.Label(ActionHelpF9) != "F9" {
		t.Errorf("labels %q and %q", keys.Label("help"), keys.Label(ActionHelpF9))
	}
	h, err := NewHelp(sampleHelpSpec(), keys, newTranslator(t, i18n.English, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	if act, handled := h.Update(pressF9); act != "help" || !handled {
		t.Errorf("f9 in the help = %q, %v; want help, true", act, handled)
	}
	if act, handled := h.Update(press("?")); act != "help" || !handled {
		t.Errorf("? in the help = %q, %v", act, handled)
	}
}

func TestFormAndDialogLeaveF9ToTheApplication(t *testing.T) {
	keys := f9Keymap(t)
	f, err := NewForm(sampleFormSpec(), keys, newTranslator(t, i18n.English, formCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	if act, handled := f.Update(pressF9); act != "" || handled {
		t.Errorf("form: f9 = %q, %v; want not handled", act, handled)
	}
	for name, spec := range map[string]DialogSpec{"input": plainInputSpec(), "question": confirmSpec()} {
		d, err := NewDialog(spec, keys, newTranslator(t, i18n.English, dialogCatalogs(), keys))
		if err != nil {
			t.Fatal(err)
		}
		if act, handled := d.Update(pressF9); act != "" || handled {
			t.Errorf("dialog %s: f9 = %q, %v; want not handled", name, act, handled)
		}
	}
}

func TestF9OpensTheHelpFromATextField(t *testing.T) {
	keys := f9Keymap(t)
	if !keys.Is(pressF9, ActionHelpF9, InputText) {
		t.Error("f9 does not reach its action while text is typed")
	}
	if keys.Is(press("?"), "help", InputText) {
		t.Error("? must type while text is typed")
	}
}

func TestF1IsRefusedForEveryAction(t *testing.T) {
	for _, a := range StandardActions() {
		if slices.Contains(a.DefaultKeys, "f1") {
			t.Errorf("standard action %s holds f1", a.ID)
		}
	}
	cases := map[string]func(*Action){
		"normal action": func(a *Action) {},
		"second way":    func(a *Action) { a.SecondWayOf = "down" },
		"overlay":       func(a *Action) { a.Level = LevelConfirm },
	}
	for name, mod := range cases {
		probe := Action{ID: "probe", Level: LevelCtrl, DefaultKeys: []string{"f1"}, Help: "x"}
		mod(&probe)
		_, err := NewKeymap(append(sampleActions(), probe), CharsetUnicode)
		if err == nil || !strings.Contains(err.Error(), neverKeys["f1"]) {
			t.Errorf("%s: NewKeymap = %v, want the f1 reason", name, err)
		}
	}
	keys := f9Keymap(t)
	if _, err := keys.CheckBinding("lock", "f1", nil); err == nil || !strings.Contains(err.Error(), neverKeys["f1"]) {
		t.Errorf("CheckBinding(lock, f1) = %v", err)
	}
}

func TestF5RemainsAllowedAsASecondWay(t *testing.T) {
	probe := Action{ID: "probe", Level: LevelCtrl, DefaultKeys: []string{"f5"}, Help: "x", SecondWayOf: "down"}
	if _, err := NewKeymap(append(sampleActions(), probe), CharsetUnicode); err != nil {
		t.Errorf("f5 as a second way: %v", err)
	}
}

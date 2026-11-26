package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func f1Keymap(t *testing.T) *Keymap {
	t.Helper()
	var f1 Action
	for _, a := range StandardActions() {
		if a.ID == ActionHelpF1 {
			f1 = a
		}
	}
	keys, err := NewKeymap(append(sampleActions(), f1), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

var pressF1 = tea.KeyPressMsg{Code: tea.KeyF1}

func TestF1IsTheSecondWayOfHelpAndClosesTheHelp(t *testing.T) {
	keys := f1Keymap(t)
	if got := keys.SecondWays("help"); len(got) != 1 || got[0] != ActionHelpF1 || keys.SecondWays("") != nil {
		t.Fatalf("SecondWays(help) = %v", got)
	}
	if keys.Label("help") != "?" || keys.Label(ActionHelpF1) != "F1" {
		t.Errorf("labels %q and %q", keys.Label("help"), keys.Label(ActionHelpF1))
	}
	h, err := NewHelp(sampleHelpSpec(), keys, newTranslator(t, i18n.English, helpCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	if act, handled := h.Update(pressF1); act != "help" || !handled {
		t.Errorf("f1 in the help = %q, %v; want help, true", act, handled)
	}
	if act, handled := h.Update(press("?")); act != "help" || !handled {
		t.Errorf("? in the help = %q, %v", act, handled)
	}
}

func TestFormAndDialogLeaveF1ToTheApplication(t *testing.T) {
	keys := f1Keymap(t)
	f, err := NewForm(sampleFormSpec(), keys, newTranslator(t, i18n.English, formCatalogs(), keys))
	if err != nil {
		t.Fatal(err)
	}
	if act, handled := f.Update(pressF1); act != "" || handled {
		t.Errorf("form: f1 = %q, %v; want not handled", act, handled)
	}
	for name, spec := range map[string]DialogSpec{"input": plainInputSpec(), "question": confirmSpec()} {
		d, err := NewDialog(spec, keys, newTranslator(t, i18n.English, dialogCatalogs(), keys))
		if err != nil {
			t.Fatal(err)
		}
		if act, handled := d.Update(pressF1); act != "" || handled {
			t.Errorf("dialog %s: f1 = %q, %v; want not handled", name, act, handled)
		}
	}
}

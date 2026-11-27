package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

func TestStandardActionsFormAValidTableWithTextsInBothLanguages(t *testing.T) {
	own := Action{ID: "deploy", Level: LevelLetter, DefaultKeys: []string{"D"}, Help: "app.deploy"}
	k, err := NewKeymap(append(StandardActions(), own), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	catalog := ModuleCatalog()
	for _, a := range StandardActions() {
		if len(k.Keys(a.ID)) == 0 || k.Label(a.ID) == "" {
			t.Errorf("%s has no key or label", a.ID)
		}
		for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
			if catalog[lang][a.Help] == "" {
				t.Errorf("%s: no text %q in %s", a.ID, a.Help, lang)
			}
		}
		if key := a.DefaultKeys[0]; reservedKeys[key] != a.ID {
			t.Errorf("%s: its key %q is not reserved for it", a.ID, key)
		}
	}
}

func TestStandardActionsAreFreshCopies(t *testing.T) {
	a := StandardActions()
	a[0].DefaultKeys[0] = "f19"
	a[1].ID = "changed"
	if b := StandardActions(); b[0].DefaultKeys[0] != "f2" || b[1].ID != ActionHelp {
		t.Errorf("a caller changed the standard actions: %v", b[:2])
	}
}

func TestReservedKeyIsRefusedAsTheDefaultOfAnotherAction(t *testing.T) {
	for key, owner := range reservedKeys {
		table := []Action{{ID: "other", Level: LevelLetter, DefaultKeys: []string{key}, Help: "x"}}
		_, err := NewKeymap(table, CharsetUnicode)
		if err == nil || !strings.Contains(err.Error(), `standard action "`+string(owner)+`"`) {
			t.Errorf("%q as the key of another action: %v, want a refusal naming %s", key, err, owner)
		}
	}
}

func TestReservedKeyIsRefusedAsARebindingWhateverTheTable(t *testing.T) {
	zoom := Action{ID: "zoom", Level: LevelCtrl, DefaultKeys: []string{"f3"}, Help: "x", Rebind: RebindReplace}
	k, err := NewKeymap([]Action{zoom}, CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	for key, owner := range reservedKeys {
		_, err := k.CheckBinding("zoom", key, nil)
		var be *BindingError
		if !errors.As(err, &be) {
			t.Errorf("CheckBinding(zoom, %q) = %v, want a *BindingError", key, err)
		} else if key != "ctrl+c" && key != "esc" && !strings.Contains(be.Reason, string(owner)) {
			t.Errorf("CheckBinding(zoom, %q): %q does not name %s", key, be.Reason, owner)
		}
		usable, errs := k.Resolve(map[ActionID]string{"zoom": key})
		if len(usable) != 0 || len(errs) != 1 {
			t.Errorf("Resolve(zoom: %q) = %v, %v, want one refusal", key, usable, errs)
		}
	}
	if err := k.Bind(map[ActionID]string{"zoom": "?"}); err == nil {
		t.Error("Bind takes a reserved key")
	}
}

func TestStandardActionKeepsItsOwnKeyAndMayMoveToAFreeOne(t *testing.T) {
	k, err := NewKeymap(StandardActions(), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.CheckBinding(ActionSave, "f2", nil); err != nil {
		t.Errorf("save on its own key: %v", err)
	}
	if _, err := k.CheckBinding(ActionSave, "f4", nil); err != nil {
		t.Errorf("save on a free key: %v", err)
	}
	if _, err := k.CheckBinding(ActionSave, "?", nil); err == nil {
		t.Error("save takes the key of help")
	}
}

func TestCtrlSIsOnlyASecondWayOfSave(t *testing.T) {
	second := Action{ID: "save.alt", Level: LevelCtrl, DefaultKeys: []string{"ctrl+s"}, Help: "x", SecondWayOf: ActionSave}
	if _, err := NewKeymap(append(StandardActions(), second), CharsetUnicode); err != nil {
		t.Errorf("ctrl+s as the second way of save: %v", err)
	}
	second.SecondWayOf = ""
	if _, err := NewKeymap(append(StandardActions(), second), CharsetUnicode); err == nil {
		t.Error("ctrl+s without a first way is accepted")
	}
}

func TestOverlayLevelsMayRepeatReservedKeys(t *testing.T) {
	var overlay []Action
	for _, level := range []KeyLevel{LevelConfirm, LevelHelp} {
		overlay = append(overlay, Action{ID: ActionID("o." + level), Level: level, DefaultKeys: []string{"d", "esc"}, Help: "x"})
	}
	k, err := NewKeymap(append(StandardActions(), overlay...), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := k.Match(typed('d'), InputNavigation, LevelConfirm); !ok || id != "o.confirm" {
		t.Errorf("d under the confirm overlay = %q, %v", id, ok)
	}
	if !slices.Contains(k.Keys(ActionDelete), "d") {
		t.Error("delete lost its key")
	}
}

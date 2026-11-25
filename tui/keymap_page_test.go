package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// pageActions are invented page actions beside the sample table: "g" and f3/f4 carry a different meaning on
// each page, "w" and f6 belong to page a only.
func pageActions() []Action {
	act := func(id ActionID, page string, r Rebinding, keys ...string) Action {
		return Action{ID: id, Level: LevelLetter, DefaultKeys: keys, Help: TextID("sample." + id), Rebind: r, Page: page}
	}
	return []Action{
		act("a.go", "a", RebindNone, "g"),
		act("b.go", "b", RebindNone, "g"),
		act("a.only", "a", RebindNone, "w"),
		act("a.run", "a", RebindReplace, "f3"),
		act("b.run", "b", RebindReplace, "f4"),
		act("a.six", "a", RebindReplace, "f6"),
	}
}

func pageKeymap(t *testing.T) *Keymap {
	t.Helper()
	k, err := NewKeymap(append(sampleActions(), pageActions()...), CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSameKeyOnTwoPagesIsValid(t *testing.T) {
	pageKeymap(t)
}

func TestNewKeymapRejectsPageCollisions(t *testing.T) {
	page := func(id ActionID, page string, keys ...string) Action {
		return Action{ID: id, Level: LevelLetter, DefaultKeys: keys, Help: "sample.new", Page: page}
	}
	for _, tt := range []struct {
		name  string
		extra Action
		want  string
	}{
		{"same page", page("a.dup", "a", "g"), `"a.go" and "a.dup" share the key "g"`},
		{"no page", page("plain", "", "g"), `"a.go" and "plain" share the key "g"`},
		{"no page held earlier", page("a.new", "a", "n"), `"new" and "a.new" share the key "n"`},
		{"reserved key", page("a.save", "a", "f2"), `key "f2" is reserved for the standard action "save"`},
		{"reserved key of another page", page("b.search", "b", "/"), `key "/" is reserved for the standard action "search"`},
	} {
		_, err := NewKeymap(append(append(sampleActions(), pageActions()...), tt.extra), CharsetUnicode)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestPagesSplitMatchIsAndLabel(t *testing.T) {
	k := pageKeymap(t)
	a, b := k.Page("a"), k.Page("b")
	for _, tt := range []struct {
		view *Keymap
		name string
		key  string
		want ActionID
		ok   bool
	}{
		{a, "a sees its g", "g", "a.go", true},
		{b, "b sees its g", "g", "b.go", true},
		{k, "register sees no page action", "g", "", false},
		{a, "a sees w", "w", "a.only", true},
		{b, "b does not see w", "w", "", false},
		{b, "a sees no-page keys", "n", "new", true},
		{a, "f3 on a", "f3", "a.run", true},
		{b, "f3 not on b", "f3", "", false},
	} {
		if got, ok := tt.view.Match(press(tt.key), InputNavigation); got != tt.want || ok != tt.ok {
			t.Errorf("%s: Match = %q, %v", tt.name, got, ok)
		}
	}
	if !a.Is(press("g"), "a.go", InputNavigation) || a.Is(press("g"), "b.go", InputNavigation) || b.Is(press("g"), "a.go", InputNavigation) {
		t.Error("Is does not follow the page")
	}
	if !a.Is(press("n"), "new", InputNavigation) || k.Is(press("g"), "a.go", InputNavigation) {
		t.Error("Is: a no-page action must match on every view and a page action not on the register")
	}
	if got := a.Label("a.run"); got != "F3" {
		t.Errorf("a.Label(a.run) = %q", got)
	}
	if got, ok := b.KeyLabel("a.run"); got != "" || ok {
		t.Errorf("b.KeyLabel(a.run) = %q, %v; another page's action is unknown there", got, ok)
	}
	if got := k.Label("a.run"); got != "" {
		t.Errorf("register Label(a.run) = %q", got)
	}
	if got, ok := b.Labels(InputNavigation).KeyLabel("b.run"); !ok || got != "F4" {
		t.Errorf("b.Labels(b.run) = %q, %v", got, ok)
	}
	if got := b.Label("new"); got != "n" {
		t.Errorf("b.Label(new) = %q", got)
	}
}

func TestKeysActionsAndBindSeeTheWholeTable(t *testing.T) {
	k := pageKeymap(t)
	b := k.Page("b")
	if got := b.Keys("a.go"); len(got) != 1 || got[0] != "g" {
		t.Errorf("Keys(a.go) through b = %v", got)
	}
	if len(b.Actions()) != len(k.Actions()) {
		t.Error("Actions differ between the register and a view")
	}
}

func TestBindingReachesRegisterAndViews(t *testing.T) {
	k := pageKeymap(t)
	a, b := k.Page("a"), k.Page("b")
	if err := a.Bind(map[ActionID]string{"a.run": "f7", "save": "f8"}); err == nil {
		// f8 is the default key of "remove", so the whole set is refused.
		t.Fatal("a colliding set was bound")
	}
	if err := a.Bind(map[ActionID]string{"a.run": "f7"}); err != nil {
		t.Fatal(err)
	}
	for name, view := range map[string]*Keymap{"register": k, "a": a, "b": b} {
		if got := view.Keys("a.run"); len(got) != 1 || got[0] != "f7" {
			t.Errorf("%s: Keys = %v", name, got)
		}
	}
	if id, ok := a.Match(press("f7"), InputNavigation); !ok || id != "a.run" {
		t.Errorf("a.Match(f7) = %q, %v", id, ok)
	}
	if _, ok := a.Match(press("f3"), InputNavigation); ok {
		t.Error("the old key still acts")
	}
	if a.Label("a.run") != "F7" {
		t.Errorf("Label = %q", a.Label("a.run"))
	}
	// A bind through the register shows in the view that was made before.
	if err := k.Bind(map[ActionID]string{"b.run": "f9"}); err != nil {
		t.Fatal(err)
	}
	if got := b.Label("b.run"); got != "F9" {
		t.Errorf("b.Label = %q", got)
	}
	if got := a.Label("a.run"); got != "F3" {
		t.Errorf("a Bind from the register must replace the whole set, a.run = %q", got)
	}
}

func TestRebindingIsCheckedPerPage(t *testing.T) {
	k := pageKeymap(t)
	check := func(id ActionID, key string, others map[ActionID]string) error {
		_, err := k.CheckBinding(id, key, others)
		return err
	}
	// f4 is the key of b.run: free for a.run on another page.
	if err := check("a.run", "f4", nil); err != nil {
		t.Errorf("the key of another page's action was refused: %v", err)
	}
	if err := check("a.run", "f6", nil); err == nil || !strings.Contains(err.Error(), "a.six") {
		t.Errorf("a key of the same page: %v", err)
	}
	// f8 belongs to "remove", an action without a page.
	if err := check("a.run", "f8", nil); err == nil || !strings.Contains(err.Error(), "remove") {
		t.Errorf("a key of an action without a page: %v", err)
	}
	// A rebound key of another page does not count, one of the same page does.
	if err := check("a.run", "f7", map[ActionID]string{"b.run": "f7"}); err != nil {
		t.Errorf("a rebound key of another page: %v", err)
	}
	if err := check("a.run", "f7", map[ActionID]string{"a.six": "f7"}); err == nil {
		t.Error("a rebound key of the same page was accepted")
	}
	// An action without a page is checked against every page.
	if err := check("save", "f4", nil); err == nil || !strings.Contains(err.Error(), "b.run") {
		t.Errorf("an action without a page against a page action: %v", err)
	}
	// A standard key (R, reload, is in no table here) is no key of a page action, whatever the page.
	if err := check("b.run", "R", nil); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("a reserved key: %v", err)
	}
	usable, errs := k.Resolve(map[ActionID]string{"a.run": "f7", "a.six": "f7", "b.run": "f7"})
	if len(usable) != 1 || usable["b.run"] != "f7" || len(errs) != 2 {
		t.Errorf("two entries of one page on one key: %v, %v", usable, errs)
	}
	usable, errs = k.Resolve(map[ActionID]string{"a.run": "f4", "b.run": "f7"})
	if len(errs) != 0 || usable["a.run"] != "f4" || usable["b.run"] != "f7" {
		t.Errorf("a swap across pages: %v, %v", usable, errs)
	}
	if err := k.Bind(map[ActionID]string{"a.run": "f4"}); err != nil {
		t.Errorf("Bind of the same key as another page's action: %v", err)
	}
}

func TestHintsShowOnlyTheActivePage(t *testing.T) {
	k := pageKeymap(t)
	hints := []Hint{{Action: "a.go", Label: "x.go"}, {Action: "b.go", Label: "x.go2"}, {Action: "new", Label: "x.new"},
		{Action: "fly", Label: "x.fly"}}
	text := func(g []HintGroup) string {
		var parts []string
		for _, h := range g {
			parts = append(parts, h.Text())
		}
		return strings.Join(parts, "|")
	}
	if got, want := text(k.Page("a").Hints(hints, InputNavigation, nil)), "g x.go|n x.new|{fly} x.fly"; got != want {
		t.Errorf("page a: %q, want %q", got, want)
	}
	if got, want := text(k.Page("b").Hints(hints, InputNavigation, nil)), "g x.go2|n x.new|{fly} x.fly"; got != want {
		t.Errorf("page b: %q, want %q", got, want)
	}
	if got, want := text(k.Hints(hints, InputNavigation, nil)), "n x.new|{fly} x.fly"; got != want {
		t.Errorf("register: %q, want %q", got, want)
	}
}

func TestHelpAndFooterShowOnlyTheActivePage(t *testing.T) {
	k := pageKeymap(t)
	st := looks(t)["noneascii"]
	spec := sampleHelpSpec()
	spec.Screen = []Hint{{Action: "a.go", Label: "h.new"}, {Action: "b.go", Label: "h.water"}, {Action: "new", Label: "sample.help"}}
	spec.Global = []ActionID{"save", "b.run"}
	for page, want := range map[string]struct{ in string }{"a": {"g new plant"}, "b": {"g water"}} {
		view := k.Page(page)
		// The text of a hint of another page is no error, the entry is left out.
		h, err := NewHelp(spec, view, newTranslator(t, i18n.English, helpCatalogs(), view))
		if err != nil {
			t.Fatalf("page %s: %v", page, err)
		}
		out, _ := h.View(st, 70, 40)
		if !strings.Contains(out, want.in) {
			t.Errorf("page %s: %q is missing:\n%s", page, want.in, out)
		}
		if strings.Contains(out, "{") || page == "a" && strings.Contains(out, "g water") || page == "b" && strings.Contains(out, "F3") {
			t.Errorf("page %s: the key list shows another page:\n%s", page, out)
		}
	}
	// A key that names no action at all stays an error.
	spec.Global = []ActionID{"fly"}
	if _, err := NewHelp(spec, k.Page("a"), newTranslator(t, i18n.English, helpCatalogs(), k)); err == nil {
		t.Error("an unknown global action was accepted")
	}
	// A navigation key of another page is no key of this one.
	spec = sampleHelpSpec()
	spec.Keys.Left = "a.go"
	if _, err := NewHelp(spec, k.Page("b"), newTranslator(t, i18n.English, helpCatalogs(), k)); err == nil {
		t.Error("a help key of another page was accepted")
	}
}

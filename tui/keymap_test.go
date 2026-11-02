package tui

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// sampleActions is an invented key table that follows the shared key layout.
func sampleActions() []Action {
	act := func(id ActionID, level KeyLevel, keys ...string) Action {
		return Action{ID: id, Level: level, DefaultKeys: keys, Help: TextID("sample." + id)}
	}
	rebind := func(a Action, r Rebinding) Action { a.Rebind = r; return a }
	second := func(a Action, of ActionID) Action { a.SecondWayOf = of; return a }
	return []Action{
		rebind(act("save", LevelGlobal, "f2"), RebindReplace),
		rebind(act("help", LevelGlobal, "?"), RebindReplace),
		rebind(act("command", LevelGlobal, ":"), RebindReplace),
		rebind(act("lock", LevelGlobal, "ctrl+l"), RebindReplace),
		act("back", LevelGlobal, "esc"),
		act("forcequit", LevelGlobal, "ctrl+c"),
		rebind(act("info", LevelLetter, "i"), RebindExtra),
		act("delete", LevelLetter, "d"),
		act("new", LevelLetter, "n"),
		act("quit", LevelLetter, "q"),
		act("upgrade", LevelLetter, "U"),
		act("reload", LevelCtrl, "ctrl+r"),
		act("remove", LevelCtrl, "f8"),
		second(act("suggest.prev", LevelCtrl, "ctrl+p"), "up"),
		act("up", LevelMove, "up"),
		act("down", LevelMove, "down"),
		act("left", LevelMove, "left"),
		act("right", LevelMove, "right"),
		act("first", LevelMove, "home"),
		act("last", LevelMove, "end"),
		act("pageup", LevelMove, "pgup"),
		act("pagedown", LevelMove, "pgdown"),
		act("next", LevelMove, "tab"),
		act("prev", LevelMove, "shift+tab"),
		act("enter", LevelMove, "enter"),
		act("toggle", LevelMove, "space"),
		act("search", LevelMove, "/"),
		act("area", LevelMove, "1", "2", "3", "4", "5", "6"),
		act("vim.up", LevelVim, "k"),
		act("vim.down", LevelVim, "j"),
		act("vim.left", LevelVim, "h"),
		act("vim.right", LevelVim, "l"),
		act("topic.next", LevelHelp, "tab"),
		act("topic.prev", LevelHelp, "shift+tab"),
		act("yes", LevelConfirm, "y"),
		act("no", LevelConfirm, "n"),
		act("discard", LevelConfirm, "d"),
	}
}

func sampleKeymap(t *testing.T, charset CharsetMode) *Keymap {
	t.Helper()
	k, err := NewKeymap(sampleActions(), charset)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// press is the key press a terminal reports for a key name of the sample table.
func press(name string) tea.KeyPressMsg {
	named := map[string]rune{"f2": tea.KeyF2, "f3": tea.KeyF3, "f4": tea.KeyF4, "f8": tea.KeyF8, "esc": tea.KeyEscape,
		"enter": tea.KeyEnter, "tab": tea.KeyTab, "up": tea.KeyUp, "space": tea.KeySpace}
	if code, ok := named[name]; ok {
		if code == tea.KeySpace {
			return tea.KeyPressMsg{Code: code, Text: " "}
		}
		return tea.KeyPressMsg{Code: code}
	}
	if rest, ok := strings.CutPrefix(name, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	if name == "shift+tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	return tea.KeyPressMsg{Code: []rune(name)[0], Text: name}
}

func TestSampleKeyNamesAreTheReportedOnes(t *testing.T) {
	for _, a := range sampleActions() {
		for _, key := range a.DefaultKeys {
			if strings.HasPrefix(key, "f") && len(key) > 2 || key == "home" || key == "end" || key == "pgup" ||
				key == "pgdown" || key == "down" || key == "left" || key == "right" {
				continue
			}
			if got := KeyName(press(key)); got != key {
				t.Errorf("press(%q) is reported as %q", key, got)
			}
		}
	}
}

func TestLabelsSpellTheKeysInForce(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	for id, want := range map[ActionID]string{"save": "F2", "lock": "ctrl+l", "area": "1-6", "up": "↑", "info": "i",
		"remove": "F8", "upgrade": "U", "toggle": "space"} {
		if got := k.Label(id); got != want {
			t.Errorf("Label(%s) = %q, want %q", id, got, want)
		}
	}
	if got := sampleKeymap(t, CharsetASCII).Label("up"); got != "up" {
		t.Errorf("ASCII label of up = %q, want the word", got)
	}
	if label, ok := k.KeyLabel("gone"); ok || label != "" || k.Label("gone") != "" {
		t.Errorf("an unknown action has the label %q, %v", label, ok)
	}
}

func TestNewKeymapRejectsBrokenTables(t *testing.T) {
	cases := []struct {
		name   string
		change func([]Action) []Action
		want   string
	}{
		{"empty", func([]Action) []Action { return nil }, "no action"},
		{"upper case ID", func(a []Action) []Action { a[0].ID = "Save"; return a }, "an ID starts with a-z"},
		{"digit ID", func(a []Action) []Action { a[0].ID = "1save"; return a }, "an ID starts with a-z"},
		{"empty ID", func(a []Action) []Action { a[0].ID = ""; return a }, "an ID starts with a-z"},
		{"brace ID", func(a []Action) []Action { a[0].ID = "sa{ve}"; return a }, "an ID starts with a-z"},
		{"ID twice", func(a []Action) []Action { a[1].ID = "save"; return a }, "listed twice"},
		{"level", func(a []Action) []Action { a[0].Level = "screen"; return a }, "unknown level"},
		{"no help", func(a []Action) []Action { a[0].Help = ""; return a }, "no help text"},
		{"no key", func(a []Action) []Action { a[7].DefaultKeys = nil; return a }, "no default key"},
		{"capitalised name", func(a []Action) []Action { a[11].DefaultKeys = []string{"Ctrl+R"}; return a }, "not a key name"},
		{"blank", func(a []Action) []Action { a[11].DefaultKeys = []string{" "}; return a }, "not a key name"},
		{"inner space", func(a []Action) []Action { a[11].DefaultKeys = []string{"ctrl +r"}; return a }, "not a key name"},
		{"key twice", func(a []Action) []Action { a[7].DefaultKeys = []string{"d", "d"}; return a }, "listed twice"},
		{"letter on vim key", func(a []Action) []Action { a[8].DefaultKeys = []string{"k"}; return a }, `share the key "k"`},
		{"global on move key", func(a []Action) []Action { a[4].DefaultKeys = []string{"enter"}; return a }, `share the key "enter"`},
		{"two answers", func(a []Action) []Action { a[len(a)-1].DefaultKeys = []string{"y"}; return a }, `share the key "y"`},
		{"multiplexer key", func(a []Action) []Action { a[11].DefaultKeys = []string{"ctrl+t"}; return a }, "zellij"},
		{"text line key", func(a []Action) []Action { a[11].DefaultKeys = []string{"ctrl+w"}; return a }, "edits text"},
		{"alt digit", func(a []Action) []Action { a[11].DefaultKeys = []string{"alt+1"}; return a }, "macOS"},
		{"terminal F-key", func(a []Action) []Action { a[12].DefaultKeys = []string{"f5"}; return a }, "many terminals"},
		{"second way of nothing", func(a []Action) []Action { a[13].SecondWayOf = "gone"; return a }, "no other action"},
		{"second way of itself", func(a []Action) []Action { a[13].SecondWayOf = a[13].ID; return a }, "no other action"},
		{"second way of a second way", func(a []Action) []Action {
			a = append(a, Action{ID: "suggest.again", Level: LevelCtrl, DefaultKeys: []string{"ctrl+n"}, Help: "x", SecondWayOf: "suggest.prev"})
			return a
		}, "a second way itself"},
		{"replace with two keys", func(a []Action) []Action { a[0].DefaultKeys = []string{"f2", "f3"}; return a }, "exactly one default key"},
		{"replace default against the rules", func(a []Action) []Action { a[3].DefaultKeys = []string{"f21"}; return a }, "F1 to F20"},
		{"replace default letter", func(a []Action) []Action { a[1].DefaultKeys = []string{"z"}; return a }, "letters"},
		{"rebinding", func(a []Action) []Action { a[0].Rebind = 9; return a }, "unknown rebinding"},
	}
	for _, c := range cases {
		_, err := NewKeymap(c.change(sampleActions()), CharsetUnicode)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: NewKeymap = %v, want an error with %q", c.name, err, c.want)
		}
	}
}

// Every key terminals, multiplexers or text lines take is refused as a default key, and allowed for the
// second way of another action.
func TestTakenKeysAreOnlySecondWays(t *testing.T) {
	for key, reason := range takenKeys {
		table := sampleActions()
		table = append(table, Action{ID: "probe", Level: LevelCtrl, DefaultKeys: []string{key}, Help: "x"})
		if _, err := NewKeymap(table, CharsetUnicode); err == nil || !strings.Contains(err.Error(), reason) {
			t.Errorf("default key %s: %v, want the reason %q", key, err, reason)
		}
		table[len(table)-1].SecondWayOf = "down"
		if _, err := NewKeymap(table, CharsetUnicode); err != nil && !strings.Contains(err.Error(), "share") {
			t.Errorf("second way on %s: %v", key, err)
		}
	}
}

func TestOverlaysMayRepeatAMainKey(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	for _, pair := range [][2]ActionID{{"delete", "discard"}, {"new", "no"}, {"next", "topic.next"}} {
		if k.Label(pair[0]) != k.Label(pair[1]) {
			t.Errorf("%s and %s should share a key on different levels", pair[0], pair[1])
		}
	}
}

func TestCheckBindingRejectsWithReason(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	cases := []struct {
		action      ActionID
		key, reason string
	}{
		{"info", "f2", "already the key of save"},
		{"info", "i", "already the key of info"},
		{"save", "?", "already the key of help"},
		{"save", "x", "acts on text lines"},
		{"lock", "!", "acts on text lines"},
		{"help", "ctrl+c", "quits"},
		{"help", "esc", "goes back"},
		{"help", "tab", "move between fields"},
		{"help", "shift+tab", "move between fields"},
		{"help", "space", "space and control"},
		{"info", "space", "space and control"},
		{"info", "3", "already the key of area"},
		{"help", "9", "digits"},
		{"info", "f5", "terminals take"},
		{"lock", "f1", "terminals take"},
		{"info", "f21", "F1 to F20"},
		{"info", "f0", "F1 to F20"},
		{"info", "ctrl+a", "free are ctrl+l and ctrl+x"},
		{"lock", "ctrl+r", "already the key of reload"},
		{"save", "ctrl+g", "free are ctrl+l and ctrl+x"},
		{"save", "ctrl+o", "free are ctrl+l and ctrl+x"},
		{"save", "f8", "already the key of remove"},
		{"info", "j", "already the key of vim.down"},
		{"help", "z", "letters"},
		{"help", "ä", "letters"},
		{"help", "/", "already the key of search"},
		{"info", "enter", "already the key of enter"},
		{"info", "alt+x", "not a single key"},
		{"save", "", "needs a key"},
		{"back", "f3", "cannot be rebound"},
		{"gone", "f3", "no action"},
	}
	for _, c := range cases {
		_, err := k.CheckBinding(c.action, c.key, nil)
		var be *BindingError
		if err == nil || !strings.Contains(err.Error(), c.reason) || !errors.As(err, &be) || be.Action != c.action {
			t.Errorf("CheckBinding(%s, %q) = %v, want a *BindingError with %q", c.action, c.key, err, c.reason)
		}
	}
	// "/" is refused for its own reason also in a table without a search action.
	table := slices.DeleteFunc(sampleActions(), func(a Action) bool { return a.ID == "search" })
	other, err := NewKeymap(table, CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.CheckBinding("help", "/", nil); err == nil || !strings.Contains(err.Error(), "/ searches") {
		t.Errorf("/ without a search action: %v", err)
	}
}

func TestCheckBindingAccepts(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	for _, c := range [][2]string{{"info", "F3"}, {"info", ""}, {"save", "f4"}, {"help", "ctrl+x"}, {"command", "Ctrl+X"},
		{"save", "f13"}, {"save", "f20"}, {"help", "!"}, {"save", "f2"}, {"lock", "ctrl+x"}} {
		if _, err := k.CheckBinding(ActionID(c[0]), c[1], nil); err != nil {
			t.Errorf("CheckBinding(%s, %q) = %v, want it accepted", c[0], c[1], err)
		}
	}
	if got, _ := k.CheckBinding("info", " F3 ", nil); got != "f3" {
		t.Errorf("normalised = %q, want f3", got)
	}
	// The other rows of a form decide: the key of command is free once command holds another one.
	if _, err := k.CheckBinding("help", ":", map[ActionID]string{"command": "ctrl+x"}); err != nil {
		t.Errorf(": after command moved away: %v", err)
	}
	if _, err := k.CheckBinding("help", ":", nil); err == nil {
		t.Error(": is accepted while command holds it")
	}
	if _, err := k.CheckBinding("save", "ctrl+x", map[ActionID]string{"help": "ctrl+x"}); err == nil {
		t.Error("ctrl+x is accepted while help holds it")
	}
}

// Every key of an action that cannot be rebound is refused for every rebindable action, whatever the other
// rules say.
func TestEveryFixedKeyIsRefused(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	for _, fixed := range k.Actions() {
		if fixed.Rebind != RebindNone {
			continue
		}
		for _, key := range fixed.DefaultKeys {
			for _, a := range k.Actions() {
				if a.Rebind == RebindNone {
					continue
				}
				if _, err := k.CheckBinding(a.ID, key, nil); err == nil {
					t.Errorf("CheckBinding(%s, %q) accepts the key of %s", a.ID, key, fixed.ID)
				}
			}
		}
	}
	// The table decides, not a copy of it: a free key the table newly gives away is refused.
	table := append(sampleActions(), Action{ID: "probe", Level: LevelCtrl, DefaultKeys: []string{"ctrl+x"}, Help: "x"})
	probe, err := NewKeymap(table, CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.CheckBinding("help", "ctrl+x", nil); err == nil || !strings.Contains(err.Error(), "probe") {
		t.Errorf("CheckBinding does not consult the table: %v", err)
	}
}

func TestBindChangesMatchLabelAndTextTogether(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	text, err := NewTranslator(i18n.English, i18n.Catalogs[TextID]{i18n.English: {"sample.footer": "{save} save · {help} help"}}, k)
	if err != nil {
		t.Fatal(err)
	}
	if got := text.Lookup("sample.footer"); got != "F2 save · ? help" {
		t.Fatalf("footer = %q", got)
	}
	if err := k.Bind(map[ActionID]string{"save": "f4", "help": "ctrl+x", "info": "f3"}); err != nil {
		t.Fatal(err)
	}
	if got := text.Lookup("sample.footer"); got != "F4 save · ctrl+x help" {
		t.Errorf("footer after the binding = %q", got)
	}
	if id, ok := k.Match(press("f4"), InputNavigation); !ok || id != "save" {
		t.Errorf("f4 = %q %v, want save", id, ok)
	}
	if _, ok := k.Match(press("f2"), InputNavigation); ok {
		t.Error("the old save key still matches")
	}
	if !k.Is(press("ctrl+x"), "help", InputNavigation) || k.Is(press("?"), "help", InputNavigation) {
		t.Error("help does not follow its binding")
	}
	if k.Label("info") != "i/F3" {
		t.Errorf("info = %q, want the letter and the extra key", k.Label("info"))
	}
	// A new set replaces the old one as a whole: what it leaves out is back on its default.
	if err := k.Bind(map[ActionID]string{"lock": "ctrl+x"}); err != nil {
		t.Fatal(err)
	}
	if k.Label("save") != "F2" || k.Label("help") != "?" || k.Label("info") != "i" || k.Label("lock") != "ctrl+x" {
		t.Errorf("labels = %s %s %s %s", k.Label("save"), k.Label("help"), k.Label("info"), k.Label("lock"))
	}
}

func TestBindIsAllOrNothing(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	err := k.Bind(map[ActionID]string{"save": "f4", "help": "f4", "back": "f6", "lock": "x", "gone": "f7"})
	if err == nil {
		t.Fatal("Bind accepted a broken set")
	}
	for _, want := range []string{"already the key of", "cannot be rebound", "acts on text lines", "no action"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if k.Label("save") != "F2" || k.Label("lock") != "ctrl+l" {
		t.Errorf("a refused set changed the keys: save %s lock %s", k.Label("save"), k.Label("lock"))
	}
}

// Two key tables of one process share nothing.
func TestKeymapsAreIndependent(t *testing.T) {
	a, b := sampleKeymap(t, CharsetUnicode), sampleKeymap(t, CharsetUnicode)
	if err := a.Bind(map[ActionID]string{"save": "f4"}); err != nil {
		t.Fatal(err)
	}
	if b.Label("save") != "F2" || a.Label("save") != "F4" {
		t.Errorf("a binding leaked: a %s, b %s", a.Label("save"), b.Label("save"))
	}
	table := sampleActions()
	k, err := NewKeymap(table, CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	table[0].DefaultKeys[0] = "f9"
	got := k.Actions()
	got[1].DefaultKeys[0] = "!"
	if k.Label("save") != "F2" || k.Label("help") != "?" {
		t.Errorf("the table is shared with the caller: save %s help %s", k.Label("save"), k.Label("help"))
	}
}

func TestResolveKeepsWhatIsValid(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	cases := []struct {
		name    string
		in      map[ActionID]string
		want    map[ActionID]string
		dropped []ActionID
	}{
		{"swap", map[ActionID]string{"save": "ctrl+l", "lock": "ctrl+x"}, map[ActionID]string{"save": "ctrl+l", "lock": "ctrl+x"}, nil},
		{"collision drops both", map[ActionID]string{"save": "f3", "help": "f3", "info": "f5", "lock": "x"}, nil,
			[]ActionID{"save", "help", "info", "lock"}},
		{"cascade", map[ActionID]string{"save": "f3", "info": "f3", "lock": "f2"}, nil, []ActionID{"save", "info", "lock"}},
		{"defaults are no deviation", map[ActionID]string{"save": "F2", "info": ""}, nil, nil},
		{"fixed and unknown", map[ActionID]string{"back": "f3", "gone": "f4", "help": "f6"}, map[ActionID]string{"help": "f6"},
			[]ActionID{"gone", "back"}},
	}
	for _, c := range cases {
		got, errs := k.Resolve(c.in)
		if !maps.Equal(got, c.want) {
			t.Errorf("%s: usable = %v, want %v", c.name, got, c.want)
		}
		var dropped []ActionID
		for _, err := range errs {
			dropped = append(dropped, err.Action)
		}
		slices.Sort(dropped)
		want := slices.Clone(c.dropped)
		slices.Sort(want)
		if !slices.Equal(dropped, want) {
			t.Errorf("%s: dropped %v (%v), want %v", c.name, dropped, errs, want)
		}
		if err := sampleKeymap(t, CharsetUnicode).Bind(got); err != nil {
			t.Errorf("%s: the usable part does not bind: %v", c.name, err)
		}
	}
}

func TestMatchGivesOverlaysPrecedence(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	cases := []struct {
		key      string
		overlays []KeyLevel
		want     ActionID
	}{
		{"d", nil, "delete"},
		{"d", []KeyLevel{LevelConfirm}, "discard"},
		{"n", []KeyLevel{LevelConfirm}, "no"},
		{"esc", []KeyLevel{LevelConfirm}, "back"},
		{"q", []KeyLevel{LevelConfirm}, "quit"},
		{"tab", nil, "next"},
		{"tab", []KeyLevel{LevelHelp}, "topic.next"},
		{"tab", []KeyLevel{LevelConfirm, LevelHelp}, "topic.next"},
		{"d", []KeyLevel{LevelHelp, LevelConfirm}, "discard"},
		{"tab", []KeyLevel{LevelMove, LevelGlobal}, "next"},
		{"k", nil, "vim.up"},
		{"3", nil, "area"},
	}
	for _, c := range cases {
		if id, ok := k.Match(press(c.key), InputNavigation, c.overlays...); !ok || id != c.want {
			t.Errorf("Match(%s, %v) = %q %v, want %s", c.key, c.overlays, id, ok, c.want)
		}
	}
	if id, ok := k.Match(press("y"), InputNavigation); ok {
		t.Errorf("an answer matched outside its question: %s", id)
	}
	if id, ok := k.Match(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}, InputNavigation); ok {
		t.Errorf("an unbound key matched %s", id)
	}
}

// Where text is typed every key that types a character is text; while a key is captured only ctrl+c and
// esc act; in an embedded terminal no key acts.
func TestMatchFollowsTheInputFocus(t *testing.T) {
	k := sampleKeymap(t, CharsetUnicode)
	if err := k.Bind(map[ActionID]string{"info": "f3"}); err != nil {
		t.Fatal(err)
	}
	var zero InputFocus
	if zero != InputText {
		t.Fatal("the zero focus is not text input")
	}
	cases := []struct {
		focus InputFocus
		acts  []string
		text  []string
	}{
		{InputText, []string{"f2", "ctrl+l", "esc", "ctrl+c", "enter", "tab", "f3", "up", "ctrl+r", "ctrl+p"},
			[]string{"q", "i", "?", ":", "k", "j", "3", "/", "space", "U", "d"}},
		{InputNavigation, []string{"f2", "q", "i", "?", "k", "3", "/", "space", "U", "f3"}, nil},
		{InputKeyCapture, []string{"esc", "ctrl+c"}, []string{"f2", "ctrl+l", "?", "q", "enter", "tab", "f3"}},
		{InputNativeTerminal, nil, []string{"ctrl+c", "esc", "f2", "q", "enter", "f3"}},
	}
	for _, c := range cases {
		for _, key := range c.acts {
			if _, ok := k.Match(press(key), c.focus); !ok {
				t.Errorf("focus %d: %s does not act", c.focus, key)
			}
		}
		for _, key := range c.text {
			if id, ok := k.Match(press(key), c.focus); ok {
				t.Errorf("focus %d: %s triggers %s", c.focus, key, id)
			}
			for _, a := range k.Actions() {
				if k.Is(press(key), a.ID, c.focus) {
					t.Errorf("focus %d: Is(%s, %s) is true", c.focus, key, a.ID)
				}
			}
		}
	}
	if k.Is(press("i"), "info", InputText) || !k.Is(press("f3"), "info", InputText) || !k.Is(press("i"), "info", InputNavigation) {
		t.Error("the extra key does not work alone on text lines")
	}
	if got, _ := k.Labels(InputText).KeyLabel("info"); got != "F3" {
		t.Errorf("text line label of info = %q, want only the key that works there", got)
	}
	if got, _ := k.Labels(InputKeyCapture).KeyLabel("back"); got != "esc" {
		t.Errorf("capture label of back = %q", got)
	}
	if got, ok := k.Labels(InputNativeTerminal).KeyLabel("forcequit"); !ok || got != "" {
		t.Errorf("terminal label of forcequit = %q %v", got, ok)
	}
	if k.Is(press("f2"), "gone", InputNavigation) {
		t.Error("an unknown action matched")
	}
}

func TestKeyNameGivesShiftedFunctionKeysTheirStoredName(t *testing.T) {
	cases := map[string]tea.KeyPressMsg{
		"f13":       {Code: tea.KeyF1, Mod: tea.ModShift},
		"f20":       {Code: tea.KeyF8, Mod: tea.ModShift},
		"alt+f14":   {Code: tea.KeyF2, Mod: tea.ModShift | tea.ModAlt},
		"shift+f9":  {Code: tea.KeyF9, Mod: tea.ModShift},
		"ctrl+f2":   {Code: tea.KeyF2, Mod: tea.ModCtrl},
		"f2":        {Code: tea.KeyF2},
		"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
		"A":         {Code: 'a', Mod: tea.ModShift, Text: "A"},
	}
	for want, msg := range cases {
		if got := KeyName(msg); got != want {
			t.Errorf("KeyName(%v) = %q, want %q", msg, got, want)
		}
	}
	k := sampleKeymap(t, CharsetUnicode)
	if err := k.Bind(map[ActionID]string{"save": "f13"}); err != nil {
		t.Fatal(err)
	}
	if !k.Is(tea.KeyPressMsg{Code: tea.KeyF1, Mod: tea.ModShift}, "save", InputText) {
		t.Error("shift+F1 does not reach the binding f13")
	}
}

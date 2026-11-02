package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// KeyLevel is the set of actions that can be pressed in the same place.
type KeyLevel string

// The levels of a key table. The main levels share one key space: a key means one thing on every screen.
// LevelConfirm and LevelHelp are overlays whose keys may repeat a key of the main levels, because a
// question or the help overlay shows what they mean.
const (
	LevelGlobal  KeyLevel = "global"
	LevelLetter  KeyLevel = "letter"
	LevelCtrl    KeyLevel = "ctrl"
	LevelMove    KeyLevel = "move"
	LevelVim     KeyLevel = "vim"
	LevelConfirm KeyLevel = "confirm"
	LevelHelp    KeyLevel = "help"
)

func (l KeyLevel) known() bool {
	switch l {
	case LevelGlobal, LevelLetter, LevelCtrl, LevelMove, LevelVim, LevelConfirm, LevelHelp:
		return true
	}
	return false
}

func (l KeyLevel) overlay() bool { return l == LevelConfirm || l == LevelHelp }

// Rebinding says how a user may change the keys of an action. The zero value keeps them fixed.
type Rebinding uint8

const (
	// RebindNone keeps the default keys.
	RebindNone Rebinding = iota
	// RebindReplace lets one key take the place of the single default key.
	RebindReplace
	// RebindExtra keeps the default keys and lets one more key join them, a key that also works where
	// text is typed.
	RebindExtra
)

// InputFocus says what the screen does with a key press right now. The zero value is InputText, where
// every key that types text is text, so a screen that forgets to say otherwise loses no typed character.
type InputFocus uint8

const (
	// InputText reads typed text: a key that types a character never triggers an action.
	InputText InputFocus = iota
	// InputNavigation reads no text: every key of the table acts.
	InputNavigation
	// InputKeyCapture takes the next key as a new binding: only ctrl+c and esc keep their action.
	InputKeyCapture
	// InputNativeTerminal passes every key on to an embedded terminal: no key acts.
	InputNativeTerminal
)

// Action is one entry of a key table. Keys are named as KeyName names a key press: "f2", "ctrl+l", "?",
// "space", "shift+tab".
type Action struct {
	// ID is stable and untranslated. Texts name its keys by the placeholder "{ID}".
	ID          ActionID
	Level       KeyLevel
	DefaultKeys []string
	// Help is the catalog text that says what the action does.
	Help   TextID
	Rebind Rebinding
	// SecondWayOf names the action that does the same with keys no terminal or multiplexer takes. Only
	// such a second way may hold a key that terminals, multiplexers or text lines take for themselves.
	SecondWayOf ActionID
}

// Keymap is the key table of one TUI: every action with its level, keys and help, and the keys bound
// now. Screens ask for an action, never for a key name. Bind changes the keys in force; it must not run
// concurrently with another method of the same Keymap.
type Keymap struct {
	actions []Action
	index   map[ActionID]int
	bound   [][]string
	charset CharsetMode
}

// NewKeymap checks the table and binds the default keys. charset selects how Label spells the arrow
// keys. It rejects an action without a valid placeholder ID, level, key or help text, an ID or a key
// listed twice, a key that two actions of the main levels or of one overlay level share, a key that
// terminals, multiplexers or text lines take for themselves unless the action is the second way of
// another, and a rebindable action whose default key breaks the rules of a binding.
func NewKeymap(actions []Action, charset CharsetMode) (*Keymap, error) {
	if len(actions) == 0 {
		return nil, errors.New("the key table holds no action")
	}
	k := &Keymap{index: map[ActionID]int{}, charset: charset}
	var errs []error
	for _, a := range actions {
		a.DefaultKeys = slices.Clone(a.DefaultKeys)
		if !isPlaceholderName(string(a.ID)) {
			errs = append(errs, fmt.Errorf("action %q: an ID starts with a-z and holds only a-z, 0-9, '.', '_' and '-'", a.ID))
			continue
		}
		if _, dup := k.index[a.ID]; dup {
			errs = append(errs, fmt.Errorf("action %q is listed twice", a.ID))
			continue
		}
		k.index[a.ID] = len(k.actions)
		k.actions = append(k.actions, a)
	}
	for _, a := range k.actions {
		errs = append(errs, k.actionErrors(a)...)
	}
	errs = append(errs, k.collisionErrors()...)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	k.bindDefaults()
	return k, nil
}

func (k *Keymap) actionErrors(a Action) []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("action %q: "+format, append([]any{a.ID}, args...)...))
	}
	if !a.Level.known() {
		fail("unknown level %q", a.Level)
	}
	if a.Help == "" {
		fail("no help text")
	}
	if len(a.DefaultKeys) == 0 {
		fail("no default key")
	}
	for i, key := range a.DefaultKeys {
		switch {
		case key == "" || key != normalizeKey(key) || strings.ContainsAny(key, " \t\r\n"):
			fail("key %q is not a key name; names are lower case except single characters, and a space is \"space\"", key)
		case slices.Contains(a.DefaultKeys[:i], key):
			fail("key %q is listed twice", key)
		case takenKeys[key] != "" && a.SecondWayOf == "":
			fail("key %q %s; only a second way of another action may hold it", key, takenKeys[key])
		}
	}
	if a.SecondWayOf != "" {
		first, ok := k.index[a.SecondWayOf]
		switch {
		case !ok || a.SecondWayOf == a.ID:
			fail("is the second way of %q, which is no other action of the table", a.SecondWayOf)
		case k.actions[first].SecondWayOf != "":
			fail("is the second way of %q, which is a second way itself", a.SecondWayOf)
		}
	}
	switch a.Rebind {
	case RebindNone, RebindExtra:
	case RebindReplace:
		if len(a.DefaultKeys) != 1 {
			fail("a replaceable action has exactly one default key")
		} else if _, err := checkKeyRules(a, a.DefaultKeys[0]); err != nil {
			fail("default %v", err)
		}
	default:
		fail("unknown rebinding %d", a.Rebind)
	}
	return errs
}

// collisionErrors reports every key two actions share where both could be pressed at once.
func (k *Keymap) collisionErrors() []error {
	var errs []error
	main := map[string]ActionID{}
	overlay := map[KeyLevel]map[string]ActionID{}
	for _, a := range k.actions {
		seen := main
		if a.Level.overlay() {
			if overlay[a.Level] == nil {
				overlay[a.Level] = map[string]ActionID{}
			}
			seen = overlay[a.Level]
		}
		for _, key := range a.DefaultKeys {
			if other, ok := seen[key]; ok && other != a.ID {
				errs = append(errs, fmt.Errorf("actions %q and %q share the key %q", other, a.ID, key))
				continue
			}
			seen[key] = a.ID
		}
	}
	return errs
}

func (k *Keymap) bindDefaults() {
	k.bound = make([][]string, len(k.actions))
	for i, a := range k.actions {
		k.bound[i] = slices.Clone(a.DefaultKeys)
	}
}

// Actions returns a copy of the table in its order.
func (k *Keymap) Actions() []Action {
	out := slices.Clone(k.actions)
	for i := range out {
		out[i].DefaultKeys = slices.Clone(out[i].DefaultKeys)
	}
	return out
}

// Bind makes overrides the keys in force: each entry is the key of a RebindReplace action or the extra
// key of a RebindExtra action, and every action left out keeps its default keys. Bind checks the whole
// set first and changes nothing if one entry breaks a rule or two collide.
func (k *Keymap) Bind(overrides map[ActionID]string) error {
	usable, errs := k.Resolve(overrides)
	if len(errs) > 0 {
		joined := make([]error, len(errs))
		for i, err := range errs {
			joined[i] = err
		}
		return errors.Join(joined...)
	}
	k.bindDefaults()
	for id, key := range usable {
		i := k.index[id]
		k.bound[i] = effectiveKeys(k.actions[i], key, true)
	}
	return nil
}

// KeyName names a key press as a key table names it. The shifted F1 to F8 that xterm-like terminals send
// are F13 to F20, the names a binding stores for them.
func KeyName(msg tea.KeyPressMsg) string {
	if msg.Mod&^tea.ModAlt == tea.ModShift && msg.Code >= tea.KeyF1 && msg.Code <= tea.KeyF8 {
		msg.Code += tea.KeyF13 - tea.KeyF1
		msg.Mod &^= tea.ModShift
	}
	return msg.String()
}

// Match returns the action a key press triggers under focus. The overlay levels named in overlays, in
// their order, come before the main levels, so a question's answers and the help overlay's keys win
// there; other levels in overlays are ignored.
func (k *Keymap) Match(msg tea.KeyPressMsg, focus InputFocus, overlays ...KeyLevel) (ActionID, bool) {
	name := KeyName(msg)
	if !k.reachable(msg, name, focus) {
		return "", false
	}
	for _, level := range overlays {
		if !level.overlay() {
			continue
		}
		if id, ok := k.find(name, focus, func(l KeyLevel) bool { return l == level }); ok {
			return id, true
		}
	}
	return k.find(name, focus, func(l KeyLevel) bool { return !l.overlay() })
}

// Is reports whether the key press triggers the action id under focus, whatever its level. An unknown
// action never matches.
func (k *Keymap) Is(msg tea.KeyPressMsg, id ActionID, focus InputFocus) bool {
	i, ok := k.index[id]
	name := KeyName(msg)
	return ok && k.reachable(msg, name, focus) && slices.Contains(k.keysUnder(i, focus), name)
}

// reachable reports whether a key press can trigger any action under focus.
func (k *Keymap) reachable(msg tea.KeyPressMsg, name string, focus InputFocus) bool {
	switch focus {
	case InputNavigation:
		return true
	case InputText:
		return msg.Text == "" && !isTextKey(name)
	case InputKeyCapture:
		return captureExits[name]
	}
	return false
}

func (k *Keymap) find(name string, focus InputFocus, level func(KeyLevel) bool) (ActionID, bool) {
	for i, a := range k.actions {
		if level(a.Level) && slices.Contains(k.keysUnder(i, focus), name) {
			return a.ID, true
		}
	}
	return "", false
}

// keysUnder are the bound keys of the action at i that act under focus.
func (k *Keymap) keysUnder(i int, focus InputFocus) []string {
	switch focus {
	case InputNavigation:
		return k.bound[i]
	case InputText:
		return slices.DeleteFunc(slices.Clone(k.bound[i]), isTextKey)
	case InputKeyCapture:
		if k.actions[i].Level != LevelGlobal {
			return nil
		}
		return slices.DeleteFunc(slices.Clone(k.bound[i]), func(key string) bool { return !captureExits[key] })
	}
	return nil
}

// Label is how a footer names the keys of an action in force where nothing is typed, joined by "/":
// function keys capitalised, arrows as glyphs (as words in ASCII), a run of three or more digits as a
// range, an extra key last. An unknown action has the empty label.
func (k *Keymap) Label(id ActionID) string {
	label, _ := k.labelUnder(id, InputNavigation)
	return label
}

// KeyLabel is Label for a Translator; it reports false for an unknown action.
func (k *Keymap) KeyLabel(id ActionID) (string, bool) { return k.labelUnder(id, InputNavigation) }

// Labels returns the labels of the keys that act under focus, for the footer of a screen in that focus:
// on a text line an action shows only the keys that work there. The result follows later bindings.
func (k *Keymap) Labels(focus InputFocus) KeyLabels { return focusLabels{k, focus} }

type focusLabels struct {
	k     *Keymap
	focus InputFocus
}

func (f focusLabels) KeyLabel(id ActionID) (string, bool) { return f.k.labelUnder(id, f.focus) }

func (k *Keymap) labelUnder(id ActionID, focus InputFocus) (string, bool) {
	i, ok := k.index[id]
	if !ok {
		return "", false
	}
	keys := k.keysUnder(i, focus)
	if len(keys) > 2 && digitRun(keys) {
		return keys[0] + "-" + keys[len(keys)-1], true
	}
	names := make([]string, len(keys))
	for j, key := range keys {
		names[j] = k.keyLabel(key)
	}
	return strings.Join(names, "/"), true
}

func (k *Keymap) keyLabel(key string) string {
	if n, ok := functionKey(key); ok {
		return fmt.Sprintf("F%d", n)
	}
	if glyph, ok := arrowGlyphs[key]; ok && k.charset == CharsetUnicode {
		return glyph
	}
	return key
}

// digitRun reports whether keys are the single digits n, n+1, n+2 and so on.
func digitRun(keys []string) bool {
	for i, key := range keys {
		if len(key) != 1 || key[0] < '0' || key[0] > '9' || key[0] != keys[0][0]+byte(i) {
			return false
		}
	}
	return true
}

// isTextKey reports whether a key name types text where text is read.
func isTextKey(name string) bool { return name == spaceKey || len([]rune(name)) == 1 }

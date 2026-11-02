package tui

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const spaceKey = "space"

// takenKeys are the keys terminals, multiplexers and text lines take for themselves in their default
// setup, with the reason. No action holds one unless it is the second way of another.
var takenKeys = func() map[string]string {
	m := map[string]string{}
	add := func(reason string, keys ...string) {
		for _, k := range keys {
			m[k] = reason
		}
	}
	add("stops or resumes the output of the terminal", "ctrl+s", "ctrl+q")
	add("suspends the process", "ctrl+z")
	add("is the tmux prefix", "ctrl+b")
	add("is the GNU screen prefix", "ctrl+a")
	add("switches a zellij mode", "ctrl+g", "ctrl+n", "ctrl+o", "ctrl+p", "ctrl+t")
	add("is taken by the VS Code terminal", "ctrl+f", "ctrl+k")
	add("is the same as tab, enter or backspace", "ctrl+i", "ctrl+m", "ctrl+h")
	add("is taken by many terminals", "f1", "f5", "f10", "f11", "f12")
	add("is a zellij action", "alt+f", "alt+n", "alt+i", "alt+o", "alt+p", "alt+h", "alt+j", "alt+k", "alt+l",
		"alt+left", "alt+right", "alt+up", "alt+down", "alt+[", "alt+]", "alt+=", "alt+-", "alt++")
	add("is no meta key on macOS and taken by GNOME Terminal",
		"alt+0", "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9")
	add("edits text in a text line", "ctrl+e", "ctrl+w", "ctrl+u", "ctrl+d", "alt+b", "alt+d", "ctrl+left",
		"ctrl+right", "backspace", "delete")
	return m
}()

// fixedKeys keep their meaning on every screen, so no binding may take them.
var fixedKeys = map[string]string{
	"ctrl+c":    "ctrl+c quits",
	"esc":       "esc goes back",
	"tab":       "tab and shift+tab move between fields",
	"shift+tab": "tab and shift+tab move between fields",
	spaceKey:    "space and control characters are no binding",
}

// captureExits are the keys that keep their action while a key is captured as a new binding.
var captureExits = map[string]bool{"ctrl+c": true, "esc": true}

// freeCtrl are the control keys no text line, terminal, multiplexer or screen needs.
var freeCtrl = map[string]bool{"ctrl+l": true, "ctrl+x": true}

// interceptedF are the function keys terminals and window systems take for themselves.
var interceptedF = map[int]bool{1: true, 5: true, 10: true, 11: true, 12: true}

var arrowGlyphs = map[string]string{"up": "↑", "down": "↓", "left": "←", "right": "→"}

var (
	functionKeyName = regexp.MustCompile(`^f([0-9]{1,2})$`)
	ctrlLetter      = regexp.MustCompile(`^ctrl\+[a-z]$`)
)

func functionKey(key string) (int, bool) {
	m := functionKeyName.FindStringSubmatch(key)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// normalizeKey is the spelling a key press has: trimmed, and lower case for names such as F3 or Ctrl+X; a
// single character keeps its case.
func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	if len([]rune(key)) > 1 {
		key = strings.ToLower(key)
	}
	return key
}

// BindingError says why a key cannot be bound to an action.
type BindingError struct {
	Action ActionID
	// Key is the key as normalised, empty for a missing key.
	Key    string
	Reason string
}

func (e *BindingError) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("%s: %s", e.Action, e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Key, e.Reason)
}

// CheckBinding says whether key may be bound to the action id while the other rebindable actions hold
// the keys in others; an action missing from others counts with its default keys. It returns the key as
// normalised, or a *BindingError that names the reason. An empty key means no extra key for a RebindExtra
// action and is refused for a RebindReplace action. A settings screen checks a new key with it against
// the keys its other rows show.
func (k *Keymap) CheckBinding(id ActionID, key string, others map[ActionID]string) (string, error) {
	i, ok := k.index[id]
	if !ok {
		return "", &BindingError{Action: id, Key: normalizeKey(key), Reason: "is no action of the key table"}
	}
	a := k.actions[i]
	key = normalizeKey(key)
	if a.Rebind != RebindNone && key != "" && fixedKeys[key] == "" {
		if owner := k.owner(id, key, others); owner != "" {
			return "", &BindingError{Action: id, Key: key, Reason: fmt.Sprintf("already the key of %s", owner)}
		}
	}
	return k.checkAlone(a, key)
}

// Resolve returns the part of overrides that can be bound and one error for each entry it drops. Every
// entry is first checked on its own; then two entries that end on the same key are both dropped, and so
// on until no key collides, so a swap of two keys stays valid as a whole. The result holds only keys that
// differ from the default and is never in conflict.
func (k *Keymap) Resolve(overrides map[ActionID]string) (map[ActionID]string, []*BindingError) {
	var errs []*BindingError
	var unknown []ActionID
	for id := range overrides {
		if _, ok := k.index[id]; !ok {
			unknown = append(unknown, id)
		}
	}
	slices.Sort(unknown)
	for _, id := range unknown {
		errs = append(errs, &BindingError{Action: id, Key: normalizeKey(overrides[id]), Reason: "is no action of the key table"})
	}
	usable := map[ActionID]string{}
	for _, a := range k.actions {
		v, ok := overrides[a.ID]
		if !ok {
			continue
		}
		key, err := k.checkAlone(a, v)
		if err != nil {
			errs = append(errs, err.(*BindingError))
			continue
		}
		if !isDefault(a, key) {
			usable[a.ID] = key
		}
	}
	for {
		var dropped []*BindingError
		for _, a := range k.actions {
			key, ok := usable[a.ID]
			if !ok {
				continue
			}
			others := maps.Clone(usable)
			delete(others, a.ID)
			if owner := k.owner(a.ID, key, others); owner != "" {
				dropped = append(dropped, &BindingError{Action: a.ID, Key: key, Reason: fmt.Sprintf("already the key of %s", owner)})
			}
		}
		if len(dropped) == 0 {
			break
		}
		for _, d := range dropped {
			delete(usable, d.Action)
		}
		errs = append(errs, dropped...)
	}
	if len(usable) == 0 {
		usable = nil
	}
	return usable, errs
}

// checkAlone refuses the keys of the actions no binding changes, then applies the rules of a binding. A
// key that keeps its meaning everywhere is refused for that reason rather than for its owner.
func (k *Keymap) checkAlone(a Action, key string) (string, error) {
	if a.Rebind == RebindNone {
		return "", &BindingError{Action: a.ID, Key: normalizeKey(key), Reason: "cannot be rebound"}
	}
	key = normalizeKey(key)
	if key != "" && fixedKeys[key] == "" {
		for _, b := range k.actions {
			if (b.Rebind == RebindNone || b.ID == a.ID && b.Rebind == RebindExtra) && slices.Contains(b.DefaultKeys, key) {
				return "", &BindingError{Action: a.ID, Key: key, Reason: fmt.Sprintf("already the key of %s", b.ID)}
			}
		}
	}
	return checkKeyRules(a, key)
}

// owner names the action other than id that holds key while the rebindable actions hold others, or "".
// The default keys of a RebindExtra action id count as taken too, since they stay beside the extra key.
func (k *Keymap) owner(id ActionID, key string, others map[ActionID]string) ActionID {
	for _, b := range k.actions {
		var keys []string
		switch {
		case b.ID == id && b.Rebind == RebindExtra:
			keys = b.DefaultKeys
		case b.ID == id:
			continue
		case b.Rebind == RebindNone:
			keys = b.DefaultKeys
		default:
			v, has := others[b.ID]
			keys = effectiveKeys(b, normalizeKey(v), has)
		}
		if slices.Contains(keys, key) {
			return b.ID
		}
	}
	return ""
}

func effectiveKeys(a Action, key string, has bool) []string {
	switch {
	case !has || key == "" || a.Rebind == RebindNone:
		return slices.Clone(a.DefaultKeys)
	case a.Rebind == RebindReplace:
		return []string{key}
	}
	return append(slices.Clone(a.DefaultKeys), key)
}

func isDefault(a Action, key string) bool {
	if a.Rebind == RebindExtra {
		return key == ""
	}
	return len(a.DefaultKeys) == 1 && key == a.DefaultKeys[0]
}

// worksWhileTyping reports whether an action acts on text lines: its keys type no text, or it takes an
// extra key, whose purpose is to work there.
func worksWhileTyping(a Action) bool {
	return a.Rebind == RebindExtra || !slices.ContainsFunc(a.DefaultKeys, isTextKey)
}

// checkKeyRules applies the rules a key must meet to carry the action a, without looking at other actions.
func checkKeyRules(a Action, key string) (string, error) {
	key = normalizeKey(key)
	refuse := func(format string, args ...any) (string, error) {
		return "", &BindingError{Action: a.ID, Key: key, Reason: fmt.Sprintf(format, args...)}
	}
	if key == "" {
		if a.Rebind == RebindExtra {
			return "", nil
		}
		return refuse("needs a key")
	}
	if reason, ok := fixedKeys[key]; ok {
		return refuse("%s", reason)
	}
	if runes := []rune(key); len(runes) == 1 {
		r := runes[0]
		switch {
		case r >= '0' && r <= '9':
			return refuse("digits choose an area")
		case r < ' ' || r == 0x7f || unicode.IsSpace(r):
			return refuse("space and control characters are no binding")
		case worksWhileTyping(a):
			return refuse("%s acts on text lines, where a printable key is typed text", a.ID)
		case r == '/':
			return refuse("/ searches")
		case unicode.IsLetter(r) || r > 0x7f:
			return refuse("letters belong to the screens' own keys")
		}
		return key, nil
	}
	if n, ok := functionKey(key); ok {
		switch {
		case n < 1 || n > 20:
			return refuse("function keys go from F1 to F20")
		case interceptedF[n]:
			return refuse("terminals take F1, F5, F10, F11 and F12 for themselves")
		}
		return key, nil
	}
	if ctrlLetter.MatchString(key) {
		if !freeCtrl[key] {
			return refuse("text lines, the terminal, a multiplexer or a screen use it; free are ctrl+l and ctrl+x")
		}
		return key, nil
	}
	return refuse("not a single key; use a character, F2 to F20, or ctrl with a letter")
}

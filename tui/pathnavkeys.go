package tui

import (
	"errors"
	"fmt"
	"slices"
)

// entryKey is the entry a typed key stands for: a tab by its item, an area by its group key.
type entryKey struct {
	item  ItemID
	group string
}

// CheckAreas reports why the entry keys of areas cannot be used with this PathNav: what the package-level
// CheckAreas reports about them, and a key that is one of the keys of PathNavKeys or of ActionMenu. Applications
// run it with CheckAreas, in their tests or at start. The overlays ignore all keys of areas that fail it.
func (p *PathNav) CheckAreas(areas []Area) error {
	return errors.Join(checkAreaKeys(areas, p.reservedKeys())...)
}

// entryKeys are the areas' keys if they pass CheckAreas, else nil; nil is also the result where none is set.
func (p *PathNav) entryKeys(areas []Area) []string {
	var keys []string
	for _, a := range areas {
		keys = append(keys, a.Key)
		for _, t := range a.Tabs {
			keys = append(keys, t.Key)
		}
	}
	keys = slices.DeleteFunc(keys, func(k string) bool { return k == "" })
	if len(keys) == 0 || p.CheckAreas(areas) != nil {
		return nil
	}
	return keys
}

// reservedKeys are the keys the overlays and the menu action already use.
func (p *PathNav) reservedKeys() map[string]bool {
	k := p.keys
	out := map[string]bool{}
	for _, id := range []ActionID{k.Up, k.Down, k.PageUp, k.PageDown, k.First, k.Last, k.Left, k.Right, k.Enter,
		k.Back, ActionMenu} {
		for _, name := range p.km.Keys(id) {
			out[name] = true
		}
	}
	return out
}

// checkAreaKeys reports a key that is no ASCII letter or digit, one used twice in the overview, which lists
// every area and tab, and one in reserved.
func checkAreaKeys(areas []Area, reserved map[string]bool) []error {
	var errs []error
	seen := map[string]string{}
	check := func(what, key string) {
		switch {
		case key == "":
			return
		case len(key) != 1 || !(key[0] >= '0' && key[0] <= '9' || key[0] >= 'a' && key[0] <= 'z' || key[0] >= 'A' && key[0] <= 'Z'):
			errs = append(errs, fmt.Errorf("%s: key %q is no ASCII letter or digit", what, key))
			return
		case reserved[key]:
			errs = append(errs, fmt.Errorf("%s: key %q is a key of the overview or the menu", what, key))
		}
		if first, dup := seen[key]; dup {
			errs = append(errs, fmt.Errorf("%s: key %q is already the key of %s", what, key, first))
		}
		seen[key] = what
	}
	for _, a := range areas {
		check(fmt.Sprintf("area %q", a.ID), a.Key)
		for _, t := range a.Tabs {
			check(fmt.Sprintf("area %q: tab %q", a.ID, t.ID), t.Key)
		}
	}
	return errs
}

// byKey chooses the entry of a typed key as Enter on it does. A tab inside a folded area is not shown with its
// key, so its key does nothing.
func (p *PathNav) byKey(k entryKey) (PathChoice, bool) {
	if k.item != "" {
		if p.list.folded[k.group] {
			return PathChoice{}, true
		}
		return p.pick(k.item)
	}
	p.list.reselect(listMark{group: k.group, isGroup: true, ok: true})
	return p.act(p.keys.Enter)
}

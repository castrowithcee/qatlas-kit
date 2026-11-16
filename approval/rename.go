package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// anyElement is the path segment that stands for every item of a list.
const anyElement = "*"

// NameChange describes a rename that approved states are carried over to.
type NameChange struct {
	// From and To are the old and the new name. They must differ.
	From, To string
	// Paths address every string value in a state that holds the name. A path is a list of object member
	// names, and "*" stands for every item of a list; a member named "*" cannot be addressed. A path
	// without "*" must reach a value equal to From. A path with "*" replaces the items equal to From and
	// leaves the others, and must replace at least one.
	Paths [][]string
	// Unordered addresses the lists, in the same form, whose items may be reordered by the rename, such as
	// a list sorted by name. Only whole items may move; the order of every other list must stay.
	Unordered [][]string
}

// RenameItem is one approval that is carried over to a renamed state.
type RenameItem struct {
	// Name is the name the approval is filed under now and NewName the one it is filed under afterwards;
	// an empty NewName keeps Name.
	Name, NewName string
	// Old is the state as it is approved and New the state after the rename, as the application builds them.
	Old, New any
}

// Rename carries approvals over to states that differ from the approved ones only in a name, without a new
// approval and without an Admin: no approved use is added. The approved state is the one stored in the
// file, never Old, which must only match it (ErrDeviates). The kit replaces change.From by change.To in the
// stored state at change.Paths only, refuses a path that does not hold change.From, and requires the result
// to equal New, with the same order of every list but those in change.Unordered, which may be permuted
// whole item by whole item. Any other difference fails with ErrChanged. Each approval keeps its approval
// time and gets the fingerprint of New under the current label. A name missing from the approvals fails
// with ErrNotApproved and a NewName that is taken with ErrExists. All approvals are written in one change
// or none is, and the Undo reverts them like that of Move.
func (s *Store) Rename(change NameChange, items ...RenameItem) (Undo, error) {
	if err := change.validate(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("approval: no approval to rename")
	}
	type prepared struct {
		item RenameItem
		next Approval
	}
	items = slices.Clone(items)
	seen, targets := map[string]bool{}, map[string]bool{}
	for i, item := range items {
		if item.Name == "" {
			return nil, errors.New("approval: an item has no name")
		}
		if item.NewName == "" {
			items[i].NewName = item.Name
		}
		if seen[item.Name] || targets[items[i].NewName] {
			return nil, fmt.Errorf("approval: %s is listed twice", item.Name)
		}
		seen[item.Name], targets[items[i].NewName] = true, true
	}
	return s.update(func(approvals map[string]Approval) (bool, error) {
		next := make([]prepared, 0, len(items))
		for _, item := range items {
			stored, ok := approvals[item.Name]
			if !ok {
				return false, fmt.Errorf("%s: %w", item.Name, ErrNotApproved)
			}
			old, _, err := encode(s.opts.FingerprintLabel, item.Old)
			if err != nil {
				return false, err
			}
			if !s.holds(stored.Fingerprint, old) {
				return false, fmt.Errorf("%s: %w", item.Name, ErrDeviates)
			}
			data, fingerprint, err := encode(s.opts.FingerprintLabel, item.New)
			if err != nil {
				return false, err
			}
			if err := change.carry(stored.State, data); err != nil {
				return false, fmt.Errorf("%s: %w", item.Name, err)
			}
			next = append(next, prepared{item, Approval{Fingerprint: fingerprint, State: data,
				Approved: stored.Approved}})
		}
		for _, p := range next {
			delete(approvals, p.item.Name)
		}
		for _, p := range next {
			if _, taken := approvals[p.item.NewName]; taken {
				return false, fmt.Errorf("%s: %w", p.item.NewName, ErrExists)
			}
			approvals[p.item.NewName] = p.next
		}
		return true, nil
	})
}

func (c NameChange) validate() error {
	if c.From == "" || c.To == "" || c.From == c.To {
		return errors.New("approval: the old and the new name must be set and differ")
	}
	if len(c.Paths) == 0 {
		return errors.New("approval: no path names a value to rename")
	}
	for _, p := range slices.Concat(c.Paths, c.Unordered) {
		if len(p) == 0 || slices.Contains(p, "") {
			return errors.New("approval: a path is empty or has an empty segment")
		}
	}
	return nil
}

// carry replaces the name in the stored state and requires the result to equal the new state.
func (c NameChange) carry(stored, next json.RawMessage) error {
	candidate, err := decodeState(stored)
	if err != nil {
		return err
	}
	for _, p := range c.Paths {
		replaced, err := c.replace(candidate, p, false)
		if err != nil {
			return err
		}
		if replaced == 0 {
			return fmt.Errorf("approval: the path %s reaches no value", strings.Join(p, "/"))
		}
	}
	presented, err := decodeState(next)
	if err != nil {
		return err
	}
	if !c.equal(candidate, presented, nil) {
		return ErrChanged
	}
	return nil
}

func decodeState(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("approval: the state is not JSON: %w", err)
	}
	return v, nil
}

// replace sets every string below node at path from c.From to c.To in place and counts them. Outside a
// list item (listed is false) a value that is not c.From is ErrChanged, as is a path that does not fit the
// state.
func (c NameChange) replace(node any, path []string, listed bool) (int, error) {
	head, rest := path[0], path[1:]
	switch v := node.(type) {
	case map[string]any:
		if head == anyElement {
			return 0, fmt.Errorf("approval: a name path expects a list: %w", ErrChanged)
		}
		child, ok := v[head]
		if !ok {
			return 0, fmt.Errorf("approval: the member %q is missing: %w", head, ErrChanged)
		}
		if len(rest) == 0 {
			if child != any(c.From) {
				if listed {
					return 0, nil
				}
				return 0, fmt.Errorf("approval: the member %q does not hold the old name: %w", head, ErrChanged)
			}
			v[head] = c.To
			return 1, nil
		}
		return c.replace(child, rest, listed)
	case []any:
		if head != anyElement {
			return 0, fmt.Errorf("approval: a name path expects an object: %w", ErrChanged)
		}
		count := 0
		for i, child := range v {
			if len(rest) == 0 {
				if child == any(c.From) {
					v[i] = c.To
					count++
				}
				continue
			}
			n, err := c.replace(child, rest, true)
			if err != nil {
				return 0, err
			}
			count += n
		}
		return count, nil
	}
	return 0, fmt.Errorf("approval: a name path does not fit the state: %w", ErrChanged)
}

// equal compares two decoded states. Lists keep their order unless their path is unordered, where they
// must hold the same whole items. path has "*" for each list level.
func (c NameChange) equal(a, b any, path []string) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, va := range x {
			vb, ok := y[key]
			if !ok || !c.equal(va, vb, append(path[:len(path):len(path)], key)) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		item := append(path[:len(path):len(path)], anyElement)
		if !c.isUnordered(path) {
			for i := range x {
				if !c.equal(x[i], y[i], item) {
					return false
				}
			}
			return true
		}
		used := make([]bool, len(y))
	next:
		for _, ea := range x {
			for j, eb := range y {
				if !used[j] && c.equal(ea, eb, item) {
					used[j] = true
					continue next
				}
			}
			return false
		}
		return true
	}
	return a == b
}

func (c NameChange) isUnordered(path []string) bool {
	return slices.ContainsFunc(c.Unordered, func(p []string) bool { return slices.Equal(p, path) })
}

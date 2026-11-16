package approval

import (
	"bytes"
	"errors"
	"testing"
)

type renameForward struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type renameState struct {
	Name     string          `json:"name"`
	Note     string          `json:"note"`
	Steps    []string        `json:"steps"`
	Forwards []renameForward `json:"forwards"`
}

var renameBase = renameState{
	Name:  "alpha",
	Note:  "alpha",
	Steps: []string{"one", "two"},
	Forwards: []renameForward{
		{Name: "beta", Scope: "read"},
		{Name: "delta", Scope: "write"},
	},
}

func renamed(mutate func(*renameState)) renameState {
	s := renameBase
	s.Steps = append([]string(nil), s.Steps...)
	s.Forwards = append([]renameForward(nil), s.Forwards...)
	s.Name = "omega"
	mutate(&s)
	return s
}

var (
	ownName = NameChange{From: "alpha", To: "omega", Paths: [][]string{{"name"}}}
	// listed is the change for a forwarded name, which a list sorted by name keeps ordered.
	listed = NameChange{From: "beta", To: "zeta", Paths: [][]string{{"forwards", "*", "name"}},
		Unordered: [][]string{{"forwards"}}}
)

func renameItem(name string, old, next any) RenameItem {
	return RenameItem{Name: name, Old: old, New: next}
}

func TestRenameCarriesApprovalOver(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "alpha", renameBase)
	was, _ := s.Approvals()
	next := renamed(func(*renameState) {})
	next.Note = "alpha"
	item := renameItem("alpha", renameBase, next)
	item.NewName = "omega"
	if _, err := s.Rename(ownName, item); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	got, err := s.Approvals()
	if err != nil || len(got) != 1 {
		t.Fatalf("Approvals() = %v, %v", got, err)
	}
	a := got["omega"]
	if !a.Approved.Equal(was["alpha"].Approved) || a.Fingerprint != fingerprint(t, next) {
		t.Fatalf("approval = %+v, want the old time and the fingerprint of the new state", a)
	}
	if err := s.Check("omega", next); err != nil {
		t.Fatalf("Check() of the new state error = %v", err)
	}
	if err := s.Check("omega", renameBase); !errors.Is(err, ErrDeviates) {
		t.Fatalf("Check() of the old state error = %v, want ErrDeviates", err)
	}
}

func TestRenameKeepsKeyWhenNewNameEmpty(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "key", renameBase)
	next := renamed(func(*renameState) {})
	if _, err := s.Rename(ownName, renameItem("key", renameBase, next)); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := s.Check("key", next); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestRenameSortedListWithReordering(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "one", renameBase)
	next := renameBase
	next.Forwards = []renameForward{{Name: "delta", Scope: "write"}, {Name: "zeta", Scope: "read"}}
	if _, err := s.Rename(listed, renameItem("one", renameBase, next)); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := s.Check("one", next); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestRenameSeveralApprovalsInOneWrite(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	approve(t, s, a, "one", renameBase)
	approve(t, s, a, "two", renameBase)
	approve(t, s, a, "bystander", otherState)
	next := renameBase
	next.Forwards = []renameForward{{Name: "delta", Scope: "write"}, {Name: "zeta", Scope: "read"}}
	if _, err := s.Rename(listed, renameItem("one", renameBase, next), renameItem("two", renameBase, next)); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	for _, name := range []string{"one", "two"} {
		if err := s.Check(name, next); err != nil {
			t.Fatalf("Check(%s) error = %v", name, err)
		}
	}
	if err := s.Check("bystander", otherState); err != nil {
		t.Fatalf("Check(bystander) error = %v", err)
	}
	// A second item that is refused keeps the first from being written.
	before := sealedBytes(t, v)
	bad := next
	bad.Note = "changed"
	_, err := s.Rename(NameChange{From: "zeta", To: "eta", Paths: [][]string{{"forwards", "*", "name"}},
		Unordered: [][]string{{"forwards"}}},
		renameItem("one", next, renameState{Name: next.Name, Note: next.Note, Steps: next.Steps,
			Forwards: []renameForward{{Name: "delta", Scope: "write"}, {Name: "eta", Scope: "read"}}}),
		renameItem("two", next, bad))
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("Rename() error = %v, want ErrChanged", err)
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("a refused Rename() wrote the file")
	}
}

func TestRenameRefusedWithoutWriting(t *testing.T) {
	swapped := renameBase
	swapped.Forwards = []renameForward{{Name: "delta", Scope: "read"}, {Name: "zeta", Scope: "write"}}
	reordered := renameBase
	reordered.Steps = []string{"two", "one"}
	reordered.Forwards = []renameForward{{Name: "delta", Scope: "write"}, {Name: "zeta", Scope: "read"}}
	unlisted := renameBase
	unlisted.Forwards = []renameForward{{Name: "delta", Scope: "write"}, {Name: "zeta", Scope: "read"}}
	ordered := listed
	ordered.Unordered = nil
	otherwise := renamed(func(s *renameState) { s.Note = "alpha"; s.Steps = []string{"one", "two", "three"} })
	oldNameElsewhere := renamed(func(s *renameState) { s.Note = "omega" })
	missing := NameChange{From: "alpha", To: "omega", Paths: [][]string{{"name"}, {"absent"}}}
	notHeld := NameChange{From: "alpha", To: "omega", Paths: [][]string{{"name"}, {"steps"}}}
	cases := []struct {
		name   string
		change NameChange
		old    any
		next   any
		want   error
	}{
		{"additional value change", ownName, renameBase, otherwise, ErrChanged},
		{"old name at an unnamed path", ownName, renameBase, oldNameElsewhere, ErrChanged},
		{"named path without the old name", notHeld, renameBase, renamed(func(*renameState) {}), ErrChanged},
		{"named path that is absent", missing, renameBase, renamed(func(*renameState) {}), ErrChanged},
		{"list reordered without permission", ordered, renameBase, unlisted, ErrChanged},
		{"ordered list reordered beside an unordered one", listed, renameBase, reordered, ErrChanged},
		{"fields swapped between items", listed, renameBase, swapped, ErrChanged},
		{"approval does not hold for the old state", ownName, otherState, renamed(func(*renameState) {}), ErrDeviates},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, _ := encryptedVault(t)
			s := newStore(t, v)
			approve(t, s, admin(t, v), "one", renameBase)
			before := sealedBytes(t, v)
			undo, err := s.Rename(c.change, renameItem("one", c.old, c.next))
			if undo != nil || !errors.Is(err, c.want) {
				t.Fatalf("Rename() = %v, %v, want %v", undo != nil, err, c.want)
			}
			if !bytes.Equal(before, sealedBytes(t, v)) {
				t.Fatal("a refused Rename() wrote the file")
			}
		})
	}
}

func TestRenameRefusesTakenOrMissingNames(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	approve(t, s, a, "one", renameBase)
	approve(t, s, a, "two", otherState)
	before := sealedBytes(t, v)
	next := renamed(func(*renameState) {})
	taken := renameItem("one", renameBase, next)
	taken.NewName = "two"
	if _, err := s.Rename(ownName, taken); !errors.Is(err, ErrExists) {
		t.Fatalf("Rename() onto a taken name error = %v, want ErrExists", err)
	}
	if _, err := s.Rename(ownName, renameItem("absent", renameBase, next)); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Rename() of a missing name error = %v, want ErrNotApproved", err)
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("a refused Rename() wrote the file")
	}
}

func TestRenameRefusesBadArguments(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "one", renameBase)
	before := sealedBytes(t, v)
	item := renameItem("one", renameBase, renamed(func(*renameState) {}))
	for name, change := range map[string]NameChange{
		"equal names": {From: "alpha", To: "alpha", Paths: [][]string{{"name"}}},
		"empty name":  {From: "alpha", Paths: [][]string{{"name"}}},
		"no path":     {From: "alpha", To: "omega"},
		"empty path":  {From: "alpha", To: "omega", Paths: [][]string{{}}},
		"no match":    {From: "absent", To: "omega", Paths: [][]string{{"forwards", "*", "name"}}},
	} {
		if undo, err := s.Rename(change, item); err == nil || undo != nil {
			t.Fatalf("%s: Rename() = %v, %v, want an error", name, undo != nil, err)
		}
	}
	if _, err := s.Rename(ownName); err == nil {
		t.Fatal("Rename() without an item succeeded")
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("a refused Rename() wrote the file")
	}
}

func TestRenameNeedsNoAdminAndUndoRestoresBytes(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	approve(t, s, admin(t, v), "one", renameBase)
	before := sealedBytes(t, v)
	next := renamed(func(*renameState) {})
	undo, err := s.Rename(ownName, renameItem("one", renameBase, next))
	if err != nil || undo == nil {
		t.Fatalf("Rename() = %v, %v", undo != nil, err)
	}
	if err := undo(); err != nil {
		t.Fatalf("Undo() error = %v", err)
	}
	if !bytes.Equal(before, sealedBytes(t, v)) {
		t.Fatal("Undo() did not restore the previous bytes")
	}
	if err := s.Check("one", renameBase); err != nil {
		t.Fatalf("Check() after Undo() error = %v", err)
	}
}

func TestRenameUndoRefusedAfterForeignChange(t *testing.T) {
	v, _ := encryptedVault(t)
	s := newStore(t, v)
	a := admin(t, v)
	approve(t, s, a, "one", renameBase)
	undo, err := s.Rename(ownName, renameItem("one", renameBase, renamed(func(*renameState) {})))
	if err != nil {
		t.Fatal(err)
	}
	approve(t, s, a, "other", otherState)
	foreign := sealedBytes(t, v)
	if err := undo(); !errors.Is(err, ErrModified) {
		t.Fatalf("Undo() error = %v, want ErrModified", err)
	}
	if !bytes.Equal(foreign, sealedBytes(t, v)) {
		t.Fatal("a refused Undo() wrote the file")
	}
}

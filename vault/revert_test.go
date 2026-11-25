package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
)

// testRevert sets back every value the write changed wherever the current document still holds the
// written one, so what another writer changed stays.
func testRevert(before, after, current []byte) ([]byte, error) {
	values := func(document []byte) (map[string]string, error) {
		doc := testDoc{Values: map[string]string{}}
		if document != nil {
			if err := json.Unmarshal(document, &doc); err != nil {
				return nil, err
			}
		}
		if doc.Values == nil {
			doc.Values = map[string]string{}
		}
		return doc.Values, nil
	}
	b, err := values(before)
	if err != nil {
		return nil, err
	}
	a, err := values(after)
	if err != nil {
		return nil, err
	}
	c, err := values(current)
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Concat(slices.Collect(maps.Keys(b)), slices.Collect(maps.Keys(a))) {
		bv, inBefore := b[name]
		av, inAfter := a[name]
		cv, inCurrent := c[name]
		if inBefore == inAfter && bv == av || inCurrent != inAfter || cv != av {
			continue
		}
		if inBefore {
			c[name] = bv
		} else {
			delete(c, name)
		}
	}
	return json.Marshal(testDoc{Schema: 1, Values: c})
}

// revertPair is lockedPair with testRevert.
func revertPair(t *testing.T, values map[string]string) (a, b *Vault) {
	t.Helper()
	a, b = lockedPair(t, values)
	a.opts.Revert, b.opts.Revert = testRevert, testRevert
	return a, b
}

// setAndMerge merges every pending entry like testMerge and then sets the value name.
func setAndMerge(name, value string) UpdateFunc {
	return func(document []byte, pending []Pending) ([]byte, []string, error) {
		merged, done, err := testMerge(document, pending)
		if err != nil {
			return nil, nil, err
		}
		var doc testDoc
		if err := json.Unmarshal(merged, &doc); err != nil {
			return nil, nil, err
		}
		doc.Values[name] = value
		updated, err := json.Marshal(doc)
		return updated, done, err
	}
}

// documentOnDisk decrypts the document file with the key v holds, merging nothing.
func documentOnDisk(t *testing.T, v *Vault) map[string]string {
	t.Helper()
	key, err := v.Identity()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptFrom(readBytes(t, v.documentPath()), key)
	if err != nil {
		t.Fatal(err)
	}
	return decodeDoc(t, plain)
}

func wantValues(t *testing.T, got, want map[string]string) {
	t.Helper()
	if !maps.Equal(got, want) {
		t.Fatalf("values = %v, want %v", got, want)
	}
}

// ownChanges are the operations whose undo takes back this value's change alone. Each sets reader to
// example-2 on a document that holds reader example-1 and token example-old.
var ownChanges = []struct {
	name   string
	change func(t *testing.T, v *Vault) (func() error, error)
}{
	{"update", func(t *testing.T, v *Vault) (func() error, error) {
		return v.Update(setAndMerge("reader", "example-2"))
	}},
	{"write with undo", func(t *testing.T, v *Vault) (func() error, error) {
		return v.WriteWithUndo(encodeDoc(t, map[string]string{"reader": "example-2", "token": "example-old"}))
	}},
}

func TestLockedUndoWithoutAnotherWritePutsBytesBack(t *testing.T) {
	for _, c := range ownChanges {
		t.Run(c.name, func(t *testing.T) {
			a, b := lockedPair(t, map[string]string{"reader": "example-1", "token": "example-old"})
			addPending(t, b, testEntry{Name: "writer", Value: "example-3"})
			before := vaultFiles(t, a.Dir())
			held := heldDocument(t, a)
			undo, err := c.change(t, a)
			if err != nil {
				t.Fatal(err)
			}
			if err := undo(); err != nil {
				t.Fatalf("undo() error = %v", err)
			}
			wantFiles(t, vaultFiles(t, a.Dir()), before)
			if !bytes.Equal(heldDocument(t, a), held) {
				t.Fatal("undo did not hold the restored document")
			}
		})
	}
}

func TestLockedUndoKeepsAnotherWrite(t *testing.T) {
	for _, c := range ownChanges {
		t.Run(c.name, func(t *testing.T) {
			a, b := revertPair(t, map[string]string{"reader": "example-1", "token": "example-old"})
			entry := addPending(t, b, testEntry{Name: "writer", Value: "example-3"})
			undo, err := c.change(t, a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Update(replaceToken("example-old", "example-b", nil)); err != nil {
				t.Fatal(err)
			}
			other := addPending(t, b, testEntry{Name: "other", Value: "example-4"})
			if err := undo(); err != nil {
				t.Fatalf("undo() error = %v", err)
			}
			want := map[string]string{"reader": "example-1", "token": "example-b"}
			wantValues(t, documentOnDisk(t, a), want)
			wantValues(t, decodeDoc(t, heldDocument(t, a)), want)
			wantNames := []string{entry, other}
			slices.Sort(wantNames)
			if names := pendingNames(t, a); !slices.Equal(names, wantNames) {
				t.Fatalf("pending entries = %v, want the merged one back and the other one kept: %v", names,
					wantNames)
			}
			after := vaultFiles(t, a.Dir())
			if err := undo(); err != nil {
				t.Fatalf("a second undo() error = %v", err)
			}
			wantFiles(t, vaultFiles(t, a.Dir()), after)
		})
	}
}

// Each undo runs while another value keeps updating; the other value's updates all stay.
func TestLockedUndoDuringAnotherUpdate(t *testing.T) {
	a, b := revertPair(t, map[string]string{})
	const rounds = 15
	var wg sync.WaitGroup
	wg.Go(func() {
		for range rounds {
			undo, err := a.Update(increment("own"))
			if err == nil {
				err = undo()
			}
			if err != nil {
				t.Errorf("Update() and undo() error = %v", err)
				return
			}
		}
	})
	wg.Go(func() {
		for range rounds {
			if _, err := b.Update(increment("other")); err != nil {
				t.Errorf("Update() error = %v", err)
				return
			}
		}
	})
	wg.Wait()
	got := documentOnDisk(t, a)
	if got["other"] != strconv.Itoa(rounds) || got["own"] != "" && got["own"] != "0" {
		t.Fatalf("values = %v, want other at %d and own taken back", got, rounds)
	}
}

func TestLockedUndoRefusals(t *testing.T) {
	failing := func([]byte, []byte, []byte) ([]byte, error) { return nil, errExample }
	invalid := func([]byte, []byte, []byte) ([]byte, error) { return []byte(`{"schema":7}`), nil }
	cases := []struct {
		name    string
		revert  RevertFunc
		between func(t *testing.T, a, b *Vault)
		want    error
	}{
		{"no revert", nil, nil, ErrConflict},
		{"revert fails", failing, nil, errExample},
		{"revert result fails the schema check", invalid, nil, ErrUnsupportedSchema},
		{"forgotten", testRevert, func(t *testing.T, a, _ *Vault) { a.Forget() }, ErrLocked},
		{"decrypted by another value", testRevert, func(t *testing.T, _, b *Vault) {
			if err := b.Decrypt(testPassphrase); err != nil {
				t.Fatal(err)
			}
		}, ErrSwitched},
	}
	for _, c := range cases {
		for _, op := range ownChanges {
			t.Run(c.name+"/"+op.name, func(t *testing.T) {
				a, b := lockedPair(t, map[string]string{"reader": "example-1", "token": "example-old"})
				a.opts.Revert = c.revert
				addPending(t, b, testEntry{Name: "writer", Value: "example-3"})
				undo, err := op.change(t, a)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := b.Update(replaceToken("example-old", "example-b", nil)); err != nil {
					t.Fatal(err)
				}
				if c.between != nil {
					c.between(t, a, b)
				}
				before := vaultFiles(t, a.Dir())
				if err := undo(); !errors.Is(err, c.want) {
					t.Fatalf("undo() error = %v, want %v", err, c.want)
				}
				wantFiles(t, vaultFiles(t, a.Dir()), before)
			})
		}
	}
}

// checkpointed returns a pair as revertPair, with or without testRevert, a pending entry of the other value
// and a checkpoint of a.
func checkpointed(t *testing.T, revert RevertFunc) (a, b *Vault, restore func() error) {
	t.Helper()
	a, b = lockedPair(t, map[string]string{"reader": "example-1", "token": "example-old"})
	a.opts.Revert = revert
	addPending(t, b, testEntry{Name: "writer", Value: "example-3"})
	restore, err := a.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	return a, b, restore
}

func TestLockedRestoreWithoutAnotherWritePutsBytesBack(t *testing.T) {
	a, b, restore := checkpointed(t, nil)
	before := vaultFiles(t, a.Dir())
	addPending(t, a, testEntry{Name: "own", Value: "example-5"})
	if _, err := a.Update(setAndMerge("reader", "example-2")); err != nil {
		t.Fatal(err)
	}
	if err := a.Write(encodeDoc(t, map[string]string{"reader": "example-6"})); err != nil {
		t.Fatal(err)
	}
	other := addPending(t, b, testEntry{Name: "other", Value: "example-4"})
	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v", err)
	}
	after := vaultFiles(t, a.Dir())
	otherPath := filepath.Join(a.opts.Files.Pending, other)
	if _, ok := after[otherPath]; !ok {
		t.Fatal("restore removed the entry another value added")
	}
	delete(after, otherPath)
	wantFiles(t, after, before)
	want := map[string]string{"reader": "example-1", "token": "example-old"}
	wantValues(t, decodeDoc(t, heldDocument(t, a)), want)
}

func TestLockedRestoreKeepsAnotherWrite(t *testing.T) {
	a, b, restore := checkpointed(t, testRevert)
	merged := pendingNames(t, a)
	own := addPending(t, a, testEntry{Name: "own", Value: "example-5"})
	if _, err := a.Update(setAndMerge("reader", "example-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Update(replaceToken("example-old", "example-b", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Update(setAndMerge("extra", "example-7")); err != nil {
		t.Fatal(err)
	}
	other := addPending(t, b, testEntry{Name: "other", Value: "example-4"})
	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v", err)
	}
	want := map[string]string{"reader": "example-1", "token": "example-b"}
	wantValues(t, documentOnDisk(t, a), want)
	wantValues(t, decodeDoc(t, heldDocument(t, a)), want)
	names := pendingNames(t, a)
	wantNames := append(merged, other)
	slices.Sort(wantNames)
	if !slices.Equal(names, wantNames) {
		t.Fatalf("pending entries = %v, want %v: the merged ones back, the other one kept, %s removed", names,
			wantNames, own)
	}
	after := vaultFiles(t, a.Dir())
	if err := restore(); err != nil {
		t.Fatalf("a second restore() error = %v", err)
	}
	wantFiles(t, vaultFiles(t, a.Dir()), after)
}

func TestLockedRestoreRefusals(t *testing.T) {
	failing := func([]byte, []byte, []byte) ([]byte, error) { return nil, errExample }
	cases := []struct {
		name    string
		revert  RevertFunc
		between func(t *testing.T, a *Vault)
		want    error
	}{
		{"no revert", nil, nil, ErrConflict},
		{"revert fails", failing, nil, errExample},
		{"forgotten", testRevert, func(t *testing.T, a *Vault) { a.Forget() }, ErrLocked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b, restore := checkpointed(t, c.revert)
			addPending(t, a, testEntry{Name: "own", Value: "example-5"})
			if _, err := a.Update(setAndMerge("reader", "example-2")); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Update(replaceToken("example-old", "example-b", nil)); err != nil {
				t.Fatal(err)
			}
			if c.between != nil {
				c.between(t, a)
			}
			before := vaultFiles(t, a.Dir())
			if err := restore(); !errors.Is(err, c.want) {
				t.Fatalf("restore() error = %v, want %v", err, c.want)
			}
			wantFiles(t, vaultFiles(t, a.Dir()), before)
		})
	}
}

func TestLockedRestoreRemovesADirectoryWithOnlyTheLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	v := newVault(t, lockedOptions(dir))
	restore, err := v.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	addPending(t, v, testEntry{Name: "own", Value: "example-5"})
	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v", err)
	}
	wantState(t, v, StateAbsent)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the vault directory with only the lock file is still there: %v", err)
	}

	// An entry another value added meanwhile keeps the directory.
	restore, err = v.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	other := addPending(t, newVault(t, lockedOptions(dir)), testEntry{Name: "other", Value: "example-4"})
	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v", err)
	}
	if names := pendingNames(t, v); !slices.Equal(names, []string{other}) {
		t.Fatalf("pending entries = %v, want the other one kept", names)
	}
	if exists, err := fileExists(v.keyPath()); err != nil || exists {
		t.Fatalf("restore left the key file: %v, %v", exists, err)
	}
}

package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var errExample = errors.New("example failure")

// renameChange moves the value from to to and merges the pending entries of from into it. It refuses when
// an entry waits under to, and leaves every other entry pending.
func renameChange(from, to string) UpdateFunc {
	return func(document []byte, pending []Pending) ([]byte, []string, error) {
		doc := testDoc{Values: map[string]string{}}
		if document != nil {
			if err := json.Unmarshal(document, &doc); err != nil {
				return nil, nil, err
			}
		}
		doc.Schema = 1
		if doc.Values == nil {
			doc.Values = map[string]string{}
		}
		var done []string
		for _, p := range pending {
			var entry testEntry
			if err := json.Unmarshal(p.Payload, &entry); err != nil {
				return nil, nil, err
			}
			switch entry.Name {
			case to:
				return nil, nil, errExample
			case from:
				doc.Values[from] = entry.Value
				done = append(done, p.Name)
			}
		}
		doc.Values[to] = doc.Values[from]
		delete(doc.Values, from)
		updated, err := json.Marshal(doc)
		return updated, done, err
	}
}

// vaultFiles returns the content of every file below dir by its path relative to dir.
func vaultFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[rel] = string(readBytes(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func wantFiles(t *testing.T, got, want map[string]string) {
	t.Helper()
	if maps.Equal(got, want) {
		return
	}
	for name := range maps.Keys(want) {
		if got[name] != want[name] {
			t.Errorf("%s differs", name)
		}
	}
	for name := range maps.Keys(got) {
		if _, ok := want[name]; !ok {
			t.Errorf("%s appeared", name)
		}
	}
}

func heldDocument(t *testing.T, v *Vault) []byte {
	t.Helper()
	doc, err := v.Document()
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func failRename(target string) func(string, string) error {
	return func(oldpath, newpath string) error {
		if filepath.Base(newpath) == target {
			return errExample
		}
		return os.Rename(oldpath, newpath)
	}
}

func pendingNames(t *testing.T, v *Vault) []string {
	t.Helper()
	names, err := v.pendingFiles()
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestReadPending(t *testing.T) {
	v, locked := created(t, nil)
	added := []string{
		addPending(t, locked, testEntry{Name: "writer", Value: "example-1"}),
		addPending(t, locked, testEntry{Name: "reader", Value: "example-2"}),
		addPending(t, locked, testEntry{Name: "admin", Value: "example-3"}),
	}
	slices.Sort(added)
	for _, name := range []string{".0123.age.4567.tmp", ".hidden.age", "notes.txt"} {
		overwrite(t, v, filepath.Join(v.pendingPath(), name), []byte("example"))
	}
	var payloads [][]byte
	err := v.ReadPending(func(pending []Pending) error {
		var names []string
		for _, p := range pending {
			var entry testEntry
			if err := json.Unmarshal(p.Payload, &entry); err != nil || entry.Value == "" {
				t.Errorf("payload of %s = %q, %v", p.Name, p.Payload, err)
			}
			names = append(names, p.Name)
			payloads = append(payloads, p.Payload)
		}
		if !slices.Equal(names, added) {
			t.Errorf("ReadPending() names = %v, want %v", names, added)
		}
		// fn runs after the vault is released.
		if _, err := v.Status(); err != nil {
			t.Errorf("Status() inside ReadPending error = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReadPending() error = %v", err)
	}
	for _, payload := range payloads {
		if !bytes.Equal(payload, make([]byte, len(payload))) {
			t.Error("a payload was not cleared after fn returned")
		}
	}
	if err := v.ReadPending(func([]Pending) error { return errExample }); !errors.Is(err, errExample) {
		t.Fatalf("ReadPending() error = %v, want the error of fn", err)
	}

	called := false
	fn := func([]Pending) error { called = true; return nil }
	if err := locked.ReadPending(fn); !errors.Is(err, ErrLocked) {
		t.Fatalf("ReadPending() of a locked vault error = %v, want ErrLocked", err)
	}
	if err := newVault(t, testOptions(t.TempDir())).ReadPending(fn); !errors.Is(err, ErrAbsent) {
		t.Fatalf("ReadPending() of an absent vault error = %v, want ErrAbsent", err)
	}
	plain := newVault(t, testOptions(filepath.Join(t.TempDir(), "vault")))
	if err := plain.CreateUnencrypted(encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	if err := plain.ReadPending(fn); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("ReadPending() of an unencrypted vault error = %v, want ErrNotEncrypted", err)
	}
	overwrite(t, v, filepath.Join(v.pendingPath(), added[0]),
		encryptFor(t, foreignIdentity(t).Recipient(), []byte(`{"name":"x","value":"y"}`)))
	if err := v.ReadPending(fn); !errors.Is(err, ErrDamaged) {
		t.Fatalf("ReadPending() with a foreign entry error = %v, want ErrDamaged", err)
	}
	if called {
		t.Fatal("fn was called although the read was refused")
	}
}

func TestUpdateMergesSelectedEntriesAndUndoes(t *testing.T) {
	v, locked := created(t, map[string]string{"old": "example-1", "kept": "example-2"})
	oldA := addPending(t, locked, testEntry{Name: "old", Value: "example-3"})
	oldB := addPending(t, locked, testEntry{Name: "old", Value: "example-3"})
	other := addPending(t, locked, testEntry{Name: "other", Value: "example-4"})
	before := vaultFiles(t, v.Dir())
	heldBefore := heldDocument(t, v)

	v.remove = func(path string) error {
		if bytes.Equal(readBytes(t, v.documentPath()), []byte(before[v.opts.Files.Document])) {
			t.Errorf("%s was removed before the document was written", filepath.Base(path))
		}
		return os.Remove(path)
	}
	undo, err := v.Update(renameChange("old", "new"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	values := decodeDoc(t, heldDocument(t, v))
	if values["new"] != "example-3" || values["kept"] != "example-2" || values["old"] != "" {
		t.Fatalf("held document after Update = %v", values)
	}
	if names := pendingNames(t, v); !slices.Equal(names, []string{other}) {
		t.Fatalf("pending after Update = %v, want only %s", names, other)
	}
	key, _ := v.Identity()
	opened, err := OpenWithIdentity(v.opts, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(heldDocument(t, opened), heldDocument(t, v)) {
		t.Fatal("the document file differs from the held document")
	}

	// Another process adds an entry after the update; the undo leaves it alone.
	late := addPending(t, locked, testEntry{Name: "late", Value: "example-5"})
	if err := undo(); err != nil {
		t.Fatalf("undo() error = %v", err)
	}
	after := vaultFiles(t, v.Dir())
	lateFile := filepath.Join(v.opts.Files.Pending, late)
	if _, ok := after[lateFile]; !ok {
		t.Fatal("undo removed an entry added after the update")
	}
	delete(after, lateFile)
	wantFiles(t, after, before)
	for _, name := range []string{oldA, oldB} {
		wantPrivate(t, filepath.Join(v.pendingPath(), name))
	}
	wantPrivate(t, v.documentPath())
	if !bytes.Equal(heldDocument(t, v), heldBefore) {
		t.Fatalf("held document after undo = %s, want %s", heldDocument(t, v), heldBefore)
	}

	// The undo takes effect once.
	changed := encodeDoc(t, map[string]string{"later": "example-6"})
	if err := v.Write(changed); err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatalf("second undo() error = %v", err)
	}
	if !bytes.Equal(heldDocument(t, v), changed) {
		t.Fatal("a second undo changed the document again")
	}
}

func TestUpdateUndoWithoutDocumentAndAfterForget(t *testing.T) {
	v, locked := created(t, nil)
	if err := os.Remove(v.documentPath()); err != nil {
		t.Fatal(err)
	}
	v.Forget()
	if _, err := v.Unlock(testPassphrase); err != nil {
		t.Fatal(err)
	}
	if doc := heldDocument(t, v); doc != nil {
		t.Fatalf("held document = %s, want none", doc)
	}
	addPending(t, locked, testEntry{Name: "old", Value: "example-1"})
	before := vaultFiles(t, v.Dir())
	undo, err := v.Update(renameChange("old", "new"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := undo(); err != nil {
		t.Fatalf("undo() error = %v", err)
	}
	wantFiles(t, vaultFiles(t, v.Dir()), before)
	if _, err := os.Stat(v.documentPath()); !os.IsNotExist(err) {
		t.Fatalf("the document file is back although it was missing before: %v", err)
	}
	if doc := heldDocument(t, v); doc != nil {
		t.Fatalf("held document after undo = %s, want none", doc)
	}

	// The restored entry merges on Unlock; then an undo runs after Forget.
	if _, err := locked.Unlock(testPassphrase); err != nil {
		t.Fatal(err)
	}
	entry := addPending(t, locked, testEntry{Name: "old", Value: "example-2"})
	before = vaultFiles(t, v.Dir())
	undo, err = locked.Update(renameChange("old", "new"))
	if err != nil {
		t.Fatal(err)
	}
	locked.Forget()
	if err := undo(); err != nil {
		t.Fatalf("undo() after Forget error = %v", err)
	}
	wantFiles(t, vaultFiles(t, v.Dir()), before)
	wantPrivate(t, filepath.Join(v.pendingPath(), entry))
	wantState(t, locked, StateLocked)
}

func TestUpdateRejections(t *testing.T) {
	type setup func(t *testing.T, v, locked *Vault, entries []string) (*Vault, UpdateFunc)
	rename := renameChange("old", "new")
	flip := func(t *testing.T, path string) {
		data := readBytes(t, path)
		data[len(data)-1] ^= 0x01
		if err := os.WriteFile(path, data, fileMode); err != nil {
			t.Fatal(err)
		}
	}
	for name, tc := range map[string]struct {
		setup setup
		want  func(error) bool
	}{
		"locked": {
			func(_ *testing.T, _, locked *Vault, _ []string) (*Vault, UpdateFunc) { return locked, rename },
			func(err error) bool { return errors.Is(err, ErrLocked) },
		},
		"error of the function": {
			func(_ *testing.T, v, _ *Vault, _ []string) (*Vault, UpdateFunc) {
				return v, func([]byte, []Pending) ([]byte, []string, error) { return nil, nil, errExample }
			},
			func(err error) bool { return errors.Is(err, errExample) },
		},
		"entry under the new name": {
			func(t *testing.T, _, locked *Vault, _ []string) (*Vault, UpdateFunc) {
				addPending(t, locked, testEntry{Name: "new", Value: "example-9"})
				return nil, rename
			},
			func(err error) bool { return errors.Is(err, errExample) },
		},
		"unknown entry reported": {
			func(_ *testing.T, v, _ *Vault, entries []string) (*Vault, UpdateFunc) {
				return v, func(doc []byte, _ []Pending) ([]byte, []string, error) {
					return doc, []string{entries[0], "00000000000000000000000000000000.age"}, nil
				}
			},
			func(err error) bool { return err != nil },
		},
		"document without schema": {
			func(_ *testing.T, v, _ *Vault, entries []string) (*Vault, UpdateFunc) {
				return v, func([]byte, []Pending) ([]byte, []string, error) {
					return []byte(`{"values":{}}`), entries, nil
				}
			},
			func(err error) bool { return errors.Is(err, ErrUnsupportedSchema) },
		},
		"unsupported schema": {
			func(_ *testing.T, v, _ *Vault, entries []string) (*Vault, UpdateFunc) {
				return v, func([]byte, []Pending) ([]byte, []string, error) {
					return []byte(`{"schema":2}`), entries, nil
				}
			},
			func(err error) bool { return errors.Is(err, ErrUnsupportedSchema) },
		},
		"damaged entry": {
			func(t *testing.T, v, _ *Vault, entries []string) (*Vault, UpdateFunc) {
				flip(t, filepath.Join(v.pendingPath(), entries[1]))
				return v, rename
			},
			func(err error) bool { return errors.Is(err, ErrDamaged) },
		},
		"entry for a foreign recipient": {
			func(t *testing.T, v, _ *Vault, _ []string) (*Vault, UpdateFunc) {
				overwrite(t, v, filepath.Join(v.pendingPath(), "ffffffffffffffffffffffffffffffff.age"),
					encryptFor(t, foreignIdentity(t).Recipient(), []byte(`{"name":"old","value":"x"}`)))
				return v, rename
			},
			func(err error) bool { return errors.Is(err, ErrDamaged) },
		},
		"entry others can read": {
			func(t *testing.T, v, _ *Vault, entries []string) (*Vault, UpdateFunc) {
				widen(t, filepath.Join(v.pendingPath(), entries[0]))
				return v, rename
			},
			func(err error) bool { var perm *PermissionError; return errors.As(err, &perm) },
		},
		"document write fails": {
			func(_ *testing.T, v, _ *Vault, _ []string) (*Vault, UpdateFunc) {
				v.rename = failRename(v.opts.Files.Document)
				return v, rename
			},
			func(err error) bool { return errors.Is(err, errExample) },
		},
		"removal fails": {
			func(_ *testing.T, v, _ *Vault, entries []string) (*Vault, UpdateFunc) {
				// The first entry is removed and has to come back.
				v.remove = func(path string) error {
					if filepath.Base(path) == entries[1] {
						return errExample
					}
					return os.Remove(path)
				}
				return v, rename
			},
			func(err error) bool { return errors.Is(err, errExample) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			v, locked := created(t, map[string]string{"old": "example-1"})
			entries := []string{
				addPending(t, locked, testEntry{Name: "old", Value: "example-2"}),
				addPending(t, locked, testEntry{Name: "old", Value: "example-3"}),
			}
			slices.Sort(entries)
			target, change := tc.setup(t, v, locked, entries)
			if target == nil {
				target = v
			}
			before := vaultFiles(t, v.Dir())
			held := heldDocument(t, v)
			undo, err := target.Update(change)
			if !tc.want(err) || undo != nil {
				t.Fatalf("Update() = %v, error %v, want it refused", undo != nil, err)
			}
			wantFiles(t, vaultFiles(t, v.Dir()), before)
			if !bytes.Equal(heldDocument(t, v), held) {
				t.Fatal("a refused Update changed the held document")
			}
			if target == v {
				wantState(t, v, StateUnlocked)
			}
			if name == "removal fails" {
				wantPrivate(t, filepath.Join(v.pendingPath(), entries[0]))
			}
		})
	}

	plain := newVault(t, testOptions(filepath.Join(t.TempDir(), "vault")))
	if err := plain.CreateUnencrypted(encodeDoc(t, map[string]string{"old": "example-1"})); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Update(rename); err != nil {
		t.Fatalf("Update() of an unencrypted vault error = %v", err)
	}
	wantValues(t, decodeDoc(t, heldDocument(t, plain)), map[string]string{"new": "example-1"})
	if _, err := newVault(t, testOptions(t.TempDir())).Update(rename); !errors.Is(err, ErrAbsent) {
		t.Fatalf("Update() of an absent vault error = %v, want ErrAbsent", err)
	}
}

func TestWriteWithUndo(t *testing.T) {
	t.Run("encrypted", func(t *testing.T) {
		v, locked := created(t, map[string]string{"reader": "example-1"})
		before := vaultFiles(t, v.Dir())
		held := heldDocument(t, v)
		changed := encodeDoc(t, map[string]string{"reader": "example-2"})
		undo, err := v.WriteWithUndo(changed)
		if err != nil {
			t.Fatalf("WriteWithUndo() error = %v", err)
		}
		if !bytes.Equal(heldDocument(t, v), changed) {
			t.Fatal("WriteWithUndo() did not hold the new document")
		}
		late := addPending(t, locked, testEntry{Name: "late", Value: "example-3"})
		if err := undo(); err != nil {
			t.Fatalf("undo() error = %v", err)
		}
		after := vaultFiles(t, v.Dir())
		lateFile := filepath.Join(v.opts.Files.Pending, late)
		if _, ok := after[lateFile]; !ok {
			t.Fatal("undo removed an entry added after the write")
		}
		delete(after, lateFile)
		wantFiles(t, after, before)
		wantPrivate(t, v.documentPath())
		if !bytes.Equal(heldDocument(t, v), held) {
			t.Fatal("undo did not hold the previous document again")
		}
	})
	t.Run("unencrypted", func(t *testing.T) {
		v := newVault(t, testOptions(filepath.Join(t.TempDir(), "vault")))
		if err := v.CreateUnencrypted(encodeDoc(t, map[string]string{"reader": "example-1"})); err != nil {
			t.Fatal(err)
		}
		before := vaultFiles(t, v.Dir())
		undo, err := v.WriteWithUndo(encodeDoc(t, map[string]string{"reader": "example-2"}))
		if err != nil {
			t.Fatalf("WriteWithUndo() error = %v", err)
		}
		late := filepath.Join(v.opts.Files.Pending, "0123456789abcdef0123456789abcdef.age")
		overwrite(t, v, filepath.Join(v.Dir(), late), []byte("example"))
		if err := undo(); err != nil {
			t.Fatalf("undo() error = %v", err)
		}
		after := vaultFiles(t, v.Dir())
		if _, ok := after[late]; !ok {
			t.Fatal("undo removed a file added after the write")
		}
		delete(after, late)
		wantFiles(t, after, before)
		wantPrivate(t, v.plainPath())
	})
	t.Run("refused", func(t *testing.T) {
		v, locked := created(t, map[string]string{"reader": "example-1"})
		plain := newVault(t, testOptions(filepath.Join(t.TempDir(), "vault")))
		if err := plain.CreateUnencrypted(encodeDoc(t, nil)); err != nil {
			t.Fatal(err)
		}
		changed := encodeDoc(t, map[string]string{"reader": "example-2"})
		for name, tc := range map[string]struct {
			v        *Vault
			document []byte
			prepare  func()
			want     error
		}{
			"locked":           {locked, changed, nil, ErrLocked},
			"invalid document": {v, []byte(`[]`), nil, ErrDamaged},
			"absent":           {newVault(t, testOptions(t.TempDir())), changed, nil, ErrAbsent},
			"write fails":      {v, changed, func() { v.rename = failRename(v.opts.Files.Document) }, errExample},
			"plain write fails": {plain, changed, func() { plain.rename = failRename(plain.opts.Files.Plain) },
				errExample},
		} {
			t.Run(name, func(t *testing.T) {
				if tc.prepare != nil {
					tc.prepare()
					defer func() { v.rename, plain.rename = os.Rename, os.Rename }()
				}
				before, plainBefore := vaultFiles(t, v.Dir()), vaultFiles(t, plain.Dir())
				held := heldDocument(t, v)
				if undo, err := tc.v.WriteWithUndo(tc.document); !errors.Is(err, tc.want) || undo != nil {
					t.Fatalf("WriteWithUndo() error = %v, want it refused", err)
				}
				wantFiles(t, vaultFiles(t, v.Dir()), before)
				wantFiles(t, vaultFiles(t, plain.Dir()), plainBefore)
				if !bytes.Equal(heldDocument(t, v), held) {
					t.Fatal("a refused WriteWithUndo changed the held document")
				}
			})
		}
	})
}

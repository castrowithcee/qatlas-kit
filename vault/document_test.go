package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// dropExtra is a filter that removes the member "extra" and so keeps what the application allows.
func dropExtra(document []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(document, &doc); err != nil {
		return nil, err
	}
	delete(doc, "extra")
	return json.Marshal(doc)
}

func docWithExtra(t *testing.T) []byte {
	t.Helper()
	return []byte(`{"schema":1,"values":{"reader":"example-1"},"extra":"example-grant"}`)
}

func optionsWith(t *testing.T, mutate func(*Options)) Options {
	t.Helper()
	opts := testOptions(filepath.Join(t.TempDir(), "vault"))
	mutate(&opts)
	return opts
}

func TestSwitchFilterAppliesToBothDirections(t *testing.T) {
	opts := optionsWith(t, func(o *Options) { o.SwitchFilter = dropExtra })
	v := newVault(t, opts)
	if err := v.CreateUnencrypted(docWithExtra(t)); err != nil {
		t.Fatal(err)
	}
	if err := v.Encrypt(testPassphrase); err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	doc, err := v.Document()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(doc), "extra") || decodeDoc(t, doc)["reader"] != "example-1" {
		t.Fatalf("held document after Encrypt = %s", doc)
	}
	// The file on disk is filtered too, not only the held copy.
	locked := newVault(t, opts)
	if _, err := locked.Unlock(testPassphrase); err != nil {
		t.Fatal(err)
	}
	if got, _ := locked.Document(); strings.Contains(string(got), "extra") {
		t.Fatalf("encrypted document = %s", got)
	}

	// An older encrypted document that carries the field loses it on Decrypt.
	id := locked.identity
	if err := locked.writeDocument(id, docWithExtra(t)); err != nil {
		t.Fatal(err)
	}
	locked.Forget()
	if err := locked.Decrypt(testPassphrase); err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if got := readBytes(t, locked.plainPath()); strings.Contains(string(got), "extra") {
		t.Fatalf("plain document = %s", got)
	}
}

func TestWithoutSwitchFilterNothingChanges(t *testing.T) {
	opts := testOptions(filepath.Join(t.TempDir(), "vault"))
	v := newVault(t, opts)
	if err := v.CreateUnencrypted(docWithExtra(t)); err != nil {
		t.Fatal(err)
	}
	if err := v.Encrypt(testPassphrase); err != nil {
		t.Fatal(err)
	}
	if doc, _ := v.Document(); !strings.Contains(string(doc), "example-grant") {
		t.Fatalf("document after Encrypt = %s", doc)
	}
	v.Forget()
	if err := v.Decrypt(testPassphrase); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, v.plainPath()); string(got) != string(docWithExtra(t)) {
		t.Fatalf("plain document after Decrypt = %s", got)
	}
}

func TestFailedSwitchFilterWritesNothing(t *testing.T) {
	boom := errors.New("filter failed with example detail")
	filters := map[string]func([]byte) ([]byte, error){
		"error":      func([]byte) ([]byte, error) { return nil, boom },
		"not json":   func([]byte) ([]byte, error) { return []byte("not json"), nil },
		"schema":     func([]byte) ([]byte, error) { return []byte(`{"schema":9}`), nil },
		"empty":      func([]byte) ([]byte, error) { return nil, nil },
		"no version": func([]byte) ([]byte, error) { return []byte(`{}`), nil },
	}
	for name, filter := range filters {
		t.Run(name, func(t *testing.T) {
			opts := optionsWith(t, func(o *Options) { o.SwitchFilter = filter })
			v := newVault(t, opts)
			plain := docWithExtra(t)
			if err := v.CreateUnencrypted(plain); err != nil {
				t.Fatal(err)
			}
			if err := v.Encrypt(testPassphrase); err == nil {
				t.Fatal("Encrypt() error = nil")
			}
			for _, p := range []string{v.keyPath(), v.recipientPath(), v.documentPath()} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s exists after a failed Encrypt: %v", p, err)
				}
			}
			if string(readBytes(t, v.plainPath())) != string(plain) {
				t.Error("the plain document changed")
			}
			wantState(t, v, StateUnencrypted)

			// Decrypt: build an encrypted vault without the filter, then switch with it.
			plainOpts := opts
			plainOpts.SwitchFilter = nil
			plainOpts.Dir = filepath.Join(t.TempDir(), "vault")
			enc := newVault(t, plainOpts)
			if err := enc.Create(testPassphrase, plain); err != nil {
				t.Fatal(err)
			}
			if err := enc.WriteSealed(sealedName, []byte("example settings")); err != nil {
				t.Fatal(err)
			}
			filtered := plainOpts
			filtered.SwitchFilter = filter
			dec := newVault(t, filtered)
			err := dec.Decrypt(testPassphrase)
			if err == nil {
				t.Fatal("Decrypt() error = nil")
			}
			if name == "error" && !errors.Is(err, boom) {
				t.Errorf("Decrypt() error = %v, want the filter's error", err)
			}
			if _, statErr := os.Stat(dec.plainPath()); !os.IsNotExist(statErr) {
				t.Errorf("a plain document exists after a failed Decrypt: %v", statErr)
			}
			for _, p := range []string{dec.keyPath(), dec.recipientPath(), dec.documentPath(), dec.path(sealedName)} {
				if _, statErr := os.Stat(p); statErr != nil {
					t.Errorf("%s is gone after a failed Decrypt: %v", p, statErr)
				}
			}
			again := newVault(t, plainOpts)
			if _, err := again.Unlock(testPassphrase); err != nil {
				t.Errorf("Unlock() after a failed Decrypt error = %v", err)
			}
		})
	}
}

func TestEmptyDocumentOption(t *testing.T) {
	empty := []byte(`{"schema":1,"values":{}}`)
	opts := optionsWith(t, func(o *Options) { o.EmptyDocument = empty })

	// Plain file of 0 bytes.
	v := newVault(t, opts)
	if err := v.CreateUnencrypted(encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	overwrite(t, v, v.plainPath(), nil)
	doc, err := v.Document()
	if err != nil || string(doc) != string(empty) {
		t.Fatalf("Document() of an empty plain file = %q, %v", doc, err)
	}
	// Encrypt carries the empty document over.
	if err := v.Encrypt(testPassphrase); err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if got, _ := v.Document(); string(got) != string(empty) {
		t.Fatalf("document after Encrypt = %q", got)
	}

	// Encrypted document that holds 0 bytes: Unlock, OpenWithIdentity, undo.
	recipient, _ := age.ParseX25519Recipient(strings.TrimSpace(string(readBytes(t, v.recipientPath()))))
	emptyFile := encryptFor(t, recipient, nil)
	overwrite(t, v, v.documentPath(), emptyFile)
	locked := newVault(t, opts)
	if _, err := locked.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if got, _ := locked.Document(); string(got) != string(empty) {
		t.Fatalf("Unlock() document = %q", got)
	}
	opened, err := OpenWithIdentity(opts, locked.identity.key)
	if err != nil {
		t.Fatalf("OpenWithIdentity() error = %v", err)
	}
	if got, _ := opened.Document(); string(got) != string(empty) {
		t.Fatalf("OpenWithIdentity() document = %q", got)
	}
	undo, err := locked.WriteWithUndo(encodeDoc(t, map[string]string{"reader": "example-1"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatalf("undo() error = %v", err)
	}
	if got, _ := locked.Document(); string(got) != string(empty) {
		t.Fatalf("document after undo = %q", got)
	}

	// Decrypt writes the empty document as plain JSON.
	locked.Forget()
	if err := locked.Decrypt(testPassphrase); err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if got := readBytes(t, locked.plainPath()); string(got) != string(empty) {
		t.Fatalf("plain document after Decrypt = %q", got)
	}

	// Writes never accept 0 bytes.
	if err := locked.Write(nil); !errors.Is(err, ErrDamaged) {
		t.Errorf("Write(nil) error = %v, want ErrDamaged", err)
	}
	if _, err := locked.WriteWithUndo([]byte{}); !errors.Is(err, ErrDamaged) {
		t.Errorf("WriteWithUndo(empty) error = %v, want ErrDamaged", err)
	}
	if err := newVault(t, optionsWith(t, func(o *Options) { o.EmptyDocument = empty })).Create(testPassphrase, nil); !errors.Is(err, ErrDamaged) {
		t.Errorf("Create(nil) error = %v, want ErrDamaged", err)
	}
}

func TestEmptyDocumentWithoutOptionIsDamaged(t *testing.T) {
	for _, content := range [][]byte{nil, []byte(" \n")} {
		opts := optionsWith(t, func(o *Options) { o.EmptyDocument = []byte(`{"schema":1}`) })
		v := newVault(t, opts)
		if err := v.CreateUnencrypted(encodeDoc(t, nil)); err != nil {
			t.Fatal(err)
		}
		overwrite(t, v, v.plainPath(), content)
		plainOpts := opts
		plainOpts.EmptyDocument = nil
		if len(content) == 0 {
			if _, err := newVault(t, plainOpts).Document(); !errors.Is(err, ErrDamaged) {
				t.Errorf("Document() of 0 bytes without the option error = %v, want ErrDamaged", err)
			}
		}
		// Only blanks stay damaged even with the option.
		if len(content) > 0 {
			if _, err := v.Document(); !errors.Is(err, ErrDamaged) {
				t.Errorf("Document() of blanks error = %v, want ErrDamaged", err)
			}
		}
	}
	// A decrypted document of 0 bytes is damaged without the option, too.
	v, locked := created(t, nil)
	recipient, _ := age.ParseX25519Recipient(strings.TrimSpace(string(readBytes(t, locked.recipientPath()))))
	overwrite(t, v, v.documentPath(), encryptFor(t, recipient, nil))
	if _, err := locked.Unlock(testPassphrase); !errors.Is(err, ErrDamaged) {
		t.Errorf("Unlock() of an empty document error = %v, want ErrDamaged", err)
	}
}

func TestInvalidEmptyDocumentFailsNew(t *testing.T) {
	for _, doc := range []string{``, `{}`, `{"schema":2}`, `not json`, `[]`} {
		opts := testOptions(t.TempDir())
		opts.EmptyDocument = []byte(doc)
		if _, err := New(opts); err == nil {
			t.Errorf("New() with empty document %q error = nil", doc)
		}
	}
}

func TestDocumentErrorKinds(t *testing.T) {
	v, locked := created(t, nil)
	recipient, _ := age.ParseX25519Recipient(strings.TrimSpace(string(readBytes(t, locked.recipientPath()))))

	check := func(name string, err error, kind DocumentErrorKind, version int, sentinel error) {
		t.Helper()
		var de *DocumentError
		if !errors.As(err, &de) {
			t.Fatalf("%s: error = %v, want a *DocumentError", name, err)
		}
		if de.Kind != kind || de.Version != version || de.Path != v.documentPath() {
			t.Errorf("%s: DocumentError = %+v, want kind %s version %d path %s", name, *de, kind, version, v.documentPath())
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("%s: errors.Is(%v, %v) = false", name, err, sentinel)
		}
	}

	overwrite(t, v, v.documentPath(), []byte("not an age file"))
	_, err := locked.Unlock(testPassphrase)
	check("decrypt", err, DocumentDecrypt, 0, ErrDamaged)
	_, err = OpenWithIdentity(testOptionsAt(v), v.identity.key)
	check("decrypt via identity", err, DocumentDecrypt, 0, ErrDamaged)

	overwrite(t, v, v.documentPath(), encryptFor(t, recipient, []byte("secret-looking text")))
	_, err = locked.Unlock(testPassphrase)
	check("decode", err, DocumentDecode, 0, ErrDamaged)
	if strings.Contains(err.Error(), "secret-looking") {
		t.Errorf("error leaks content: %v", err)
	}

	overwrite(t, v, v.documentPath(), encryptFor(t, recipient, []byte(`{"schema":7}`)))
	_, err = locked.Unlock(testPassphrase)
	check("schema", err, DocumentSchema, 7, ErrUnsupportedSchema)
	if errors.Is(err, ErrDamaged) {
		t.Error("a schema error must not be ErrDamaged")
	}

	// Writes and updates report the same type.
	check("write", v.Write([]byte(`{"schema":8}`)), DocumentSchema, 8, ErrUnsupportedSchema)
	_, err = v.Update(func(doc []byte, _ []Pending) ([]byte, []string, error) { return []byte(`{`), nil, nil })
	check("update", err, DocumentDecode, 0, ErrDamaged)

	// The plain file.
	opts := testOptions(filepath.Join(t.TempDir(), "plain"))
	p := newVault(t, opts)
	if err := p.CreateUnencrypted(encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	overwrite(t, p, p.plainPath(), []byte(`{"schema":3}`))
	_, err = p.Document()
	var de *DocumentError
	if !errors.As(err, &de) || de.Kind != DocumentSchema || de.Version != 3 || de.Path != p.plainPath() {
		t.Errorf("plain Document() error = %v", err)
	}
}

func TestDocumentWithoutSchemaIsVersionZero(t *testing.T) {
	versionZero := []string{`{}`, `{"schema":null}`, ` {"values":{}}`}
	decode := []string{`[]`, `null`, `{"schema":"x"}`, `{"schema":1.5}`, `not json`}

	check := func(name, doc string, err error, schemaErr bool) {
		t.Helper()
		var de *DocumentError
		if !errors.As(err, &de) {
			t.Fatalf("%s %q: error = %v, want a *DocumentError", name, doc, err)
		}
		if schemaErr {
			if de.Kind != DocumentSchema || de.Version != 0 || !errors.Is(err, ErrUnsupportedSchema) || errors.Is(err, ErrDamaged) {
				t.Errorf("%s %q: error = %v (%+v), want DocumentSchema version 0", name, doc, err, *de)
			}
		} else if de.Kind != DocumentDecode || !errors.Is(err, ErrDamaged) {
			t.Errorf("%s %q: error = %v (%+v), want DocumentDecode", name, doc, err, *de)
		}
		if strings.Contains(err.Error(), "values") || strings.Contains(err.Error(), "not json") {
			t.Errorf("%s %q: error leaks content: %v", name, doc, err)
		}
	}

	for _, withEmpty := range []bool{false, true} {
		mutate := func(o *Options) {
			if withEmpty {
				o.EmptyDocument = []byte(`{"schema":1}`)
			}
		}
		opts := optionsWith(t, mutate)
		v, locked := newVault(t, opts), newVault(t, opts)
		if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
			t.Fatal(err)
		}
		recipient, _ := age.ParseX25519Recipient(strings.TrimSpace(string(readBytes(t, locked.recipientPath()))))
		run := func(doc string, schemaErr bool) {
			name := "read"
			if withEmpty {
				name = "read with EmptyDocument"
			}
			overwrite(t, v, v.documentPath(), encryptFor(t, recipient, []byte(doc)))
			_, err := locked.Unlock(testPassphrase)
			check(name, doc, err, schemaErr)
			before := readBytes(t, v.documentPath())
			check(strings.Replace(name, "read", "write", 1), doc, v.Write([]byte(doc)), schemaErr)
			if string(readBytes(t, v.documentPath())) != string(before) {
				t.Errorf("Write(%q) changed the document file", doc)
			}
		}
		for _, doc := range versionZero {
			run(doc, true)
		}
		for _, doc := range decode {
			run(doc, false)
		}
	}
}

func testOptionsAt(v *Vault) Options { return v.opts }

// dirState returns every file below dir by relative path, for byte-exact comparisons.
func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		state[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestDecryptWithPendingWritesNothingOnFailure(t *testing.T) {
	boom := errors.New("filter failed")
	cases := map[string]struct {
		filter func([]byte) ([]byte, error)
		fail   bool // fail the write of the plain document
	}{
		"filter error":   {filter: func([]byte) ([]byte, error) { return nil, boom }},
		"invalid result": {filter: func([]byte) ([]byte, error) { return []byte(`{"schema":9}`), nil }},
		"plain write":    {filter: dropExtra, fail: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := optionsWith(t, func(o *Options) { o.SwitchFilter = tc.filter })
			v := newVault(t, opts)
			if err := v.Create(testPassphrase, docWithExtra(t)); err != nil {
				t.Fatal(err)
			}
			addPending(t, v, testEntry{Name: "writer", Value: "example-2"})
			addPending(t, v, testEntry{Name: "editor", Value: "example-3"})
			if err := v.WriteSealed(sealedName, []byte("example settings")); err != nil {
				t.Fatal(err)
			}
			v.Forget()
			before := dirState(t, opts.Dir)
			if tc.fail {
				v.rename = func(oldpath, newpath string) error {
					if filepath.Base(newpath) == "secrets.json" {
						return errors.New("example interruption")
					}
					return os.Rename(oldpath, newpath)
				}
			}
			if err := v.Decrypt(testPassphrase); err == nil {
				t.Fatal("Decrypt() error = nil")
			}
			after := dirState(t, opts.Dir)
			for _, k := range sortedKeys(before) {
				if after[k] != before[k] {
					t.Errorf("%s changed or is gone", k)
				}
			}
			if len(after) != len(before) {
				t.Errorf("files after = %d, before = %d", len(after), len(before))
			}
			if _, err := newVault(t, opts).Unlock(testPassphrase); err != nil {
				t.Errorf("Unlock() error = %v", err)
			}
		})
	}
}

func TestDecryptRemovesMergedPendingEntries(t *testing.T) {
	opts := optionsWith(t, func(o *Options) { o.SwitchFilter = dropExtra })
	v := newVault(t, opts)
	if err := v.Create(testPassphrase, docWithExtra(t)); err != nil {
		t.Fatal(err)
	}
	addPending(t, v, testEntry{Name: "writer", Value: "example-2"})
	v.Forget()
	if err := v.Decrypt(testPassphrase); err != nil {
		t.Fatal(err)
	}
	plain := readBytes(t, v.plainPath())
	if decodeDoc(t, plain)["writer"] != "example-2" || strings.Contains(string(plain), "extra") {
		t.Fatalf("plain document = %s", plain)
	}
	if _, err := os.Stat(v.pendingPath()); !os.IsNotExist(err) {
		t.Errorf("the pending directory is still there: %v", err)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"filippo.io/age"
)

const (
	testPassphrase = "example passphrase"
	sealedName     = "settings.age"
)

// testDoc is the document shape the tests use: named values under a schema version.
type testDoc struct {
	Schema int               `json:"schema"`
	Values map[string]string `json:"values"`
}

// testEntry is a pending payload of the tests. Keep leaves it pending in testMerge.
type testEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Keep  bool   `json:"keep,omitempty"`
}

func encodeDoc(t *testing.T, values map[string]string) []byte {
	t.Helper()
	data, err := json.Marshal(testDoc{Schema: 1, Values: values})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeDoc(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var doc testDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("document %q: %v", data, err)
	}
	return doc.Values
}

func testMerge(document []byte, pending []Pending) ([]byte, []string, error) {
	doc := testDoc{Schema: 1, Values: map[string]string{}}
	if document != nil {
		if err := json.Unmarshal(document, &doc); err != nil {
			return nil, nil, err
		}
	}
	if doc.Values == nil {
		doc.Values = map[string]string{}
	}
	var done []string
	for _, p := range pending {
		var entry testEntry
		if err := json.Unmarshal(p.Payload, &entry); err != nil {
			return nil, nil, err
		}
		if entry.Keep {
			continue
		}
		doc.Values[entry.Name] = entry.Value
		done = append(done, p.Name)
	}
	merged, err := json.Marshal(doc)
	return merged, done, err
}

func testOptions(dir string) Options {
	return Options{
		Dir: dir,
		Files: Files{Key: "key.age", Recipient: "recipient", Document: "secrets.age", Pending: "pending",
			Plain: "secrets.json", Sealed: []string{sealedName}},
		Schemas:    []int{1},
		Merge:      testMerge,
		WorkFactor: 4,
	}
}

func newVault(t *testing.T, opts Options) *Vault {
	t.Helper()
	v, err := New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return v
}

// created returns an encrypted vault holding values, unlocked, and a second value of it that is locked.
func created(t *testing.T, values map[string]string) (unlocked, locked *Vault) {
	t.Helper()
	opts := testOptions(filepath.Join(t.TempDir(), "vault"))
	unlocked = newVault(t, opts)
	if err := unlocked.Create(testPassphrase, encodeDoc(t, values)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return unlocked, newVault(t, opts)
}

func addPending(t *testing.T, v *Vault, entry testEntry) string {
	t.Helper()
	payload, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	name, err := v.AddPending(payload)
	if err != nil {
		t.Fatalf("AddPending() error = %v", err)
	}
	return name
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// overwrite replaces path with data through the vault's own private write.
func overwrite(t *testing.T, v *Vault, path string, data []byte) {
	t.Helper()
	if err := v.write(path, data); err != nil {
		t.Fatal(err)
	}
}

func foreignIdentity(t *testing.T) *age.X25519Identity {
	t.Helper()
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func encryptFor(t *testing.T, recipient age.Recipient, plaintext []byte) []byte {
	t.Helper()
	data, err := encryptTo(plaintext, recipient)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func wantState(t *testing.T, v *Vault, want State) {
	t.Helper()
	if state, err := v.State(); err != nil || state != want {
		t.Fatalf("State() = %q, %v, want %q", state, err, want)
	}
}

func TestOptionsAreChecked(t *testing.T) {
	for name, change := range map[string]func(*Options){
		"no directory":         func(o *Options) { o.Dir = "" },
		"no key file":          func(o *Options) { o.Files.Key = "" },
		"no recipient file":    func(o *Options) { o.Files.Recipient = "" },
		"no document file":     func(o *Options) { o.Files.Document = "" },
		"no pending directory": func(o *Options) { o.Files.Pending = "" },
		"slash in a name":      func(o *Options) { o.Files.Document = "sub/secrets.age" },
		"backslash in a name":  func(o *Options) { o.Files.Document = `sub\secrets.age` },
		"parent directory":     func(o *Options) { o.Files.Pending = ".." },
		"current directory":    func(o *Options) { o.Files.Pending = "." },
		"empty sealed name":    func(o *Options) { o.Files.Sealed = []string{""} },
		"duplicate name":       func(o *Options) { o.Files.Recipient = "key.age" },
		"duplicate by case":    func(o *Options) { o.Files.Sealed = []string{"KEY.age"} },
		"plain as document":    func(o *Options) { o.Files.Plain = "secrets.age" },
		"no schema":            func(o *Options) { o.Schemas = nil },
		"schema zero":          func(o *Options) { o.Schemas = []int{0} },
		"work factor too high": func(o *Options) { o.WorkFactor = MaxWorkFactor + 1 },
		"negative work factor": func(o *Options) { o.WorkFactor = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			opts := testOptions(t.TempDir())
			change(&opts)
			if _, err := New(opts); err == nil {
				t.Fatal("New() error = nil, want the options refused")
			}
		})
	}
}

func TestCreateUnlockAndWrite(t *testing.T) {
	v, locked := created(t, map[string]string{"reader": "example-1"})
	wantState(t, v, StateUnlocked)
	wantState(t, locked, StateLocked)

	if _, err := locked.Document(); !errors.Is(err, ErrLocked) {
		t.Fatalf("Document() of a locked vault error = %v, want ErrLocked", err)
	}
	if err := locked.Write(encodeDoc(t, nil)); !errors.Is(err, ErrLocked) {
		t.Fatalf("Write() to a locked vault error = %v, want ErrLocked", err)
	}
	if merged, err := locked.Unlock(testPassphrase); err != nil || merged != 0 {
		t.Fatalf("Unlock() = %d, %v", merged, err)
	}
	doc, err := locked.Document()
	if err != nil || decodeDoc(t, doc)["reader"] != "example-1" {
		t.Fatalf("Document() = %s, %v", doc, err)
	}

	if err := locked.Write(encodeDoc(t, map[string]string{"reader": "example-2"})); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	locked.Forget()
	wantState(t, locked, StateLocked)
	if _, err := locked.Unlock(testPassphrase); err != nil {
		t.Fatal(err)
	}
	if doc, _ := locked.Document(); decodeDoc(t, doc)["reader"] != "example-2" {
		t.Fatalf("Document() after Write = %s", doc)
	}
}

func TestCreateRefusals(t *testing.T) {
	v := newVault(t, testOptions(filepath.Join(t.TempDir(), "vault")))
	if err := v.Create("", encodeDoc(t, nil)); !errors.Is(err, ErrEmptyPassphrase) {
		t.Fatalf("Create(\"\") error = %v, want ErrEmptyPassphrase", err)
	}
	wantState(t, v, StateAbsent)
	if _, err := v.Unlock(testPassphrase); !errors.Is(err, ErrAbsent) {
		t.Fatalf("Unlock() of an absent vault error = %v, want ErrAbsent", err)
	}
	if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	if err := v.Create(testPassphrase, encodeDoc(t, nil)); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create() error = %v, want ErrExists", err)
	}
	if err := v.CreateUnencrypted(encodeDoc(t, nil)); !errors.Is(err, ErrExists) {
		t.Fatalf("CreateUnencrypted() over a vault error = %v, want ErrExists", err)
	}
}

func TestWrongPassphraseIsRefused(t *testing.T) {
	_, locked := created(t, map[string]string{"reader": "example-1"})
	for _, passphrase := range []string{"", "other passphrase", testPassphrase + " "} {
		if _, err := locked.Unlock(passphrase); !errors.Is(err, ErrWrongPassphrase) {
			t.Errorf("Unlock(%q) error = %v, want ErrWrongPassphrase", passphrase, err)
		}
		wantState(t, locked, StateLocked)
	}
	if err := locked.VerifyPassphrase("other passphrase"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("VerifyPassphrase() error = %v, want ErrWrongPassphrase", err)
	}
}

func TestDocumentSchemaIsEnforced(t *testing.T) {
	v, locked := created(t, nil)
	for _, doc := range []string{``, `[]`, `null`, `{}`, `{"schema":"1"}`, `{"schema":1.5}`, `{"schema":2}`, `not json`} {
		if err := v.Write([]byte(doc)); err == nil {
			t.Errorf("Write(%q) error = nil, want it refused", doc)
		}
		if err := newVault(t, testOptions(t.TempDir())).Create(testPassphrase, []byte(doc)); err == nil {
			t.Errorf("Create(%q) error = nil, want it refused", doc)
		}
	}
	if err := v.Write([]byte(`{"schema":2}`)); !errors.Is(err, ErrUnsupportedSchema) {
		t.Errorf("Write() of schema 2 error = %v, want ErrUnsupportedSchema", err)
	}

	// A document of a foreign schema on disk is refused, not read.
	recipient, _ := age.ParseX25519Recipient(strings.TrimSpace(string(readBytes(t, locked.recipientPath()))))
	overwrite(t, v, v.documentPath(), encryptFor(t, recipient, []byte(`{"schema":2,"values":{}}`)))
	if _, err := locked.Unlock(testPassphrase); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("Unlock() of schema 2 error = %v, want ErrUnsupportedSchema", err)
	}
	wantState(t, locked, StateLocked)
}

func TestPendingEntriesNeedNoPassphrase(t *testing.T) {
	v, locked := created(t, map[string]string{"reader": "example-1"})
	addPending(t, locked, testEntry{Name: "writer", Value: "example-2"})
	addPending(t, locked, testEntry{Name: "reader", Value: "example-3"})
	if st, err := locked.Status(); err != nil || st.Pending != 2 || st.State != StateLocked {
		t.Fatalf("Status() = %+v, %v", st, err)
	}
	v.Forget()
	merged, err := v.Unlock(testPassphrase)
	if err != nil || merged != 2 {
		t.Fatalf("Unlock() = %d, %v, want 2 merged", merged, err)
	}
	doc, _ := v.Document()
	if values := decodeDoc(t, doc); values["writer"] != "example-2" || values["reader"] != "example-3" {
		t.Fatalf("merged document = %s", doc)
	}
	if st, _ := v.Status(); st.Pending != 0 {
		t.Fatalf("pending after merge = %d", st.Pending)
	}
	// An unlocked vault queues too; the entry merges on the next unlock.
	name := addPending(t, v, testEntry{Name: "later", Value: "example-4"})
	if err := v.RemovePending(name); err != nil {
		t.Fatalf("RemovePending() error = %v", err)
	}
	if st, _ := v.Status(); st.Pending != 0 {
		t.Fatalf("pending after RemovePending = %d", st.Pending)
	}
}

func TestRemovePendingRefusesOtherFiles(t *testing.T) {
	v, _ := created(t, nil)
	for _, name := range []string{"", "../key.age", `..\key.age`, "sub/x.age", "notes.txt", ".hidden.age", ".."} {
		if err := v.RemovePending(name); err == nil {
			t.Errorf("RemovePending(%q) error = nil, want it refused", name)
		}
	}
	if _, err := os.Stat(v.keyPath()); err != nil {
		t.Fatalf("key file after refused removals: %v", err)
	}
}

func TestMergeMayKeepEntriesPending(t *testing.T) {
	v, locked := created(t, nil)
	kept := addPending(t, locked, testEntry{Name: "kept", Value: "example-1", Keep: true})
	addPending(t, locked, testEntry{Name: "merged", Value: "example-2"})
	v.Forget()
	if merged, err := v.Unlock(testPassphrase); err != nil || merged != 1 {
		t.Fatalf("Unlock() = %d, %v, want 1", merged, err)
	}
	names, _ := v.pendingFiles()
	if !slices.Equal(names, []string{kept}) {
		t.Fatalf("pending after a partial merge = %v, want %v", names, kept)
	}
}

func TestMergeRejections(t *testing.T) {
	for name, merge := range map[string]MergeFunc{
		"no merge function": nil,
		"merge error": func([]byte, []Pending) ([]byte, []string, error) {
			return nil, nil, errors.New("example merge failure")
		},
		"unknown entry reported": func(doc []byte, _ []Pending) ([]byte, []string, error) {
			return []byte(`{"schema":1}`), []string{"0000.age"}, nil
		},
		"invalid merged document": func(_ []byte, p []Pending) ([]byte, []string, error) {
			return []byte(`{"values":{}}`), []string{p[0].Name}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			v, _ := created(t, map[string]string{"reader": "example-1"})
			addPending(t, v, testEntry{Name: "writer", Value: "example-2"})
			before := readBytes(t, v.documentPath())
			opts := v.opts
			opts.Merge = merge
			locked := newVault(t, opts)
			if _, err := locked.Unlock(testPassphrase); err == nil {
				t.Fatal("Unlock() error = nil, want the merge refused")
			}
			wantState(t, locked, StateLocked)
			if st, _ := locked.Status(); st.Pending != 1 {
				t.Fatalf("pending after a refused merge = %d, want 1", st.Pending)
			}
			if !bytes.Equal(before, readBytes(t, v.documentPath())) {
				t.Fatal("the document changed after a refused merge")
			}
		})
	}
	_, locked := created(t, nil)
	addPending(t, locked, testEntry{Name: "writer", Value: "example-2"})
	opts := locked.opts
	opts.Merge = nil
	if _, err := newVault(t, opts).Unlock(testPassphrase); !errors.Is(err, ErrPendingUnmerged) {
		t.Fatalf("Unlock() without a MergeFunc error = %v, want ErrPendingUnmerged", err)
	}
}

func TestTamperedFilesAreRefused(t *testing.T) {
	flip := func(data []byte) []byte {
		data = slices.Clone(data)
		data[len(data)-1] ^= 0x01
		return data
	}
	for name, tamper := range map[string]func(t *testing.T, v *Vault){
		"key body changed": func(t *testing.T, v *Vault) {
			overwrite(t, v, v.keyPath(), flip(readBytes(t, v.keyPath())))
		},
		"key from another vault": func(t *testing.T, v *Vault) {
			other, _ := created(t, nil)
			overwrite(t, v, v.keyPath(), readBytes(t, other.keyPath()))
		},
		"recipient of another vault": func(t *testing.T, v *Vault) {
			overwrite(t, v, v.recipientPath(), []byte(foreignIdentity(t).Recipient().String()+"\n"))
		},
		"recipient not age text": func(t *testing.T, v *Vault) {
			overwrite(t, v, v.recipientPath(), []byte("example-not-a-recipient\n"))
		},
		"document body changed": func(t *testing.T, v *Vault) {
			overwrite(t, v, v.documentPath(), flip(readBytes(t, v.documentPath())))
		},
		"document truncated": func(t *testing.T, v *Vault) {
			data := readBytes(t, v.documentPath())
			overwrite(t, v, v.documentPath(), data[:len(data)/2])
		},
		"document for a foreign recipient": func(t *testing.T, v *Vault) {
			overwrite(t, v, v.documentPath(), encryptFor(t, foreignIdentity(t).Recipient(), encodeDoc(t, nil)))
		},
		"pending entry for a foreign recipient": func(t *testing.T, v *Vault) {
			overwrite(t, v, filepath.Join(v.pendingPath(), "00000000000000000000000000000000.age"),
				encryptFor(t, foreignIdentity(t).Recipient(), []byte(`{"name":"x","value":"y"}`)))
		},
		"pending entry changed": func(t *testing.T, v *Vault) {
			name := addPending(t, v, testEntry{Name: "writer", Value: "example-2"})
			path := filepath.Join(v.pendingPath(), name)
			overwrite(t, v, path, flip(readBytes(t, path)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			v, locked := created(t, map[string]string{"reader": "example-1"})
			tamper(t, v)
			_, err := locked.Unlock(testPassphrase)
			if !errors.Is(err, ErrDamaged) && !errors.Is(err, ErrKeyMismatch) {
				t.Fatalf("Unlock() error = %v, want ErrDamaged or ErrKeyMismatch", err)
			}
			wantState(t, locked, StateLocked)
		})
	}
}

func TestForeignRecipientIsAMismatch(t *testing.T) {
	v, locked := created(t, nil)
	overwrite(t, v, v.recipientPath(), []byte(foreignIdentity(t).Recipient().String()+"\n"))
	if _, err := locked.Unlock(testPassphrase); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("Unlock() error = %v, want ErrKeyMismatch", err)
	}
	if err := locked.VerifyPassphrase(testPassphrase); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("VerifyPassphrase() error = %v, want ErrKeyMismatch", err)
	}
	if _, err := OpenWithIdentity(v.opts, foreignIdentity(t)); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("OpenWithIdentity() with the recipient's foreign key error = %v, want ErrKeyMismatch", err)
	}
}

func TestOpenWithIdentity(t *testing.T) {
	v, locked := created(t, map[string]string{"reader": "example-1"})
	addPending(t, locked, testEntry{Name: "writer", Value: "example-2"})
	snap, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locked.Snapshot(); !errors.Is(err, ErrLocked) {
		t.Fatalf("Snapshot() of a locked vault error = %v, want ErrLocked", err)
	}
	opened, err := OpenWithIdentity(v.opts, snap.Identity)
	if err != nil {
		t.Fatalf("OpenWithIdentity() error = %v", err)
	}
	doc, _ := opened.Document()
	if !bytes.Equal(doc, snap.Document) {
		t.Fatalf("Document() = %s, want %s", doc, snap.Document)
	}
	if st, _ := opened.Status(); st.Pending != 1 || st.State != StateUnlocked {
		t.Fatalf("Status() = %+v, want unlocked with the pending entry kept", st)
	}
	if _, err := OpenWithIdentity(v.opts, foreignIdentity(t)); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("OpenWithIdentity() with a foreign key error = %v, want ErrKeyMismatch", err)
	}
	if _, err := OpenWithIdentity(v.opts, nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("OpenWithIdentity(nil) error = %v, want ErrLocked", err)
	}
	if _, err := OpenWithIdentity(testOptions(t.TempDir()), snap.Identity); !errors.Is(err, ErrAbsent) {
		t.Fatalf("OpenWithIdentity() of an absent vault error = %v, want ErrAbsent", err)
	}
}

func TestChangePassphrase(t *testing.T) {
	v, locked := created(t, map[string]string{"reader": "example-1"})
	recipient, document, key := readBytes(t, v.recipientPath()), readBytes(t, v.documentPath()), readBytes(t, v.keyPath())

	if err := locked.ChangePassphrase("other passphrase", "new passphrase"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("ChangePassphrase() with a wrong old passphrase error = %v", err)
	}
	if err := locked.ChangePassphrase(testPassphrase, ""); !errors.Is(err, ErrEmptyPassphrase) {
		t.Fatalf("ChangePassphrase() to \"\" error = %v", err)
	}
	if !bytes.Equal(key, readBytes(t, v.keyPath())) {
		t.Fatal("a refused change rewrote the key file")
	}
	if err := locked.ChangePassphrase(testPassphrase, "new passphrase"); err != nil {
		t.Fatalf("ChangePassphrase() error = %v", err)
	}
	if !bytes.Equal(recipient, readBytes(t, v.recipientPath())) || !bytes.Equal(document, readBytes(t, v.documentPath())) {
		t.Fatal("a passphrase change touched more than the key file")
	}
	wantState(t, locked, StateLocked)
	if err := locked.VerifyPassphrase(testPassphrase); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("the old passphrase still works: %v", err)
	}
	if _, err := locked.Unlock("new passphrase"); err != nil {
		t.Fatalf("Unlock() with the new passphrase error = %v", err)
	}
	if _, err := v.Document(); err != nil {
		t.Fatalf("the value unlocked before the change: %v", err)
	}
}

func TestVerifyPassphraseHasNoEffect(t *testing.T) {
	_, locked := created(t, nil)
	addPending(t, locked, testEntry{Name: "writer", Value: "example-2"})
	if err := locked.VerifyPassphrase(testPassphrase); err != nil {
		t.Fatal(err)
	}
	wantState(t, locked, StateLocked)
	if st, _ := locked.Status(); st.Pending != 1 {
		t.Fatalf("VerifyPassphrase() merged pending entries: %+v", st)
	}
}

func TestUnencryptedModeIsOptIn(t *testing.T) {
	opts := testOptions(filepath.Join(t.TempDir(), "vault"))
	opts.Files.Plain = ""
	v := newVault(t, opts)
	if err := v.CreateUnencrypted(encodeDoc(t, nil)); !errors.Is(err, ErrUnencryptedDisabled) {
		t.Fatalf("CreateUnencrypted() error = %v, want ErrUnencryptedDisabled", err)
	}
	wantState(t, v, StateAbsent)
	if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	if err := v.Decrypt(testPassphrase); !errors.Is(err, ErrUnencryptedDisabled) {
		t.Fatalf("Decrypt() error = %v, want ErrUnencryptedDisabled", err)
	}
	wantState(t, v, StateUnlocked)
}

func TestUnencryptedModeAndSwitching(t *testing.T) {
	opts := testOptions(filepath.Join(t.TempDir(), "vault"))
	v := newVault(t, opts)
	if err := v.CreateUnencrypted(encodeDoc(t, map[string]string{"reader": "example-1"})); err != nil {
		t.Fatal(err)
	}
	wantState(t, v, StateUnencrypted)
	if _, err := v.Unlock(testPassphrase); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("Unlock() error = %v, want ErrNotEncrypted", err)
	}
	if _, err := v.AddPending([]byte(`{}`)); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("AddPending() error = %v, want ErrNotEncrypted", err)
	}
	if err := v.Write(encodeDoc(t, map[string]string{"reader": "example-2"})); err != nil {
		t.Fatal(err)
	}
	if err := v.Encrypt(""); !errors.Is(err, ErrEmptyPassphrase) {
		t.Fatalf("Encrypt(\"\") error = %v", err)
	}
	if err := v.Encrypt(testPassphrase); err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if err := v.Encrypt(testPassphrase); !errors.Is(err, ErrEncrypted) {
		t.Fatalf("second Encrypt() error = %v, want ErrEncrypted", err)
	}
	if _, err := os.Stat(v.plainPath()); !os.IsNotExist(err) {
		t.Fatalf("the plain document is still there: %v", err)
	}
	if err := v.WriteSealed(sealedName, []byte("example settings")); err != nil {
		t.Fatal(err)
	}

	locked := newVault(t, opts)
	addPending(t, locked, testEntry{Name: "writer", Value: "example-3"})
	if err := locked.Decrypt("other passphrase"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("Decrypt() with a wrong passphrase error = %v", err)
	}
	wantState(t, locked, StateLocked)
	if err := locked.Decrypt(testPassphrase); err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	wantState(t, locked, StateUnencrypted)
	doc, err := locked.Document()
	if err != nil {
		t.Fatal(err)
	}
	if values := decodeDoc(t, doc); values["reader"] != "example-2" || values["writer"] != "example-3" {
		t.Fatalf("decrypted document = %s", doc)
	}
	for _, name := range []string{"key.age", "recipient", "secrets.age", sealedName, "pending"} {
		if _, err := os.Stat(filepath.Join(opts.Dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s is still there after Decrypt: %v", name, err)
		}
	}
}

func TestDecryptNeedsEveryPendingEntry(t *testing.T) {
	v, locked := created(t, nil)
	addPending(t, locked, testEntry{Name: "kept", Value: "example-1", Keep: true})
	if err := locked.Decrypt(testPassphrase); !errors.Is(err, ErrPendingUnmerged) {
		t.Fatalf("Decrypt() error = %v, want ErrPendingUnmerged", err)
	}
	wantState(t, locked, StateLocked)
	if _, err := os.Stat(v.plainPath()); !os.IsNotExist(err) {
		t.Fatalf("a refused Decrypt wrote the plain document: %v", err)
	}
}

func TestCheckpointRestores(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	v := newVault(t, testOptions(dir))
	restore, err := v.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatalf("restore() error = %v", err)
	}
	wantState(t, v, StateAbsent)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the vault directory the checkpoint did not find is still there: %v", err)
	}

	v, locked := created(t, map[string]string{"reader": "example-1"})
	before := readBytes(t, v.documentPath())
	restore, err = locked.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	addPending(t, locked, testEntry{Name: "writer", Value: "example-2"})
	if err := v.Write(encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	if err := locked.ChangePassphrase(testPassphrase, "new passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if st, _ := locked.Status(); st.Pending != 0 || st.State != StateLocked {
		t.Fatalf("Status() after restore = %+v", st)
	}
	if !bytes.Equal(before, readBytes(t, v.documentPath())) {
		t.Fatal("restore did not put the document back")
	}
	if err := locked.VerifyPassphrase(testPassphrase); err != nil {
		t.Fatalf("restore did not put the key back: %v", err)
	}
}

func TestInterruptedWrites(t *testing.T) {
	failing := func(target string) func(string, string) error {
		return func(oldpath, newpath string) error {
			if filepath.Base(newpath) == target {
				return errors.New("example interruption")
			}
			return os.Rename(oldpath, newpath)
		}
	}
	noTemporaryFiles := func(t *testing.T, dirs ...string) {
		t.Helper()
		for _, dir := range dirs {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".tmp") {
					t.Errorf("temporary file %s left behind", e.Name())
				}
			}
		}
	}

	t.Run("create", func(t *testing.T) {
		v := newVault(t, testOptions(filepath.Join(t.TempDir(), "vault")))
		v.rename = failing("key.age")
		if err := v.Create(testPassphrase, encodeDoc(t, nil)); err == nil {
			t.Fatal("Create() error = nil")
		}
		wantState(t, v, StateAbsent)
		noTemporaryFiles(t, v.Dir())
		v.rename = os.Rename
		if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
			t.Fatalf("Create() after an interrupted one error = %v", err)
		}
	})
	t.Run("write", func(t *testing.T) {
		v, locked := created(t, map[string]string{"reader": "example-1"})
		v.rename = failing("secrets.age")
		if err := v.Write(encodeDoc(t, map[string]string{"reader": "example-2"})); err == nil {
			t.Fatal("Write() error = nil")
		}
		noTemporaryFiles(t, v.Dir())
		if doc, _ := v.Document(); decodeDoc(t, doc)["reader"] != "example-1" {
			t.Fatalf("held document after a failed write = %s", doc)
		}
		if _, err := locked.Unlock(testPassphrase); err != nil {
			t.Fatal(err)
		}
		if doc, _ := locked.Document(); decodeDoc(t, doc)["reader"] != "example-1" {
			t.Fatalf("document on disk after a failed write = %s", doc)
		}
	})
	t.Run("merge", func(t *testing.T) {
		_, locked := created(t, nil)
		addPending(t, locked, testEntry{Name: "writer", Value: "example-2"})
		locked.rename = failing("secrets.age")
		if _, err := locked.Unlock(testPassphrase); err == nil {
			t.Fatal("Unlock() error = nil")
		}
		wantState(t, locked, StateLocked)
		if st, _ := locked.Status(); st.Pending != 1 {
			t.Fatalf("pending after a failed merge = %d, want 1", st.Pending)
		}
	})
	t.Run("leftovers", func(t *testing.T) {
		_, locked := created(t, nil)
		pending := locked.pendingPath()
		if err := os.MkdirAll(pending, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{".0123.age.4567.tmp", "notes.txt"} {
			overwrite(t, locked, filepath.Join(pending, name), []byte("example"))
		}
		if st, _ := locked.Status(); st.Pending != 0 {
			t.Fatalf("Status().Pending = %d, want leftovers ignored", st.Pending)
		}
		if _, err := locked.Unlock(testPassphrase); err != nil {
			t.Fatalf("Unlock() with leftovers error = %v", err)
		}
	})
}

func TestSealedFiles(t *testing.T) {
	const label = "example settings v1"
	v, locked := created(t, nil)
	if err := locked.WriteSealed(sealedName, []byte("x")); !errors.Is(err, ErrLocked) {
		t.Fatalf("WriteSealed() while locked error = %v, want ErrLocked", err)
	}
	if err := v.WriteSealed("other.age", []byte("x")); err == nil {
		t.Fatal("WriteSealed() of an undeclared file error = nil")
	}
	if _, err := v.ReadSealed(sealedName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadSealed() of a missing file error = %v, want ErrNotExist", err)
	}
	id, _ := v.Identity()
	mac, err := MAC(id, label, []byte("example settings"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.WriteSealed(sealedName, []byte("example settings")); err != nil {
		t.Fatal(err)
	}
	data, err := v.ReadSealed(sealedName)
	if err != nil || string(data) != "example settings" {
		t.Fatalf("ReadSealed() = %q, %v", data, err)
	}
	if err := VerifyMAC(id, label, data, mac); err != nil {
		t.Fatalf("VerifyMAC() error = %v", err)
	}

	// Anyone can encrypt to the recipient; only the key holder can compute the check value.
	outsider := foreignIdentity(t)
	forged, _ := MAC(outsider, label, []byte("forged settings"))
	overwrite(t, v, v.path(sealedName), encryptFor(t, id.Recipient(), []byte("forged settings")))
	data, err = v.ReadSealed(sealedName)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyMAC(id, label, data, forged); !errors.Is(err, ErrMACMismatch) {
		t.Fatalf("VerifyMAC() of a forged check value error = %v, want ErrMACMismatch", err)
	}
	if err := VerifyMAC(id, "example other label", []byte("example settings"), mac); !errors.Is(err, ErrMACMismatch) {
		t.Fatalf("VerifyMAC() under another label error = %v, want ErrMACMismatch", err)
	}
	overwrite(t, v, v.path(sealedName), encryptFor(t, outsider.Recipient(), []byte("example")))
	if _, err := v.ReadSealed(sealedName); !errors.Is(err, ErrDamaged) {
		t.Fatalf("ReadSealed() of a file for another key error = %v, want ErrDamaged", err)
	}
}

func TestDeriveKey(t *testing.T) {
	id := foreignIdentity(t)
	a, err := DeriveKey(id, "example label a")
	if err != nil || len(a) != 32 {
		t.Fatalf("DeriveKey() = %x, %v", a, err)
	}
	again, _ := DeriveKey(id, "example label a")
	b, _ := DeriveKey(id, "example label b")
	other, _ := DeriveKey(foreignIdentity(t), "example label a")
	if !bytes.Equal(a, again) || bytes.Equal(a, b) || bytes.Equal(a, other) {
		t.Fatal("DeriveKey() is not deterministic per key and label")
	}
	if _, err := DeriveKey(id, ""); err == nil {
		t.Fatal("DeriveKey() with an empty label error = nil")
	}
	if _, err := DeriveKey(nil, "example label a"); !errors.Is(err, ErrLocked) {
		t.Fatalf("DeriveKey(nil) error = %v, want ErrLocked", err)
	}
	if _, err := MAC(nil, "example label a", nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("MAC(nil) error = %v, want ErrLocked", err)
	}
}

func TestWorkFactor(t *testing.T) {
	stanza := func(t *testing.T, v *Vault) string {
		t.Helper()
		header, _, _ := strings.Cut(string(readBytes(t, v.keyPath())), "\n---")
		for _, line := range strings.Split(header, "\n") {
			if strings.HasPrefix(line, "-> scrypt ") {
				fields := strings.Fields(line)
				return fields[len(fields)-1]
			}
		}
		t.Fatalf("no scrypt stanza in %q", header)
		return ""
	}
	opts := testOptions(filepath.Join(t.TempDir(), "vault"))
	opts.WorkFactor = 0
	v := newVault(t, opts)
	if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	if got := stanza(t, v); got != "18" {
		t.Fatalf("default work factor = %s, want 18", got)
	}
	opts = testOptions(filepath.Join(t.TempDir(), "vault"))
	v = newVault(t, opts)
	if err := v.Create(testPassphrase, encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	if got := stanza(t, v); got != "4" {
		t.Fatalf("work factor = %s, want 4", got)
	}
}

func TestRecipientAndStatus(t *testing.T) {
	v, locked := created(t, nil)
	want := strings.TrimSpace(string(readBytes(t, v.recipientPath())))
	for _, vault := range []*Vault{v, locked} {
		if got, err := vault.Recipient(); err != nil || got != want {
			t.Fatalf("Recipient() = %q, %v, want %q", got, err, want)
		}
	}
	absent := newVault(t, testOptions(t.TempDir()))
	if _, err := absent.Recipient(); !errors.Is(err, ErrAbsent) {
		t.Fatalf("Recipient() of an absent vault error = %v, want ErrAbsent", err)
	}
	if st, err := absent.Status(); err != nil || st.State != StateAbsent || st.Pending != 0 {
		t.Fatalf("Status() of an absent vault = %+v, %v", st, err)
	}
	if _, err := locked.Identity(); !errors.Is(err, ErrLocked) {
		t.Fatalf("Identity() of a locked vault error = %v, want ErrLocked", err)
	}
}

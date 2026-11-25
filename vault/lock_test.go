package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	lockName          = ".vault.lock"
	anotherPassphrase = "another example passphrase"
)

func lockedOptions(dir string) Options {
	opts := testOptions(dir)
	opts.Files.Lock = lockName
	return opts
}

// lockedPair returns two unlocked values of one encrypted vault with a write lock, as two processes hold it.
func lockedPair(t *testing.T, values map[string]string) (a, b *Vault) {
	t.Helper()
	opts := lockedOptions(filepath.Join(t.TempDir(), "vault"))
	a = newVault(t, opts)
	if err := a.Create(testPassphrase, encodeDoc(t, values)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	b = newVault(t, opts)
	if _, err := b.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	return a, b
}

// increment counts the value name up by one.
func increment(name string) UpdateFunc {
	return func(document []byte, _ []Pending) ([]byte, []string, error) {
		var doc testDoc
		if err := json.Unmarshal(document, &doc); err != nil {
			return nil, nil, err
		}
		if doc.Values == nil {
			doc.Values = map[string]string{}
		}
		n, _ := strconv.Atoi(doc.Values[name])
		doc.Values[name] = strconv.Itoa(n + 1)
		updated, err := json.Marshal(doc)
		return updated, nil, err
	}
}

// replaceToken sets the value token to value only while it still holds old, and records what it found.
func replaceToken(old, value string, found *string) UpdateFunc {
	return func(document []byte, _ []Pending) ([]byte, []string, error) {
		var doc testDoc
		if err := json.Unmarshal(document, &doc); err != nil {
			return nil, nil, err
		}
		if found != nil {
			*found = doc.Values["token"]
		}
		if doc.Values["token"] != old {
			return nil, nil, fmt.Errorf("token moved on: %w", ErrNoChange)
		}
		doc.Values["token"] = value
		updated, err := json.Marshal(doc)
		return updated, nil, err
	}
}

// diskValues reads the encrypted document on disk through a value of its own.
func diskValues(t *testing.T, opts Options) map[string]string {
	t.Helper()
	v := newVault(t, opts)
	if _, err := v.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	return decodeDoc(t, heldDocument(t, v))
}

func TestLockedUpdatesLoseNoChange(t *testing.T) {
	a, b := lockedPair(t, map[string]string{})
	const rounds = 25
	var wg sync.WaitGroup
	for _, v := range []*Vault{a, b} {
		wg.Go(func() {
			for range rounds {
				if _, err := v.Update(increment("count")); err != nil {
					t.Errorf("Update() error = %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
	if got := diskValues(t, a.opts)["count"]; got != strconv.Itoa(2*rounds) {
		t.Fatalf("count = %s, want %d: an update was based on a document read before another one", got, 2*rounds)
	}
}

// Of a Write and an Update at the same time the Update either comes first or sees the written document, so
// what Write stored is never overwritten with a document read before it.
func TestLockedWriteAndUpdateKeepTheWrite(t *testing.T) {
	a, b := lockedPair(t, map[string]string{})
	for round := range 10 {
		written := encodeDoc(t, map[string]string{"round": strconv.Itoa(round)})
		var wg sync.WaitGroup
		wg.Go(func() {
			if err := b.Write(written); err != nil {
				t.Errorf("Write() error = %v", err)
			}
		})
		wg.Go(func() {
			if _, err := a.Update(increment("count")); err != nil {
				t.Errorf("Update() error = %v", err)
			}
		})
		wg.Wait()
		if got := diskValues(t, a.opts)["round"]; got != strconv.Itoa(round) {
			t.Fatalf("round %d: the document on disk holds round %q, want the written one", round, got)
		}
	}
}

// A Decrypt by another process at the same time as an Update either carries the update into the plain
// document or makes the Update fail with ErrSwitched; it never lands in an encrypted file beside it.
func TestLockedDecryptAndUpdate(t *testing.T) {
	for round := range 5 {
		a, b := lockedPair(t, map[string]string{"count": "0"})
		var updateErr, decryptErr error
		var wg sync.WaitGroup
		wg.Go(func() { _, updateErr = a.Update(increment("count")) })
		wg.Go(func() { decryptErr = b.Decrypt(testPassphrase) })
		wg.Wait()
		if decryptErr != nil {
			t.Fatalf("round %d: Decrypt() error = %v", round, decryptErr)
		}
		plain := decodeDoc(t, readBytes(t, a.plainPath()))
		switch {
		case updateErr == nil && plain["count"] != "1":
			t.Fatalf("round %d: the plain document holds count %q, want the update", round, plain["count"])
		case updateErr != nil && !errors.Is(updateErr, ErrSwitched):
			t.Fatalf("round %d: Update() error = %v, want nil or ErrSwitched", round, updateErr)
		}
		if exists, err := fileExists(a.documentPath()); err != nil || exists {
			t.Fatalf("round %d: an encrypted document lies beside the plain one: %v, %v", round, exists, err)
		}
	}
}

func TestLockedWritesWaitForTheLock(t *testing.T) {
	document := func(t *testing.T) []byte { return encodeDoc(t, map[string]string{"reader": "example-2"}) }
	encrypted := func(t *testing.T) Options {
		a, _ := lockedPair(t, map[string]string{"reader": "example-1"})
		return a.opts
	}
	unlocked := func(t *testing.T, opts Options) *Vault {
		v := newVault(t, opts)
		if _, err := v.Unlock(testPassphrase); err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		name string
		// prepare returns the options of the vault and the write that must wait for the lock.
		prepare func(t *testing.T) (Options, func() error)
	}{
		{"create", func(t *testing.T) (Options, func() error) {
			opts := lockedOptions(filepath.Join(t.TempDir(), "vault"))
			return opts, func() error { return newVault(t, opts).Create(testPassphrase, document(t)) }
		}},
		{"create unencrypted", func(t *testing.T) (Options, func() error) {
			opts := lockedOptions(filepath.Join(t.TempDir(), "vault"))
			return opts, func() error { return newVault(t, opts).CreateUnencrypted(document(t)) }
		}},
		{"unlock", func(t *testing.T) (Options, func() error) {
			opts := encrypted(t)
			v := newVault(t, opts)
			addPending(t, v, testEntry{Name: "writer", Value: "example-3"})
			return opts, func() error { _, err := v.Unlock(testPassphrase); return err }
		}},
		{"write", func(t *testing.T) (Options, func() error) {
			opts := encrypted(t)
			v := unlocked(t, opts)
			return opts, func() error { return v.Write(document(t)) }
		}},
		{"write with undo", func(t *testing.T) (Options, func() error) {
			opts := encrypted(t)
			v := unlocked(t, opts)
			return opts, func() error { _, err := v.WriteWithUndo(document(t)); return err }
		}},
		{"update", func(t *testing.T) (Options, func() error) {
			opts := encrypted(t)
			v := unlocked(t, opts)
			return opts, func() error { _, err := v.Update(increment("count")); return err }
		}},
		{"undo", func(t *testing.T) (Options, func() error) {
			opts := encrypted(t)
			undo, err := unlocked(t, opts).Update(increment("count"))
			if err != nil {
				t.Fatal(err)
			}
			return opts, undo
		}},
		{"restore", func(t *testing.T) (Options, func() error) {
			opts := encrypted(t)
			v := unlocked(t, opts)
			restore, err := v.Checkpoint()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.Update(increment("count")); err != nil {
				t.Fatal(err)
			}
			return opts, restore
		}},
		{"change passphrase", func(t *testing.T) (Options, func() error) {
			opts := encrypted(t)
			v := newVault(t, opts)
			return opts, func() error { return v.ChangePassphrase(testPassphrase, anotherPassphrase) }
		}},
		{"decrypt", func(t *testing.T) (Options, func() error) {
			opts := encrypted(t)
			v := newVault(t, opts)
			return opts, func() error { return v.Decrypt(testPassphrase) }
		}},
		{"encrypt", func(t *testing.T) (Options, func() error) {
			opts := lockedOptions(filepath.Join(t.TempDir(), "vault"))
			v := newVault(t, opts)
			if err := v.CreateUnencrypted(document(t)); err != nil {
				t.Fatal(err)
			}
			return opts, func() error { return v.Encrypt(testPassphrase) }
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, write := c.prepare(t)
			holder := newVault(t, opts)
			release, err := holder.lockWrites(true)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- write() }()
			select {
			case err := <-done:
				release()
				t.Fatalf("the write finished while another value held the lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			release()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("the write failed once the lock was free: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the write did not finish once the lock was free")
			}
		})
	}
}

func TestLockedUpdateSeesTheDiskAndMayChangeNothing(t *testing.T) {
	a, b := lockedPair(t, map[string]string{"token": "example-old"})
	name := addPending(t, a, testEntry{Name: "writer", Value: "example-3"})
	if _, err := b.Update(replaceToken("example-old", "example-b", nil)); err != nil {
		t.Fatalf("Update() by the other value error = %v", err)
	}
	before := readBytes(t, a.documentPath())

	var found string
	undo, err := a.Update(replaceToken("example-old", "example-a", &found))
	if err != nil || undo == nil {
		t.Fatalf("Update() = %v, %v, want an undo and no error", undo != nil, err)
	}
	if found != "example-b" {
		t.Fatalf("the change saw token %q, want the one on disk", found)
	}
	if !bytes.Equal(readBytes(t, a.documentPath()), before) {
		t.Fatal("an update that changed nothing wrote the document")
	}
	if got := decodeDoc(t, heldDocument(t, a))["token"]; got != "example-b" {
		t.Fatalf("the value holds token %q, want the one on disk", got)
	}
	if err := undo(); err != nil || !bytes.Equal(readBytes(t, a.documentPath()), before) {
		t.Fatalf("undo() of an update without a write = %v or changed the document", err)
	}
	if names := pendingNames(t, a); len(names) != 1 || names[0] != name {
		t.Fatalf("pending entries = %v, want %s untouched", names, name)
	}

	if _, err := a.Update(replaceToken("example-b", "example-a", nil)); err != nil {
		t.Fatalf("Update() on the current token error = %v", err)
	}
	if got := diskValues(t, a.opts)["token"]; got != "example-a" {
		t.Fatalf("token on disk = %q, want the replacement", got)
	}
}

func TestUpdateWithoutChangeWritesNothing(t *testing.T) {
	v, _ := created(t, map[string]string{"token": "example-old"})
	before := vaultFiles(t, v.Dir())
	undo, err := v.Update(replaceToken("example-other", "example-new", nil))
	if err != nil || undo == nil {
		t.Fatalf("Update() = %v, %v, want an undo and no error", undo != nil, err)
	}
	wantFiles(t, vaultFiles(t, v.Dir()), before)
	if err := undo(); err != nil {
		t.Fatalf("undo() error = %v", err)
	}
	wantFiles(t, vaultFiles(t, v.Dir()), before)
}

func TestLockNameIsChecked(t *testing.T) {
	for name, lock := range map[string]string{
		"slash":         "sub/vault.lock",
		"parent":        "..",
		"document":      "secrets.age",
		"pending":       "PENDING",
		"sealed":        sealedName,
		"plain by case": "Secrets.json",
	} {
		t.Run(name, func(t *testing.T) {
			opts := testOptions(t.TempDir())
			opts.Files.Lock = lock
			if _, err := New(opts); err == nil {
				t.Fatalf("New() with lock file %q error = nil, want the options refused", lock)
			}
		})
	}
}

func TestLockedWriteRefusals(t *testing.T) {
	document := []byte(`{"schema":1,"values":{"reader":"example-9"}}`)
	write := func(v *Vault) error { return v.Write(document) }
	update := func(v *Vault) error {
		ran := false
		_, err := v.Update(func([]byte, []Pending) ([]byte, []string, error) {
			ran = true
			return nil, nil, ErrNoChange
		})
		if ran {
			return errors.New("the change ran although the vault on disk was refused")
		}
		return err
	}
	withUndo := func(v *Vault) error {
		_, err := v.WriteWithUndo(document)
		return err
	}

	t.Run("absent vault", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "vault")
		v := newVault(t, lockedOptions(dir))
		for _, err := range []error{write(v), update(v), withUndo(v), v.ChangePassphrase(testPassphrase, "x")} {
			if !errors.Is(err, ErrAbsent) {
				t.Fatalf("error = %v, want ErrAbsent", err)
			}
		}
		if _, err := v.Unlock(testPassphrase); !errors.Is(err, ErrAbsent) {
			t.Fatalf("Unlock() error = %v, want ErrAbsent", err)
		}
		if exists, err := fileExists(dir); err != nil || exists {
			t.Fatalf("a refused write created the vault directory: %v, %v", exists, err)
		}
	})

	t.Run("lock file cannot be opened", func(t *testing.T) {
		a, _ := lockedPair(t, map[string]string{"reader": "example-1"})
		before := vaultFiles(t, a.Dir())
		lock := a.path(lockName)
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(lock, dirMode); err != nil {
			t.Fatal(err)
		}
		delete(before, lockName)
		for _, err := range []error{write(a), update(a), withUndo(a), a.ChangePassphrase(testPassphrase, "x")} {
			if err == nil {
				t.Fatal("a write without its lock error = nil")
			}
		}
		wantFiles(t, vaultFiles(t, a.Dir()), before)
	})

	t.Run("damaged document", func(t *testing.T) {
		a, b := lockedPair(t, map[string]string{"reader": "example-1"})
		addPending(t, a, testEntry{Name: "writer", Value: "example-3"})
		overwrite(t, b, b.documentPath(), []byte("not an age file"))
		held := heldDocument(t, a)
		before := vaultFiles(t, a.Dir())
		if err := update(a); !errors.Is(err, ErrDamaged) {
			t.Fatalf("Update() error = %v, want ErrDamaged", err)
		}
		wantFiles(t, vaultFiles(t, a.Dir()), before)
		if !bytes.Equal(heldDocument(t, a), held) {
			t.Fatal("a refused update changed the held document")
		}
	})

	t.Run("damaged recipient", func(t *testing.T) {
		a, b := lockedPair(t, map[string]string{"reader": "example-1"})
		overwrite(t, b, b.recipientPath(), []byte("not a recipient\n"))
		before := vaultFiles(t, a.Dir())
		for _, err := range []error{write(a), update(a), withUndo(a)} {
			if !errors.Is(err, ErrDamaged) {
				t.Fatalf("error = %v, want ErrDamaged", err)
			}
		}
		wantFiles(t, vaultFiles(t, a.Dir()), before)
	})

	t.Run("decrypted by another value", func(t *testing.T) {
		a, b := lockedPair(t, map[string]string{"reader": "example-1"})
		if err := b.Decrypt(testPassphrase); err != nil {
			t.Fatal(err)
		}
		before := vaultFiles(t, a.Dir())
		for _, err := range []error{write(a), update(a), withUndo(a)} {
			if !errors.Is(err, ErrSwitched) {
				t.Fatalf("error = %v, want ErrSwitched", err)
			}
		}
		wantFiles(t, vaultFiles(t, a.Dir()), before)
	})

	t.Run("key replaced by another value", func(t *testing.T) {
		a, b := lockedPair(t, map[string]string{"reader": "example-1"})
		if err := b.Decrypt(testPassphrase); err != nil {
			t.Fatal(err)
		}
		if err := b.Encrypt(anotherPassphrase); err != nil {
			t.Fatal(err)
		}
		before := vaultFiles(t, a.Dir())
		for _, err := range []error{write(a), update(a), withUndo(a)} {
			if !errors.Is(err, ErrSwitched) {
				t.Fatalf("error = %v, want ErrSwitched", err)
			}
		}
		wantFiles(t, vaultFiles(t, a.Dir()), before)
	})
}

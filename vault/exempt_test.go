package vault

import (
	"errors"
	"path/filepath"
	"testing"
)

// exemptModes are the two vault modes with a write lock: each returns two values of one vault, as two
// processes hold it, and reads the document on disk.
var exemptModes = []struct {
	name   string
	pair   func(t *testing.T, values map[string]string) (a, b *Vault)
	onDisk func(t *testing.T, v *Vault) map[string]string
}{
	{"encrypted", lockedPair, documentOnDisk},
	{"unencrypted", plainPair, plainOnDisk},
}

func exempt(t *testing.T, v *Vault, change UpdateFunc) func() error {
	t.Helper()
	undo, err := v.UpdateWithOptions(change, ExemptFromCheckpoints())
	if err != nil {
		t.Fatalf("UpdateWithOptions() error = %v", err)
	}
	return undo
}

func mustUpdate(t *testing.T, v *Vault, change UpdateFunc) {
	t.Helper()
	if _, err := v.Update(change); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
}

func takeCheckpoint(t *testing.T, v *Vault) func() error {
	t.Helper()
	restore, err := v.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	return restore
}

func TestRestoreKeepsAnExemptUpdate(t *testing.T) {
	for _, m := range exemptModes {
		t.Run(m.name, func(t *testing.T) {
			a, b := m.pair(t, map[string]string{"reader": "example-1", "token": "example-old"})
			a.opts.Revert = testRevert
			restore := takeCheckpoint(t, a)
			mustUpdate(t, a, setAndMerge("reader", "example-2"))
			exempt(t, a, replaceToken("example-old", "example-new", nil))
			mustUpdate(t, b, setAndMerge("writer", "example-3"))
			mustUpdate(t, a, setAndMerge("extra", "example-7"))
			if err := restore(); err != nil {
				t.Fatalf("restore() error = %v", err)
			}
			want := map[string]string{"reader": "example-1", "token": "example-new", "writer": "example-3"}
			wantValues(t, m.onDisk(t, a), want)
			wantValues(t, decodeDoc(t, heldDocument(t, a)), want)
		})
	}
}

func TestRestoreWithAnExemptUpdateNeedsRevert(t *testing.T) {
	for _, m := range exemptModes {
		t.Run(m.name, func(t *testing.T) {
			a, _ := m.pair(t, map[string]string{"reader": "example-1", "token": "example-old"})
			restore := takeCheckpoint(t, a)
			mustUpdate(t, a, setAndMerge("reader", "example-2"))
			exempt(t, a, replaceToken("example-old", "example-new", nil))
			before := vaultFiles(t, a.Dir())
			if err := restore(); !errors.Is(err, ErrConflict) {
				t.Fatalf("restore() error = %v, want ErrConflict", err)
			}
			wantFiles(t, vaultFiles(t, a.Dir()), before)
		})
	}
}

func TestRestoreAfterOnlyExemptUpdatesLeavesTheDocument(t *testing.T) {
	for _, m := range exemptModes {
		t.Run(m.name, func(t *testing.T) {
			a, b := m.pair(t, map[string]string{"token": "example-old"})
			var entry string
			if m.name == "encrypted" {
				entry = addPending(t, b, testEntry{Name: "writer", Value: "example-3"})
			}
			restore := takeCheckpoint(t, a)
			exempt(t, a, setAndMerge("token", "example-new"))
			before := vaultFiles(t, a.Dir())
			if err := restore(); err != nil {
				t.Fatalf("restore() error = %v", err)
			}
			wantFiles(t, vaultFiles(t, a.Dir()), before)
			want := map[string]string{"token": "example-new"}
			if entry != "" {
				want["writer"] = "example-3"
				if names := pendingNames(t, a); len(names) != 0 {
					t.Fatalf("pending entries = %v, want the entry the exempt update merged gone", names)
				}
			}
			wantValues(t, decodeDoc(t, heldDocument(t, a)), want)
		})
	}
}

func TestRestoreAfterTheUndoOfAnExemptUpdate(t *testing.T) {
	for _, m := range exemptModes {
		t.Run(m.name, func(t *testing.T) {
			a, _ := m.pair(t, map[string]string{"token": "example-old"})
			a.opts.Revert = testRevert
			restore := takeCheckpoint(t, a)
			undo := exempt(t, a, replaceToken("example-old", "example-new", nil))
			if err := undo(); err != nil {
				t.Fatalf("undo() error = %v", err)
			}
			if err := restore(); err != nil {
				t.Fatalf("restore() error = %v", err)
			}
			wantValues(t, m.onDisk(t, a), map[string]string{"token": "example-old"})
		})
	}
}

func TestExemptUpdateNeedsTheLock(t *testing.T) {
	encrypted, _ := created(t, map[string]string{"token": "example-old"})
	plain := newVault(t, testOptions(filepath.Join(t.TempDir(), "vault")))
	if err := plain.CreateUnencrypted(encodeDoc(t, map[string]string{"token": "example-old"})); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*Vault{encrypted, plain} {
		if _, err := v.Checkpoint(); err != nil {
			t.Fatal(err)
		}
		before := vaultFiles(t, v.Dir())
		undo, err := v.UpdateWithOptions(replaceToken("example-old", "example-new", nil), ExemptFromCheckpoints())
		if !errors.Is(err, ErrExemptNeedsLock) || undo != nil {
			t.Fatalf("UpdateWithOptions() = %v, error %v, want ErrExemptNeedsLock", undo != nil, err)
		}
		wantFiles(t, vaultFiles(t, v.Dir()), before)
	}
}

func TestRestoreRefusesASwitchAndAnExemptUpdate(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, a *Vault)
	}{
		{"encrypt, then exempt update", func(t *testing.T, a *Vault) {
			if err := a.Encrypt(testPassphrase); err != nil {
				t.Fatal(err)
			}
			exempt(t, a, replaceToken("example-old", "example-new", nil))
		}},
		{"exempt update, then encrypt", func(t *testing.T, a *Vault) {
			exempt(t, a, replaceToken("example-old", "example-new", nil))
			if err := a.Encrypt(testPassphrase); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := plainPair(t, map[string]string{"token": "example-old"})
			a.opts.Revert = testRevert
			restore := takeCheckpoint(t, a)
			c.run(t, a)
			before := vaultFiles(t, a.Dir())
			if err := restore(); !errors.Is(err, ErrConflict) {
				t.Fatalf("restore() error = %v, want ErrConflict", err)
			}
			wantFiles(t, vaultFiles(t, a.Dir()), before)
		})
	}
	t.Run("change passphrase, then exempt update", func(t *testing.T) {
		a, _ := lockedPair(t, map[string]string{"token": "example-old"})
		a.opts.Revert = testRevert
		restore := takeCheckpoint(t, a)
		if err := a.ChangePassphrase(testPassphrase, anotherPassphrase); err != nil {
			t.Fatal(err)
		}
		exempt(t, a, replaceToken("example-old", "example-new", nil))
		before := vaultFiles(t, a.Dir())
		if err := restore(); !errors.Is(err, ErrConflict) {
			t.Fatalf("restore() error = %v, want ErrConflict", err)
		}
		wantFiles(t, vaultFiles(t, a.Dir()), before)
	})
}

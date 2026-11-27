package vault

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var createModes = []struct {
	name      string
	encrypted bool
	state     State
}{
	{"encrypted", true, StateLocked},
	{"unencrypted", false, StateUnencrypted},
}

func createWithUndo(t *testing.T, v *Vault, encrypted bool, values map[string]string) (func() error, error) {
	t.Helper()
	if encrypted {
		return v.CreateWithUndo(testPassphrase, encodeDoc(t, values))
	}
	return v.CreateUnencryptedWithUndo(encodeDoc(t, values))
}

// createdWithUndo creates a vault with a write lock and testRevert holding values and returns the creating
// value, a second value of it, unlocked when encrypted, and the undo of the create.
func createdWithUndo(t *testing.T, encrypted bool, values map[string]string) (a, b *Vault, undo func() error) {
	t.Helper()
	opts := lockedOptions(filepath.Join(t.TempDir(), "vault"))
	opts.Revert = testRevert
	a = newVault(t, opts)
	undo, err := createWithUndo(t, a, encrypted, values)
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	b = newVault(t, opts)
	if encrypted {
		if _, err := b.Unlock(testPassphrase); err != nil {
			t.Fatal(err)
		}
	}
	return a, b, undo
}

func createdOnDisk(t *testing.T, v *Vault, encrypted bool) map[string]string {
	t.Helper()
	if encrypted {
		return diskValues(t, v.opts)
	}
	return plainOnDisk(t, v)
}

func TestCreateUndoAfterForgetTakesBackOnlyTheCreate(t *testing.T) {
	for _, m := range createModes {
		t.Run(m.name, func(t *testing.T) {
			a, b, undo := createdWithUndo(t, m.encrypted, map[string]string{"own": "example-1"})
			mustUpdate(t, b, setAndMerge("other", "example-2"))
			a.Forget()
			if err := undo(); err != nil {
				t.Fatalf("undo() error = %v", err)
			}
			wantValues(t, createdOnDisk(t, a, m.encrypted), map[string]string{"other": "example-2"})
			wantState(t, a, m.state)
		})
	}
}

func TestCreateUndoKeepsARotationSince(t *testing.T) {
	for _, m := range createModes {
		t.Run(m.name, func(t *testing.T) {
			a, b, undo := createdWithUndo(t, m.encrypted,
				map[string]string{"own": "example-1", "token": "example-old"})
			mustUpdate(t, b, replaceToken("example-old", "example-new", nil))
			if err := undo(); err != nil {
				t.Fatalf("undo() error = %v", err)
			}
			want := map[string]string{"token": "example-new"}
			wantValues(t, createdOnDisk(t, a, m.encrypted), want)
			if m.encrypted {
				wantValues(t, decodeDoc(t, heldDocument(t, a)), want)
			}
		})
	}
}

func TestCreateUndoAfterASwitch(t *testing.T) {
	refused := func(t *testing.T, a *Vault, undo func() error) {
		t.Helper()
		before := vaultFiles(t, a.Dir())
		if err := undo(); !errors.Is(err, ErrSwitched) {
			t.Fatalf("undo() error = %v, want ErrSwitched", err)
		}
		wantFiles(t, vaultFiles(t, a.Dir()), before)
	}
	t.Run("change passphrase", func(t *testing.T) {
		a, b, undo := createdWithUndo(t, true, map[string]string{"own": "example-1"})
		if err := b.ChangePassphrase(testPassphrase, anotherPassphrase); err != nil {
			t.Fatal(err)
		}
		a.Forget()
		if err := undo(); err != nil {
			t.Fatalf("undo() error = %v", err)
		}
		v := newVault(t, a.opts)
		if _, err := v.Unlock(anotherPassphrase); err != nil {
			t.Fatal(err)
		}
		wantValues(t, decodeDoc(t, heldDocument(t, v)), map[string]string{})
	})
	t.Run("decrypt, then encrypt", func(t *testing.T) {
		a, b, undo := createdWithUndo(t, true, map[string]string{"own": "example-1"})
		if err := b.Decrypt(testPassphrase); err != nil {
			t.Fatal(err)
		}
		refused(t, a, undo)
		if err := b.Encrypt(anotherPassphrase); err != nil {
			t.Fatal(err)
		}
		refused(t, a, undo)
	})
	t.Run("encrypt an unencrypted vault", func(t *testing.T) {
		a, b, undo := createdWithUndo(t, false, map[string]string{"own": "example-1"})
		if err := b.Encrypt(testPassphrase); err != nil {
			t.Fatal(err)
		}
		refused(t, a, undo)
	})
}

func TestCreateWithUndoNeedsLockAndRevert(t *testing.T) {
	withoutRevert := func(dir string) Options { return lockedOptions(dir) }
	withoutLock := func(dir string) Options {
		opts := testOptions(dir)
		opts.Revert = testRevert
		return opts
	}
	for name, options := range map[string]func(string) Options{"no revert": withoutRevert, "no lock": withoutLock} {
		for _, m := range createModes {
			t.Run(name+", "+m.name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "vault")
				undo, err := createWithUndo(t, newVault(t, options(dir)), m.encrypted, nil)
				if !errors.Is(err, ErrCreateUndoNeedsRevert) || undo != nil {
					t.Fatalf("create error = %v, want ErrCreateUndoNeedsRevert and no undo", err)
				}
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("the refused create wrote the vault directory: %v", err)
				}
			})
		}
	}
}

func TestCreateUndoTakesEffectOnceAndKeepsPendingAndCheckpoint(t *testing.T) {
	for _, m := range createModes {
		t.Run(m.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "vault")
			opts := lockedOptions(dir)
			opts.Revert = testRevert
			a := newVault(t, opts)
			restore := takeCheckpoint(t, a)
			undo, err := createWithUndo(t, a, m.encrypted, map[string]string{"own": "example-1"})
			if err != nil {
				t.Fatal(err)
			}
			var entries []string
			if m.encrypted {
				entries = []string{addPending(t, a, testEntry{Name: "later", Value: "example-3"})}
			}
			if err := undo(); err != nil {
				t.Fatalf("undo() error = %v", err)
			}
			if names := pendingNames(t, a); !slices.Equal(names, entries) {
				t.Fatalf("pending entries = %v, want %v", names, entries)
			}
			after := vaultFiles(t, dir)
			if err := undo(); err != nil {
				t.Fatalf("a second undo() error = %v", err)
			}
			wantFiles(t, vaultFiles(t, dir), after)
			if err := restore(); err != nil {
				t.Fatalf("restore() error = %v", err)
			}
			wantState(t, a, StateAbsent)
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("restore left the vault directory: %v", err)
			}
		})
	}
}

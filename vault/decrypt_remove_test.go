package vault

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// createdWithSealed returns a locked vault that holds a sealed file.
func createdWithSealed(t *testing.T) *Vault {
	t.Helper()
	unlocked, locked := created(t, map[string]string{"reader": "example-1"})
	if err := unlocked.WriteSealed(sealedName, []byte("example settings")); err != nil {
		t.Fatal(err)
	}
	return locked
}

var errBlocked = errors.New("blocked")

// failRemove makes remove fail with errBlocked for every path in blocked and removes the rest.
func failRemove(v *Vault, blocked ...string) {
	v.remove = func(path string) error {
		if slices.Contains(blocked, path) {
			return errBlocked
		}
		return os.Remove(path)
	}
}

func TestDecryptCompletesWhenAnEntryStays(t *testing.T) {
	for _, withCheckpoint := range []bool{false, true} {
		name := "no checkpoint"
		if withCheckpoint {
			name = "checkpoint"
		}
		t.Run(name, func(t *testing.T) {
			// A checkpoint does not record sealed files, so only the run without one holds a sealed file.
			var v *Vault
			if withCheckpoint {
				_, v = created(t, map[string]string{"reader": "example-1"})
			} else {
				v = createdWithSealed(t)
			}
			entry := addPending(t, v, testEntry{Name: "writer", Value: "example-2"})
			entryPath := filepath.Join(v.pendingPath(), entry)
			before := dirState(t, v.Dir())
			var restore func() error
			if withCheckpoint {
				var err error
				if restore, err = v.Checkpoint(); err != nil {
					t.Fatal(err)
				}
			}
			failRemove(v, entryPath)
			if err := v.Decrypt(testPassphrase); err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			if decodeDoc(t, readBytes(t, v.plainPath()))["writer"] != "example-2" {
				t.Fatalf("plain document = %s", readBytes(t, v.plainPath()))
			}
			for _, p := range []string{v.keyPath(), v.recipientPath(), v.documentPath(), v.path(sealedName)} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Errorf("%s is still there: %v", filepath.Base(p), err)
				}
			}
			if names := pendingNames(t, v); !slices.Equal(names, []string{entry}) {
				t.Fatalf("pending = %v, want %s", names, entry)
			}
			if st, _ := v.Status(); st.Pending != 1 || st.State != StateUnencrypted {
				t.Fatalf("Status() = %+v, want unencrypted with the entry pending", st)
			}
			if withCheckpoint {
				v.remove = os.Remove
				if err := restore(); err != nil {
					t.Fatalf("restore() error = %v", err)
				}
				if after := dirState(t, v.Dir()); !maps.Equal(before, after) {
					t.Fatalf("restore left %v, want %v", slices.Sorted(maps.Keys(after)), slices.Sorted(maps.Keys(before)))
				}
				return
			}
			if err := v.RemovePending(entry); err != nil {
				t.Fatal(err)
			}
			if names := pendingNames(t, v); len(names) != 0 {
				t.Fatalf("pending after RemovePending = %v", names)
			}
		})
	}
}

func TestDecryptReportsEveryFailedRemoval(t *testing.T) {
	v := createdWithSealed(t)
	key, sealed := v.keyPath(), v.path(sealedName)
	failRemove(v, key, sealed)
	err := v.Decrypt(testPassphrase)
	if !errors.Is(err, errBlocked) {
		t.Fatalf("Decrypt() error = %v, want the removal errors", err)
	}
	for _, p := range []string{v.recipientPath(), v.documentPath()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", filepath.Base(p), err)
		}
	}
	for _, p := range []string{key, sealed} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v, want it left", filepath.Base(p), err)
		}
	}
	if st, _ := v.Status(); st.State != StateUnencrypted {
		t.Fatalf("State = %v, want unencrypted", st.State)
	}
}

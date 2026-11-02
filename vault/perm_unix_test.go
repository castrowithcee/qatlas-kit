//go:build !windows

package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultFilesArePrivate(t *testing.T) {
	v, _ := created(t, nil)
	addPending(t, v, testEntry{Name: "writer", Value: "example-1"})
	if err := v.WriteSealed(sealedName, []byte("example")); err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(v.Dir(), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		want := os.FileMode(fileMode)
		if info.IsDir() {
			want = dirMode
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s has mode %04o, want %04o", path, info.Mode().Perm(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWidenedModeIsRefused(t *testing.T) {
	for _, name := range []string{"key.age", "recipient", "secrets.age", "pending"} {
		t.Run(name, func(t *testing.T) {
			v, locked := created(t, nil)
			path := filepath.Join(v.Dir(), name)
			if name == "pending" {
				path = filepath.Join(v.pendingPath(), addPending(t, v, testEntry{Name: "writer", Value: "example-1"}))
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := locked.Unlock(testPassphrase)
			var perm *PermissionError
			if !errors.As(err, &perm) || perm.Path != path || !strings.Contains(err.Error(), "chmod 600") {
				t.Fatalf("Unlock() error = %v, want a PermissionError for %s naming the fix", err, path)
			}
			wantState(t, locked, StateLocked)
		})
	}
	v := newVault(t, testOptions(filepath.Join(t.TempDir(), "vault")))
	if err := v.CreateUnencrypted(encodeDoc(t, nil)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(v.plainPath(), 0o640); err != nil {
		t.Fatal(err)
	}
	var perm *PermissionError
	if _, err := v.Document(); !errors.As(err, &perm) {
		t.Fatalf("Document() of a widened plain file error = %v, want a PermissionError", err)
	}
}

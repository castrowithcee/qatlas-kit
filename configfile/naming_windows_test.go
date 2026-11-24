//go:build windows

package configfile

import (
	"path/filepath"
	"testing"
)

func TestOwnNamingBackupsHaveUserOnlyACL(t *testing.T) {
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(testNaming), WithTempPrefix("tmp-"))
	mustSaveNumber(t, store, 1)
	mustSaveNumber(t, store, 2)
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	copied, err := store.BackupCopy(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{store.backupDir(), filepath.Join(store.backupDir(), backups[0].Name), copied} {
		assertUserOnlyACL(t, name)
	}
}

func TestCopySuffixCopyHasUserOnlyACL(t *testing.T) {
	n := testNaming
	n.CopySuffix = ".bak"
	store := namedStore(t, t.TempDir(), numberCodec, WithBackupNaming(n))
	mustSaveNumber(t, store, 1)
	copied, err := store.BackupCopy(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	assertUserOnlyACL(t, copied)
}

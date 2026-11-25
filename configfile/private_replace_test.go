package configfile

import (
	"os"
	"runtime"
	"testing"
)

// writeWide creates the target as a file others can read, by whatever means the platform has.
func writeWide(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	widenACL(t, path)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return // Windows permissions are checked by the ACL tests.
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != want {
		t.Fatalf("%s mode = %v, %v; want %o", path, info, err, want)
	}
}

func privateReplaceStore(t *testing.T, private bool) *Store[int] {
	t.Helper()
	var opts []Option
	if private {
		opts = append(opts, WithPrivateReplace())
	}
	return namedStore(t, t.TempDir(), numberCodec, opts...)
}

func TestWithoutPrivateReplaceKeepsTargetPermissions(t *testing.T) {
	store := privateReplaceStore(t, false)
	writeWide(t, store.Path(), "1")
	_, rev, err := store.LoadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveIfUnchanged(2, rev); err != nil {
		t.Fatal(err)
	}
	assertMode(t, store.Path(), 0o644)
	assertWideACL(t, store.Path())
}

func TestPrivateReplaceMakesSavedFilePrivate(t *testing.T) {
	store := privateReplaceStore(t, true)
	writeWide(t, store.Path(), "1")
	_, rev, _ := store.LoadVersioned()
	if _, err := store.SaveIfUnchanged(2, rev); err != nil {
		t.Fatal(err)
	}
	assertMode(t, store.Path(), 0o600)
	assertPrivateACL(t, store.Path())
}

func TestPrivateReplaceMakesUpdatedAndTransactedFilePrivate(t *testing.T) {
	store := privateReplaceStore(t, true)
	writeWide(t, store.Path(), "1")
	if _, err := store.Update(func(n int) (int, error) { return n + 1, nil }); err != nil {
		t.Fatal(err)
	}
	assertMode(t, store.Path(), 0o600)
	assertPrivateACL(t, store.Path())
	writeWide(t, store.Path(), "5")
	_, rev, _ := store.LoadVersioned()
	err := store.Transact(rev, func(save func(int) (Revision, error)) error {
		_, err := save(6)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMode(t, store.Path(), 0o600)
	assertPrivateACL(t, store.Path())
}

func TestRestoreAndUndoKeepPermissionsWithoutPrivateReplace(t *testing.T) {
	store := privateReplaceStore(t, false)
	restoreAndUndo(t, store, 0o644)
}

func TestPrivateReplaceMakesRestoredAndUndoneFilePrivate(t *testing.T) {
	store := privateReplaceStore(t, true)
	restoreAndUndo(t, store, 0o600)
}

func restoreAndUndo(t *testing.T, store *Store[int], want os.FileMode) {
	t.Helper()
	writeWide(t, store.Path(), "1")
	_, rev, _ := store.LoadVersioned()
	rev, err := store.SaveIfUnchanged(2, rev)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	// Widen again so the check below sees the effect of the restore alone.
	writeWide(t, store.Path(), "2")
	_, rev, _ = store.LoadVersioned()
	restored, err := store.RestoreBackup(backups[0].Name, rev)
	if err != nil {
		t.Fatal(err)
	}
	checkPermissions(t, store.Path(), want)
	writeWide(t, store.Path(), "1")
	restored.Revision = revisionOf([]byte("1"))
	if err := store.UndoRestore(restored); err != nil {
		t.Fatal(err)
	}
	checkPermissions(t, store.Path(), want)
}

func checkPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	assertMode(t, path, want)
	if want == 0o600 {
		assertPrivateACL(t, path)
	} else {
		assertWideACL(t, path)
	}
}

func TestNewFileIsPrivateWithAndWithoutPrivateReplace(t *testing.T) {
	for _, private := range []bool{false, true} {
		store := privateReplaceStore(t, private)
		if _, err := store.SaveIfUnchanged(1, RevisionAbsent); err != nil {
			t.Fatal(err)
		}
		assertMode(t, store.Path(), 0o600)
		assertPrivateACL(t, store.Path())
	}
}

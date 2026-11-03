package configfile

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/fileretry"
)

func TestTransactChecksRevisionBeforeWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	ran := false
	err := store.Transact(RevisionAbsent, func(func(int) (Revision, error)) error {
		ran = true
		return nil
	})
	if !errors.Is(err, ErrConflict) || ran {
		t.Fatalf("stale transaction = %v, work ran %v", err, ran)
	}
	if err := store.Transact(RevisionAbsent, nil); err == nil {
		t.Fatal("accepted a missing callback")
	}
}

func TestTransactSavesOnceAndReturnsWorkError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	rev := mustSaveNumber(t, store, 1)
	stop := errors.New("stop")
	var saved Revision
	var second error
	var save func(int) (Revision, error)
	err := store.Transact(rev, func(s func(int) (Revision, error)) error {
		save = s
		var err error
		if saved, err = s(2); err != nil {
			return err
		}
		_, second = s(3)
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("transaction = %v; want work error", err)
	}
	if !errors.Is(second, ErrConflict) {
		t.Fatalf("second save = %v; want conflict", second)
	}
	if got := readNumberFile(t, path); string(got) != "2" {
		t.Fatalf("file = %q", got)
	}
	if _, now, err := store.LoadVersioned(); err != nil || now != saved {
		t.Fatalf("saved revision = %q, file %q, %v", saved, now, err)
	}
	if backups, err := store.Backups(); err != nil || len(backups) != 1 {
		t.Fatalf("transaction backups = %v, %v", backups, err)
	}
	if _, err := save(4); err == nil {
		t.Fatal("save succeeded after the transaction ended")
	}
	if got := readNumberFile(t, path); string(got) != "2" {
		t.Fatalf("late save changed file: %q", got)
	}
}

func TestTransactSaveRejectsOutsideWriteAndInvalidValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	rev := mustSaveNumber(t, store, 1)
	var invalid, outside error
	err := store.Transact(rev, func(save func(int) (Revision, error)) error {
		_, invalid = save(-1)
		if err := os.WriteFile(path, []byte("9"), 0o600); err != nil {
			return err
		}
		_, outside = save(2)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if invalid == nil || errors.Is(invalid, ErrConflict) {
		t.Fatalf("invalid save = %v", invalid)
	}
	if !errors.Is(outside, ErrConflict) {
		t.Fatalf("save after outside write = %v", outside)
	}
	if got := readNumberFile(t, path); string(got) != "9" {
		t.Fatalf("outside write was replaced: %q", got)
	}
}

func TestTransactThroughDanglingLinkCreatesNothing(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing", "settings")
	link := filepath.Join(root, "settings")
	if err := os.Symlink(missing, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := numberStore(t, link)
	ran := false
	if err := store.Transact(RevisionAbsent, func(func(int) (Revision, error)) error {
		ran = true
		return nil
	}); err == nil || ran {
		t.Fatalf("dangling link transaction = %v, work ran %v", err, ran)
	}
	if _, _, err := store.Snapshot(); err == nil || isNotFound(err) {
		t.Fatalf("dangling link snapshot = %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("link target directory created: %v", err)
	}
}

func isNotFound(err error) bool {
	var nf *NotFoundError
	return errors.As(err, &nf)
}

func TestRevisionOfMatchesSavedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	written := mustSaveNumber(t, store, 7)
	rev, err := store.RevisionOf(7)
	if err != nil || rev != written {
		t.Fatalf("RevisionOf = %q, %v; want %q", rev, err, written)
	}
	if _, err := store.RevisionOf(-1); err == nil {
		t.Fatal("revision of an invalid value")
	}
}

func TestSnapshotReturnsRawBytesAndTypedMissingError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	if _, rev, err := store.Snapshot(); !isNotFound(err) || !errors.Is(err, fs.ErrNotExist) || rev != RevisionAbsent {
		t.Fatalf("missing snapshot = %q, %v", rev, err)
	}
	if _, _, err := store.LoadVersioned(); !isNotFound(err) {
		t.Fatalf("missing load = %v", err)
	}
	if err := os.WriteFile(path, []byte("not a number"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, rev, err := store.Snapshot()
	if err != nil || string(data) != "not a number" || rev != revisionOf(data) {
		t.Fatalf("snapshot = %q, %q, %v", data, rev, err)
	}
	if _, loaded, err := store.LoadVersioned(); err == nil || loaded != rev {
		t.Fatalf("malformed load = %q, %v", loaded, err)
	}
}

func TestUndoRestorePutsBackReplacedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	rev := mustSaveNumber(t, store, 2)
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	restored, err := store.RestoreBackup(backups[0].Name, rev)
	if err != nil || restored.Previous == "" {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
	if got, err := store.ReadBackup(restored.Previous); err != nil || string(got) != "2" {
		t.Fatalf("previous backup = %q, %v", got, err)
	}
	if err := store.UndoRestore(restored); err != nil {
		t.Fatal(err)
	}
	if got := readNumberFile(t, path); string(got) != "2" {
		t.Fatalf("undone file = %q", got)
	}
	if after, err := store.Backups(); err != nil || len(after) != 2 {
		t.Fatalf("undo made a backup: %v, %v", after, err)
	}
}

func TestUndoRestoreRejectsChangedFileAndForeignBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	rev := mustSaveNumber(t, store, 2)
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	restored, err := store.RestoreBackup(backups[0].Name, rev)
	if err != nil {
		t.Fatal(err)
	}
	forged := restored
	forged.Previous = "../settings"
	if err := store.UndoRestore(forged); !isNotFound(err) {
		t.Fatalf("undo from outside the backup set = %v", err)
	}
	if err := store.UndoRestore(Restored{}); err == nil {
		t.Fatal("undo of an empty result")
	}
	if got := readNumberFile(t, path); string(got) != "1" {
		t.Fatalf("rejected undo changed file: %q", got)
	}
	mustSaveNumber(t, store, 5)
	if err := store.UndoRestore(restored); !errors.Is(err, ErrConflict) {
		t.Fatalf("undo over a later change = %v", err)
	}
	if got := readNumberFile(t, path); string(got) != "5" {
		t.Fatalf("later change was replaced: %q", got)
	}
}

func TestUndoRestoreRemovesFileTheRestoreCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	mustSaveNumber(t, store, 2)
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	restored, err := store.RestoreBackup(backups[0].Name, RevisionAbsent)
	if err != nil || restored.Previous != "" {
		t.Fatalf("restore into absent file = %+v, %v", restored, err)
	}
	if err := os.WriteFile(path, []byte("3"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.UndoRestore(restored); !errors.Is(err, ErrConflict) {
		t.Fatalf("undo over a later change = %v", err)
	}
	if err := os.WriteFile(path, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.UndoRestore(restored); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restored file kept: %v", err)
	}
}

func TestBackupCopyIsPrivateAndOutsideRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings")
	store := numberStore(t, path)
	store.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	mustSaveNumber(t, store, 1)
	own, err := store.BackupCopy(path)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("other bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := store.BackupCopy(other)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.BackupCopy(other)
	if err != nil || second == first {
		t.Fatalf("second copy = %q, %v", second, err)
	}
	for i := 2; i <= 14; i++ {
		mustSaveNumber(t, store, i)
	}
	backups, err := store.Backups()
	if err != nil || len(backups) != keepBackups {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	for _, backup := range backups {
		for _, copied := range []string{own, first, second} {
			if backup.Name == filepath.Base(copied) {
				t.Fatalf("copy %s listed as backup", copied)
			}
		}
	}
	for copied, want := range map[string]string{own: "1", first: "other bytes", second: "other bytes"} {
		if filepath.Dir(copied) != store.backupDir() {
			t.Fatalf("copy outside backup directory: %s", copied)
		}
		if got := readNumberFile(t, copied); string(got) != want {
			t.Fatalf("copy %s = %q; want %q", copied, got, want)
		}
		if runtime.GOOS != "windows" {
			if info, err := os.Stat(copied); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("copy mode = %v, %v", info, err)
			}
		}
	}
	if _, err := store.BackupCopy(filepath.Join(dir, "missing")); !isNotFound(err) {
		t.Fatalf("copy of missing file = %v", err)
	}
}

func TestBackupCopyFailsWhenBackupPathIsFile(t *testing.T) {
	dir := t.TempDir()
	store := numberStore(t, filepath.Join(dir, "settings"))
	src := filepath.Join(dir, "other")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.backupDir(), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BackupCopy(src); err == nil {
		t.Fatal("copy succeeded without backup directory")
	}
	if got := readNumberFile(t, store.backupDir()); string(got) != "occupied" {
		t.Fatalf("backup path changed: %q", got)
	}
}

func TestObserverReportsLockWaitsAndRetries(t *testing.T) {
	dir := t.TempDir()
	base := numberStore(t, filepath.Join(dir, "settings"))
	var waits, retries int
	store := base.Observed(Observer{
		LockWaited: func(d time.Duration) {
			if d < 0 {
				t.Errorf("negative lock wait %v", d)
			}
			waits++
		},
		FileRetry: func(time.Duration) { retries++ },
	})
	if base.observe.LockWaited != nil || base.observe.FileRetry != nil {
		t.Fatal("Observed changed the original store")
	}
	mustSaveNumber(t, store, 1)
	if waits != 1 || retries != 0 {
		t.Fatalf("waits = %d, retries = %d", waits, retries)
	}
	store.transient = func(error) bool { return true }
	if _, err := store.BackupCopy(dir); err == nil {
		t.Fatal("copied a directory")
	}
	if retries != len(fileretry.DefaultDelays()) {
		t.Fatalf("retries = %d; want %d", retries, len(fileretry.DefaultDelays()))
	}
}

func TestWriteNewPrivateFileNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export")
	if err := WriteNewPrivateFile(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("new file mode = %v, %v", info, err)
		}
	}
	err := WriteNewPrivateFile(path, []byte("second"))
	var exists *ExistsError
	if !errors.As(err, &exists) || !errors.Is(err, fs.ErrExist) || exists.Path != path {
		t.Fatalf("existing file = %v", err)
	}
	if got := readNumberFile(t, path); !bytes.Equal(got, []byte("first")) {
		t.Fatalf("existing file replaced: %q", got)
	}
	if err := WriteNewPrivateFile(filepath.Join(dir, "missing", "export"), nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing parent = %v", err)
	}
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := WriteNewPrivateFile(link, []byte("through link")); !errors.As(err, &exists) {
		t.Fatalf("dangling link = %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("link target created: %v", err)
	}
}

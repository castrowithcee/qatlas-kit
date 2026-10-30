package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func mustSaveNumber(t *testing.T, store *Store[int], n int) Revision {
	t.Helper()
	_, rev, err := store.LoadVersioned()
	if errors.Is(err, os.ErrNotExist) {
		rev = RevisionAbsent
	} else if err != nil {
		t.Fatal(err)
	}
	written, err := store.SaveIfUnchanged(n, rev)
	if err != nil {
		t.Fatal(err)
	}
	return written
}

func readNumberFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBackupCopiesReplacedBytesAndKeepsFirstHandVersion(t *testing.T) {
	oldClock := backupNow
	backupNow = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	t.Cleanup(func() { backupNow = oldClock })
	path := filepath.Join(t.TempDir(), "settings.yaml")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	if backups, err := store.Backups(); err != nil || len(backups) != 0 {
		t.Fatalf("first save backups = %v, %v", backups, err)
	}
	if err := os.WriteFile(path, []byte("001"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustSaveNumber(t, store, 2)
	for i := 3; i <= 17; i++ {
		mustSaveNumber(t, store, i)
	}
	backups, err := store.Backups()
	if err != nil || len(backups) != 11 {
		t.Fatalf("rotated backups = %v, %v; want ten plus first hand version", backups, err)
	}
	firsts, ordinary := 0, 0
	for _, backup := range backups {
		if !strings.HasPrefix(backup.Name, "settings.yaml-") {
			t.Fatalf("backup name does not derive from file: %q", backup.Name)
		}
		data, err := store.ReadBackup(backup.Name)
		if err != nil {
			t.Fatal(err)
		}
		if backup.FirstHand {
			firsts++
			if !bytes.Equal(data, []byte("001")) {
				t.Fatalf("first hand backup = %q", data)
			}
		} else {
			ordinary++
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Join(store.backupDir(), backup.Name))
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("backup mode = %v, %v", info, err)
			}
		}
	}
	if firsts != 1 || ordinary != 10 {
		t.Fatalf("first hand = %d, rotating = %d", firsts, ordinary)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.backupDir())
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("backup directory mode = %v, %v", info, err)
		}
	}
	if backups[0].seq <= backups[1].seq {
		t.Fatalf("same-second names not ordered: %v", backups)
	}
}

func TestBackupFailureAndConflictLeaveConfigurationUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	old := mustSaveNumber(t, store, 1)
	if err := os.WriteFile(store.backupDir(), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readNumberFile(t, path)
	if _, err := store.SaveIfUnchanged(2, old); err == nil {
		t.Fatal("save succeeded without backup")
	}
	if after := readNumberFile(t, path); !bytes.Equal(after, before) {
		t.Fatalf("backup failure changed target: %q", after)
	}
	if err := os.Remove(store.backupDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("3"), 0o600); err != nil {
		t.Fatal(err)
	}
	before = readNumberFile(t, path)
	if _, err := store.SaveIfUnchanged(4, old); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write = %v", err)
	}
	if after := readNumberFile(t, path); !bytes.Equal(after, before) {
		t.Fatalf("conflict changed target: %q", after)
	}
	if backups, err := store.Backups(); err != nil || len(backups) != 0 {
		t.Fatalf("conflict made backups: %v, %v", backups, err)
	}
}

func TestBackupStaysBesideSymlinkAndRestoreReplacesTarget(t *testing.T) {
	root := t.TempDir()
	linkDir := filepath.Join(root, "home")
	targetDir := filepath.Join(root, "dotfiles")
	for _, dir := range []string{linkDir, targetDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(targetDir, "settings")
	link := filepath.Join(linkDir, "settings")
	real := numberStore(t, target)
	mustSaveNumber(t, real, 1)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := numberStore(t, link)
	mustSaveNumber(t, store, 2)
	if _, err := os.Stat(filepath.Join(targetDir, backupDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup directory beside target: %v", err)
	}
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	data, err := store.ReadBackup(backups[0].Name)
	if err != nil || string(data) != "1" {
		t.Fatalf("first backup = %q, %v", data, err)
	}
	_, rev, err := store.LoadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	written, err := store.RestoreBackup(backups[0].Name, rev)
	if err != nil || written != revisionOf(data) {
		t.Fatalf("restore revision = %q, %v", written, err)
	}
	if got := readNumberFile(t, target); string(got) != "1" {
		t.Fatalf("restored target = %q", got)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v, %v", info, err)
	}
	backups, err = store.Backups()
	if err != nil || len(backups) != 2 {
		t.Fatalf("restore did not back up replaced bytes: %v, %v", backups, err)
	}
	if got, err := store.ReadBackup(backups[0].Name); err != nil || string(got) != "2" {
		t.Fatalf("restore backup = %q, %v", got, err)
	}
}

func TestRestoreConflictAndBackupFailureDoNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	mustSaveNumber(t, store, 2)
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	_, rev, err := store.LoadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("3"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readNumberFile(t, path)
	if _, err := store.RestoreBackup(backups[0].Name, rev); !errors.Is(err, ErrConflict) {
		t.Fatalf("restore conflict = %v", err)
	}
	if after := readNumberFile(t, path); !bytes.Equal(after, before) {
		t.Fatalf("restore conflict changed target: %q", after)
	}
	_, rev, err = store.LoadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	oldClock := backupNow
	backupNow = func() time.Time {
		if err := os.Rename(store.backupDir(), store.backupDir()+"-moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.backupDir(), []byte("occupied"), 0o600); err != nil {
			t.Fatal(err)
		}
		return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	}
	t.Cleanup(func() { backupNow = oldClock })
	if _, err := store.RestoreBackup(backups[0].Name, rev); err == nil {
		t.Fatal("restore succeeded without creating backup")
	}
	if after := readNumberFile(t, path); !bytes.Equal(after, before) {
		t.Fatalf("failed restore changed target: %q", after)
	}
}

func TestConflictAfterBackupDoesNotReplaceOutsideWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	rev := mustSaveNumber(t, store, 1)
	oldClock := backupNow
	backupNow = func() time.Time {
		if err := os.WriteFile(path, []byte("9"), 0o600); err != nil {
			t.Fatal(err)
		}
		return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	}
	t.Cleanup(func() { backupNow = oldClock })
	if _, err := store.SaveIfUnchanged(2, rev); !errors.Is(err, ErrConflict) {
		t.Fatalf("save after outside write = %v", err)
	}
	if got := readNumberFile(t, path); string(got) != "9" {
		t.Fatalf("outside write was replaced: %q", got)
	}
}

func TestBackupsIgnoreForeignAndSymlinkEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	mustSaveNumber(t, store, 2)
	foreign := filepath.Join(store.backupDir(), "other-20260102T030405Z-000000.bak")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("settings-%s-999999.bak", backupNow().UTC().Format("20060102T150405Z"))
	if err := os.Symlink(foreign, filepath.Join(store.backupDir(), name)); err == nil {
		if _, err := store.ReadBackup(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("symlink backup accepted: %v", err)
		}
	}
	if _, err := store.ReadBackup("../settings"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path outside backup directory accepted: %v", err)
	}
}

func TestStorePreservesExistingFileAndBackupDirectoryModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions are ACLs")
	}
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.backupDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.backupDir(), 0o750); err != nil {
		t.Fatal(err)
	}
	mustSaveNumber(t, store, 2)
	for _, tc := range []struct {
		path string
		want os.FileMode
	}{{path, 0o640}, {store.backupDir(), 0o750}} {
		info, err := os.Stat(tc.path)
		if err != nil || info.Mode().Perm() != tc.want {
			t.Fatalf("%s mode = %v, %v; want %o", tc.path, info, err, tc.want)
		}
	}
}

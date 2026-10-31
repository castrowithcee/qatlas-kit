package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

func numberStore(t *testing.T, path string) *Store[int] {
	t.Helper()
	store, err := NewStore(path, Codec[int]{
		Validate: func(n int) error {
			if n < 0 {
				return errors.New("negative value")
			}
			return nil
		},
		Encode: func(n int) ([]byte, error) { return []byte(strconv.Itoa(n)), nil },
		Decode: func(data []byte) (int, error) { return strconv.Atoi(string(data)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAbsentRevisionCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "settings")
	store := numberStore(t, path)
	_, rev, err := store.LoadVersioned()
	if !errors.Is(err, os.ErrNotExist) || rev != RevisionAbsent {
		t.Fatalf("missing file = %q, %v", rev, err)
	}
	written, err := store.SaveIfUnchanged(1, rev)
	if err != nil || written == RevisionAbsent {
		t.Fatalf("create = %q, %v", written, err)
	}
	_, got, err := store.LoadVersioned()
	if err != nil || got != written {
		t.Fatalf("loaded revision = %q, %v; want %q", got, err, written)
	}
	if _, err := store.SaveIfUnchanged(2, RevisionAbsent); !errors.Is(err, ErrConflict) {
		t.Fatalf("second creation = %v; want conflict", err)
	}
	for _, name := range []string{path, path + ".lock"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
			t.Fatalf("%s mode = %o", name, info.Mode().Perm())
		}
	}
}

func TestStaleRevisionLeavesBytesUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	if _, err := store.SaveIfUnchanged(1, RevisionAbsent); err != nil {
		t.Fatal(err)
	}
	_, old, err := store.LoadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("002"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveIfUnchanged(3, old); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save = %v; want conflict", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("conflicting save changed bytes: %q, %v", after, err)
	}
}

func TestUpdateFailureBeforeRenameLeavesBytesUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	if _, err := store.SaveIfUnchanged(1, RevisionAbsent); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(int) (int, error){
		func(int) (int, error) { return 2, errors.New("change failed") },
		func(int) (int, error) { return -1, nil },
	} {
		if _, err := store.Update(change); err == nil {
			t.Fatal("invalid update succeeded")
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("failed update changed bytes: %q, %v", after, err)
		}
	}
}

func TestSymlinkUsesTargetLockAndReplacesTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "actual", "settings")
	link := filepath.Join(root, "alias")
	real := numberStore(t, target)
	if _, err := real.SaveIfUnchanged(1, RevisionAbsent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viaLink := numberStore(t, link)
	if _, err := viaLink.Update(func(n int) (int, error) { return n + 1, nil }); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v, %v", info, err)
	}
	if _, err := os.Stat(link + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock beside alias: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "2" {
		t.Fatalf("real target = %q, %v", data, err)
	}
}

func TestLockSymlinkIsRejectedWithoutChangingItsTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings")
	foreign := filepath.Join(root, "foreign")
	if err := os.WriteFile(foreign, []byte("unchanged"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, path+".lock"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := numberStore(t, path)
	if _, err := store.SaveIfUnchanged(1, RevisionAbsent); err == nil {
		t.Fatal("accepted a symlinked lock file")
	}
	after, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(foreign)
	if err != nil || string(data) != "unchanged" || before.Mode().Perm() != after.Mode().Perm() {
		t.Fatalf("lock target changed: %q, %v, modes %o and %o", data, err, before.Mode().Perm(), after.Mode().Perm())
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration was written: %v", err)
	}
}

func TestExistingOpenLockPermissionsAreNotChanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACLs need a platform-specific runtime check")
	}
	path := filepath.Join(t.TempDir(), "settings")
	lock := path + ".lock"
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lock, 0o644); err != nil {
		t.Fatal(err)
	}
	store := numberStore(t, path)
	if _, err := store.SaveIfUnchanged(1, RevisionAbsent); err == nil {
		t.Fatal("accepted an open lock file")
	}
	info, err := os.Stat(lock)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("lock mode changed: %v, %v", info, err)
	}
}

func TestConcurrentProcessesKeepBothUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	if _, err := store.SaveIfUnchanged(0, RevisionAbsent); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateWorker$")
			cmd.Env = append(os.Environ(), "CONFIGFILE_TEST_WORKER="+path)
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("worker: %w: %s", err, out)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	value, _, err := store.LoadVersioned()
	if err != nil || value != 2 {
		t.Fatalf("concurrent updates = %d, %v; want 2", value, err)
	}
}

func TestUpdateWorker(t *testing.T) {
	path := os.Getenv("CONFIGFILE_TEST_WORKER")
	if path == "" {
		return
	}
	store := numberStore(t, path)
	if _, err := store.Update(func(n int) (int, error) { return n + 1, nil }); err != nil {
		t.Fatal(err)
	}
}

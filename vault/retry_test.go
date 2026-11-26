package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/fileretry"
)

var errBusy = errors.New("synthetic busy file")

// seamFile makes every failure transient for one test and counts the waits instead of sleeping. onSleep
// runs at each wait, with the number of waits so far.
func seamFile(t *testing.T, onSleep func(n int)) *int {
	t.Helper()
	oldTransient, oldSleep := transientFile, sleepFile
	t.Cleanup(func() { transientFile, sleepFile = oldTransient, oldSleep })
	slept := 0
	transientFile = func(error) bool { return true }
	sleepFile = func(time.Duration) {
		slept++
		if onSleep != nil {
			onSleep(slept)
		}
	}
	return &slept
}

func retryVault(t *testing.T, retry bool) *Vault {
	t.Helper()
	opts := testOptions(t.TempDir())
	opts.RetryFiles = retry
	return newVault(t, opts)
}

// busyFor fails the first n calls with errBusy and counts the calls.
func busyFor(n int, calls *int) func(string, string) error {
	return func(string, string) error {
		*calls++
		if *calls <= n {
			return errBusy
		}
		return nil
	}
}

func TestRetryReadFile(t *testing.T) {
	v := retryVault(t, true)
	// A directory where the file belongs stands in for a lock that is released at the second wait.
	path := filepath.Join(t.TempDir(), "secrets.age")
	if err := os.Mkdir(path, dirMode); err != nil {
		t.Fatal(err)
	}
	want := []byte("synthetic-body")
	slept := seamFile(t, func(n int) {
		if n == 2 {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, fileMode); err != nil {
				t.Fatal(err)
			}
		}
	})
	got, err := v.readFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("readFile = %q, %v; want %q", got, err, want)
	}
	if *slept != 2 {
		t.Errorf("waited %d times, want 2", *slept)
	}
}

func TestRetryStatAndReadDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a path below a file is a missing path on Windows; retry_windows_test.go holds real files")
	}
	for name, op := range map[string]func(v *Vault, path string) error{
		"stat":    func(v *Vault, path string) error { _, err := v.stat(path); return err },
		"readDir": func(v *Vault, path string) error { _, err := v.readDir(path); return err },
	} {
		t.Run(name, func(t *testing.T) {
			v := retryVault(t, true)
			// A file where the directory belongs fails with ENOTDIR until the second wait replaces it.
			dir := filepath.Join(t.TempDir(), "d")
			if err := os.WriteFile(dir, nil, fileMode); err != nil {
				t.Fatal(err)
			}
			path := dir
			if name == "stat" {
				path = filepath.Join(dir, "f")
			}
			slept := seamFile(t, func(n int) {
				if n == 2 {
					if err := os.Remove(dir); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(dir, dirMode); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "f"), nil, fileMode); err != nil {
						t.Fatal(err)
					}
				}
			})
			if err := op(v, path); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if *slept != 2 {
				t.Errorf("waited %d times, want 2", *slept)
			}
		})
	}
}

func TestRetryRename(t *testing.T) {
	v := retryVault(t, true)
	calls := 0
	v.rename = func(from, to string) error {
		if err := busyFor(3, &calls)(from, to); err != nil {
			return err
		}
		return os.Rename(from, to)
	}
	slept := seamFile(t, nil)
	target := filepath.Join(v.opts.Dir, "secrets.age")
	if err := v.write(target, []byte("synthetic-body")); err != nil {
		t.Fatal(err)
	}
	if calls != 4 || *slept != 3 {
		t.Errorf("calls = %d, waits = %d; want 4 and 3", calls, *slept)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "synthetic-body" {
		t.Errorf("target = %q, %v", data, err)
	}
}

func TestRetryRemove(t *testing.T) {
	v := retryVault(t, true)
	path := filepath.Join(v.opts.Dir, "pending", "a.pending")
	if err := v.write(path, []byte("synthetic-entry")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	slept := seamFile(t, nil)
	remove := func(p string) error {
		if err := busyFor(2, &calls)(p, ""); err != nil {
			return err
		}
		return os.Remove(p)
	}
	if err := v.removeEntry(remove, path, false); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || *slept != 2 {
		t.Errorf("calls = %d, waits = %d; want 3 and 2", calls, *slept)
	}
	if exists, _ := fileExists(path); exists {
		t.Error("entry still exists")
	}
	// removeAll goes through the same wait, also for a directory.
	if err := v.removeAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
}

func TestRetryGivesUpAfterTheDelays(t *testing.T) {
	v := retryVault(t, true)
	calls := 0
	v.rename = busyFor(1<<30, &calls)
	slept := seamFile(t, nil)
	err := v.write(filepath.Join(v.opts.Dir, "secrets.age"), []byte("synthetic-body"))
	if !errors.Is(err, errBusy) {
		t.Fatalf("write error = %v, want errBusy", err)
	}
	want := len(fileretry.DefaultDelays())
	if *slept != want || calls != want+1 {
		t.Errorf("waits = %d, calls = %d; want %d and %d", *slept, calls, want, want+1)
	}
}

func TestRetryNeverRepeatsAMissingFile(t *testing.T) {
	v := retryVault(t, true)
	slept := seamFile(t, nil)
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := v.readFile(missing); !isNotExist(err) {
		t.Fatalf("readFile error = %v, want not exist", err)
	}
	if _, err := v.readDir(missing); !isNotExist(err) {
		t.Fatalf("readDir error = %v, want not exist", err)
	}
	if err := v.removeAll(missing); err != nil {
		t.Fatal(err)
	}
	if *slept != 0 {
		t.Errorf("waited %d times for a missing file", *slept)
	}
}

func TestNoRetryWithoutOption(t *testing.T) {
	v := retryVault(t, false)
	calls := 0
	v.rename = busyFor(1, &calls)
	slept := seamFile(t, nil)
	if err := v.write(filepath.Join(v.opts.Dir, "secrets.age"), []byte("x")); !errors.Is(err, errBusy) {
		t.Fatalf("write error = %v, want errBusy", err)
	}
	if calls != 1 || *slept != 0 {
		t.Errorf("calls = %d, waits = %d; want 1 and 0", calls, *slept)
	}
	if err := v.retry(func() error { calls++; return errBusy }); !errors.Is(err, errBusy) || calls != 2 {
		t.Errorf("retry = %v after %d calls, want one attempt", err, calls)
	}
}

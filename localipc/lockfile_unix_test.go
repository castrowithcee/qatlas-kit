//go:build linux || darwin

package localipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func wantEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("left behind after close: %s", entry.Name())
	}
}

func TestCloseRemovesSocketAndLock(t *testing.T) {
	e := testEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(mustPath(t, e))
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	wantEmptyDir(t, dir)

	successor, err := e.ListenWith(ListenOptions{Successor: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := successor.Close(); err != nil {
		t.Fatal(err)
	}
	wantEmptyDir(t, dir)
}

func TestHandOverKeepsSuccessorFilesUntilItCloses(t *testing.T) {
	e := testEndpoint(t)
	old, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	successor, err := e.ListenWith(ListenOptions{Successor: true})
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	if err := HandOver(old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := AwaitHandover(ctx, successor); err != nil {
		t.Fatal(err)
	}
	path := mustPath(t, e)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + lockSuffix} {
		if _, err := os.Lstat(name); err != nil {
			t.Fatalf("closing the old listener removed %s: %v", name, err)
		}
	}
	accepted := make(chan error, 1)
	go func() {
		c, err := successor.Accept()
		if c != nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	conn, err := e.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	if err := successor.Close(); err != nil {
		t.Fatal(err)
	}
	wantEmptyDir(t, filepath.Dir(path))
}

func TestListenRetriesOnReplacedLock(t *testing.T) {
	for _, replace := range []string{"replaced", "removed"} {
		t.Run(replace, func(t *testing.T) {
			e := testEndpoint(t)
			path := mustPath(t, e)
			dir := filepath.Dir(path)
			if err := privateDir(dir); err != nil {
				t.Fatal(err)
			}
			lockPath := path + lockSuffix
			attempts := 0
			spec := socketSpec{path: path, mode: socketMode, gid: -1, prepareDir: privateDir,
				verify: func(net.Conn) (int, error) { return 0, nil }}
			spec.beforeLock = func() {
				attempts++
				if attempts > 1 {
					return
				}
				if err := os.Remove(lockPath); err != nil {
					t.Error(err)
				}
				if replace == "replaced" {
					if err := os.WriteFile(lockPath, nil, lockMode); err != nil {
						t.Error(err)
					}
				}
			}
			l, err := listenSocket(spec)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			if attempts != 2 {
				t.Fatalf("attempts = %d; want 2", attempts)
			}
			current, err := os.Lstat(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			held, err := l.(*checkedListener).lock.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(current, held) {
				t.Fatal("listener holds a lock that is not the file under the name")
			}
			if _, err := e.Listen(); !errors.Is(err, ErrRunning) {
				t.Fatalf("second listener = %v; want ErrRunning", err)
			}
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			wantEmptyDir(t, dir)
		})
	}
}

func TestFailedListenRemovesItsLockButRunningDoesNot(t *testing.T) {
	e := testEndpoint(t)
	path := mustPath(t, e)
	dir := filepath.Dir(path)
	if err := privateDir(dir); err != nil {
		t.Fatal(err)
	}
	// A foreign file under the socket name is refused after the lock was won.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRefused) {
		t.Fatalf("listen over a regular file = %v; want ErrRefused", err)
	}
	if _, err := os.Lstat(path + lockSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed listen left its lock: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	before, err := os.Lstat(path + lockSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRunning) {
		t.Fatalf("second listener = %v; want ErrRunning", err)
	}
	after, err := os.Lstat(path + lockSuffix)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("running listener's lock changed: %v", err)
	}
}

func TestConcurrentListenAndCloseKeepSingleOwner(t *testing.T) {
	e := testEndpoint(t)
	dir := filepath.Dir(mustPath(t, e))
	var owners, started atomic.Int32
	var wg sync.WaitGroup
	deadline := time.Now().Add(1500 * time.Millisecond)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				l, err := e.Listen()
				if err != nil {
					if !errors.Is(err, ErrRunning) {
						t.Errorf("listen: %v", err)
						return
					}
					continue
				}
				if n := owners.Add(1); n > 1 {
					t.Errorf("%d listeners own the channel", n)
				}
				started.Add(1)
				time.Sleep(time.Millisecond)
				owners.Add(-1)
				_ = l.Close()
			}
		}()
	}
	wg.Wait()
	if started.Load() == 0 {
		t.Fatal("no listener ever started")
	}
	wantEmptyDir(t, dir)
}

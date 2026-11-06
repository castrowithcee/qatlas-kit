//go:build linux || darwin

package localipc

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func wantEndedByHolder(t *testing.T, state *os.ProcessState) {
	t.Helper()
	if status, ok := state.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("the helper ended with %v; want SIGTERM", state)
	}
}

func TestHolderRefusesSocketWithoutLock(t *testing.T) {
	e := testEndpoint(t)
	h := startHolderHelper(t, "raw", e)
	wantNoHolder(t, e, ErrNotServed)
	// A lock file nobody holds proves nothing either.
	lock, err := os.OpenFile(mustPath(t, e)+lockSuffix, os.O_RDWR|os.O_CREATE|os.O_EXCL, lockMode)
	if err != nil {
		t.Fatal(err)
	}
	_ = lock.Close()
	wantNoHolder(t, e, ErrNotServed)
	h.wantAlive(t)
}

func TestHolderOfHeldSocketWithoutListenerIsNotServed(t *testing.T) {
	e := testEndpoint(t)
	path := mustPath(t, e)
	if err := privateDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(path+lockSuffix, os.O_RDWR|os.O_CREATE|os.O_EXCL, lockMode)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := holdAndBind(lock, path); err != nil {
		t.Fatal(err)
	}
	wantNoHolder(t, e, ErrNotServed)
}

func TestHolderRefusesOpenDirectory(t *testing.T) {
	e := testEndpoint(t)
	h := startHolderHelper(t, "serve", e)
	dir := filepath.Dir(mustPath(t, e))
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, dirMode)
	wantNoHolder(t, e, ErrRefused)
	h.wantAlive(t)
}

func TestHolderRefusesSocketNotPrivate(t *testing.T) {
	e := testEndpoint(t)
	h := startHolderHelper(t, "serve", e)
	if err := os.Chmod(mustPath(t, e), 0o660); err != nil {
		t.Fatal(err)
	}
	wantNoHolder(t, e, ErrRefused)
	h.wantAlive(t)
}

func TestHolderRefusesLinkToServedSocket(t *testing.T) {
	e := testEndpoint(t)
	h := startHolderHelper(t, "serve", e)
	path := mustPath(t, e)
	link := e
	link.SocketBase = "link"
	linked := mustPath(t, link)
	for _, suffix := range []string{"", lockSuffix} {
		if err := os.Symlink(path+suffix, linked+suffix); err != nil {
			t.Fatal(err)
		}
	}
	wantNoHolder(t, link, ErrRefused)
	h.wantAlive(t)
}

func TestHolderRefusesFileWithHeldLock(t *testing.T) {
	e := testEndpoint(t)
	path := mustPath(t, e)
	if err := privateDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, socketMode); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(path+lockSuffix, os.O_RDWR|os.O_CREATE|os.O_EXCL, lockMode)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	wantNoHolder(t, e, ErrRefused)
}

// A process of another user cannot be started without root, so only a socket
// of another owner is tested, and only as root.
func TestHolderRefusesSocketOfAnotherOwner(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("giving the socket another owner needs root")
	}
	e := testEndpoint(t)
	h := startHolderHelper(t, "serve", e)
	if err := os.Lchown(mustPath(t, e), 65534, -1); err != nil {
		t.Fatal(err)
	}
	wantNoHolder(t, e, ErrRefused)
	h.wantAlive(t)
}

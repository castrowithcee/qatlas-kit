//go:build linux

package localipc

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const releaseWait = time.Second

func listen(e Endpoint) (net.Listener, error) {
	path, err := e.Path()
	if err != nil {
		return nil, err
	}
	if err := privateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, socketMode)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot open socket lock: %w", e.label(), err)
	}
	defer func() {
		if lock != nil {
			_ = lock.Close()
		}
	}()
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownedByMe(info) || info.Mode().Perm() != socketMode {
		return nil, fmt.Errorf("%s: %w: socket lock is not private", e.label(), ErrRefused)
	}
	if err := lockSocket(lock, path); err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	if err := replaceDeadSocket(path); err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	unix, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("%s: cannot listen: %w", e.label(), err)
	}
	unix.SetUnlinkOnClose(false)
	if err := os.Chmod(path, socketMode); err != nil {
		_ = unix.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("%s: cannot set socket permissions: %w", e.label(), err)
	}
	own, err := os.Lstat(path)
	if err != nil {
		_ = unix.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("%s: cannot inspect socket: %w", e.label(), err)
	}
	l := &checkedListener{UnixListener: unix, lock: lock, own: own, path: path}
	lock = nil
	return l, nil
}

func lockSocket(lock *os.File, path string) error {
	deadline := time.Now().Add(releaseWait)
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) || time.Now().After(deadline) {
			return ErrRunning
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func replaceDeadSocket(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case info.Mode().Type() != fs.ModeSocket || !ownedByMe(info) || info.Mode().Perm() != socketMode:
		return fmt.Errorf("%w: existing socket is not private and owned by this user", ErrRefused)
	default:
		return os.Remove(path)
	}
}

type checkedListener struct {
	*net.UnixListener
	lock *os.File
	own  fs.FileInfo
	path string
	once sync.Once
	err  error
}

func (l *checkedListener) Accept() (net.Conn, error) {
	conn, err := l.UnixListener.Accept()
	if err != nil {
		return nil, err
	}
	if err := VerifyPeer(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (l *checkedListener) Close() error {
	l.once.Do(func() {
		if now, err := os.Lstat(l.path); err == nil && os.SameFile(now, l.own) {
			_ = os.Remove(l.path)
		}
		l.err = l.UnixListener.Close()
		_ = l.lock.Close()
	})
	return l.err
}

//go:build linux || darwin

package localipc

import (
	"context"
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

const (
	releaseWait = time.Second
	lockSuffix  = ".lock"
	nextSuffix  = ".next"
)

// socketSpec is what differs between the private and the service channel.
type socketSpec struct {
	path       string
	mode       fs.FileMode
	gid        int // group given to the socket, or -1
	prepareDir func(dir string) error
	verify     func(net.Conn) (int, error)
	refused    func(net.Conn, error)
	// target is the channel path a successor waits for, empty otherwise.
	target string
}

func listen(e Endpoint, opts ListenOptions) (net.Listener, error) {
	path, err := e.Path()
	if err != nil {
		return nil, err
	}
	spec := socketSpec{path: path, mode: socketMode, gid: -1, prepareDir: privateDir,
		verify: func(c net.Conn) (int, error) { return checkPeer(c, true) }, refused: opts.Refused}
	if opts.Successor {
		spec.target, spec.path = path, path+nextSuffix
		if len(spec.path) > maxSocketPath {
			return nil, &PathTooLongError{Path: spec.path}
		}
	}
	l, err := listenSocket(spec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	return l, nil
}

func listenSocket(spec socketSpec) (net.Listener, error) {
	path := spec.path
	if err := spec.prepareDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+lockSuffix, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, lockMode)
	if err != nil {
		return nil, fmt.Errorf("cannot open socket lock: %w", err)
	}
	defer func() {
		if lock != nil {
			_ = lock.Close()
		}
	}()
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownedByMe(info) || info.Mode().Perm() != lockMode {
		return nil, fmt.Errorf("%w: socket lock is not private", ErrRefused)
	}
	if err := lockSocket(lock, path); err != nil {
		return nil, err
	}
	if err := replaceDeadSocket(path, spec.mode); err != nil {
		return nil, err
	}
	unix, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("cannot listen: %w", err)
	}
	unix.SetUnlinkOnClose(false)
	fail := func(format string, err error) (net.Listener, error) {
		_ = unix.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf(format, err)
	}
	if spec.gid >= 0 {
		if err := os.Chown(path, -1, spec.gid); err != nil {
			return fail("cannot set socket group: %w", err)
		}
	}
	if err := os.Chmod(path, spec.mode); err != nil {
		return fail("cannot set socket permissions: %w", err)
	}
	own, err := os.Lstat(path)
	if err != nil {
		return fail("cannot inspect socket: %w", err)
	}
	l := &checkedListener{UnixListener: unix, lock: lock, own: own, mode: spec.mode, verify: spec.verify,
		refused: spec.refused, path: path, target: spec.target}
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

func replaceDeadSocket(path string, mode fs.FileMode) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case info.Mode().Type() != fs.ModeSocket || !ownedByMe(info) || info.Mode().Perm() != mode:
		return fmt.Errorf("%w: existing socket is not private and owned by this user", ErrRefused)
	default:
		return os.Remove(path)
	}
}

type checkedListener struct {
	*net.UnixListener
	lock    *os.File
	own     fs.FileInfo
	mode    fs.FileMode
	verify  func(net.Conn) (int, error)
	refused func(net.Conn, error)

	mu         sync.Mutex
	path       string
	target     string
	handedOver bool
	closed     bool
	err        error
}

func (l *checkedListener) Accept() (net.Conn, error) {
	conn, err := l.UnixListener.Accept()
	if err != nil {
		return nil, err
	}
	if pid, err := l.verify(conn); err != nil {
		return nil, refuse(conn, pid, err, l.refused)
	}
	return conn, nil
}

func (l *checkedListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.err
	}
	l.closed = true
	if now, err := os.Lstat(l.path); !l.handedOver && err == nil && os.SameFile(now, l.own) {
		_ = os.Remove(l.path)
	}
	l.err = l.UnixListener.Close()
	_ = l.lock.Close()
	return l.err
}

// The lock moves before the socket, so from the first rename on no other
// process can take the channel, and from the second every new connection
// reaches the successor.
func handOver(l net.Listener) error {
	cl, ok := l.(*checkedListener)
	if !ok {
		return errors.New("the listener was not opened by this package")
	}
	cl.mu.Lock()
	defer cl.mu.Unlock()
	switch {
	case cl.closed:
		return net.ErrClosed
	case cl.target != "":
		return errors.New("a successor cannot hand over before it took over")
	case cl.handedOver:
		return errors.New("the channel was already handed over")
	}
	next := cl.path + nextSuffix
	if err := checkSuccessor(next, cl.mode); err != nil {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}
	if err := os.Rename(next+lockSuffix, cl.path+lockSuffix); err != nil {
		return fmt.Errorf("cannot move the successor's lock: %w", err)
	}
	if err := os.Rename(next, cl.path); err != nil {
		return fmt.Errorf("cannot move the successor's socket: %w", err)
	}
	cl.handedOver = true
	return nil
}

func checkSuccessor(next string, mode fs.FileMode) error {
	info, err := os.Lstat(next)
	if err != nil || info.Mode().Type() != fs.ModeSocket || !ownedByMe(info) || info.Mode().Perm() != mode {
		return errors.New("the successor's socket is missing or not private")
	}
	lock, err := os.OpenFile(next+lockSuffix, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("the successor's lock is missing")
	}
	defer lock.Close()
	info, err = lock.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownedByMe(info) || info.Mode().Perm() != lockMode {
		return errors.New("the successor's lock is not private")
	}
	// Taking the lock proves that no successor holds it.
	err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		return errors.New("no successor holds the lock beside the channel")
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		return fmt.Errorf("cannot inspect the successor's lock: %v", err)
	}
	return nil
}

func awaitHandover(ctx context.Context, l net.Listener) error {
	cl, ok := l.(*checkedListener)
	if !ok {
		return errors.New("the listener was not opened by this package")
	}
	cl.mu.Lock()
	target := cl.target
	cl.mu.Unlock()
	if target == "" {
		return errors.New("the listener is not a successor's")
	}
	ownLock, err := cl.lock.Stat()
	if err != nil {
		return fmt.Errorf("cannot inspect the socket lock: %w", err)
	}
	for {
		socket, socketErr := os.Lstat(target)
		lock, lockErr := os.Lstat(target + lockSuffix)
		if socketErr == nil && lockErr == nil && os.SameFile(socket, cl.own) && os.SameFile(lock, ownLock) {
			cl.mu.Lock()
			defer cl.mu.Unlock()
			if cl.closed {
				return net.ErrClosed
			}
			cl.path, cl.target = target, ""
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the channel was not handed over: %w", ctx.Err())
		case <-time.After(handoverPoll):
		}
	}
}

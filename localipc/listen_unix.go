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
	// beforeLock runs between opening the lock file and locking it. Only tests set it.
	beforeLock func()
}

func listen(e Endpoint, opts ListenOptions) (net.Listener, error) {
	verify, err := opts.peerCheck()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	path, err := e.Path()
	if err != nil {
		return nil, err
	}
	spec := socketSpec{path: path, mode: socketMode, gid: -1, prepareDir: privateDir, verify: verify,
		refused: opts.Refused}
	if opts.Earlier != nil && !opts.Successor {
		return nil, fmt.Errorf("%s: an earlier endpoint needs Successor", e.label())
	}
	if opts.Successor {
		earlier, err := earlierChannel(opts.Earlier, path)
		if err != nil {
			return nil, fmt.Errorf("%s: earlier endpoint: %w", e.label(), err)
		}
		if earlier != "" {
			return listenTakingOver(e, spec, earlier)
		}
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

// earlierChannel returns the socket of the earlier endpoint when a listener
// serves it and none serves current, and "" otherwise.
func earlierChannel(earlier *Endpoint, current string) (string, error) {
	if earlier == nil {
		return "", nil
	}
	path, err := earlier.Path()
	if err != nil {
		return "", err
	}
	if path == current || serves(current) || !serves(path) {
		return "", nil
	}
	return path, nil
}

// serves reports whether a listener serves the channel at path, by the checks
// a handover makes of a successor plus the private directory. It only looks;
// a lock it wins is let go at once.
func serves(path string) bool {
	return privateExistingDir(filepath.Dir(path)) == nil && checkSuccessor(path, socketMode) == nil
}

// listenTakingOver listens at the endpoint's own channel, which no listener
// serves, and places the handover's mark beside the earlier socket.
func listenTakingOver(e Endpoint, spec socketSpec, earlier string) (net.Listener, error) {
	l, err := listenSocket(spec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	if err := l.(*checkedListener).markBeside(earlier); err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("%s: cannot prepare the handover from the earlier channel: %w", e.label(), err)
	}
	return l, nil
}

// markBeside places at the earlier socket's successor paths what its listener
// moves onto its own in a handover: a lock this listener holds and a socket
// nothing listens on, so no client reaches anything there. O_EXCL and bind fail
// on anything already in place, which stays untouched.
func (l *checkedListener) markBeside(earlier string) error {
	next := earlier + nextSuffix
	if len(next) > maxSocketPath {
		return &PathTooLongError{Path: next}
	}
	lock, err := os.OpenFile(next+lockSuffix, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, lockMode)
	if err != nil {
		return fmt.Errorf("cannot create the lock beside the earlier socket: %w", err)
	}
	mark, err := holdAndBind(lock, next)
	if err != nil {
		removeLock(next, lock)
		_ = lock.Close()
		return err
	}
	l.mu.Lock()
	l.earlier, l.earlierMark, l.earlierLock = earlier, mark, lock
	l.mu.Unlock()
	return nil
}

func holdAndBind(lock *os.File, path string) (fs.FileInfo, error) {
	// The umask may have taken bits the earlier listener checks for.
	if err := lock.Chmod(lockMode); err != nil {
		return nil, fmt.Errorf("cannot set the lock's permissions: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("cannot lock beside the earlier socket: %w", err)
	}
	syscall.ForkLock.RLock()
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("cannot create the mark beside the earlier socket: %w", err)
	}
	err = syscall.Bind(fd, &syscall.SockaddrUnix{Name: path})
	_ = syscall.Close(fd)
	if err != nil {
		return nil, fmt.Errorf("cannot bind the mark beside the earlier socket: %w", err)
	}
	mark, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect the mark beside the earlier socket: %w", err)
	}
	if err := os.Chmod(path, socketMode); err != nil {
		removeIfSame(path, mark)
		return nil, fmt.Errorf("cannot set the mark's permissions: %w", err)
	}
	return mark, nil
}

func removeIfSame(path string, own fs.FileInfo) {
	if now, err := os.Lstat(path); err == nil && os.SameFile(now, own) {
		_ = os.Remove(path)
	}
}

// leaveEarlier removes the mark and the lock markBeside placed, wherever the
// earlier listener moved them, and lets the lock go last. The caller holds l.mu.
func (l *checkedListener) leaveEarlier() {
	if l.earlier == "" {
		return
	}
	next := l.earlier + nextSuffix
	removeIfSame(l.earlier, l.earlierMark)
	removeIfSame(next, l.earlierMark)
	removeLock(l.earlier, l.earlierLock)
	removeLock(next, l.earlierLock)
	_ = l.earlierLock.Close()
	l.earlier, l.earlierMark, l.earlierLock = "", nil, nil
}

func listenSocket(spec socketSpec) (net.Listener, error) {
	path := spec.path
	if err := spec.prepareDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	lock, err := acquireLock(spec)
	if err != nil {
		return nil, err
	}
	defer func() {
		if lock != nil {
			// Removing before closing keeps the name from being taken over
			// while the file still exists.
			removeLock(path, lock)
			_ = lock.Close()
		}
	}()
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

// acquireLock returns the exclusive lock of the channel at spec.path. The file
// is only trusted while it is still the one under the name: a holder removes
// its lock file before it lets go, so a lock won on a file that is gone or
// replaced would grant a channel another process may already own.
func acquireLock(spec socketSpec) (*os.File, error) {
	path := spec.path
	deadline := time.Now().Add(releaseWait)
	for {
		lock, err := os.OpenFile(path+lockSuffix, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, lockMode)
		if err != nil {
			return nil, fmt.Errorf("cannot open socket lock: %w", err)
		}
		info, err := lock.Stat()
		if err != nil || !info.Mode().IsRegular() || !ownedByMe(info) || info.Mode().Perm() != lockMode {
			_ = lock.Close()
			return nil, fmt.Errorf("%w: socket lock is not private", ErrRefused)
		}
		if spec.beforeLock != nil {
			spec.beforeLock()
		}
		if err := lockSocket(lock, path, deadline); err != nil {
			_ = lock.Close()
			return nil, err
		}
		current, err := os.Lstat(path + lockSuffix)
		switch {
		case err == nil && os.SameFile(current, info):
			return lock, nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			_ = lock.Close()
			return nil, fmt.Errorf("cannot inspect socket lock: %w", err)
		}
		_ = lock.Close()
		if time.Now().After(deadline) {
			return nil, ErrRunning
		}
	}
}

// removeLock removes the lock file of the channel at path if it is still the
// file of lock. The caller holds the lock.
func removeLock(path string, lock *os.File) {
	own, err := lock.Stat()
	if err != nil {
		return
	}
	if now, err := os.Lstat(path + lockSuffix); err == nil && os.SameFile(now, own) {
		_ = os.Remove(path + lockSuffix)
	}
}

func lockSocket(lock *os.File, path string, deadline time.Time) error {
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
	// earlier is the socket of the earlier endpoint's listener this successor
	// waits on, with the mark and the held lock placed beside it.
	earlier     string
	earlierMark fs.FileInfo
	earlierLock *os.File
}

func (l *checkedListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	waiting := l.earlier != ""
	l.mu.Unlock()
	if waiting {
		// The earlier listener still serves the application until it hands over.
		return nil, errors.New("a successor cannot accept before it took over")
	}
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
	l.leaveEarlier()
	if !l.handedOver {
		removeIfSame(l.path, l.own)
	}
	// Removing the lock file while still holding the lock keeps a concurrent
	// listener from locking a file that is gone; after a handover the name
	// belongs to the successor's lock and stays.
	removeLock(l.path, l.lock)
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
	case cl.target != "" || cl.earlier != "":
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
	target, earlier, mark := cl.target, cl.earlier, cl.earlierMark
	cl.mu.Unlock()
	var moved func() bool
	switch {
	case earlier != "":
		// The earlier listener moves the lock before the mark.
		moved = func() bool {
			socket, err := os.Lstat(earlier)
			return err == nil && os.SameFile(socket, mark)
		}
	case target != "":
		ownLock, err := cl.lock.Stat()
		if err != nil {
			return fmt.Errorf("cannot inspect the socket lock: %w", err)
		}
		moved = func() bool {
			socket, socketErr := os.Lstat(target)
			lock, lockErr := os.Lstat(target + lockSuffix)
			return socketErr == nil && lockErr == nil && os.SameFile(socket, cl.own) && os.SameFile(lock, ownLock)
		}
	default:
		return errors.New("the listener is not a successor's")
	}
	for {
		if moved() {
			cl.mu.Lock()
			defer cl.mu.Unlock()
			if cl.closed {
				return net.ErrClosed
			}
			if earlier != "" {
				cl.leaveEarlier()
			} else {
				cl.path, cl.target = target, ""
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the channel was not handed over: %w", ctx.Err())
		case <-time.After(handoverPoll):
		}
	}
}

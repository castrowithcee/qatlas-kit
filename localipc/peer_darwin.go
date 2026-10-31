//go:build darwin

package localipc

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var errProgramGone = errors.New("was removed or replaced since it started")

type programFile struct {
	path string
	info fs.FileInfo
}

// Keep the file identity from startup so an update cannot make an old process
// look as though it runs the replacement.
var ownProgram, ownProgramErr = selfProgram()

// VerifyPeer checks both kernel-reported identity and the peer's program file.
// If any identity cannot be read, the connection is refused.
func VerifyPeer(conn net.Conn) error {
	uid, pid, err := peerCred(conn)
	if err != nil {
		return err
	}
	return checkProcess(pid, uid)
}

func peerCred(conn net.Conn) (uint32, int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, fmt.Errorf("%w: not a Unix socket connection", ErrRefused)
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: cannot inspect peer: %v", ErrRefused, err)
	}
	var cred *unix.Xucred
	var pid int
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if credErr == nil {
			pid, credErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		}
	}); err != nil {
		return 0, 0, fmt.Errorf("%w: cannot inspect peer: %v", ErrRefused, err)
	}
	if credErr != nil || cred == nil || cred.Version != 0 {
		return 0, 0, fmt.Errorf("%w: peer credentials cannot be read: %v", ErrRefused, credErr)
	}
	return cred.Uid, pid, nil
}

func checkProcess(pid int, uid uint32) error {
	if uid != uint32(os.Getuid()) {
		return fmt.Errorf("%w: peer has another user ID", ErrRefused)
	}
	if pid <= 0 {
		return fmt.Errorf("%w: peer process cannot be identified", ErrRefused)
	}
	if ownProgramErr != nil {
		return fmt.Errorf("%w: this program cannot be read: %v", ErrRefused, ownProgramErr)
	}
	if replaced, err := ownProgramReplaced(); err != nil || replaced {
		return fmt.Errorf("%w: this program was removed, replaced, or cannot be read: %v", ErrRefused, err)
	}
	peer, err := programOf(pid)
	if err != nil {
		return fmt.Errorf("%w: peer program %w", ErrRefused, err)
	}
	if peer.path != ownProgram.path || !os.SameFile(peer.info, ownProgram.info) {
		return fmt.Errorf("%w: peer runs another program", ErrRefused)
	}
	return nil
}

func ownProgramReplaced() (bool, error) {
	_, now, err := resolveProgram(ownProgram.path)
	if errors.Is(err, errProgramGone) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !os.SameFile(now, ownProgram.info), nil
}

func selfProgram() (programFile, error) {
	path, err := os.Executable()
	if err != nil {
		return programFile{}, errors.New("cannot be read")
	}
	resolved, info, err := resolveProgram(path)
	return programFile{path: resolved, info: info}, err
}

func programOf(pid int) (programFile, error) {
	args, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return programFile{}, errors.New("cannot be read")
	}
	path, err := execPath(args)
	if err != nil {
		return programFile{}, err
	}
	resolved, info, err := resolveProgram(path)
	return programFile{path: resolved, info: info}, err
}

// kern.procargs2 starts with argc and then a zero-terminated exec path.
func execPath(args []byte) (string, error) {
	if len(args) < 4 {
		return "", errors.New("cannot be read")
	}
	end := bytes.IndexByte(args[4:], 0)
	if end <= 0 {
		return "", errors.New("cannot be read")
	}
	path := string(args[4 : 4+end])
	if !filepath.IsAbs(path) {
		return "", errors.New("was started by a relative path")
	}
	return path, nil
}

func resolveProgram(path string) (string, fs.FileInfo, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, errProgramGone
	}
	if err != nil {
		return "", nil, errors.New("cannot be read")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, errors.New("cannot be read")
	}
	return resolved, info, nil
}

// Harden disables core dumps and debugger attachment. It does not hide the
// executable path from another process of the same user on macOS.
func Harden() error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		return fmt.Errorf("cannot disable core dumps: %w", err)
	}
	if err := unix.PtraceDenyAttach(); err != nil {
		return fmt.Errorf("cannot deny debugger attachment: %w", err)
	}
	return nil
}

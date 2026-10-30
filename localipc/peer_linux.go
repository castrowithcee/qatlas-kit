//go:build linux

package localipc

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const deletedSuffix = " (deleted)"

// VerifyPeer checks the kernel-reported peer user and executable. It fails
// closed when either executable cannot be inspected.
func VerifyPeer(conn net.Conn) error {
	cred, err := peerCred(conn)
	if err != nil {
		return err
	}
	return checkProcess(int(cred.Pid), cred.Uid)
}

func peerCred(conn net.Conn) (*syscall.Ucred, error) {
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("%w: not a Unix socket connection", ErrRefused)
	}
	raw, err := unix.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot inspect peer: %v", ErrRefused, err)
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return nil, fmt.Errorf("%w: cannot inspect peer: %v", ErrRefused, err)
	}
	if credErr != nil {
		return nil, fmt.Errorf("%w: cannot inspect peer: %v", ErrRefused, credErr)
	}
	return cred, nil
}

func checkProcess(pid int, uid uint32) error {
	if uid != uint32(os.Getuid()) {
		return fmt.Errorf("%w: peer has another user ID", ErrRefused)
	}
	if pid <= 0 {
		return fmt.Errorf("%w: peer process cannot be identified", ErrRefused)
	}
	own, ownInfo, err := programOf("/proc/self/exe")
	if err != nil {
		return fmt.Errorf("%w: this program %w", ErrRefused, err)
	}
	peer, peerInfo, err := programOf(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return fmt.Errorf("%w: peer program %w", ErrRefused, err)
	}
	if peer != own || !os.SameFile(peerInfo, ownInfo) {
		return fmt.Errorf("%w: peer runs another program", ErrRefused)
	}
	return nil
}

func programOf(link string) (string, fs.FileInfo, error) {
	path, err := os.Readlink(link)
	if err != nil {
		return "", nil, errors.New("cannot be read")
	}
	if _, replaced := strings.CutSuffix(path, deletedSuffix); replaced {
		return "", nil, errors.New("was removed or replaced since it started")
	}
	info, err := os.Stat(link)
	if err != nil {
		return "", nil, errors.New("cannot be read")
	}
	return path, info, nil
}

// Harden disables core dumps and same-user inspection. On Linux this also
// hides /proc/<pid>/exe, so peers must reject this process after Harden.
func Harden() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0); errno != 0 {
		return fmt.Errorf("cannot harden process: %w", errno)
	}
	return nil
}

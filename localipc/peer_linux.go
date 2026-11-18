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

// Harden hides /proc/<pid>/exe, so a client cannot check this program then.
const hardenedProgramCheck = false

// VerifyPeer checks the kernel-reported peer user and executable. It fails
// closed when either executable cannot be inspected.
func VerifyPeer(conn net.Conn) error {
	pid, err := checkPeer(conn, true)
	if err != nil {
		return &PeerError{PID: pid, Err: err}
	}
	return nil
}

// PeerPID returns the process id of the peer as the kernel reported it.
func PeerPID(conn net.Conn) (int, error) {
	cred, err := peerCred(conn)
	if err != nil {
		return 0, err
	}
	return int(cred.Pid), nil
}

// PeerUID returns the user id of the peer as the kernel reported it.
func PeerUID(conn net.Conn) (uint32, error) {
	cred, err := peerCred(conn)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}

func checkPeer(conn net.Conn, program bool) (int, error) {
	cred, err := peerCred(conn)
	if err != nil {
		return 0, err
	}
	if !program {
		return int(cred.Pid), checkUser(int(cred.Pid), cred.Uid)
	}
	return int(cred.Pid), checkProcess(int(cred.Pid), cred.Uid)
}

func peerCred(conn net.Conn) (*syscall.Ucred, error) {
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, refusal(RefusedUnidentified, "not a Unix socket connection")
	}
	raw, err := unix.SyscallConn()
	if err != nil {
		return nil, refusal(RefusedUnidentified, "cannot inspect peer: %v", err)
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return nil, refusal(RefusedUnidentified, "cannot inspect peer: %v", err)
	}
	if credErr != nil {
		return nil, refusal(RefusedUnidentified, "cannot inspect peer: %v", credErr)
	}
	return cred, nil
}

func checkUser(pid int, uid uint32) error {
	if uid != uint32(os.Getuid()) {
		return refusal(RefusedOtherUser, "peer has another user ID")
	}
	// A peer in another PID namespace is reported as 0.
	if pid <= 0 {
		return refusal(RefusedUnidentified, "peer process cannot be identified")
	}
	return nil
}

func checkProcess(pid int, uid uint32) error {
	if err := checkUser(pid, uid); err != nil {
		return err
	}
	own, ownInfo, err := programOf("/proc/self/exe")
	if errors.Is(err, errProgramGone) {
		return refusal(RefusedProgramReplaced, "%w", ErrProgramReplaced)
	}
	if err != nil {
		return refusal(RefusedOwnUnreadable, "this program %w", err)
	}
	peer, peerInfo, err := programOf(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return refusal(peerProgramReason(err), "peer program %w", err)
	}
	if peer != own || !os.SameFile(peerInfo, ownInfo) {
		return refusal(RefusedOtherProgram, "peer runs another program")
	}
	return nil
}

var errProgramGone = errors.New("was removed or replaced since it started")

// peerProgramReason classifies a failure to read the peer's program file: a
// program file that is gone means the peer runs another program.
func peerProgramReason(err error) RefusalReason {
	if errors.Is(err, errProgramGone) {
		return RefusedOtherProgram
	}
	return RefusedUnidentified
}

func programOf(link string) (string, fs.FileInfo, error) {
	path, replaced, err := readProgramLink(link)
	if err != nil {
		return "", nil, err
	}
	if replaced {
		return "", nil, errProgramGone
	}
	info, err := os.Stat(link)
	if err != nil {
		return "", nil, errors.New("cannot be read")
	}
	return path, info, nil
}

func readProgramLink(link string) (path string, replaced bool, err error) {
	path, err = os.Readlink(link)
	if err != nil {
		return "", false, errors.New("cannot be read")
	}
	path, replaced = strings.CutSuffix(path, deletedSuffix)
	return path, replaced, nil
}

func programReplaced() (string, bool, error) {
	path, replaced, err := readProgramLink("/proc/self/exe")
	if err != nil {
		return "", false, fmt.Errorf("this program %w", err)
	}
	return path, replaced, nil
}

// Harden disables core dumps and same-user inspection. On Linux this also
// hides /proc/<pid>/exe, so a client must dial a hardened listener with
// CheckHardened.
func Harden() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0); errno != 0 {
		return fmt.Errorf("cannot harden process: %w", errno)
	}
	return nil
}

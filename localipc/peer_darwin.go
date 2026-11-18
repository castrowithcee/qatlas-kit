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

// Harden leaves the program path readable, so a client still checks it.
const hardenedProgramCheck = true

var errProgramGone = errors.New("was removed or replaced since it started")

// peerProgramReason classifies a failure to read the peer's program file: a
// program file that is gone means the peer runs another program.
func peerProgramReason(err error) RefusalReason {
	if errors.Is(err, errProgramGone) {
		return RefusedOtherProgram
	}
	return RefusedUnidentified
}

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
	pid, err := checkPeer(conn, true)
	if err != nil {
		return &PeerError{PID: pid, Err: err}
	}
	return nil
}

// PeerPID returns the process id of the peer as the kernel reported it.
func PeerPID(conn net.Conn) (int, error) {
	_, pid, err := peerCred(conn)
	return pid, err
}

// PeerUID returns the user id of the peer as the kernel reported it.
func PeerUID(conn net.Conn) (uint32, error) {
	uid, _, err := peerCred(conn)
	return uid, err
}

func checkPeer(conn net.Conn, program bool) (int, error) {
	uid, pid, err := peerCred(conn)
	if err != nil {
		return 0, err
	}
	if !program {
		return pid, checkUser(pid, uid)
	}
	return pid, checkProcess(pid, uid)
}

func peerCred(conn net.Conn) (uint32, int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, refusal(RefusedUnidentified, "not a Unix socket connection")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, 0, refusal(RefusedUnidentified, "cannot inspect peer: %v", err)
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
		return 0, 0, refusal(RefusedUnidentified, "cannot inspect peer: %v", err)
	}
	if credErr != nil || cred == nil || cred.Version != 0 {
		return 0, 0, refusal(RefusedUnidentified, "peer credentials cannot be read: %v", credErr)
	}
	return cred.Uid, pid, nil
}

func checkUser(pid int, uid uint32) error {
	if uid != uint32(os.Getuid()) {
		return refusal(RefusedOtherUser, "peer has another user ID")
	}
	if pid <= 0 {
		return refusal(RefusedUnidentified, "peer process cannot be identified")
	}
	return nil
}

func checkProcess(pid int, uid uint32) error {
	if err := checkUser(pid, uid); err != nil {
		return err
	}
	if ownProgramErr != nil {
		return refusal(RefusedOwnUnreadable, "this program cannot be read: %v", ownProgramErr)
	}
	replaced, err := ownProgramReplaced()
	if err != nil {
		return refusal(RefusedOwnUnreadable, "this program cannot be read: %v", err)
	}
	if replaced {
		return refusal(RefusedProgramReplaced, "%w", ErrProgramReplaced)
	}
	peer, err := programOf(pid)
	if err != nil {
		return refusal(peerProgramReason(err), "peer program %w", err)
	}
	if peer.path != ownProgram.path || !os.SameFile(peer.info, ownProgram.info) {
		return refusal(RefusedOtherProgram, "peer runs another program")
	}
	return nil
}

func ownProgramReplaced() (bool, error) {
	if ownProgramErr != nil {
		return false, ownProgramErr
	}
	_, now, err := resolveProgram(ownProgram.path)
	if errors.Is(err, errProgramGone) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !os.SameFile(now, ownProgram.info), nil
}

func programReplaced() (string, bool, error) {
	replaced, err := ownProgramReplaced()
	if err != nil {
		return "", false, fmt.Errorf("this program cannot be read: %w", err)
	}
	return ownProgram.path, replaced, nil
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

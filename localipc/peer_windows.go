//go:build windows

package localipc

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

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
	id   fileID
}

type fileID struct {
	volume, high, low uint32
}

var ownProgram, ownProgramErr = selfProgram()

// Harden leaves the image name queryable, so a client still checks it.
const hardenedProgramCheck = true

// VerifyPeer checks the peer's user and the file identity at its image path.
// If any identity cannot be read, the connection is refused.
func VerifyPeer(conn net.Conn) error {
	pid, err := checkPeer(conn, true)
	if err != nil {
		return &PeerError{PID: pid, Err: err}
	}
	return nil
}

// PeerPID returns the process id of the peer as the system reported it.
func PeerPID(conn net.Conn) (int, error) {
	pid, err := peerProcessID(conn)
	return int(pid), err
}

// PeerUID is unavailable on Windows, which identifies users by SID.
func PeerUID(net.Conn) (uint32, error) {
	return 0, fmt.Errorf("a peer user ID is unavailable on Windows: %w", errors.ErrUnsupported)
}

func checkPeer(conn net.Conn, program bool) (int, error) {
	pid, err := peerProcessID(conn)
	if err != nil {
		return 0, err
	}
	if !program {
		return int(pid), checkPeerUser(pid)
	}
	return int(pid), checkProcess(pid)
}

func peerProcessID(conn net.Conn) (uint32, error) {
	pipe, ok := conn.(*pipeConn)
	if !ok {
		return 0, refusal(RefusedUnidentified, "not a named pipe connection")
	}
	var pid uint32
	var err error
	if pipe.server {
		err = windows.GetNamedPipeClientProcessId(pipe.h, &pid)
	} else {
		err = windows.GetNamedPipeServerProcessId(pipe.h, &pid)
	}
	if err != nil {
		return 0, refusal(RefusedUnidentified, "the peer's process is unavailable: %w", err)
	}
	return pid, nil
}

func checkProcess(pid uint32) error {
	process, err := openPeer(pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err := checkUser(process); err != nil {
		return err
	}
	if ownProgramErr != nil {
		return refusal(RefusedOwnUnreadable, "this program %w", ownProgramErr)
	}
	replaced, err := ownProgramReplaced()
	if err != nil {
		return refusal(RefusedOwnUnreadable, "this program cannot be read")
	}
	if replaced {
		return refusal(RefusedProgramReplaced, "%w", ErrProgramReplaced)
	}
	peer, err := programOf(process)
	if err != nil {
		return refusal(peerProgramReason(err), "its program %w", err)
	}
	if !samePath(peer.path, ownProgram.path) || peer.id != ownProgram.id {
		return refusal(RefusedOtherProgram, "it runs another program")
	}
	return nil
}

func checkPeerUser(pid uint32) error {
	process, err := openPeer(pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return checkUser(process)
}

func ownProgramReplaced() (bool, error) {
	if ownProgramErr != nil {
		return false, ownProgramErr
	}
	now, err := idOf(ownProgram.path)
	if errors.Is(err, errProgramGone) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return now != ownProgram.id, nil
}

func programReplaced() (string, bool, error) {
	replaced, err := ownProgramReplaced()
	if err != nil {
		return "", false, fmt.Errorf("this program cannot be read: %w", err)
	}
	return ownProgram.path, replaced, nil
}

func openPeer(pid uint32) (windows.Handle, error) {
	if pid == 0 {
		return 0, refusal(RefusedUnidentified, "its process cannot be identified")
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, refusal(RefusedUnidentified, "its process cannot be read")
	}
	return process, nil
}

func checkUser(process windows.Handle) error {
	own, err := currentUserSID()
	if err != nil {
		return refusal(RefusedOwnUnreadable, "this process's user cannot be read")
	}
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return refusal(RefusedUnidentified, "its user cannot be read")
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return refusal(RefusedUnidentified, "its user cannot be read")
	}
	if !user.User.Sid.Equals(own) {
		return refusal(RefusedOtherUser, "it runs as another user")
	}
	return nil
}

var currentUserSID = readCurrentUserSID

func readCurrentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("cannot determine this process's user: %w", err)
	}
	return user.User.Sid, nil
}

func selfProgram() (programFile, error) {
	return programOf(windows.CurrentProcess())
}

func programOf(process windows.Handle) (programFile, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(process, 0, &buf[0], &size); err != nil {
		return programFile{}, errors.New("cannot be read")
	}
	path := windows.UTF16ToString(buf[:size])
	id, err := idOf(path)
	if errors.Is(err, errProgramGone) {
		return programFile{}, err
	}
	if err != nil {
		return programFile{}, errors.New("cannot be read")
	}
	return programFile{path: path, id: id}, nil
}

func idOf(path string) (fileID, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fileID{}, err
	}
	h, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return fileID{}, errProgramGone
	}
	if err != nil {
		return fileID{}, err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return fileID{}, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fileID{}, errors.New("is a directory")
	}
	return fileID{volume: info.VolumeSerialNumber, high: info.FileIndexHigh, low: info.FileIndexLow}, nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(plainPath(a), plainPath(b))
}

func plainPath(path string) string {
	if strings.HasPrefix(path, `\\?\`) && !strings.HasPrefix(path, `\\?\UNC\`) {
		path = path[len(`\\?\`):]
	}
	return filepath.Clean(path)
}

const processAccess = windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.SYNCHRONIZE | windows.PROCESS_TERMINATE

// Harden limits access to this process to identity queries, waiting, and
// termination through its DACL.
func Harden() error {
	sid, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("cannot protect the local process's memory: %w", err)
	}
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;;%#x;;;%s)(A;;%#x;;;OW)(A;;%#x;;;SY)",
		processAccess, sid.String(), processAccess, processAccess))
	if err != nil {
		return fmt.Errorf("cannot protect the local process's memory: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("cannot protect the local process's memory: %w", err)
	}
	if err := windows.SetSecurityInfo(windows.CurrentProcess(), windows.SE_KERNEL_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("cannot protect the local process's memory: %w", err)
	}
	return nil
}

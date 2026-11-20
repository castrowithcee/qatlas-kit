//go:build linux || darwin

package exteditor

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/sys/unix"
)

// targetPath is the PATH of an editor that runs as the target user; the inherited one belongs to root.
const targetPath = "/usr/local/bin:/usr/bin:/bin"

// session is one prepared edit. The directory stays open, so the copy is read and removed in the
// directory that was checked, even if its path is renamed or replaced meanwhile.
type session struct {
	cmd   *exec.Cmd
	dirFD int
	name  string
	owner uint32
	text  []byte
	limit int64
}

func prepare(r Request) (*session, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	geteuid := r.geteuid
	if geteuid == nil {
		geteuid = os.Geteuid
	}
	euid := geteuid()
	owner := uint32(euid)
	switch {
	case euid == 0 && r.Target == nil:
		return nil, ErrRoot
	case euid != 0 && r.Target != nil:
		return nil, fmt.Errorf("%w: a target user needs effective user ID 0", ErrInvalidRequest)
	case r.Target != nil:
		owner = r.Target.UID
	}
	dirFD, err := openPrivateDir(r.Dir, owner)
	if err != nil {
		return nil, err
	}
	ext, _ := extension(r.Name)
	name := rand.Text() + ext
	if err := writeCopy(dirFD, name, r.Text, r.Target); err != nil {
		_ = unix.Close(dirFD)
		return nil, err
	}
	cmd := exec.Command(r.Editor.Path, append(slices.Clone(r.Editor.Args), filepath.Join(r.Dir, name))...)
	cmd.Dir = r.Dir
	if t := r.Target; t != nil {
		// An empty, non-nil group list makes the child drop the supplementary groups of root.
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: t.UID, Gid: t.GID, Groups: []uint32{}}}
		cmd.Env = targetEnv(t.Home)
	}
	return &session{cmd: cmd, dirFD: dirFD, name: name, owner: owner, text: bytes.Clone(r.Text), limit: r.MaxSize}, nil
}

func targetEnv(home string) []string {
	env := []string{"HOME=" + home, "PATH=" + targetPath}
	for _, key := range []string{"TERM", "COLORTERM", "LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// openPrivateDir opens dir without following a symbolic link and checks the open directory itself, so a
// path swapped after the check cannot redirect the copy.
func openPrivateDir(dir string, owner uint32) (int, error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	switch {
	case errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EMLINK) || errors.Is(err, unix.ENOTDIR):
		// The platforms differ in the error for a link; Lstat only names the reason.
		if info, lerr := os.Lstat(dir); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			return -1, fmt.Errorf("%w: %s is a symbolic link", ErrUnsafeDir, dir)
		}
		return -1, fmt.Errorf("%w: %s is not a directory", ErrUnsafeDir, dir)
	case err != nil:
		return -1, &os.PathError{Op: "open", Path: dir, Err: err}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return -1, &os.PathError{Op: "stat", Path: dir, Err: err}
	}
	mode := uint32(st.Mode)
	switch {
	case mode&unix.S_IFMT != unix.S_IFDIR:
		err = fmt.Errorf("%w: %s is not a directory", ErrUnsafeDir, dir)
	case st.Uid != owner:
		err = fmt.Errorf("%w: %s belongs to user %d, not %d", ErrUnsafeDir, dir, st.Uid, owner)
	case mode&0o777 != 0o700:
		err = fmt.Errorf("%w: %s has mode %04o, want 0700", ErrUnsafeDir, dir, mode&0o777)
	}
	if err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func writeCopy(dirFD int, name string, text []byte, target *Target) error {
	fd, err := unix.Openat(dirFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create the copy: %w", err)
	}
	f := os.NewFile(uintptr(fd), name)
	_, err = f.Write(text)
	if err == nil && target != nil {
		err = f.Chown(int(target.UID), int(target.GID))
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = unix.Unlinkat(dirFD, name, 0)
		return fmt.Errorf("write the copy: %w", err)
	}
	return nil
}

// finish runs after the editor; it reads the copy only after a successful run and removes it always.
func (s *session) finish(runErr error) tea.Msg {
	defer unix.Close(s.dirFD)
	var text []byte
	var readErr error
	if runErr == nil {
		text, readErr = s.read()
	}
	removeErr := s.remove()
	switch {
	case runErr != nil:
		return FailedMsg{Err: errors.Join(fmt.Errorf("%w: %w", ErrEditorFailed, runErr), removeErr)}
	case readErr != nil || removeErr != nil:
		return FailedMsg{Err: errors.Join(readErr, removeErr)}
	case bytes.Equal(text, s.text):
		return UnchangedMsg{}
	}
	return ChangedMsg{Text: text}
}

// read accepts only a regular file with a single link that belongs to the user the editor ran as. The
// editor may have replaced the copy, and a link or a hard link to a file of another user must not be
// read with the rights of this process.
func (s *session) read() ([]byte, error) {
	// O_NONBLOCK keeps a FIFO put in place of the copy from blocking the open.
	fd, err := unix.Openat(s.dirFD, s.name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	switch {
	case errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EMLINK):
		return nil, fmt.Errorf("%w: the copy was replaced by a symbolic link", ErrUnsafeCopy)
	case err != nil:
		return nil, fmt.Errorf("read the copy: %w", err)
	}
	f := os.NewFile(uintptr(fd), s.name)
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, fmt.Errorf("read the copy: %w", err)
	}
	switch {
	case uint32(st.Mode)&unix.S_IFMT != unix.S_IFREG:
		return nil, fmt.Errorf("%w: the copy is no longer a regular file", ErrUnsafeCopy)
	case st.Uid != s.owner:
		return nil, fmt.Errorf("%w: the copy belongs to user %d, not %d", ErrUnsafeCopy, st.Uid, s.owner)
	case uint64(st.Nlink) != 1:
		return nil, fmt.Errorf("%w: the copy has %d links", ErrUnsafeCopy, st.Nlink)
	case st.Size > s.limit:
		return nil, fmt.Errorf("%w: %d bytes, the limit is %d", ErrTooLarge, st.Size, s.limit)
	}
	text, err := io.ReadAll(io.LimitReader(f, s.limit+1))
	if err != nil {
		return nil, fmt.Errorf("read the copy: %w", err)
	}
	if err := checkText(text, s.limit); err != nil {
		return nil, err
	}
	return text, nil
}

func (s *session) remove() error {
	if err := unix.Unlinkat(s.dirFD, s.name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("remove the copy: %w", err)
	}
	return nil
}

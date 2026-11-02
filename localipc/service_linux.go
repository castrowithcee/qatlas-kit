//go:build linux

package localipc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

const serviceSocketMode = 0o660

// groupsReader returns the supplementary groups of process pid, failing if it
// no longer runs with the effective uid and gid the kernel reported.
type groupsReader func(pid int32, uid, gid uint32) ([]uint32, error)

func servicePath(e ServiceEndpoint) (string, error) {
	if !filepath.IsAbs(e.Dir) || filepath.Clean(e.Dir) != e.Dir || !component(e.SocketBase) ||
		e.ServiceUser == "" || e.Group == "" {
		return "", fmt.Errorf("%s: directory, socket base, service user, or group is invalid", e.label())
	}
	path := filepath.Join(e.Dir, e.SocketBase+".sock")
	if len(path) > maxSocketPath {
		return "", &PathTooLongError{Path: path}
	}
	return path, nil
}

func listenService(e ServiceEndpoint, opts ListenOptions) (net.Listener, error) {
	if opts.Successor {
		return nil, fmt.Errorf("%s: %w", e.label(), errHandoverUnsupported)
	}
	path, err := e.Path()
	if err != nil {
		return nil, err
	}
	gid, err := lookupGroupID(e.Group)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	l, err := listenSocket(socketSpec{
		path:       path,
		mode:       serviceSocketMode,
		gid:        int(gid),
		prepareDir: func(dir string) error { return checkServiceDir(dir, gid) },
		verify:     func(c net.Conn) (int, error) { return admitClient(c, gid, procGroups) },
		refused:    opts.Refused,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	return l, nil
}

func checkServiceDir(dir string, gid uint32) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("service directory: %w", err)
	}
	uid, group, ok := ownerOf(info)
	if !info.IsDir() || !ok || uid != uint32(os.Getuid()) || group != gid || info.Mode().Perm()&0o007 != 0 {
		return fmt.Errorf("%w: service directory %q must belong to this user and the group and be closed to others",
			ErrRefused, dir)
	}
	return nil
}

func admitClient(conn net.Conn, gid uint32, groupsOf groupsReader) (int, error) {
	cred, err := peerCred(conn)
	if err != nil {
		return 0, err
	}
	return int(cred.Pid), admit(cred, gid, groupsOf)
}

func admit(cred *syscall.Ucred, gid uint32, groupsOf groupsReader) error {
	if cred.Uid == 0 || cred.Gid == gid {
		return nil
	}
	if cred.Pid <= 0 {
		return fmt.Errorf("%w: peer process cannot be identified", ErrRefused)
	}
	groups, err := groupsOf(cred.Pid, cred.Uid, cred.Gid)
	if err != nil {
		return fmt.Errorf("%w: peer groups cannot be read: %v", ErrRefused, err)
	}
	if !slices.Contains(groups, gid) {
		return fmt.Errorf("%w: peer is neither root nor a member of the group", ErrRefused)
	}
	return nil
}

func procGroups(pid int32, uid, gid uint32) ([]uint32, error) {
	f, err := os.Open(filepath.Join("/proc", strconv.Itoa(int(pid)), "status"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return statusGroups(f, uid, gid)
}

// statusGroups parses /proc/<pid>/status. Matching the effective ids guards
// against a process id that was reused since the peer connected.
func statusGroups(r io.Reader, uid, gid uint32) ([]uint32, error) {
	var groups []uint32
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		switch key {
		case "Uid", "Gid":
			want := uid
			if key == "Gid" {
				want = gid
			}
			if len(fields) < 2 || fields[1] != strconv.FormatUint(uint64(want), 10) {
				return nil, errors.New("the process changed since it connected")
			}
		case "Groups":
			for _, field := range fields {
				id, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return nil, errors.New("malformed group list")
				}
				groups = append(groups, uint32(id))
			}
		default:
			continue
		}
		seen[key] = true
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !seen["Uid"] || !seen["Gid"] || !seen["Groups"] {
		return nil, errors.New("incomplete process status")
	}
	return groups, nil
}

func dialService(ctx context.Context, e ServiceEndpoint) (net.Conn, error) {
	path, err := e.Path()
	if err != nil {
		return nil, err
	}
	allowed := []uint32{uint32(os.Getuid())}
	if uid, err := lookupUserID(e.ServiceUser); err == nil {
		allowed = append(allowed, uid)
	}
	if err := checkServiceFiles(path, allowed); err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	return establish(ctx, conn, e.label(), func(c net.Conn) (int, error) { return checkServer(c, allowed) }, nil)
}

func checkServiceFiles(path string, allowed []uint32) error {
	for _, file := range []struct {
		path string
		kind fs.FileMode
	}{{filepath.Dir(path), fs.ModeDir}, {path, fs.ModeSocket}} {
		info, err := os.Lstat(file.path)
		if err != nil {
			return err
		}
		uid, _, ok := ownerOf(info)
		if info.Mode().Type() != file.kind || !ok || !slices.Contains(allowed, uid) || info.Mode().Perm()&0o007 != 0 {
			return fmt.Errorf("%w: %q does not belong to the service or is open to others", ErrRefused, file.path)
		}
	}
	return nil
}

func checkServer(conn net.Conn, allowed []uint32) (int, error) {
	cred, err := peerCred(conn)
	if err != nil {
		return 0, err
	}
	if !slices.Contains(allowed, cred.Uid) {
		return int(cred.Pid), fmt.Errorf("%w: peer runs as neither the service user nor this user", ErrRefused)
	}
	return int(cred.Pid), nil
}

func lookupGroupID(name string) (uint32, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("cannot find group %q: %w", name, err)
	}
	id, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("group %q has an unusable id", name)
	}
	return uint32(id), nil
}

func lookupUserID(name string) (uint32, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(id), nil
}

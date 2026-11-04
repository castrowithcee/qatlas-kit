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
	"slices"
	"strings"
	"syscall"
)

const (
	dirMode    = 0o700
	socketMode = 0o600
	lockMode   = 0o600
)

func endpointPath(e Endpoint) (string, error) {
	if !component(e.SocketBase) {
		return "", errInvalidEndpoint(e)
	}
	root, err := endpointRoot(e)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, e.RuntimeDirName)
	if err := privateExistingDir(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s: %w", e.label(), err)
	}
	path := filepath.Join(dir, e.SocketBase+".sock")
	if len(path) > maxSocketPath {
		if e.RuntimeRoot != "" {
			return "", &PathTooLongError{Path: path}
		}
		return shortSocketPath(e, path)
	}
	return path, nil
}

func errInvalidEndpoint(e Endpoint) error {
	return fmt.Errorf("%s: runtime directory name, socket base, absolute fallback directory, or absolute runtime root is invalid", e.label())
}

// endpointRoot validates the fields shared by every operation and returns the
// directory that holds the runtime directory.
func endpointRoot(e Endpoint) (string, error) {
	fallbackOK := filepath.IsAbs(e.FallbackDir) || (e.RuntimeRoot != "" && e.FallbackDir == "")
	if !component(e.RuntimeDirName) || !fallbackOK || (e.RuntimeRoot != "" && !filepath.IsAbs(e.RuntimeRoot)) {
		return "", errInvalidEndpoint(e)
	}
	if e.RuntimeRoot != "" {
		// An explicit root replaces the search and never falls back to a
		// shorter location.
		if err := privateExistingDir(e.RuntimeRoot); err != nil {
			return "", fmt.Errorf("%s: runtime root: %w", e.label(), err)
		}
		return e.RuntimeRoot, nil
	}
	root, err := runtimeRoot(e.FallbackDir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", e.label(), err)
	}
	return root, nil
}

func component(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsRune(s, os.PathSeparator) &&
		!strings.ContainsRune(s, 0)
}

func privateExistingDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || !ownedByMe(info) || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: runtime directory %q is not a private directory of this user", ErrRefused, dir)
	}
	return nil
}

func privateDir(dir string) error {
	if err := os.Mkdir(dir, dirMode); err == nil {
		if err := os.Chmod(dir, dirMode); err != nil {
			return fmt.Errorf("cannot set runtime directory permissions %q: %w", dir, err)
		}
	} else if !os.IsExist(err) {
		return fmt.Errorf("cannot create runtime directory %q: %w", dir, err)
	}
	if err := privateExistingDir(dir); err != nil {
		return err
	}
	return nil
}

func ownedByMe(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid())
}

func ownerOf(info fs.FileInfo) (uid, gid uint32, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return st.Uid, st.Gid, true
}

func checkSocket(path string) error {
	if err := privateExistingDir(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Type() != fs.ModeSocket || !ownedByMe(info) || info.Mode().Perm() != socketMode {
		return fmt.Errorf("%w: socket %q is not private", ErrRefused, path)
	}
	return nil
}

func dial(ctx context.Context, e Endpoint) (net.Conn, error) {
	path, err := e.Path()
	if err != nil {
		return nil, err
	}
	if err := checkSocket(path); err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	return conn, nil
}

func running(e Endpoint) ([]string, error) {
	root, err := endpointRoot(e)
	if err != nil {
		return nil, err
	}
	dirs := []string{filepath.Join(root, e.RuntimeDirName)}
	if e.RuntimeRoot == "" {
		dirs = append(dirs, fallbackRuntimeDirs(e)...)
	}
	bases := []string{}
	for _, dir := range dirs {
		found, err := socketBases(dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.label(), err)
		}
		bases = append(bases, found...)
	}
	slices.Sort(bases)
	return slices.Compact(bases), nil
}

// socketBases lists the private sockets of this user in dir. Successor
// sockets, lock files, links and foreign files are not channels.
func socketBases(dir string) ([]string, error) {
	if err := privateExistingDir(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var bases []string
	for _, entry := range entries {
		base, ok := strings.CutSuffix(entry.Name(), ".sock")
		if !ok || !component(base) {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, entry.Name()))
		if err != nil || info.Mode().Type() != fs.ModeSocket || !ownedByMe(info) || info.Mode().Perm() != socketMode {
			continue
		}
		bases = append(bases, base)
	}
	return bases, nil
}

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
	"strings"
	"syscall"
)

const (
	dirMode    = 0o700
	socketMode = 0o600
)

func endpointPath(e Endpoint) (string, error) {
	if !component(e.RuntimeDirName) || !component(e.SocketBase) || !filepath.IsAbs(e.FallbackDir) {
		return "", fmt.Errorf("%s: runtime directory name, socket base, or absolute fallback directory is invalid", e.label())
	}
	root, err := runtimeRoot(e.FallbackDir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", e.label(), err)
	}
	dir := filepath.Join(root, e.RuntimeDirName)
	if err := privateExistingDir(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s: %w", e.label(), err)
	}
	path := filepath.Join(dir, e.SocketBase+".sock")
	if len(path) > maxSocketPath {
		return shortSocketPath(e, path)
	}
	return path, nil
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
	if err := VerifyPeer(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	return conn, nil
}

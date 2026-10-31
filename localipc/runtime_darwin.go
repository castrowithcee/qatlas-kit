//go:build darwin

package localipc

import (
	"fmt"
	"os"
	"path/filepath"
)

func runtimeRoot(fallback string) (string, error) {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(xdg) {
		if err := privateExistingDir(xdg); err != nil {
			return "", fmt.Errorf("XDG_RUNTIME_DIR: %w", err)
		}
		return xdg, nil
	}
	return fallback, nil
}

// macOS provides a private per-user temporary directory when the primary path
// cannot fit into a Unix socket address.
func shortSocketPath(e Endpoint, original string) (string, error) {
	tmp := os.Getenv("TMPDIR")
	if !filepath.IsAbs(tmp) {
		return "", &PathTooLongError{Path: original}
	}
	if err := privateExistingDir(tmp); err != nil {
		return "", fmt.Errorf("%s: TMPDIR: %w", e.label(), err)
	}
	dir := filepath.Join(tmp, e.RuntimeDirName)
	if err := privateExistingDir(dir); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("%s: %w", e.label(), err)
	}
	path := filepath.Join(dir, e.SocketBase+".sock")
	if len(path) > maxSocketPath {
		return "", &PathTooLongError{Path: path}
	}
	return path, nil
}

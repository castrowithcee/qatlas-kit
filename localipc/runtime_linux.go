//go:build linux

package localipc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

var userRuntimeRoot = "/run/user"

// runtimeRoot reports whether it fell back to the fallback directory.
func runtimeRoot(fallback string) (string, bool, error) {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(xdg) {
		if err := privateExistingDir(xdg); err != nil {
			return "", false, fmt.Errorf("XDG_RUNTIME_DIR: %w", err)
		}
		return xdg, false, nil
	}
	if user := filepath.Join(userRuntimeRoot, strconv.Itoa(os.Getuid())); privateExistingDir(user) == nil {
		return user, false, nil
	}
	return fallback, true, nil
}

func shortSocketPath(_ Endpoint, path string) (string, error) {
	return "", &PathTooLongError{Path: path}
}

func fallbackRuntimeDirs(Endpoint) []string { return nil }

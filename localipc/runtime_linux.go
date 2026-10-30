//go:build linux

package localipc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

var userRuntimeRoot = "/run/user"

func runtimeRoot(fallback string) (string, error) {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(xdg) {
		if err := privateExistingDir(xdg); err != nil {
			return "", fmt.Errorf("XDG_RUNTIME_DIR: %w", err)
		}
		return xdg, nil
	}
	if user := filepath.Join(userRuntimeRoot, strconv.Itoa(os.Getuid())); privateExistingDir(user) == nil {
		return user, nil
	}
	return fallback, nil
}

func shortSocketPath(_ Endpoint, path string) (string, error) {
	return "", &PathTooLongError{Path: path}
}

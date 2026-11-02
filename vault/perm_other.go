//go:build !windows

package vault

import (
	"io/fs"
	"os"
)

// checkMode refuses a vault file that others can read or write.
func checkMode(path string, info fs.FileInfo) error {
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return &PermissionError{Path: path, Mode: perm}
	}
	return nil
}

func restrictFile(path string) error { return os.Chmod(path, fileMode) }

func restrictDir(path string) error { return os.Chmod(path, dirMode) }

func mkdirPrivate(dir string) error { return os.MkdirAll(dir, dirMode) }

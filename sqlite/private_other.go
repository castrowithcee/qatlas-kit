//go:build !windows

package sqlite

import "os"

func restrictFile(path string) error { return os.Chmod(path, fileMode) }

func restrictDir(path string) error { return os.Chmod(path, dirMode) }

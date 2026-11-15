package sqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	fileMode = 0o600
	dirMode  = 0o700
)

// mkdirPrivate creates dir and every missing parent for the current user alone. Existing directories keep
// their permissions.
func mkdirPrivate(dir string) error {
	var missing []string
	for path := dir; ; path = filepath.Dir(path) {
		_, err := os.Lstat(path)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, path)
		if parent := filepath.Dir(path); parent == path {
			return fmt.Errorf("cannot find an existing parent of %s", dir)
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], dirMode); err != nil {
			return err
		}
		if err := restrictDir(missing[i]); err != nil {
			return err
		}
	}
	return nil
}

// createPrivate creates a new file for the current user alone. It never replaces or follows anything at
// path: an existing file or link fails with an error that matches os.ErrExist.
func createPrivate(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return nil, err
	}
	if err := restrictFile(path); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return f, nil
}

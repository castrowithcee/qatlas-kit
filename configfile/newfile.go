package configfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// ExistsError reports a path that is already taken. errors.Is(err, fs.ErrExist) is true for it.
type ExistsError struct{ Path string }

func (e *ExistsError) Error() string        { return fmt.Sprintf("%s already exists", e.Path) }
func (e *ExistsError) Is(target error) bool { return target == fs.ErrExist }

// WriteNewPrivateFile creates path with access for the current user alone and fills it with data. It never
// replaces or follows anything at path: an existing file or link is an *ExistsError. Access is restricted
// before data is written, and a file that could not be written completely is removed.
func WriteNewPrivateFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if errors.Is(err, fs.ErrExist) {
		return &ExistsError{Path: path}
	}
	if err != nil {
		return err
	}
	err = restrictFile(path)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write new private file %s: %w", path, err)
	}
	return nil
}

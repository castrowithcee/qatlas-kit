package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/castrowithcee/qatlas-kit/fileretry"
)

const (
	fileMode = 0o600
	dirMode  = 0o700
)

// ResolvePath chooses a flag path, file environment variable, directory environment variable, or default directory.
// Environment variable names, the default directory, and the file name belong to the caller.
func ResolvePath(flagPath, fileEnv, dirEnv, defaultDir, fileName string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	if fileEnv != "" {
		if path := os.Getenv(fileEnv); path != "" {
			return path, nil
		}
	}
	if fileName == "" || filepath.Base(fileName) != fileName || fileName == "." || fileName == ".." {
		return "", errors.New("invalid configuration file name")
	}
	if dirEnv != "" {
		if dir := os.Getenv(dirEnv); dir != "" {
			return filepath.Join(dir, fileName), nil
		}
	}
	if defaultDir == "" {
		return "", errors.New("missing default configuration directory")
	}
	return filepath.Join(defaultDir, fileName), nil
}

// HasCurrentSchema reads the file and asks the caller to extract its schema version.
// A missing, unreadable, or malformed file does not count as current.
func HasCurrentSchema(path string, current int, version func([]byte) (int, error)) bool {
	if version == nil {
		return false
	}
	var data []byte
	err := fileretry.Do(func() (err error) {
		data, err = os.ReadFile(path)
		return err
	}, fileretry.Transient, nil)
	if err != nil {
		return false
	}
	got, err := version(data)
	return err == nil && got == current
}

// prepare checks the caller's document before creating any file, then verifies that the
// bytes to be written decode into a valid document.
func prepare[T any](value T, validate func(T) error, encode func(T) ([]byte, error), decode func([]byte) (T, error)) ([]byte, error) {
	if validate == nil || encode == nil || decode == nil {
		return nil, errors.New("missing configuration codec callback")
	}
	if err := validate(value); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	data, err := encode(value)
	if err != nil {
		return nil, fmt.Errorf("encode configuration: %w", err)
	}
	decoded, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("encoded configuration cannot be decoded: %w", err)
	}
	if err := validate(decoded); err != nil {
		return nil, fmt.Errorf("encoded configuration is invalid: %w", err)
	}
	return data, nil
}

// replace writes only after prepare and any required pre-replacement steps have succeeded.
// The caller must hold the document lock and verify the expected revision.
func replace(path string, data []byte) error {
	target := path
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, err = filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("resolve configuration link %s: %w", path, err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect configuration path %s: %w", path, err)
	}

	dir := filepath.Dir(target)
	if err := mkdirPrivate(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	name := tmp.Name()
	moved := false
	defer func() {
		if !moved {
			_ = os.Remove(name)
		}
	}()
	if err := restrictFile(name); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("restrict temporary configuration: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := preservePermissions(target, name); err != nil {
		return fmt.Errorf("preserve configuration permissions: %w", err)
	}
	if err := fileretry.Do(func() error { return os.Rename(name, target) }, fileretry.Transient, nil); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	moved = true
	return nil
}

func mkdirPrivate(dir string) error {
	var missing []string
	for path := dir; ; path = filepath.Dir(path) {
		_, err := os.Lstat(path)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect configuration directory %s: %w", path, err)
		}
		missing = append(missing, path)
		if parent := filepath.Dir(path); parent == path {
			return fmt.Errorf("cannot find existing parent for %s", dir)
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], dirMode); err != nil {
			return fmt.Errorf("create configuration directory %s: %w", missing[i], err)
		}
		if err := restrictDir(missing[i]); err != nil {
			return fmt.Errorf("restrict configuration directory %s: %w", missing[i], err)
		}
	}
	return nil
}

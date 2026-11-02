package vault

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	fileMode = 0o600
	dirMode  = 0o700
)

// PermissionError reports a vault file that other users can reach. It is refused, not read.
type PermissionError struct {
	Path string
	Mode fs.FileMode
	// Grantee is set on Windows, where the ACL governs access: the account the ACL lets reach the file
	// besides its user.
	Grantee string
}

func (e *PermissionError) Error() string {
	if e.Grantee != "" {
		return fmt.Sprintf("%s holds vault data but its ACL lets %s reach it; it is not read until it is "+
			"private again: icacls \"%s\" /inheritance:r /grant:r \"%%USERNAME%%:F\"", e.Path, e.Grantee, e.Path)
	}
	return fmt.Sprintf("%s holds vault data but its mode is %04o; it is not read until it is private again: "+
		"chmod 600 %s", e.Path, e.Mode.Perm(), e.Path)
}

// readFile reads path after checking that nobody else can reach it.
func readFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := checkMode(path, info); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// write replaces path atomically with a private file, creating its directory privately if needed. A
// symbolic link at path is followed, so its target is replaced and the link stays.
func (v *Vault) write(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)
	if err := mkdirPrivate(dir); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	// The temporary name never ends in the pending suffix, so an interrupted write is never merged.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", target, err)
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
		return fmt.Errorf("cannot set the permissions of %s: %w", name, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := v.rename(name, target); err != nil {
		return fmt.Errorf("cannot replace %s: %w", target, err)
	}
	moved = true
	return nil
}

// createDir makes the vault directory private when a vault is created in it.
func (v *Vault) createDir() error {
	if err := mkdirPrivate(v.opts.Dir); err != nil {
		return fmt.Errorf("cannot create %s: %w", v.opts.Dir, err)
	}
	if err := restrictDir(v.opts.Dir); err != nil {
		return fmt.Errorf("cannot set the permissions of %s: %w", v.opts.Dir, err)
	}
	return nil
}

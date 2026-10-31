//go:build !windows

package configfile

import "os"

func restrictFile(path string) error { return os.Chmod(path, fileMode) }

func restrictDir(path string) error { return os.Chmod(path, dirMode) }

func preservePermissions(source, target string) error {
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.Chmod(target, info.Mode().Perm())
}

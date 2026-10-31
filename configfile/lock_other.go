//go:build !windows

package configfile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openLockFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, fileMode)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0) {
		err = errors.New("lock file is not a private regular file")
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

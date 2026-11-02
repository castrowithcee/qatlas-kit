package ptytest

import (
	"bytes"
	"errors"
	"unsafe"

	"golang.org/x/sys/unix"
)

const getTermios = unix.TIOCGETA

// unlock grants and unlocks the pseudo-terminal of master with the ioctls grantpt and unlockpt use, and
// returns the path of its slave side.
func unlock(master int) (string, error) {
	if err := unix.IoctlSetInt(master, unix.TIOCPTYGRANT, 0); err != nil {
		return "", err
	}
	if err := unix.IoctlSetInt(master, unix.TIOCPTYUNLK, 0); err != nil {
		return "", err
	}
	// TIOCPTYGNAME writes the path, at most 128 bytes, into the buffer it is given.
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(master), unix.TIOCPTYGNAME,
		uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		return "", errno
	}
	end := bytes.IndexByte(name[:], 0)
	if end <= 0 {
		return "", errors.New("the pseudo-terminal has no name")
	}
	return string(name[:end]), nil
}

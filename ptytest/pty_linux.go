package ptytest

import (
	"strconv"

	"golang.org/x/sys/unix"
)

const getTermios = unix.TCGETS

// unlock unlocks the pseudo-terminal of master and returns the path of its slave side.
func unlock(master int) (string, error) {
	if err := unix.IoctlSetPointerInt(master, unix.TIOCSPTLCK, 0); err != nil {
		return "", err
	}
	n, err := unix.IoctlGetInt(master, unix.TIOCGPTN)
	if err != nil {
		return "", err
	}
	return "/dev/pts/" + strconv.Itoa(n), nil
}

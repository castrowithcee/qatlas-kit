//go:build linux

package localipc

import (
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

// terminate opens the process before recheck proves that it still serves the
// channel, so the signal reaches that process even if its id is reused
// afterwards. Kernels without pidfd fall back to the id after the recheck.
func terminate(pid int, recheck func() error) error {
	fd, err := unix.PidfdOpen(pid, 0)
	switch {
	case err == nil:
		defer unix.Close(fd)
	case errors.Is(err, unix.ENOSYS):
		fd = -1
	case errors.Is(err, unix.ESRCH):
		return err
	default:
		return fmt.Errorf("cannot open process %d: %w", pid, err)
	}
	if err := recheck(); err != nil {
		return err
	}
	if fd < 0 {
		err = syscall.Kill(pid, syscall.SIGTERM)
	} else {
		err = unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0)
	}
	if err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("cannot signal process %d: %w", pid, err)
	}
	return err
}

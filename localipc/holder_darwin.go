//go:build darwin

package localipc

import (
	"errors"
	"fmt"
	"syscall"
)

// terminate signals by process id, as macOS has no process handle for it: a
// process that ends right after recheck and whose id is reused by another
// process of the user in that moment would receive the signal instead.
func terminate(pid int, recheck func() error) error {
	if err := recheck(); err != nil {
		return err
	}
	err := syscall.Kill(pid, syscall.SIGTERM)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("cannot signal process %d: %w", pid, err)
	}
	return err
}

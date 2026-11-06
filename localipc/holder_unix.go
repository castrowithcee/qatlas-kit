//go:build linux || darwin

package localipc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"syscall"
)

func holder(ctx context.Context, e Endpoint) (int, error) {
	path, err := e.Path()
	if err != nil {
		return 0, err
	}
	if err := checkSocket(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("%s: %w", e.label(), ErrNotServed)
		}
		return 0, fmt.Errorf("%s: %w", e.label(), err)
	}
	if !serves(path) {
		return 0, fmt.Errorf("%s: %w", e.label(), ErrNotServed)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("%s: %w", e.label(), ErrNotServed)
		}
		return 0, fmt.Errorf("%s: %w", e.label(), err)
	}
	defer conn.Close()
	pid, err := checkPeer(conn, false)
	if err == nil && pid == os.Getpid() {
		err = fmt.Errorf("%w: this process serves the channel", ErrRefused)
	}
	if err != nil {
		return 0, fmt.Errorf("%s: %w", e.label(), err)
	}
	return pid, nil
}

func endHolder(ctx context.Context, e Endpoint) (int, error) {
	pid, err := holder(ctx, e)
	if err != nil {
		return 0, err
	}
	recheck := func() error {
		again, err := holder(ctx, e)
		if err == nil && again != pid {
			err = fmt.Errorf("%s: %w: another process serves the channel now", e.label(), ErrRefused)
		}
		return err
	}
	if err := terminate(pid, recheck); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return 0, fmt.Errorf("%s: %w: the process ended", e.label(), ErrNotServed)
		}
		return 0, err
	}
	return pid, nil
}

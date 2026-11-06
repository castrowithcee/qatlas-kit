//go:build windows

package localipc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func holder(ctx context.Context, e Endpoint) (int, error) {
	conn, err := dial(ctx, e)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, os.ErrDeadlineExceeded):
		return 0, fmt.Errorf("%s: %w", e.label(), ErrNotServed)
	case errors.Is(err, fs.ErrPermission):
		return 0, fmt.Errorf("%s: %w: %w", e.label(), ErrRefused, err)
	default:
		return 0, fmt.Errorf("%s: %w", e.label(), err)
	}
	defer conn.Close()
	pipe := conn.(*pipeConn)
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(pipe.h, &pid); err != nil {
		return 0, fmt.Errorf("%s: %w: the serving process is unavailable", e.label(), ErrRefused)
	}
	err = checkPipeOwner(pipe.h)
	if err == nil && pid == windows.GetCurrentProcessId() {
		err = fmt.Errorf("%w: this process serves the channel", ErrRefused)
	}
	if err == nil {
		err = checkPeerUser(pid)
	}
	if err != nil {
		return 0, fmt.Errorf("%s: %w", e.label(), err)
	}
	return int(pid), nil
}

// endHolder opens the process before the second check, so the id cannot be
// reused before TerminateProcess acts on the same handle.
func endHolder(ctx context.Context, e Endpoint) (int, error) {
	pid, err := holder(ctx, e)
	if err != nil {
		return 0, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false,
		uint32(pid))
	if err != nil {
		return 0, fmt.Errorf("%s: cannot open process %d: %w", e.label(), pid, err)
	}
	defer windows.CloseHandle(process)
	again, err := holder(ctx, e)
	if err == nil && again != pid {
		err = fmt.Errorf("%s: %w: another process serves the channel now", e.label(), ErrRefused)
	}
	if err != nil {
		return 0, err
	}
	if err := checkUser(process); err != nil {
		return 0, fmt.Errorf("%s: %w", e.label(), err)
	}
	if err := windows.TerminateProcess(process, 1); err != nil {
		return 0, fmt.Errorf("%s: cannot end process %d: %w", e.label(), pid, err)
	}
	return pid, nil
}

// checkPipeOwner requires the pipe to be owned by this user or by the default
// owner of this process's token, which an elevated token sets to the
// administrators group for every object it creates.
func checkPipeOwner(h windows.Handle) error {
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%w: the pipe's owner cannot be read", ErrRefused)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return fmt.Errorf("%w: the pipe's owner cannot be read", ErrRefused)
	}
	user, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("%w: this process's user cannot be read", ErrRefused)
	}
	if owner.Equals(user) {
		return nil
	}
	if def, err := defaultOwner(); err == nil && owner.Equals(def) {
		return nil
	}
	return fmt.Errorf("%w: the pipe has another owner", ErrRefused)
}

func defaultOwner() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	n := uint32(128)
	for {
		buf := make([]byte, n)
		err := windows.GetTokenInformation(token, windows.TokenOwner, &buf[0], n, &n)
		if err == nil {
			return (*struct{ Owner *windows.SID })(unsafe.Pointer(&buf[0])).Owner.Copy()
		}
		if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			return nil, err
		}
	}
}

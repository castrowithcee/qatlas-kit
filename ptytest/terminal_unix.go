//go:build linux || darwin

package ptytest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

type unixConsole struct {
	master *os.File
}

func start(c Command, size Size) (console, *os.Process, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("opening a pseudo-terminal: %w", err)
	}
	slave, err := openSlave(master, size)
	if err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	defer slave.Close()

	env := c.Env
	if env == nil {
		// exec.Cmd takes a nil environment as the environment of the caller.
		env = []string{}
	}
	cmd := &exec.Cmd{
		Path:   c.Path,
		Args:   append([]string{c.Path}, c.Args...),
		Env:    env,
		Dir:    c.Dir,
		Stdin:  slave,
		Stdout: slave,
		Stderr: slave,
		// Ctty is the descriptor in the program, where the terminal is standard input.
		SysProcAttr: &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0},
	}
	if err := cmd.Start(); err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	return &unixConsole{master: master}, cmd.Process, nil
}

// openSlave unlocks the terminal of master, sets its size and opens its slave side.
func openSlave(master *os.File, size Size) (*os.File, error) {
	var path string
	var opErr error
	// SyscallConn instead of Fd, which would make the master blocking so Close no longer ends a Read.
	raw, err := master.SyscallConn()
	if err != nil {
		return nil, err
	}
	if err := raw.Control(func(fd uintptr) {
		if path, opErr = unlock(int(fd)); opErr != nil {
			return
		}
		opErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ,
			&unix.Winsize{Col: uint16(size.Columns), Row: uint16(size.Rows)})
	}); err != nil {
		return nil, err
	}
	if opErr != nil {
		return nil, fmt.Errorf("preparing the pseudo-terminal: %w", opErr)
	}
	slave, err := os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("opening the pseudo-terminal: %w", err)
	}
	return slave, nil
}

func (c *unixConsole) Read(p []byte) (int, error)  { return c.master.Read(p) }
func (c *unixConsole) Write(p []byte) (int, error) { return c.master.Write(p) }

func (c *unixConsole) echo() (bool, error) {
	raw, err := c.master.SyscallConn()
	if err != nil {
		return false, err
	}
	var termios *unix.Termios
	var opErr error
	if err := raw.Control(func(fd uintptr) { termios, opErr = unix.IoctlGetTermios(int(fd), getTermios) }); err != nil {
		return false, err
	}
	if opErr != nil {
		return false, fmt.Errorf("reading the terminal mode: %w", opErr)
	}
	return termios.Lflag&unix.ECHO != 0, nil
}

// hangUp has nothing to do: the output ends once the last process holding the terminal closed it.
func (c *unixConsole) hangUp() {}

func (c *unixConsole) close() error { return c.master.Close() }

func processAlive(pid int) (bool, error) {
	err := unix.Kill(pid, 0)
	switch {
	case err == nil, errors.Is(err, unix.EPERM):
		return true, nil
	case errors.Is(err, unix.ESRCH):
		return false, nil
	default:
		return false, err
	}
}

func killProcess(pid int) error {
	err := unix.Kill(pid, unix.SIGKILL)
	if errors.Is(err, unix.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

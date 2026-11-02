package ptytest

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsConsole struct {
	handle    windows.Handle
	closeOnce sync.Once
	input     *os.File
	output    *os.File
}

func start(c Command, size Size) (console, *os.Process, error) {
	// The x/sys wrapper panics where the system lacks pseudo consoles, before Windows 10 1809.
	if windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find() != nil {
		return nil, nil, ErrUnsupported
	}
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, nil, fmt.Errorf("creating the console input: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		_ = windows.CloseHandle(inRead)
		_ = windows.CloseHandle(inWrite)
		return nil, nil, fmt.Errorf("creating the console output: %w", err)
	}
	var handle windows.Handle
	err := windows.CreatePseudoConsole(windows.Coord{X: int16(size.Columns), Y: int16(size.Rows)}, inRead, outWrite, 0, &handle)
	// The pseudo console holds its own copies of its ends.
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	if err != nil {
		_ = windows.CloseHandle(inWrite)
		_ = windows.CloseHandle(outRead)
		return nil, nil, fmt.Errorf("creating the pseudo console: %w", err)
	}
	con := &windowsConsole{
		handle: handle,
		input:  os.NewFile(uintptr(inWrite), "console-input"),
		output: os.NewFile(uintptr(outRead), "console-output"),
	}
	process, err := startIn(handle, c)
	if err != nil {
		_ = con.close()
		return nil, nil, err
	}
	return con, process, nil
}

// startIn starts the command attached to the pseudo console handle.
func startIn(handle windows.Handle, c Command) (*os.Process, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("creating the process attributes: %w", err)
	}
	defer attrs.Delete()
	// The attribute's value is the console handle itself, not a pointer to it.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&handle)),
		unsafe.Sizeof(handle)); err != nil {
		return nil, fmt.Errorf("attaching the pseudo console: %w", err)
	}
	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	// Standard handles that are named but left empty, as Windows Terminal starts its shells: without them
	// the program gets the standard handles of this process instead of those of the pseudo console.
	si.Flags = windows.STARTF_USESTDHANDLES

	app, err := windows.UTF16PtrFromString(c.Path)
	if err != nil {
		return nil, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{c.Path}, c.Args...)))
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if c.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(c.Dir); err != nil {
			return nil, err
		}
	}
	env, err := environmentBlock(c.Env)
	if err != nil {
		return nil, err
	}
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(app, commandLine, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT, env, dir,
		&si.StartupInfo, &pi); err != nil {
		return nil, fmt.Errorf("starting %s: %w", c.Path, err)
	}
	_ = windows.CloseHandle(pi.Thread)
	// The handle held until FindProcess has its own keeps the process id from being reused in between.
	defer windows.CloseHandle(pi.Process)
	process, err := os.FindProcess(int(pi.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		return nil, err
	}
	return process, nil
}

// environmentBlock is env as CreateProcess takes it, with SYSTEMROOT added the way os/exec adds it.
func environmentBlock(env []string) (*uint16, error) {
	env = append([]string{}, env...)
	found := false
	for _, kv := range env {
		found = found || strings.HasPrefix(strings.ToUpper(kv), "SYSTEMROOT=")
	}
	if !found {
		env = append(env, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
	}
	sort.SliceStable(env, func(i, j int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[j]) })
	var block []uint16
	for _, kv := range env {
		utf16, err := windows.UTF16FromString(kv)
		if err != nil {
			return nil, err
		}
		block = append(block, utf16...)
	}
	block = append(block, 0)
	return &block[0], nil
}

func (c *windowsConsole) Read(p []byte) (int, error)  { return c.output.Read(p) }
func (c *windowsConsole) Write(p []byte) (int, error) { return c.input.Write(p) }

// echo is unknown outside the pseudo console, which keeps the console mode to itself.
func (c *windowsConsole) echo() (bool, error) {
	return false, fmt.Errorf("the echo state of a pseudo console is unknown: %w", errors.ErrUnsupported)
}

// hangUp closes the pseudo console, which ends its output once everything the program wrote was read.
func (c *windowsConsole) hangUp() {
	c.closeOnce.Do(func() { windows.ClosePseudoConsole(c.handle) })
}

func (c *windowsConsole) close() error {
	c.hangUp()
	return errors.Join(c.input.Close(), c.output.Close())
}

func processAlive(pid int) (bool, error) {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	switch {
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return false, nil
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return true, nil
	case err != nil:
		return false, err
	}
	defer windows.CloseHandle(process)
	event, err := windows.WaitForSingleObject(process, 0)
	if err != nil {
		return false, err
	}
	return event == uint32(windows.WAIT_TIMEOUT), nil
}

func killProcess(pid int) error {
	process, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return os.ErrProcessDone
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.TerminateProcess(process, 1)
}

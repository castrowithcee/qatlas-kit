//go:build windows

package background

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

// The handover is the standard input of the started process and the report its standard output, two
// anonymous pipes; its standard error is the null device and it inherits no other handle.
func startProcess(ctx context.Context, c Command, handover []byte) (Result, error) {
	handoverRead, handoverWrite, err := os.Pipe()
	if err != nil {
		return Result{}, err
	}
	defer handoverWrite.Close()
	reportRead, reportWrite, err := os.Pipe()
	if err != nil {
		_ = handoverRead.Close()
		return Result{}, err
	}
	defer reportRead.Close()

	dir, err := windows.GetSystemDirectory()
	if err != nil {
		dir = filepath.VolumeName(c.Program) + `\`
	}
	start := func(flags uint32) (*exec.Cmd, error) {
		cmd := exec.Command(c.Program, c.Args...)
		cmd.Env = c.Env
		cmd.Dir = dir
		cmd.Stdin = handoverRead
		cmd.Stdout = reportWrite
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		return cmd, cmd.Start()
	}
	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	cmd, err := start(flags | windows.CREATE_BREAKAWAY_FROM_JOB)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// The job this process runs in lets no process leave it.
		cmd, err = start(flags)
	}
	_ = handoverRead.Close()
	_ = reportWrite.Close()
	if err != nil {
		return Result{}, fmt.Errorf("cannot start the background process: %w", err)
	}
	pid := cmd.Process.Pid

	// An anonymous pipe takes no deadline. A process that neither took the handover nor reported when ctx
	// ends is killed instead, which ends both pipes. Once the start is decided, the handle is released.
	var mu sync.Mutex
	released := false
	stop := context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if !released {
			_ = cmd.Process.Kill()
		}
	})
	release := func(kill bool) {
		stop()
		mu.Lock()
		defer mu.Unlock()
		released = true
		if kill {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		_ = cmd.Process.Release()
	}

	if len(handover) > 0 {
		_, err = handoverWrite.Write(handover)
	}
	_ = handoverWrite.Close()
	if err != nil {
		release(true)
		if ctx.Err() != nil {
			return Result{}, outcome("", ctx.Err())
		}
		return Result{}, errors.New("the background process did not take the handover")
	}

	report, _ := bufio.NewReader(io.LimitReader(reportRead, maxReport)).ReadString('\n')
	if err := outcome(report, ctx.Err()); err != nil {
		release(true)
		return Result{}, err
	}
	release(false)
	return Result{Report: trimReport(report), PID: pid}, nil
}

// inherited returns the standard input and output Start passed, or false unless both are pipes.
func inherited() (handover, report *os.File, ok bool) {
	if !isPipe(os.Stdin) || !isPipe(os.Stdout) {
		return nil, nil, false
	}
	return os.Stdin, os.Stdout, true
}

func isPipe(f *os.File) bool {
	if f == nil {
		return false
	}
	kind, err := windows.GetFileType(windows.Handle(f.Fd()))
	return err == nil && kind == windows.FILE_TYPE_PIPE
}

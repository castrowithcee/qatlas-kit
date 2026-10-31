//go:build linux || darwin

package background

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// The descriptors the started process inherits.
const (
	handoverFD = 3
	reportFD   = 4
)

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

	cmd := exec.Command(c.Program, c.Args...)
	cmd.Env = c.Env
	cmd.Dir = "/"
	cmd.ExtraFiles = []*os.File{handoverRead, reportWrite} // handoverFD and reportFD
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	// The process holds its own ends now; closing ours makes its end, or its report, an end of file.
	_ = handoverRead.Close()
	_ = reportWrite.Close()
	if err != nil {
		return Result{}, fmt.Errorf("cannot start the background process: %w", err)
	}
	// Reaps the process should it end while the caller still runs.
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	fail := func(err error) (Result, error) {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return Result{}, err
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = handoverWrite.SetWriteDeadline(deadline)
		_ = reportRead.SetReadDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() {
		past := time.Unix(1, 0)
		_ = handoverWrite.SetWriteDeadline(past)
		_ = reportRead.SetReadDeadline(past)
	})
	defer stop()

	if len(handover) > 0 {
		_, err = handoverWrite.Write(handover)
	}
	_ = handoverWrite.Close()
	if err != nil {
		if ctx.Err() != nil {
			return fail(outcome("", ctx.Err()))
		}
		return fail(errors.New("the background process did not take the handover"))
	}

	report, err := bufio.NewReader(io.LimitReader(reportRead, maxReport)).ReadString('\n')
	ended := ctx.Err()
	if ended == nil && errors.Is(err, os.ErrDeadlineExceeded) {
		ended = context.DeadlineExceeded // the read deadline can fire just before the context's own timer
	}
	if err := outcome(report, ended); err != nil {
		return fail(err)
	}
	return Result{Report: trimReport(report), PID: cmd.Process.Pid}, nil
}

// inherited returns the descriptors Start passed, or false unless both are pipes.
func inherited() (handover, report *os.File, ok bool) {
	if !isPipe(handoverFD) || !isPipe(reportFD) {
		return nil, nil, false
	}
	// Processes the caller starts later must not inherit them.
	syscall.CloseOnExec(handoverFD)
	syscall.CloseOnExec(reportFD)
	return os.NewFile(handoverFD, "handover"), os.NewFile(reportFD, "report"), true
}

func isPipe(fd int) bool {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return false
	}
	return stat.Mode&syscall.S_IFMT == syscall.S_IFIFO
}

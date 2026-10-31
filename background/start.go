package background

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Start runs cmd as a detached background process, writes handover (which may be empty) to the descriptor
// the process reads with Child.ReadHandover, and waits until the process reports ReportReady or
// ReportRunning, ctx ends, or the timeout passes. Any other result ends the process before Start returns
// an error. After a success the process keeps running independently of the caller.
func Start(ctx context.Context, cmd Command, handover []byte, opts Options) (Result, error) {
	if cmd.Program == "" {
		program, err := os.Executable()
		if err != nil {
			return Result{}, fmt.Errorf("cannot find this program to start it in the background: %w", err)
		}
		cmd.Program = program
	}
	timeout := opts.Timeout
	if timeout < 0 {
		return Result{}, errors.New("negative timeout")
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return startProcess(ctx, cmd, handover)
}

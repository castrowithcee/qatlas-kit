//go:build !linux && !darwin && !windows

package background

import (
	"context"
	"os"
)

func startProcess(context.Context, Command, []byte) (Result, error) { return Result{}, ErrUnsupported }

func inherited() (handover, report *os.File, ok bool) { return nil, nil, false }

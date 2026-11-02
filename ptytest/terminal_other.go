//go:build !linux && !darwin && !windows

package ptytest

import "os"

func start(Command, Size) (console, *os.Process, error) { return nil, nil, ErrUnsupported }

func processAlive(int) (bool, error) { return false, ErrUnsupported }

func killProcess(int) error { return ErrUnsupported }

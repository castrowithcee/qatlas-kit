//go:build windows

package vaultproc

import (
	"os"
	"syscall"
)

// Windows has no handover; an update locks the vault process instead.
const handoverSupported = false

// A named pipe is no file to watch, and opening it would take an instance from a client.
const socketFile = false

func stopSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }

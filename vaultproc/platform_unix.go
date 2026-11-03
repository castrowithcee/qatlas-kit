//go:build linux || darwin

package vaultproc

import (
	"os"
	"syscall"
)

// handoverSupported reports whether a vault process here hands over to a successor.
const handoverSupported = true

// socketFile reports whether the channel is a file the server can watch.
const socketFile = true

// stopSignals lock a vault process as OpLock would.
func stopSignals() []os.Signal { return []os.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT} }

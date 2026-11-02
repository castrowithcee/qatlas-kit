//go:build !linux && !darwin && !windows

package vaultproc

import "os"

const handoverSupported = false

const socketFile = false

func stopSignals() []os.Signal { return []os.Signal{os.Interrupt} }

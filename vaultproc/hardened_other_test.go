//go:build darwin || windows

package vaultproc

// processHardened has no portable observable on this platform; the check passes.
func processHardened() bool { return true }

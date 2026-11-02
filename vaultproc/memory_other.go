//go:build !linux && !darwin && !windows

package vaultproc

func allocLocked(int) *lockedBytes { return nil }

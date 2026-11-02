//go:build linux || darwin

package vaultproc

import (
	"os"

	"golang.org/x/sys/unix"
)

func allocLocked(n int) *lockedBytes {
	mem, err := unix.Mmap(-1, 0, pageSize(n, os.Getpagesize()), unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return nil
	}
	// A limit on locked memory can refuse the lock; the bytes are still held outside the Go heap.
	locked := unix.Mlock(mem) == nil
	excludeFromDump(mem)
	return &lockedBytes{mem: mem, locked: locked, free: func() {
		clear(mem)
		if locked {
			_ = unix.Munlock(mem)
		}
		_ = unix.Munmap(mem)
	}}
}

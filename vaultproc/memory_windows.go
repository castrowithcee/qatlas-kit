//go:build windows

package vaultproc

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func allocLocked(n int) *lockedBytes {
	size := pageSize(n, os.Getpagesize())
	addr, err := windows.VirtualAlloc(0, uintptr(size), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil || addr == 0 {
		return nil
	}
	// The memory is not managed by Go, so the address can be taken as a pointer.
	mem := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&addr))), size)
	// The working set limit can refuse the lock; the bytes are still held outside the Go heap.
	locked := windows.VirtualLock(addr, uintptr(size)) == nil
	return &lockedBytes{mem: mem, locked: locked, free: func() {
		clear(mem)
		if locked {
			_ = windows.VirtualUnlock(addr, uintptr(size))
		}
		_ = windows.VirtualFree(addr, 0, windows.MEM_RELEASE)
	}}
}

package vaultproc

import "syscall"

// processHardened reports whether localipc.Harden took effect in this process.
func processHardened() bool {
	dumpable, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_GET_DUMPABLE, 0, 0)
	return errno == 0 && dumpable == 0
}

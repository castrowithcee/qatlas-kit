//go:build linux

package vaultproc

import "golang.org/x/sys/unix"

func excludeFromDump(mem []byte) { _ = unix.Madvise(mem, unix.MADV_DONTDUMP) }

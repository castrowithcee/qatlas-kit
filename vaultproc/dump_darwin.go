//go:build darwin

package vaultproc

// Harden sets the core file size limit to zero on macOS, which has no per-mapping exclusion.
func excludeFromDump([]byte) {}

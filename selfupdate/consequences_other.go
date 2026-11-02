//go:build !linux

package selfupdate

// OtherProcesses is -1 off Linux, where counting another process needs more than the user's own rights.
func OtherProcesses(name string) int { return -1 }

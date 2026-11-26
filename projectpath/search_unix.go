//go:build unix

package projectpath

import (
	"os"
	"syscall"
)

// deviceLookup returns the device lookup of a search.
func deviceLookup() func(string) (uint64, bool) { return deviceOf }

// deviceOf returns the device of path, without following a link.
func deviceOf(path string) (uint64, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}

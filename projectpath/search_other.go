//go:build !unix

package projectpath

// deviceLookup is nil: a search is not limited to one device on this platform.
func deviceLookup() func(string) (uint64, bool) { return nil }

//go:build linux

package localipc

import (
	"testing"
)

// withoutRuntimeDir leaves neither XDG_RUNTIME_DIR nor a per-user runtime
// directory, so the endpoint falls back to FallbackDir.
func withoutRuntimeDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", "")
	old := userRuntimeRoot
	userRuntimeRoot = shortRoot(t, 0o700)
	t.Cleanup(func() { userRuntimeRoot = old })
}

//go:build darwin

package localipc

import "testing"

// withoutRuntimeDir leaves no XDG_RUNTIME_DIR, so the endpoint falls back to
// FallbackDir.
func withoutRuntimeDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", "")
}

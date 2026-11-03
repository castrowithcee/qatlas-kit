//go:build linux || darwin

package vaultproc

import (
	"os"
	"testing"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

func testEndpoint(t *testing.T) localipc.Endpoint {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "vp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	return localipc.Endpoint{RuntimeDirName: "session", SocketBase: "vault", FallbackDir: dir, ErrorName: "example"}
}

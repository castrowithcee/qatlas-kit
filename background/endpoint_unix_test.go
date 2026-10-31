//go:build linux || darwin

package background

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

func testEndpoint(t *testing.T) localipc.Endpoint {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "bg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	return localipc.Endpoint{RuntimeDirName: "session", SocketBase: "control", FallbackDir: dir, ErrorName: "example"}
}

func processAlive(pid int) bool { return unix.Kill(pid, 0) == nil }

// ownSession reports whether pid leads a session of its own.
func ownSession(pid int) bool {
	sid, err := unix.Getsid(pid)
	return err == nil && sid == pid
}

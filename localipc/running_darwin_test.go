//go:build darwin

package localipc

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRunningListsTempDirFallback(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	t.Setenv("TMPDIR", tmp)
	t.Setenv("XDG_RUNTIME_DIR", "")
	e := Endpoint{RuntimeDirName: "session", FallbackDir: filepath.Join("/", strings.Repeat("d", maxSocketPath)), ErrorName: "example"}
	listenBase(t, e, "moved")
	if path := mustPath(t, Endpoint{RuntimeDirName: e.RuntimeDirName, SocketBase: "moved", FallbackDir: e.FallbackDir}); filepath.Dir(path) != filepath.Join(tmp, e.RuntimeDirName) {
		t.Fatalf("socket path = %q", path)
	}
	if got := mustRunning(t, e); !slices.Equal(got, []string{"moved"}) {
		t.Fatalf("running = %v", got)
	}
	if err := os.Chmod(filepath.Join(tmp, e.RuntimeDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Running(); !errors.Is(err, ErrRefused) {
		t.Fatalf("open fallback directory error = %v, want ErrRefused", err)
	}
}

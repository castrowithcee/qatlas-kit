//go:build darwin

package localipc

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func testProcArgs(path string) []byte {
	data := binary.NativeEndian.AppendUint32(nil, 1)
	return append(append(data, path...), 0)
}

func TestExecPathRejectsUnknownIdentity(t *testing.T) {
	if got, err := execPath(testProcArgs("/opt/example/bin/app")); err != nil || got != "/opt/example/bin/app" {
		t.Fatalf("absolute exec path = %q, %v", got, err)
	}
	for name, data := range map[string][]byte{
		"relative":     testProcArgs("./app"),
		"bare":         testProcArgs("app"),
		"empty":        testProcArgs(""),
		"truncated":    {1, 0},
		"unterminated": append(binary.NativeEndian.AppendUint32(nil, 1), "/opt/example/bin/app"...),
	} {
		if got, err := execPath(data); err == nil {
			t.Errorf("%s exec path accepted: %q", name, got)
		}
	}
}

func TestMacProcessIdentityFailsClosed(t *testing.T) {
	if ownProgramErr != nil {
		t.Fatalf("own program cannot be read: %v", ownProgramErr)
	}
	if err := checkProcess(os.Getpid(), uint32(os.Getuid())); err != nil {
		t.Fatalf("own process refused: %v", err)
	}
	if err := checkProcess(2147483647, uint32(os.Getuid())); !errors.Is(err, ErrRefused) {
		t.Fatalf("unreadable peer process accepted: %v", err)
	}
	old := ownProgramErr
	ownProgramErr = errors.New("unreadable")
	t.Cleanup(func() { ownProgramErr = old })
	if err := checkProcess(os.Getpid(), uint32(os.Getuid())); !errors.Is(err, ErrRefused) {
		t.Fatalf("unreadable own program accepted: %v", err)
	}
}

func TestMacLongSocketPathUsesPrivateTempDir(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	// t.TempDir reads TMPDIR, which this test later sets to a relative path.
	linkDir := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("XDG_RUNTIME_DIR", "")
	e := Endpoint{RuntimeDirName: "session", SocketBase: "control", FallbackDir: filepath.Join("/", strings.Repeat("d", maxSocketPath))}
	path, err := e.Path()
	if err != nil || filepath.Dir(path) != filepath.Join(tmp, e.RuntimeDirName) {
		t.Fatalf("fallback socket path = %q, %v", path, err)
	}
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	t.Setenv("TMPDIR", "relative")
	var tooLong *PathTooLongError
	if _, err := e.Path(); !errors.As(err, &tooLong) {
		t.Fatalf("overlong path without private temp directory accepted: %v", err)
	}
	linked := filepath.Join(linkDir, "linked")
	if err := os.Symlink(tmp, linked); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", linked)
	if _, err := e.Path(); !errors.Is(err, ErrRefused) {
		t.Fatalf("symlink temporary directory accepted: %v", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	if _, err := e.Path(); !errors.Is(err, ErrRefused) {
		t.Fatalf("open temporary directory accepted: %v", err)
	}
}

func TestMacHardenDisablesCoreDumps(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMacHardenHelper$")
	cmd.Env = append(os.Environ(), "LOCALIPC_HARDEN_HELPER=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("harden helper: %v: %s", err, out)
	}
}

func TestMacHardenHelper(t *testing.T) {
	if os.Getenv("LOCALIPC_HARDEN_HELPER") != "1" {
		return
	}
	if err := Harden(); err != nil {
		t.Fatal(err)
	}
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &limit); err != nil || limit.Cur != 0 || limit.Max != 0 {
		t.Fatalf("core limit = %+v, %v", limit, err)
	}
}

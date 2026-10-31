//go:build linux || darwin

package localipc

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEndpoint(t *testing.T) Endpoint {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	return Endpoint{RuntimeDirName: "session", SocketBase: "control", FallbackDir: dir, ErrorName: "example"}
}

func mustPath(t *testing.T, e Endpoint) string {
	t.Helper()
	path, err := e.Path()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidConnectionAndPermissions(t *testing.T) {
	e := testEndpoint(t)
	listener, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	path := mustPath(t, e)
	for file, want := range map[string]fs.FileMode{filepath.Dir(path): 0o700, path: 0o600} {
		info, err := os.Lstat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode = %04o; want %04o", file, info.Mode().Perm(), want)
		}
	}
	accepted := make(chan error, 1)
	go func() {
		c, err := listener.Accept()
		if c != nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	conn, err := e.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// macOS cannot read peer credentials once the client has closed.
	err = <-accepted
	_ = conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("socket remains after close: %v", err)
	}
}

func TestRejectsForeignUIDAndProgram(t *testing.T) {
	if err := checkProcess(os.Getpid(), uint32(os.Getuid())+1); !errors.Is(err, ErrRefused) {
		t.Fatalf("foreign UID accepted: %v", err)
	}
	if err := checkProcess(0, uint32(os.Getuid())); !errors.Is(err, ErrRefused) {
		t.Fatalf("unidentifiable process accepted: %v", err)
	}
	e := testEndpoint(t)
	copyPath := copyTestProgram(t)
	if err := os.Mkdir(filepath.Dir(mustPath(t, e)), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := startHelper(t, copyPath, "listen", mustPath(t, e))
	defer stopHelper(cmd)
	if _, err := e.Dial(context.Background()); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "another program") {
		t.Fatalf("client accepted another program: %v", err)
	}
}

func TestServerRejectsForeignProgram(t *testing.T) {
	e := testEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	copyPath := copyTestProgram(t)
	accepted := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if c != nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	cmd := startHelper(t, copyPath, "dial", mustPath(t, e))
	defer stopHelper(cmd)
	if err := <-accepted; !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "another program") {
		t.Fatalf("server accepted another program: %v", err)
	}
}

func TestRejectsOpenDirectoryAndSymlinks(t *testing.T) {
	e := testEndpoint(t)
	path := mustPath(t, e)
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRefused) {
		t.Fatalf("open app runtime directory accepted: %v", err)
	}
	if _, err := e.Dial(context.Background()); !errors.Is(err, ErrRefused) {
		t.Fatalf("client accepted open runtime directory: %v", err)
	}
	if err := os.Remove(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRefused) {
		t.Fatalf("symlink app runtime directory accepted: %v", err)
	}
	xdg := os.Getenv("XDG_RUNTIME_DIR")
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(xdg, linked); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", linked)
	if _, err := e.Path(); !errors.Is(err, ErrRefused) {
		t.Fatalf("symlink XDG_RUNTIME_DIR accepted or silently bypassed: %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	if err := os.Chmod(xdg, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Path(); !errors.Is(err, ErrRefused) {
		t.Fatalf("open XDG_RUNTIME_DIR accepted or silently bypassed: %v", err)
	}
}

func TestRejectsOpenSocket(t *testing.T) {
	e := testEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	path := mustPath(t, e)
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Dial(context.Background()); !errors.Is(err, ErrRefused) {
		t.Fatalf("client accepted open socket: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	// A dead socket with open permissions must not be silently replaced.
	dead, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	dead.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	_ = dead.Close()
	if _, err := e.Listen(); !errors.Is(err, ErrRefused) {
		t.Fatalf("open stale socket replaced: %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0o666 {
		t.Fatalf("open stale socket changed: %v, %v", info, err)
	}
}

func TestSecondInstanceAndDeadSocket(t *testing.T) {
	e := testEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRunning) {
		t.Fatalf("second listener started: %v", err)
	}
	path := mustPath(t, e)
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("second listener removed first socket: %v", err)
	}
	_ = l.Close()
	dead, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	dead.SetUnlinkOnClose(false)
	_ = os.Chmod(path, 0o600)
	_ = dead.Close()
	replacement, err := e.Listen()
	if err != nil {
		t.Fatalf("dead private socket not replaced: %v", err)
	}
	_ = replacement.Close()
}

func TestRejectsOverlongPath(t *testing.T) {
	t.Setenv("TMPDIR", "relative")
	e := testEndpoint(t)
	e.SocketBase = strings.Repeat("x", maxSocketPath)
	var long *PathTooLongError
	if _, err := e.Listen(); !errors.As(err, &long) {
		t.Fatalf("long socket path accepted: %v", err)
	}
}

func copyTestProgram(t *testing.T) string {
	t.Helper()
	src, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	path := filepath.Join(t.TempDir(), "other")
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func startHelper(t *testing.T, program, mode, path string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(program, "-test.run=^TestHelperProcess$", "--", mode, path)
	cmd.Env = append(os.Environ(), "LOCALIPC_HELPER=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		stopHelper(cmd)
		t.Fatalf("helper failed to start: %q, %v", line, err)
	}
	return cmd
}

func stopHelper(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("LOCALIPC_HELPER") != "1" {
		return
	}
	i := 0
	for j, v := range os.Args {
		if v == "--" {
			i = j + 1
			break
		}
	}
	if i == 0 || len(os.Args) < i+2 {
		os.Exit(2)
	}
	mode, path := os.Args[i], os.Args[i+1]
	switch mode {
	case "listen", "harden-listen":
		l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			os.Exit(2)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			os.Exit(2)
		}
		if mode == "harden-listen" {
			if err := Harden(); err != nil {
				os.Exit(2)
			}
		}
		_, _ = os.Stdout.WriteString("ready\n")
		c, _ := l.Accept()
		if c != nil {
			time.Sleep(2 * time.Second)
			_ = c.Close()
		}
	case "dial":
		_, _ = os.Stdout.WriteString("ready\n")
		c, err := net.Dial("unix", path)
		if err != nil {
			os.Exit(2)
		}
		time.Sleep(2 * time.Second)
		_ = c.Close()
	}
	os.Exit(0)
}

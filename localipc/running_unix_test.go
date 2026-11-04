//go:build linux || darwin

package localipc

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func listenBase(t *testing.T, e Endpoint, base string) net.Listener {
	t.Helper()
	e.SocketBase = base
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func mustRunning(t *testing.T, e Endpoint) []string {
	t.Helper()
	got, err := e.Running()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("Running returned nil")
	}
	return got
}

func TestRunningListsListeners(t *testing.T) {
	e := rootEndpoint(t, shortRoot(t, 0o700))
	e.SocketBase = ""
	listenBase(t, e, "gamma")
	listenBase(t, e, "alpha")
	beta := listenBase(t, e, "beta")
	if got := mustRunning(t, e); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("running = %v", got)
	}
	_ = beta.Close()
	if got := mustRunning(t, e); !slices.Equal(got, []string{"alpha", "gamma"}) {
		t.Fatalf("running after close = %v", got)
	}
}

func TestRunningSkipsOtherFiles(t *testing.T) {
	root := shortRoot(t, 0o700)
	e := rootEndpoint(t, root)
	e.SocketBase = ""
	listenBase(t, e, "live")
	dir := filepath.Join(root, e.RuntimeDirName)
	if err := os.WriteFile(filepath.Join(dir, "plain.sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	open, err := net.Listen("unix", filepath.Join(dir, "open.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	if err := os.Chmod(filepath.Join(dir, "open.sock"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "live.sock"), filepath.Join(dir, "linked.sock")); err != nil {
		t.Fatal(err)
	}
	next := e
	next.SocketBase = "live"
	succ, err := next.ListenWith(ListenOptions{Successor: true})
	if err != nil {
		t.Fatal(err)
	}
	defer succ.Close()
	if _, err := os.Lstat(filepath.Join(dir, "live.sock.next")); err != nil {
		t.Fatal(err)
	}
	if got := mustRunning(t, e); !slices.Equal(got, []string{"live"}) {
		t.Fatalf("running = %v", got)
	}
}

func TestRunningMissingDirectory(t *testing.T) {
	e := rootEndpoint(t, shortRoot(t, 0o700))
	e.SocketBase = ""
	if got := mustRunning(t, e); len(got) != 0 {
		t.Fatalf("running = %v", got)
	}
}

func TestRunningRefusesOpenDirectory(t *testing.T) {
	root := shortRoot(t, 0o700)
	e := rootEndpoint(t, root)
	dir := filepath.Join(root, e.RuntimeDirName)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Running(); !errors.Is(err, ErrRefused) {
		t.Fatalf("error = %v, want ErrRefused", err)
	}
}

func TestRunningRejectsInvalidFields(t *testing.T) {
	root := shortRoot(t, 0o700)
	cases := map[string]Endpoint{
		"bad directory name": {RuntimeDirName: "a/b", RuntimeRoot: root},
		"empty name":         {RuntimeRoot: root},
		"relative fallback":  {RuntimeDirName: "session", RuntimeRoot: root, FallbackDir: "relative"},
		"empty fallback":     {RuntimeDirName: "session"},
		"relative root":      {RuntimeDirName: "session", RuntimeRoot: "relative/dir"},
		"missing root":       {RuntimeDirName: "session", RuntimeRoot: filepath.Join(root, "missing")},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := e.Running(); err == nil {
				t.Fatal("invalid endpoint accepted")
			}
		})
	}
	loose := Endpoint{RuntimeDirName: "session", RuntimeRoot: shortRoot(t, 0o755)}
	if _, err := loose.Running(); !errors.Is(err, ErrRefused) {
		t.Fatalf("open root error = %v, want ErrRefused", err)
	}
}

func TestRunningRuntimeRootOverridesEnvironment(t *testing.T) {
	root := shortRoot(t, 0o700)
	e := rootEndpoint(t, root)
	e.SocketBase = ""
	listenBase(t, e, "mine")
	xdg := shortRoot(t, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	other := Endpoint{RuntimeDirName: e.RuntimeDirName, FallbackDir: xdg, ErrorName: "example"}
	listenBase(t, other, "foreign")
	if got := mustRunning(t, e); !slices.Equal(got, []string{"mine"}) {
		t.Fatalf("running = %v", got)
	}
	if got := mustRunning(t, other); !slices.Equal(got, []string{"foreign"}) {
		t.Fatalf("running via environment = %v", got)
	}
}

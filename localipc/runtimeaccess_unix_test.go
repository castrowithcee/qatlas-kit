//go:build linux || darwin

package localipc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readableEndpoint(root string) Endpoint {
	return Endpoint{RuntimeDirName: "session", SocketBase: "control", RuntimeRoot: root,
		RuntimeAccess: RuntimeReadable, ErrorName: "example"}
}

// checkRefusedAll expects Path, Running, Listen and Dial to refuse e.
func checkRefusedAll(t *testing.T, label string, e Endpoint) {
	t.Helper()
	if _, err := e.Path(); !errors.Is(err, ErrRefused) {
		t.Errorf("%s: Path error = %v, want ErrRefused", label, err)
	}
	if _, err := e.Running(); !errors.Is(err, ErrRefused) {
		t.Errorf("%s: Running error = %v, want ErrRefused", label, err)
	}
	l, err := e.Listen()
	if l != nil {
		_ = l.Close()
	}
	if !errors.Is(err, ErrRefused) {
		t.Errorf("%s: Listen error = %v, want ErrRefused", label, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := e.Dial(ctx)
	if conn != nil {
		_ = conn.Close()
	}
	if !errors.Is(err, ErrRefused) {
		t.Errorf("%s: Dial error = %v, want ErrRefused", label, err)
	}
}

func TestRuntimePrivateIsDefault(t *testing.T) {
	for _, mode := range []os.FileMode{0o755, 0o750, 0o770} {
		e := rootEndpoint(t, shortRoot(t, mode))
		checkRefusedAll(t, mode.String(), e)
	}
}

func TestRuntimeReadableRefusedRoot(t *testing.T) {
	good := shortRoot(t, 0o755)
	link := filepath.Join(shortRoot(t, 0o700), "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"group writable": shortRoot(t, 0o775),
		"world writable": shortRoot(t, 0o777),
		"sticky":         shortRoot(t, 0o777|os.ModeSticky),
		"group only":     shortRoot(t, 0o770),
		"world only":     shortRoot(t, 0o757),
		"symlink":        link,
	}
	if os.Getuid() != 0 {
		// Directories of another owner that exist on every system.
		cases["foreign owner"] = "/"
		cases["foreign sticky"] = "/tmp"
	}
	for name, root := range cases {
		checkRefusedAllRoot(t, name, readableEndpoint(root))
	}
}

// checkRefusedAllRoot is checkRefusedAll without the error kind, for a root
// the refusal of which need not be ErrRefused.
func checkRefusedAllRoot(t *testing.T, label string, e Endpoint) {
	t.Helper()
	if _, err := e.Path(); err == nil {
		t.Errorf("%s: Path accepted root %q", label, e.RuntimeRoot)
	}
	l, err := e.Listen()
	if l != nil {
		_ = l.Close()
	}
	if err == nil {
		t.Errorf("%s: Listen accepted root %q", label, e.RuntimeRoot)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := e.Dial(ctx)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Errorf("%s: Dial accepted root %q", label, e.RuntimeRoot)
	}
	if _, err := e.Running(); err == nil {
		t.Errorf("%s: Running accepted root %q", label, e.RuntimeRoot)
	}
}

func TestRuntimeReadableRefusedOwnDirectory(t *testing.T) {
	root := shortRoot(t, 0o755)
	e := readableEndpoint(root)
	dir := filepath.Join(root, e.RuntimeDirName)
	l := listenBase(t, e, "control")
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	checkRefusedAll(t, "open directory", e)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()

	moved := filepath.Join(root, "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, dir); err != nil {
		t.Fatal(err)
	}
	checkRefusedAll(t, "symlinked directory", e)
}

func TestRuntimeReadableExistingLooseDirectoryNotCorrected(t *testing.T) {
	root := shortRoot(t, 0o755)
	e := readableEndpoint(root)
	dir := filepath.Join(root, e.RuntimeDirName)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	checkRefusedAll(t, "existing open directory", e)
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("directory = %v, %v; mode was changed", info, err)
	}
}

func TestRuntimeReadableAccepted(t *testing.T) {
	for _, mode := range []os.FileMode{0o755, 0o750, 0o700} {
		t.Run(mode.String(), func(t *testing.T) {
			root := shortRoot(t, mode)
			e := readableEndpoint(root)
			l, err := e.Listen()
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			if info, err := os.Stat(filepath.Join(root, e.RuntimeDirName)); err != nil || info.Mode().Perm() != dirMode {
				t.Fatalf("directory = %v, %v", info, err)
			}
			if got := mustRunning(t, e); len(got) != 1 || got[0] != "control" {
				t.Fatalf("running = %v", got)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			accepted := acceptHeld(l, nil)
			conn, err := e.Dial(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := <-accepted; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimeReadableEnvironmentRoot(t *testing.T) {
	withoutRuntimeDir(t)
	xdg := shortRoot(t, 0o755)
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	e := Endpoint{RuntimeDirName: "session", SocketBase: "control", FallbackDir: shortRoot(t, 0o700), ErrorName: "example"}
	checkRefusedAll(t, "private", e)
	e.RuntimeAccess = RuntimeReadable
	if want := filepath.Join(xdg, "session", "control.sock"); mustPath(t, e) != want {
		t.Fatalf("path = %q, want %q", mustPath(t, e), want)
	}
	for _, mode := range []os.FileMode{0o775, 0o777, 0o777 | os.ModeSticky} {
		if err := os.Chmod(xdg, mode); err != nil {
			t.Fatal(err)
		}
		checkRefusedAll(t, mode.String(), e)
	}
}

func TestRuntimeAccessUnknownValue(t *testing.T) {
	e := rootEndpoint(t, shortRoot(t, 0o700))
	e.RuntimeAccess = RuntimeReadable + 1
	if _, err := e.Path(); err == nil {
		t.Fatal("unknown RuntimeAccess accepted")
	}
}

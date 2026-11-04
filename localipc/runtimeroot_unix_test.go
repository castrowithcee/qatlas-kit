//go:build linux || darwin

package localipc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rootEndpoint(t *testing.T, root string) Endpoint {
	t.Helper()
	return Endpoint{RuntimeDirName: "session", SocketBase: "control", RuntimeRoot: root, ErrorName: "example"}
}

func shortRoot(t *testing.T, mode os.FileMode) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ipcr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRuntimeRootOverridesEnvironment(t *testing.T) {
	root := shortRoot(t, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", shortRoot(t, 0o700))
	path := mustPath(t, rootEndpoint(t, root))
	if want := filepath.Join(root, "session", "control.sock"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestRuntimeRootRejected(t *testing.T) {
	loose := shortRoot(t, 0o755)
	cases := map[string]struct {
		root    string
		refused bool
	}{
		"relative":    {root: "relative/dir"},
		"missing":     {root: filepath.Join(shortRoot(t, 0o700), "missing")},
		"not private": {root: loose, refused: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := rootEndpoint(t, c.root)
			check := func(op string, err error) {
				if err == nil {
					t.Fatalf("%s accepted root %q", op, c.root)
				}
				if c.refused && !errors.Is(err, ErrRefused) {
					t.Fatalf("%s error = %v, want ErrRefused", op, err)
				}
			}
			_, err := e.Path()
			check("Path", err)
			l, err := e.Listen()
			if l != nil {
				_ = l.Close()
			}
			check("Listen", err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, err := e.Dial(ctx)
			if conn != nil {
				_ = conn.Close()
			}
			check("Dial", err)
		})
	}
}

func TestRuntimeRootChannelWithoutFallback(t *testing.T) {
	e := rootEndpoint(t, shortRoot(t, 0o700))
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if c != nil {
			defer c.Close()
		}
		accepted <- err
	}()
	conn, err := e.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

func TestEmptyFallbackWithoutRuntimeRootStillInvalid(t *testing.T) {
	e := Endpoint{RuntimeDirName: "session", SocketBase: "control", ErrorName: "example"}
	if _, err := e.Path(); err == nil {
		t.Fatal("empty FallbackDir accepted without RuntimeRoot")
	}
}

func TestRuntimeRootWithRelativeFallbackRejected(t *testing.T) {
	e := rootEndpoint(t, shortRoot(t, 0o700))
	e.FallbackDir = "relative"
	if _, err := e.Path(); err == nil {
		t.Fatal("relative FallbackDir accepted")
	}
}

func TestRuntimeRootTooLong(t *testing.T) {
	root := shortRoot(t, 0o700)
	e := rootEndpoint(t, root)
	e.SocketBase = strings.Repeat("x", maxSocketPath)
	_, err := e.Path()
	var tooLong *PathTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("error = %v, want PathTooLongError", err)
	}
}

//go:build linux || darwin

package localipc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func fallbackNameEndpoint(t *testing.T) Endpoint {
	t.Helper()
	withoutRuntimeDir(t)
	return Endpoint{RuntimeDirName: "session", FallbackDirName: "vault", SocketBase: "control",
		FallbackDir: shortRoot(t, 0o700), ErrorName: "example"}
}

func TestFallbackDirNamePath(t *testing.T) {
	e := fallbackNameEndpoint(t)
	if want := filepath.Join(e.FallbackDir, "vault", "control.sock"); mustPath(t, e) != want {
		t.Fatalf("path = %q, want %q", mustPath(t, e), want)
	}
	e.FallbackDirName = ""
	if want := filepath.Join(e.FallbackDir, "session", "control.sock"); mustPath(t, e) != want {
		t.Fatalf("empty name: path = %q, want %q", mustPath(t, e), want)
	}
}

func TestFallbackDirNameIgnoredWithRuntimeDir(t *testing.T) {
	e := fallbackNameEndpoint(t)
	xdg := shortRoot(t, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	if want := filepath.Join(xdg, "session", "control.sock"); mustPath(t, e) != want {
		t.Fatalf("path = %q, want %q", mustPath(t, e), want)
	}
	e.RuntimeRoot = shortRoot(t, 0o700)
	if want := filepath.Join(e.RuntimeRoot, "session", "control.sock"); mustPath(t, e) != want {
		t.Fatalf("runtime root: path = %q, want %q", mustPath(t, e), want)
	}
}

func TestFallbackDirNameInvalid(t *testing.T) {
	for _, name := range []string{".", "..", "a/b", "/abs", "a\x00b"} {
		e := fallbackNameEndpoint(t)
		e.FallbackDirName = name
		if _, err := e.Path(); err == nil {
			t.Errorf("Path accepted %q", name)
		}
		if _, err := e.Running(); err == nil {
			t.Errorf("Running accepted %q", name)
		}
		if _, err := e.Listen(); err == nil {
			t.Errorf("Listen accepted %q", name)
		}
	}
}

func TestFallbackDirNameChecksOwnDirectory(t *testing.T) {
	e := fallbackNameEndpoint(t)
	l := listenBase(t, e, "control")
	dir := filepath.Join(e.FallbackDir, "vault")
	check := func(label string) {
		t.Helper()
		if _, err := e.Dial(context.Background()); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: Dial error = %v, want ErrRefused", label, err)
		}
		if _, err := e.Running(); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: Running error = %v, want ErrRefused", label, err)
		}
		if _, err := e.Listen(); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: Listen error = %v, want ErrRefused", label, err)
		}
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	check("open directory")
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()

	moved := filepath.Join(e.FallbackDir, "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, dir); err != nil {
		t.Fatal(err)
	}
	check("symlinked directory")
}

func TestFallbackDirNameChannel(t *testing.T) {
	e := fallbackNameEndpoint(t)
	l := listenBase(t, e, "control")
	if info, err := os.Stat(filepath.Join(e.FallbackDir, "vault")); err != nil || info.Mode().Perm() != dirMode {
		t.Fatalf("directory = %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(e.FallbackDir, "session")); !os.IsNotExist(err) {
		t.Fatalf("default directory created: %v", err)
	}
	if got := mustRunning(t, e); !slices.Equal(got, []string{"control"}) {
		t.Fatalf("running = %v", got)
	}
	accepted := acceptHeld(l, nil)
	conn, err := e.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The listener reads the client's credentials on Accept, which macOS
	// refuses once the client has closed.
	err = <-accepted
	_ = conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRunning) {
		t.Fatalf("second Listen error = %v, want ErrRunning", err)
	}
}

func TestFallbackDirNameHolder(t *testing.T) {
	e := fallbackNameEndpoint(t)
	if _, err := e.Holder(context.Background()); !errors.Is(err, ErrNotServed) {
		t.Fatalf("Holder without a channel error = %v, want ErrNotServed", err)
	}
	listenBase(t, e, "control")
	// This process serves the channel, so Holder reaches it through the new
	// path and refuses it as its own.
	if _, err := e.Holder(context.Background()); !errors.Is(err, ErrRefused) {
		t.Fatalf("Holder of own channel error = %v, want ErrRefused", err)
	}
	if _, err := e.EndHolder(context.Background()); !errors.Is(err, ErrRefused) {
		t.Fatalf("EndHolder of own channel error = %v, want ErrRefused", err)
	}
}

func TestFallbackDirNameEarlier(t *testing.T) {
	current := fallbackNameEndpoint(t)
	earlier := Endpoint{RuntimeDirName: "session", FallbackDirName: "old", SocketBase: "ctl", FallbackDir: current.FallbackDir}
	old := listenUntilEnd(t, earlier)
	successor, err := current.ListenWith(ListenOptions{Successor: true, Earlier: &earlier})
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	if !ownsSocket(successor, filepath.Join(current.FallbackDir, "vault", "control.sock")) {
		t.Fatal("the successor does not listen under the fallback name")
	}
	if err := HandOver(old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := AwaitHandover(ctx, successor); err != nil {
		t.Fatal(err)
	}
}

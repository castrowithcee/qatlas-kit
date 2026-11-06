//go:build linux || darwin

package localipc

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// earlierEndpoints returns an endpoint and an earlier one of the same
// application under one fresh runtime root.
func earlierEndpoints(t *testing.T) (current, earlier Endpoint) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return Endpoint{RuntimeRoot: root, RuntimeDirName: "session", SocketBase: "control", ErrorName: "example"},
		Endpoint{RuntimeRoot: root, RuntimeDirName: "old", SocketBase: "ctl"}
}

func listenUntilEnd(t *testing.T, e Endpoint) net.Listener {
	t.Helper()
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func wantAbsent(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is there: %v", path, err)
		}
	}
}

func ownsSocket(l net.Listener, path string) bool {
	info, err := os.Lstat(path)
	return err == nil && os.SameFile(info, l.(*checkedListener).own)
}

// dirState records every file in dir, to tell later that none changed.
func dirState(t *testing.T, dir string) map[string]fs.FileInfo {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]fs.FileInfo{}
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		state[entry.Name()] = info
	}
	return state
}

func wantUnchanged(t *testing.T, dir string, before map[string]fs.FileInfo) {
	t.Helper()
	after := dirState(t, dir)
	if len(after) != len(before) {
		t.Errorf("%s holds %d files, want %d", dir, len(after), len(before))
	}
	for name, was := range before {
		now, ok := after[name]
		if !ok || !os.SameFile(now, was) || now.Mode() != was.Mode() || now.Size() != was.Size() ||
			!now.ModTime().Equal(was.ModTime()) {
			t.Errorf("%s changed", name)
		}
	}
}

func TestSuccessorTakesOverFromEarlierEndpoint(t *testing.T) {
	current, earlier := earlierEndpoints(t)
	old := listenUntilEnd(t, earlier)
	path, earlierPath := mustPath(t, current), mustPath(t, earlier)
	successor, err := current.ListenWith(ListenOptions{Successor: true, Earlier: &earlier})
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	if !ownsSocket(successor, path) {
		t.Fatal("the successor does not listen at the endpoint's channel")
	}
	wantAbsent(t, path+nextSuffix, path+nextSuffix+lockSuffix)
	mark := earlierPath + nextSuffix
	if info, err := os.Lstat(mark); err != nil || info.Mode().Type() != fs.ModeSocket || ownsSocket(successor, mark) {
		t.Fatalf("beside the earlier socket is %v, %v; want a socket of its own", info, err)
	}
	if err := checkSuccessor(mark, socketMode); err != nil {
		t.Fatalf("the earlier listener would not hand over: %v", err)
	}
	if conn, err := net.Dial("unix", mark); err == nil {
		_ = conn.Close()
		t.Fatal("a connection beside the earlier socket reached something")
	}

	// A client of the endpoint waits unserved until the handover.
	early, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer early.Close()
	if conn, err := successor.Accept(); err == nil {
		_ = conn.Close()
		t.Fatal("the successor accepted before the handover")
	}
	if err := HandOver(successor); err == nil {
		t.Fatal("a successor handed over before it took over")
	}
	if err := HandOver(old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := AwaitHandover(ctx, successor); err != nil {
		t.Fatal(err)
	}
	wantEmptyDir(t, filepath.Dir(earlierPath))
	conn, err := successor.Accept()
	if err != nil {
		t.Fatalf("the waiting client was not accepted after the handover: %v", err)
	}
	_ = conn.Close()
	_ = early.Close()

	accepted := make(chan error, 1)
	go func() {
		c, err := successor.Accept()
		if c != nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	conn, err = current.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = <-accepted
	_ = conn.Close()
	if err != nil {
		t.Fatalf("the successor did not get the connection: %v", err)
	}
	if _, err := current.Listen(); !errors.Is(err, ErrRunning) {
		t.Fatalf("listener started beside the successor: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if !ownsSocket(successor, path) {
		t.Fatal("closing the earlier listener removed the successor's socket")
	}
	wantEmptyDir(t, filepath.Dir(earlierPath))
	if err := successor.Close(); err != nil {
		t.Fatal(err)
	}
	wantEmptyDir(t, filepath.Dir(path))
}

func TestEarlierSuccessorWithoutHandoverRemovesWhatItPlaced(t *testing.T) {
	for _, handover := range []bool{false, true} {
		current, earlier := earlierEndpoints(t)
		old := listenUntilEnd(t, earlier)
		path, earlierPath := mustPath(t, current), mustPath(t, earlier)
		successor, err := current.ListenWith(ListenOptions{Successor: true, Earlier: &earlier})
		if err != nil {
			t.Fatal(err)
		}
		if handover {
			// Moved, but never awaited.
			if err := HandOver(old); err != nil {
				t.Fatal(err)
			}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			err := AwaitHandover(ctx, successor)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("await without handover = %v", err)
			}
		}
		if err := successor.Close(); err != nil {
			t.Fatal(err)
		}
		wantEmptyDir(t, filepath.Dir(path))
		if handover {
			wantEmptyDir(t, filepath.Dir(earlierPath))
			continue
		}
		wantAbsent(t, earlierPath+nextSuffix, earlierPath+nextSuffix+lockSuffix)
		if !ownsSocket(old, earlierPath) || !serves(earlierPath) {
			t.Fatal("the earlier listener lost its channel")
		}
	}
}

func TestEarlierEndpointFallsBackOrFailsWithoutTouching(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, current, earlier Endpoint)
		// fails is the path whose content must stay "kept"; empty means the
		// successor listens beside the endpoint's channel.
		fails string
		err   error
	}{
		"nothing at the earlier socket": {setup: func(*testing.T, Endpoint, Endpoint) {}},
		"earlier socket without a held lock": {setup: func(t *testing.T, _, earlier Endpoint) {
			path := mustPath(t, earlier)
			if err := privateDir(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
			l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = l.Close() })
			if err := os.Chmod(path, socketMode); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+lockSuffix, nil, lockMode); err != nil {
				t.Fatal(err)
			}
		}},
		"earlier socket open to others": {setup: func(t *testing.T, _, earlier Endpoint) {
			listenUntilEnd(t, earlier)
			if err := os.Chmod(mustPath(t, earlier), 0o666); err != nil {
				t.Fatal(err)
			}
		}},
		"earlier socket of another user": {setup: func(t *testing.T, _, earlier Endpoint) {
			if os.Getuid() != 0 {
				t.Skip("giving the earlier socket another owner needs root")
			}
			listenUntilEnd(t, earlier)
			if err := os.Lchown(mustPath(t, earlier), 65534, -1); err != nil {
				t.Fatal(err)
			}
		}},
		"endpoint's channel served": {setup: func(t *testing.T, current, earlier Endpoint) {
			listenUntilEnd(t, earlier)
			listenUntilEnd(t, current)
		}},
		"earlier endpoint is the endpoint": {setup: func(t *testing.T, current, earlier Endpoint) {
			listenUntilEnd(t, current)
		}},
		"earlier directory open to others": {setup: func(t *testing.T, _, earlier Endpoint) {
			listenUntilEnd(t, earlier)
			if err := os.Chmod(filepath.Dir(mustPath(t, earlier)), 0o755); err != nil {
				t.Fatal(err)
			}
		}, fails: "-", err: ErrRefused},
		"lock beside the earlier socket": {setup: func(t *testing.T, _, earlier Endpoint) {
			listenUntilEnd(t, earlier)
			if err := os.WriteFile(mustPath(t, earlier)+nextSuffix+lockSuffix, []byte("kept"), lockMode); err != nil {
				t.Fatal(err)
			}
		}, fails: nextSuffix + lockSuffix},
		"file at the mark's path": {setup: func(t *testing.T, _, earlier Endpoint) {
			listenUntilEnd(t, earlier)
			if err := os.WriteFile(mustPath(t, earlier)+nextSuffix, []byte("kept"), socketMode); err != nil {
				t.Fatal(err)
			}
		}, fails: nextSuffix},
		"link at the lock's path": {setup: func(t *testing.T, _, earlier Endpoint) {
			listenUntilEnd(t, earlier)
			target := filepath.Join(t.TempDir(), "target")
			if err := os.Symlink(target, mustPath(t, earlier)+nextSuffix+lockSuffix); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { wantAbsent(t, target) })
		}, fails: "-"},
	} {
		t.Run(name, func(t *testing.T) {
			current, earlier := earlierEndpoints(t)
			opts := ListenOptions{Successor: true, Earlier: &earlier}
			if name == "earlier endpoint is the endpoint" {
				opts.Earlier = &current
			}
			tc.setup(t, current, earlier)
			path := mustPath(t, current)
			earlierDir := filepath.Join(earlier.RuntimeRoot, earlier.RuntimeDirName)
			var kept map[string]fs.FileInfo
			if tc.fails == "" {
				kept = dirState(t, earlierDir)
			}
			currentBefore := dirState(t, filepath.Dir(path))
			l, err := current.ListenWith(opts)
			if tc.fails != "" {
				if err == nil {
					_ = l.Close()
					t.Fatal("the successor listened with something in the way")
				}
				if tc.err != nil && !errors.Is(err, tc.err) {
					t.Fatalf("error = %v, want %v", err, tc.err)
				}
				if tc.fails != "-" {
					earlierPath := filepath.Join(earlierDir, earlier.SocketBase+".sock")
					if data, _ := os.ReadFile(earlierPath + tc.fails); string(data) != "kept" {
						t.Fatalf("what was in the way was replaced: %q", data)
					}
					other := map[string]string{nextSuffix: nextSuffix + lockSuffix,
						nextSuffix + lockSuffix: nextSuffix}[tc.fails]
					wantAbsent(t, earlierPath+other)
				}
				wantUnchanged(t, filepath.Dir(path), currentBefore)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			if !ownsSocket(l, path+nextSuffix) {
				t.Fatal("the successor does not listen beside the endpoint's channel")
			}
			if opts.Earlier != &current {
				wantUnchanged(t, earlierDir, kept)
			}
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			wantUnchanged(t, filepath.Dir(path), currentBefore)
		})
	}
}

func TestEarlierEndpointNeedsSuccessorAndValidEndpoint(t *testing.T) {
	current, earlier := earlierEndpoints(t)
	if _, err := current.ListenWith(ListenOptions{Earlier: &earlier}); err == nil {
		t.Fatal("an earlier endpoint was accepted without Successor")
	}
	invalid := earlier
	invalid.SocketBase = "a/b"
	if _, err := current.ListenWith(ListenOptions{Successor: true, Earlier: &invalid}); err == nil {
		t.Fatal("an invalid earlier endpoint was accepted")
	}
	wantAbsent(t, filepath.Join(current.RuntimeRoot, current.RuntimeDirName))
}

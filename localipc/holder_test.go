//go:build linux || darwin || windows

package localipc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// holderHelper is a process that serves an endpoint until it is ended or the
// test finishes, so a signal never reaches the test process.
type holderHelper struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func startHolderHelper(t *testing.T, mode string, e Endpoint) *holderHelper {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHolderHelperProcess$", "--", mode,
		e.RuntimeRoot, e.RuntimeDirName, e.SocketBase, e.FallbackDir, e.PipePrefix)
	cmd.Env = append(os.Environ(), "LOCALIPC_HOLDER_HELPER=1")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h := &holderHelper{cmd: cmd, done: make(chan struct{})}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	go func() {
		_ = cmd.Wait()
		close(h.done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-h.done
	})
	if err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("helper said %q, %v", line, err)
	}
	return h
}

func (h *holderHelper) wantAlive(t *testing.T) {
	t.Helper()
	select {
	case <-h.done:
		t.Fatalf("the helper ended: %v", h.cmd.ProcessState)
	case <-time.After(200 * time.Millisecond):
	}
}

func (h *holderHelper) wantEnded(t *testing.T) {
	t.Helper()
	select {
	case <-h.done:
		wantEndedByHolder(t, h.cmd.ProcessState)
	case <-time.After(10 * time.Second):
		t.Fatal("the helper did not end")
	}
}

func holderContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// wantNoHolder checks that both calls fail with want and report no process.
func wantNoHolder(t *testing.T, e Endpoint, want error) {
	t.Helper()
	ctx := holderContext(t)
	for name, call := range map[string]func(context.Context) (int, error){"Holder": e.Holder, "EndHolder": e.EndHolder} {
		if pid, err := call(ctx); pid != 0 || !errors.Is(err, want) {
			t.Errorf("%s() = %d, %v; want 0, %v", name, pid, err, want)
		}
	}
}

func TestHolderFindsAndEndsServingProcess(t *testing.T) {
	e := crossEndpoint(t)
	h := startHolderHelper(t, "serve", e)
	ctx := holderContext(t)
	want := h.cmd.Process.Pid
	if pid, err := e.Holder(ctx); err != nil || pid != want {
		t.Fatalf("Holder() = %d, %v; want %d", pid, err, want)
	}
	h.wantAlive(t)
	if pid, err := e.EndHolder(ctx); err != nil || pid != want {
		t.Fatalf("EndHolder() = %d, %v; want %d", pid, err, want)
	}
	h.wantEnded(t)
	wantNoHolder(t, e, ErrNotServed)
}

func TestHolderWithoutChannelIsNotServed(t *testing.T) {
	wantNoHolder(t, crossEndpoint(t), ErrNotServed)
}

func TestHolderRefusesOwnProcess(t *testing.T) {
	e := crossEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if c != nil {
				_ = c.Close()
			}
		}
	}()
	wantNoHolder(t, e, ErrRefused)
}

func TestHolderHelperProcess(t *testing.T) {
	if os.Getenv("LOCALIPC_HOLDER_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) != 7 {
		os.Exit(2)
	}
	e := Endpoint{RuntimeRoot: args[2], RuntimeDirName: args[3], SocketBase: args[4], FallbackDir: args[5],
		PipePrefix: args[6], ErrorName: "example"}
	var l net.Listener
	var err error
	switch args[1] {
	case "serve":
		l, err = e.Listen()
	case "raw":
		// A socket of the user without the channel's lock.
		var path string
		if path, err = e.Path(); err == nil {
			_ = os.Mkdir(filepath.Dir(path), 0o700)
			if l, err = net.Listen("unix", path); err == nil {
				err = os.Chmod(path, 0o600)
			}
		}
	default:
		err = errors.New("unknown helper mode")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("ready")
	time.AfterFunc(time.Minute, func() { os.Exit(3) })
	for {
		c, err := l.Accept()
		if errors.Is(err, net.ErrClosed) {
			os.Exit(2)
		}
		if c != nil {
			_ = c.Close()
		}
	}
}

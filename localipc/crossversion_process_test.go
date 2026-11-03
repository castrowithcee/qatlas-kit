//go:build linux || darwin || windows

package localipc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// crossHelper is a process started from a copied program file that the test
// replaces, as an update does, before the helper uses the channel.
type crossHelper struct {
	cmd   *exec.Cmd
	lines *bufio.Reader
}

// startCrossHelper hands the secret over the helper's stdin, never through
// its arguments or environment.
func startCrossHelper(t *testing.T, mode string, e Endpoint, secret []byte) *crossHelper {
	t.Helper()
	program := copyProgram(t)
	cmd := exec.Command(program, "-test.run=^TestCrossVersionHelperProcess$", "--", mode,
		e.RuntimeDirName, e.SocketBase, e.FallbackDir, e.PipePrefix)
	cmd.Env = append(os.Environ(), "LOCALIPC_CROSS_HELPER=1")
	cmd.Stdin = bytes.NewReader(secret)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h := &crossHelper{cmd: cmd, lines: bufio.NewReader(stdout)}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	h.expect(t, "started")
	replaceRunningProgram(t, program)
	return h
}

func (h *crossHelper) expect(t *testing.T, want string) {
	t.Helper()
	line, err := h.lines.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != want {
		t.Fatalf("helper said %q, %v; want %q", line, err, want)
	}
}

func newSecret(t *testing.T) []byte {
	t.Helper()
	secret := make([]byte, MinSecretSize)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

func wantRefusalOf(t *testing.T, err error, pid int, reason string) {
	t.Helper()
	var pe *PeerError
	if !errors.Is(err, ErrRefused) || !errors.As(err, &pe) || pe.PID != pid || !strings.Contains(err.Error(), reason) {
		t.Fatalf("got %v; want a refusal of process %d with %q", err, pid, reason)
	}
}

func TestCrossVersionReachesReplacedListener(t *testing.T) {
	e := crossEndpoint(t)
	secret := newSecret(t)
	h := startCrossHelper(t, "listen", e, secret)
	h.expect(t, "ready")
	pid := h.cmd.Process.Pid
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := e.DialWith(ctx, DialOptions{CrossVersion: &CrossVersion{Secret: secret}})
	if err != nil {
		t.Fatalf("cross-version dial to a replaced program refused: %v", err)
	}
	if got, err := PeerPID(conn); err != nil || got != pid {
		t.Fatalf("PeerPID() = %d, %v; want %d", got, err, pid)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	_ = conn.Close()
	if err != nil || line != "hello\n" {
		t.Fatalf("listener sent %q, %v", line, err)
	}

	_, err = e.DialWith(ctx, DialOptions{CrossVersion: &CrossVersion{Secret: newSecret(t)}})
	wantRefusalOf(t, err, pid, "did not prove the secret")

	_, err = e.Dial(ctx)
	wantRefusalOf(t, err, pid, "")
	if strings.Contains(err.Error(), "cross-version") {
		t.Fatalf("plain dial reached the cross-version check: %v", err)
	}
}

func TestCrossVersionAcceptsReplacedDialer(t *testing.T) {
	e := crossEndpoint(t)
	secret := newSecret(t)
	given := bytes.Clone(secret)
	refused := make(chan error, 1)
	l, err := e.ListenWith(ListenOptions{CrossVersion: &CrossVersion{Secret: given},
		Refused: func(_ net.Conn, err error) { refused <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	clear(given)
	accept := func() (net.Conn, error) {
		type result struct {
			conn net.Conn
			err  error
		}
		done := make(chan result, 1)
		go func() {
			c, err := l.Accept()
			done <- result{c, err}
		}()
		select {
		case r := <-done:
			return r.conn, r.err
		case <-time.After(15 * time.Second):
			t.Fatal("no client reached the listener")
			return nil, nil
		}
	}

	h := startCrossHelper(t, "dial", e, secret)
	conn, err := accept()
	if err != nil {
		t.Fatalf("cross-version listener refused a replaced program: %v", err)
	}
	if pid, err := PeerPID(conn); err != nil || pid != h.cmd.Process.Pid {
		t.Fatalf("PeerPID() = %d, %v; want %d", pid, err, h.cmd.Process.Pid)
	}
	h.expect(t, "dialed")
	_ = conn.Close()

	h = startCrossHelper(t, "dial", e, newSecret(t))
	_, err = accept()
	wantRefusalOf(t, err, h.cmd.Process.Pid, "cross-version check failed")
	select {
	case err := <-refused:
		wantRefusalOf(t, err, h.cmd.Process.Pid, "cross-version check failed")
	default:
		t.Fatal("refused callback did not run")
	}
	h.expect(t, "refused")
}

func TestCrossVersionHelperProcess(t *testing.T) {
	if os.Getenv("LOCALIPC_CROSS_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) != 6 {
		os.Exit(2)
	}
	mode := args[1]
	e := Endpoint{RuntimeDirName: args[2], SocketBase: args[3], FallbackDir: args[4], PipePrefix: args[5],
		ErrorName: "example"}
	secret := make([]byte, MinSecretSize)
	if _, err := io.ReadFull(os.Stdin, secret); err != nil {
		os.Exit(2)
	}
	fmt.Println("started")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, replaced, err := ProgramReplaced(); err == nil && replaced {
			break
		}
		if time.Now().After(deadline) {
			fmt.Println("not replaced")
			os.Exit(2)
		}
		time.Sleep(10 * time.Millisecond)
	}
	option := &CrossVersion{Secret: secret}
	switch mode {
	case "listen":
		l, err := e.ListenWith(ListenOptions{CrossVersion: option})
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		fmt.Println("ready")
		for {
			c, err := l.Accept()
			if errors.Is(err, net.ErrClosed) {
				os.Exit(2)
			}
			if err == nil {
				// Stay connected until the client closes: macOS cannot report the
				// peer of a closed connection.
				go func(c net.Conn) {
					_, _ = c.Write([]byte("hello\n"))
					_, _ = io.Copy(io.Discard, c)
					_ = c.Close()
				}(c)
			}
		}
	case "dial":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := e.DialWith(ctx, DialOptions{CrossVersion: option})
		if err != nil {
			fmt.Println("refused")
			os.Exit(0)
		}
		fmt.Println("dialed")
		_, _ = io.Copy(io.Discard, c)
		_ = c.Close()
	}
	os.Exit(0)
}

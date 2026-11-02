//go:build linux || darwin

package localipc

import (
	"bufio"
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func echoChallenge(want string, called *bool) Challenge {
	return func(ctx context.Context, conn net.Conn) error {
		if called != nil {
			*called = true
		}
		if _, err := conn.Write([]byte("nonce\n")); err != nil {
			return err
		}
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return err
		}
		if line != want+"\n" {
			return errors.New("wrong proof")
		}
		return nil
	}
}

func hardenedHelper(t *testing.T, program, mode string) (Endpoint, *exec.Cmd) {
	t.Helper()
	e := testEndpoint(t)
	if err := os.Mkdir(filepath.Dir(mustPath(t, e)), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := startHelper(t, program, mode, mustPath(t, e))
	t.Cleanup(func() { stopHelper(cmd) })
	return e, cmd
}

func wantPeerError(t *testing.T, err error, pid int, reason string) {
	t.Helper()
	var pe *PeerError
	if !errors.Is(err, ErrRefused) || !errors.As(err, &pe) || pe.PID != pid || !strings.Contains(err.Error(), reason) {
		t.Fatalf("got %v; want a refusal of process %d with %q", err, pid, reason)
	}
}

func TestDialOptionsRequireChallenge(t *testing.T) {
	e := testEndpoint(t)
	if _, err := e.DialWith(context.Background(), DialOptions{Server: CheckHardened}); !errors.Is(err, ErrNoChallenge) {
		t.Fatalf("hardened check without challenge = %v", err)
	}
	if _, err := e.DialWith(context.Background(), DialOptions{Server: ServerCheck(7)}); err == nil {
		t.Fatal("unknown server check accepted")
	}
}

func TestHardenedServerWithChallenge(t *testing.T) {
	e, cmd := hardenedHelper(t, os.Args[0], "harden-echo")
	conn, err := e.DialWith(context.Background(), DialOptions{Server: CheckHardened,
		Challenge: echoChallenge("proof:nonce", nil)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if pid, err := PeerPID(conn); err != nil || pid != cmd.Process.Pid {
		t.Fatalf("PeerPID() = %d, %v; want %d", pid, err, cmd.Process.Pid)
	}
	if uid, err := PeerUID(conn); err != nil || uid != uint32(os.Getuid()) {
		t.Fatalf("PeerUID() = %d, %v", uid, err)
	}
}

func TestHardenedServerRejectsWrongAndSilentChallenge(t *testing.T) {
	e, cmd := hardenedHelper(t, os.Args[0], "harden-echo")
	_, err := e.DialWith(context.Background(), DialOptions{Server: CheckHardened,
		Challenge: echoChallenge("proof:other", nil)})
	wantPeerError(t, err, cmd.Process.Pid, "wrong proof")

	e, cmd = hardenedHelper(t, os.Args[0], "harden-listen")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = e.DialWith(ctx, DialOptions{Server: CheckHardened, Challenge: echoChallenge("proof:nonce", nil)})
	wantPeerError(t, err, cmd.Process.Pid, "challenge failed")
	if time.Since(started) > 3*time.Second {
		t.Fatalf("silent peer held the challenge for %v", time.Since(started))
	}
}

func TestHardenedServerRejectsForeignProgram(t *testing.T) {
	e, cmd := hardenedHelper(t, copyTestProgram(t), "harden-echo")
	called := false
	_, err := e.DialWith(context.Background(), DialOptions{Server: CheckHardened,
		Challenge: echoChallenge("proof:other", &called)})
	if hardenedProgramCheck {
		wantPeerError(t, err, cmd.Process.Pid, "another program")
		if called {
			t.Fatal("challenge ran for another program")
		}
		return
	}
	// Linux cannot see the hardened program; the challenge is the gate.
	wantPeerError(t, err, cmd.Process.Pid, "challenge failed")
	if !called {
		t.Fatal("hardened server accepted without its challenge")
	}
}

func TestRejectsForeignUser(t *testing.T) {
	if err := checkUser(os.Getpid(), uint32(os.Getuid())+1); !errors.Is(err, ErrRefused) {
		t.Fatalf("foreign UID accepted by the user check: %v", err)
	}
	if err := checkUser(0, uint32(os.Getuid())); !errors.Is(err, ErrRefused) {
		t.Fatalf("unidentifiable process accepted by the user check: %v", err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	for _, program := range []bool{true, false} {
		if _, err := checkPeer(a, program); !errors.Is(err, ErrRefused) {
			t.Fatalf("non-socket connection accepted: %v", err)
		}
	}
	if _, err := PeerPID(a); !errors.Is(err, ErrRefused) {
		t.Fatalf("PeerPID() of a non-socket connection = %v", err)
	}
}

func TestRefusedCallbackGetsPeer(t *testing.T) {
	e := testEndpoint(t)
	type refusal struct {
		pid int
		err error
	}
	got := make(chan refusal, 1)
	l, err := e.ListenWith(ListenOptions{Refused: func(conn net.Conn, err error) {
		pid, _ := PeerPID(conn)
		got <- refusal{pid, err}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if c != nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	cmd := startHelper(t, copyTestProgram(t), "dial", mustPath(t, e))
	defer stopHelper(cmd)
	wantPeerError(t, <-accepted, cmd.Process.Pid, "another program")
	select {
	case r := <-got:
		if r.pid != cmd.Process.Pid {
			t.Fatalf("callback saw process %d; want %d", r.pid, cmd.Process.Pid)
		}
		wantPeerError(t, r.err, cmd.Process.Pid, "another program")
	default:
		t.Fatal("refused callback did not run")
	}
}

func TestHandOverMovesChannelToSuccessor(t *testing.T) {
	e := testEndpoint(t)
	old, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	successor, err := e.ListenWith(ListenOptions{Successor: true})
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	if _, err := e.ListenWith(ListenOptions{Successor: true}); !errors.Is(err, ErrRunning) {
		t.Fatalf("second successor = %v", err)
	}
	if err := HandOver(successor); err == nil {
		t.Fatal("a successor handed over before it took over")
	}
	if err := HandOver(old); err != nil {
		t.Fatal(err)
	}
	if err := HandOver(old); err == nil {
		t.Fatal("channel handed over twice")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := AwaitHandover(ctx, successor); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan error, 1)
	go func() {
		c, err := successor.Accept()
		if c != nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	conn, err := e.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("successor did not get the connection: %v", err)
	}
	_ = conn.Close()
	path := mustPath(t, e)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("closing the old listener removed the successor's socket: %v", err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRunning) {
		t.Fatalf("listener started beside the successor: %v", err)
	}
	if err := successor.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("successor's socket remains after close: %v", err)
	}
}

func TestHandOverRejectsMissingDeadOrOpenSuccessor(t *testing.T) {
	e := testEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := HandOver(l); !errors.Is(err, ErrRefused) {
		t.Fatalf("handover without successor = %v", err)
	}
	next := mustPath(t, e) + nextSuffix
	dead, err := net.ListenUnix("unix", &net.UnixAddr{Name: next, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	dead.SetUnlinkOnClose(false)
	_ = os.Chmod(next, socketMode)
	_ = dead.Close()
	if err := os.WriteFile(next+lockSuffix, nil, lockMode); err != nil {
		t.Fatal(err)
	}
	if err := HandOver(l); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "no successor holds") {
		t.Fatalf("handover to a dead successor = %v", err)
	}
	successor, err := e.ListenWith(ListenOptions{Successor: true})
	if err != nil {
		t.Fatalf("dead successor socket not replaced: %v", err)
	}
	defer successor.Close()
	if err := os.Chmod(next, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := HandOver(l); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "not private") {
		t.Fatalf("handover to an open successor socket = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := AwaitHandover(ctx, successor); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("await without handover = %v", err)
	}
	if err := AwaitHandover(ctx, l); err == nil {
		t.Fatal("await accepted a listener that is no successor")
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	if err := HandOver(tcp); err == nil {
		t.Fatal("foreign listener handed over")
	}
}

func TestProgramReplacedAfterUpdate(t *testing.T) {
	path, replaced, err := ProgramReplaced()
	if err != nil || replaced || !filepath.IsAbs(path) {
		t.Fatalf("ProgramReplaced() = %q, %v, %v", path, replaced, err)
	}
	cmd := exec.Command(copyTestProgram(t), "-test.run=^TestHelperProcess$", "--", "replaced", "unused")
	cmd.Env = append(os.Environ(), "LOCALIPC_HELPER=1")
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "replaced=true err=<nil> refused=true" {
		t.Fatalf("replaced helper said %q: %v", out, err)
	}
}

//go:build windows

package localipc

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func windowsChallenge(want string) Challenge {
	return func(ctx context.Context, conn net.Conn) error {
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

func TestWindowsHardenedDialAndChallenge(t *testing.T) {
	e := windowsEndpoint(t)
	if _, err := e.DialWith(context.Background(), DialOptions{Server: CheckHardened}); !errors.Is(err, ErrNoChallenge) {
		t.Fatalf("hardened check without challenge = %v", err)
	}
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			line, _ := bufio.NewReader(c).ReadString('\n')
			_, _ = c.Write([]byte("proof:" + line))
			time.Sleep(200 * time.Millisecond)
			_ = c.Close()
		}
	}()
	conn, err := e.DialWith(context.Background(), DialOptions{Server: CheckHardened,
		Challenge: windowsChallenge("proof:nonce")})
	if err != nil {
		t.Fatal(err)
	}
	if pid, err := PeerPID(conn); err != nil || pid != os.Getpid() {
		t.Fatalf("PeerPID() = %d, %v", pid, err)
	}
	if _, err := PeerUID(conn); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("PeerUID() = %v", err)
	}
	_ = conn.Close()
	_, err = e.DialWith(context.Background(), DialOptions{Server: CheckHardened,
		Challenge: windowsChallenge("proof:other")})
	var pe *PeerError
	if !errors.Is(err, ErrRefused) || !errors.As(err, &pe) || pe.PID != os.Getpid() ||
		!strings.Contains(err.Error(), "wrong proof") {
		t.Fatalf("wrong challenge = %v", err)
	}
}

func TestWindowsRefusedCallbackAndHandoverUnsupported(t *testing.T) {
	e := windowsEndpoint(t)
	if _, err := e.ListenWith(ListenOptions{Successor: true}); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("successor = %v", err)
	}
	if _, err := e.ListenWith(ListenOptions{Successor: true, Earlier: &e}); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("successor of an earlier endpoint = %v", err)
	}
	refused := make(chan error, 1)
	l, err := e.ListenWith(ListenOptions{Refused: func(_ net.Conn, err error) { refused <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := HandOver(l); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("HandOver() = %v", err)
	}
	if err := AwaitHandover(context.Background(), l); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("AwaitHandover() = %v", err)
	}
	accepted := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if c != nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	cmd := startWindowsHelper(t, copyWindowsProgram(t), "dial", windowsPath(t, e))
	defer stopWindowsHelper(cmd)
	var pe *PeerError
	select {
	case err := <-accepted:
		if !errors.As(err, &pe) || pe.PID != cmd.Process.Pid || !strings.Contains(err.Error(), "another program") {
			t.Fatalf("Accept() = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not inspect the client")
	}
	select {
	case err := <-refused:
		if !errors.As(err, &pe) || pe.PID != cmd.Process.Pid {
			t.Fatalf("callback got %v", err)
		}
	default:
		t.Fatal("refused callback did not run")
	}
}

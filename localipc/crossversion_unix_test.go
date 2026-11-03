//go:build linux || darwin

package localipc

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func crossEndpoint(t *testing.T) Endpoint { return testEndpoint(t) }

func copyProgram(t *testing.T) string { return copyTestProgram(t) }

// replaceRunningProgram renames a new copy over the running program, as an
// update does on Unix.
func replaceRunningProgram(t *testing.T, program string) {
	t.Helper()
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program+".new", data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(program+".new", program); err != nil {
		t.Fatal(err)
	}
}

func TestCrossVersionRejectsShortSecretBeforeSocket(t *testing.T) {
	e := testEndpoint(t)
	path := mustPath(t, e)
	for name, secret := range map[string][]byte{"nil": nil, "empty": {}, "short": testSecret(1)[1:]} {
		option := &CrossVersion{Secret: secret}
		for _, opts := range []ListenOptions{{CrossVersion: option}, {CrossVersion: option, Successor: true}} {
			if _, err := e.ListenWith(opts); !errors.Is(err, ErrShortSecret) {
				t.Fatalf("%s secret: ListenWith() = %v", name, err)
			}
		}
		if _, err := os.Lstat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s secret: listener prepared its directory: %v", name, err)
		}
		_, err := e.DialWith(context.Background(), DialOptions{CrossVersion: option})
		if !errors.Is(err, ErrShortSecret) || errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s secret: DialWith() = %v", name, err)
		}
	}
	if _, err := e.DialWith(context.Background(), DialOptions{Server: CheckHardened,
		CrossVersion: &CrossVersion{Secret: testSecret(1)}}); !errors.Is(err, ErrNoChallenge) {
		t.Fatalf("hardened check without challenge = %v", err)
	}
}

func TestCrossVersionChecksUserBeforeChallenge(t *testing.T) {
	if err := checkUser(os.Getpid(), uint32(os.Getuid())+1); !errors.Is(err, ErrRefused) {
		t.Fatalf("foreign UID accepted by the user check: %v", err)
	}
	verify, err := ListenOptions{CrossVersion: &CrossVersion{Secret: testSecret(1)}}.peerCheck()
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	done := make(chan error, 1)
	go func() {
		_ = b.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, err := b.Read(make([]byte, 1))
		done <- err
	}()
	if _, err := verify(a); !errors.Is(err, ErrRefused) || strings.Contains(err.Error(), "cross-version") {
		t.Fatalf("unidentified peer = %v; want a refusal before the challenge", err)
	}
	if err := <-done; !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the challenge ran for an unidentified peer: %v", err)
	}
}

func TestCrossVersionAcceptBoundsSilentClient(t *testing.T) {
	e := testEndpoint(t)
	refused := make(chan error, 1)
	l, err := e.ListenWith(ListenOptions{CrossVersion: &CrossVersion{Secret: testSecret(1)},
		Refused: func(_ net.Conn, err error) { refused <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	silent, err := net.Dial("unix", mustPath(t, e))
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	started := time.Now()
	_, err = l.Accept()
	wantPeerError(t, err, os.Getpid(), "cross-version check failed")
	if waited := time.Since(started); waited > crossVersionWait+2*time.Second {
		t.Fatalf("a silent client held Accept for %v", waited)
	}
	select {
	case err := <-refused:
		wantPeerError(t, err, os.Getpid(), "cross-version check failed")
	default:
		t.Fatal("refused callback did not run")
	}
}

func TestCrossVersionDialBoundsSilentListener(t *testing.T) {
	e := testEndpoint(t)
	path := mustPath(t, e)
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if err := os.Chmod(path, socketMode); err != nil {
		t.Fatal(err)
	}
	held := make(chan net.Conn, 1)
	go func() {
		c, _ := raw.Accept()
		held <- c
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = e.DialWith(ctx, DialOptions{CrossVersion: &CrossVersion{Secret: testSecret(1)}})
	wantPeerError(t, err, os.Getpid(), "cross-version check failed")
	if time.Since(started) > 3*time.Second {
		t.Fatalf("a silent listener held the dial for %v", time.Since(started))
	}
	if c := <-held; c != nil {
		_ = c.Close()
	}
}

func TestCrossVersionHandOver(t *testing.T) {
	e := testEndpoint(t)
	option := &CrossVersion{Secret: testSecret(1)}
	old, err := e.ListenWith(ListenOptions{CrossVersion: option})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	successor, err := e.ListenWith(ListenOptions{Successor: true, CrossVersion: option})
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	if err := HandOver(old); err != nil {
		t.Fatal(err)
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
	conn, err := e.DialWith(ctx, DialOptions{CrossVersion: option})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-accepted; err != nil {
		t.Fatalf("successor refused a cross-version client: %v", err)
	}
}

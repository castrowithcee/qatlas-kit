//go:build windows

package localipc

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func crossEndpoint(t *testing.T) Endpoint { return windowsEndpoint(t) }

func copyProgram(t *testing.T) string { return copyWindowsProgram(t) }

// replaceRunningProgram moves the running program aside and puts a new copy
// at its path, as an update does on Windows.
func replaceRunningProgram(t *testing.T, program string) {
	t.Helper()
	if err := os.Rename(program, program+".old"); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(program + ".old")
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	newFile, err := os.Create(program)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(newFile, old)
	if err := newFile.Close(); err != nil || copyErr != nil {
		t.Fatalf("replace program: %v, %v", copyErr, err)
	}
}

func TestWindowsCrossVersionRejectsShortSecretBeforePipe(t *testing.T) {
	e := windowsEndpoint(t)
	for name, secret := range map[string][]byte{"nil": nil, "empty": {}, "short": testSecret(1)[1:]} {
		option := &CrossVersion{Secret: secret}
		if _, err := e.ListenWith(ListenOptions{CrossVersion: option}); !errors.Is(err, ErrShortSecret) {
			t.Fatalf("%s secret: ListenWith() = %v", name, err)
		}
		_, err := e.DialWith(context.Background(), DialOptions{CrossVersion: option})
		if !errors.Is(err, ErrShortSecret) || errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s secret: DialWith() = %v", name, err)
		}
	}
	// No refused listener holds the first pipe instance.
	l, err := e.Listen()
	if err != nil {
		t.Fatalf("a refused listener created the pipe: %v", err)
	}
	_ = l.Close()
}

func TestWindowsCrossVersionChecksOnlyUser(t *testing.T) {
	pid := uint32(os.Getpid())
	saved := currentUserSID
	currentUserSID = func() (*windows.SID, error) { return windows.CreateWellKnownSid(windows.WinNetworkSid) }
	err := checkPeerUser(pid)
	currentUserSID = saved
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("foreign SID accepted: %v", err)
	}
	if err := checkPeerUser(0); !errors.Is(err, ErrRefused) {
		t.Fatalf("unidentifiable peer accepted: %v", err)
	}
	savedErr := ownProgramErr
	ownProgramErr = errors.New("cannot be read")
	defer func() { ownProgramErr = savedErr }()
	if err := checkPeerUser(pid); err != nil {
		t.Fatalf("the user check looked at the program: %v", err)
	}
	if err := checkProcess(pid); !errors.Is(err, ErrRefused) {
		t.Fatalf("the program check accepted an unreadable program: %v", err)
	}
}

func TestWindowsCrossVersionAcceptBoundsSilentClient(t *testing.T) {
	e := windowsEndpoint(t)
	refused := make(chan error, 1)
	l, err := e.ListenWith(ListenOptions{CrossVersion: &CrossVersion{Secret: testSecret(1)},
		Refused: func(_ net.Conn, err error) { refused <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	name, err := windows.UTF16PtrFromString(windowsPath(t, e))
	if err != nil {
		t.Fatal(err)
	}
	silent, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(silent)
	started := time.Now()
	_, err = l.Accept()
	var pe *PeerError
	if !errors.As(err, &pe) || pe.PID != os.Getpid() || !strings.Contains(err.Error(), "cross-version check failed") {
		t.Fatalf("Accept() = %v", err)
	}
	if waited := time.Since(started); waited > crossVersionWait+2*time.Second {
		t.Fatalf("a silent client held Accept for %v", waited)
	}
	select {
	case err := <-refused:
		if !errors.As(err, &pe) || pe.PID != os.Getpid() {
			t.Fatalf("callback got %v", err)
		}
	default:
		t.Fatal("refused callback did not run")
	}
}

func TestWindowsCrossVersionDialBoundsSilentListener(t *testing.T) {
	e := windowsEndpoint(t)
	sa, err := pipeSecurity()
	if err != nil {
		t.Fatal(err)
	}
	h, err := createPipe(windowsPath(t, e), true, sa)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = e.DialWith(ctx, DialOptions{CrossVersion: &CrossVersion{Secret: testSecret(1)}})
	var pe *PeerError
	if !errors.Is(err, ErrRefused) || !errors.As(err, &pe) || pe.PID != os.Getpid() ||
		!strings.Contains(err.Error(), "cross-version check failed") {
		t.Fatalf("DialWith() = %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("a silent listener held the dial for %v", time.Since(started))
	}
}

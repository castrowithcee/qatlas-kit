//go:build windows

package localipc

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func windowsEndpoint(t *testing.T) Endpoint {
	t.Helper()
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return Endpoint{PipePrefix: "example-", SocketBase: hex.EncodeToString(nonce[:]), ErrorName: "example"}
}

func windowsPath(t *testing.T, e Endpoint) string {
	t.Helper()
	path, err := e.Path()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWindowsPathAndNames(t *testing.T) {
	e := windowsEndpoint(t)
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if path := windowsPath(t, e); path != pipeNamespace+e.PipePrefix+sid.String()+"-"+e.SocketBase {
		t.Fatalf("unexpected pipe path: %q", path)
	}
	e.PipePrefix = `other\name`
	if _, err := e.Path(); err == nil {
		t.Fatal("invalid pipe prefix accepted")
	}
	e.PipePrefix = "example-"
	e.SocketBase = ""
	if _, err := e.Path(); err == nil {
		t.Fatal("empty socket base accepted")
	}
}

func TestWindowsConnectionAndPermissions(t *testing.T) {
	e := windowsEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan struct {
		conn net.Conn
		err  error
	}, 1)
	go func() {
		c, err := l.Accept()
		accepted <- struct {
			conn net.Conn
			err  error
		}{c, err}
	}()
	client, err := e.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	if server.err != nil {
		t.Fatal(server.err)
	}
	defer server.conn.Close()
	for _, c := range []net.Conn{client, server.conn} {
		if pid, err := peerProcessID(c); err != nil || pid != uint32(os.Getpid()) {
			t.Fatalf("peer PID = %d, %v", pid, err)
		}
		if err := VerifyPeer(c); err != nil {
			t.Fatal(err)
		}
	}
	checkWindowsPipeACL(t, client.(*pipeConn).h)
	if _, err := server.conn.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	var b [2]byte
	if _, err := io.ReadFull(client, b[:]); err != nil || string(b[:]) != "ok" {
		t.Fatalf("pipe data = %q, %v", b, err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRunning) {
		t.Fatalf("second listener = %v, want ErrRunning", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(b[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read deadline = %v", err)
	}
	if err := client.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := client.Read(b[:]); done <- err }()
	time.Sleep(20 * time.Millisecond)
	_ = client.SetReadDeadline(time.Unix(1, 0))
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("moved deadline = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("moving the read deadline did not unblock Read")
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	_ = server.conn.Close()
	restarted, err := e.Listen()
	if err != nil {
		t.Fatalf("listener could not restart: %v", err)
	}
	_ = restarted.Close()
}

func checkWindowsPipeACL(t *testing.T, h windows.Handle) {
	t.Helper()
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("pipe DACL must be protected and have exactly two entries: %s, %v", sd, err)
	}
	user, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	network, err := windows.CreateWellKnownSid(windows.WinNetworkSid)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		kind byte
		sid  *windows.SID
	}{{windows.ACCESS_DENIED_ACE_TYPE, network}, {windows.ACCESS_ALLOWED_ACE_TYPE, user}} {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(i), &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != want.kind || !want.sid.Equals((*windows.SID)(unsafe.Pointer(&ace.SidStart))) || uint32(ace.Mask) != windows.GENERIC_ALL {
			t.Fatalf("pipe ACE %d does not deny network or allow only the user: %s", i, sd)
		}
	}
}

func TestWindowsRejectsForeignUserAndUnreadableIdentity(t *testing.T) {
	saved := currentUserSID
	currentUserSID = func() (*windows.SID, error) { return windows.CreateWellKnownSid(windows.WinNetworkSid) }
	defer func() { currentUserSID = saved }()
	if err := checkProcess(uint32(os.Getpid())); !errors.Is(err, ErrRefused) {
		t.Fatalf("foreign SID accepted: %v", err)
	}
	currentUserSID = saved
	if err := checkProcess(0); !errors.Is(err, ErrRefused) {
		t.Fatalf("unidentifiable peer accepted: %v", err)
	}
	savedErr := ownProgramErr
	ownProgramErr = errors.New("cannot be read")
	defer func() { ownProgramErr = savedErr }()
	if err := checkProcess(uint32(os.Getpid())); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("unreadable program accepted: %v", err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if err := VerifyPeer(a); !errors.Is(err, ErrRefused) {
		t.Fatalf("non-pipe connection accepted: %v", err)
	}
}

func TestWindowsRejectsForeignProgramOnBothEnds(t *testing.T) {
	other := copyWindowsProgram(t)
	e := windowsEndpoint(t)
	listener, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan error, 1)
	go func() {
		c, err := listener.Accept()
		if c != nil {
			_ = c.Close()
		}
		accepted <- err
	}()
	cmd := startWindowsHelper(t, other, "dial", windowsPath(t, e))
	defer stopWindowsHelper(cmd)
	select {
	case err := <-accepted:
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "another program") {
			t.Fatalf("server accepted foreign program: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not inspect the client")
	}
	_ = listener.Close()
	e = windowsEndpoint(t)
	cmd = startWindowsHelper(t, other, "listen", windowsPath(t, e))
	defer stopWindowsHelper(cmd)
	if _, err := e.Dial(context.Background()); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "another program") {
		t.Fatalf("client accepted foreign program: %v", err)
	}
}

func TestWindowsRejectsReplacedOwnProgram(t *testing.T) {
	program := copyWindowsProgram(t)
	cmd := exec.Command(program, "-test.run=^TestWindowsReplacementHelper$")
	cmd.Env = append(os.Environ(), "LOCALIPC_REPLACEMENT_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer stopWindowsHelper(cmd)
	lines := bufio.NewReader(stdout)
	if line, err := lines.ReadString('\n'); err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("replacement helper said %q: %v", line, err)
	}
	if err := os.Rename(program, program+".old"); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(program + ".old")
	if err != nil {
		t.Fatal(err)
	}
	newFile, err := os.Create(program)
	if err != nil {
		_ = old.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(newFile, old)
	closeErr := newFile.Close()
	_ = old.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("replace program: %v, %v", copyErr, closeErr)
	}
	line, err := lines.ReadString('\n')
	if err != nil || !strings.Contains(line, "removed or replaced") {
		t.Fatalf("replaced program accepted: %q, %v", line, err)
	}
}

func TestWindowsReplacementHelper(t *testing.T) {
	if os.Getenv("LOCALIPC_REPLACEMENT_HELPER") != "1" {
		return
	}
	fmt.Println("ready")
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if replaced, err := ownProgramReplaced(); err != nil || replaced {
			break
		}
	}
	fmt.Println(checkProcess(uint32(os.Getpid())))
	os.Exit(0)
}

func TestWindowsBusyPipeHasFiniteDialWait(t *testing.T) {
	e := windowsEndpoint(t)
	name, err := windows.UTF16PtrFromString(windowsPath(t, e))
	if err != nil {
		t.Fatal(err)
	}
	sa, err := pipeSecurity()
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, pipeBuffer, pipeBuffer, 0, sa)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	client, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(client)
	saved := dialWait
	dialWait = 100 * time.Millisecond
	defer func() { dialWait = saved }()
	done := make(chan error, 1)
	go func() {
		c, err := e.Dial(context.Background())
		if c != nil {
			_ = c.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("busy pipe returned %v, want deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Dial with no context deadline waited indefinitely")
	}
}

func TestWindowsPipeCloseAndEOF(t *testing.T) {
	e := windowsEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := l.Accept(); accepted <- c }()
	client, err := e.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	if server == nil {
		t.Fatal("server did not accept connection")
	}
	defer server.Close()
	if _, err := server.Write([]byte("last")); err != nil {
		t.Fatal(err)
	}
	_ = server.Close()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	var b [4]byte
	if _, err := io.ReadFull(client, b[:]); err != nil || string(b[:]) != "last" {
		t.Fatalf("data before close = %q, %v", b, err)
	}
	if _, err := client.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("read after close = %v, want EOF", err)
	}
	if _, err := client.Write([]byte("later")); err == nil {
		t.Fatal("write to closed peer succeeded")
	}
	_ = client.Close()
	accepted = make(chan net.Conn, 1)
	go func() { c, _ := l.Accept(); accepted <- c }()
	client, err = e.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	server = <-accepted
	if server == nil {
		t.Fatal("server did not accept second connection")
	}
	defer server.Close()
	blocked := make(chan error, 1)
	go func() { _, err := server.Read(b[:]); blocked <- err }()
	time.Sleep(20 * time.Millisecond)
	_ = server.Close()
	select {
	case err := <-blocked:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("read after local Close = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not unblock Read")
	}
	_ = client.Close()
}

func copyWindowsProgram(t *testing.T) string {
	t.Helper()
	src, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	path := filepath.Join(t.TempDir(), "other.exe")
	dst, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func startWindowsHelper(t *testing.T, program, mode, path string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(program, "-test.run=^TestWindowsHelper$", "--", mode, path)
	cmd.Env = append(os.Environ(), "LOCALIPC_WINDOWS_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		stopWindowsHelper(cmd)
		t.Fatalf("helper said %q: %v", line, err)
	}
	return cmd
}

func stopWindowsHelper(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func TestWindowsHelper(t *testing.T) {
	if os.Getenv("LOCALIPC_WINDOWS_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 3 {
		os.Exit(2)
	}
	mode, path := args[1], args[2]
	var h windows.Handle
	var err error
	switch mode {
	case "listen":
		var sa *windows.SecurityAttributes
		sa, err = pipeSecurity()
		if err == nil {
			h, err = createPipe(path, true, sa)
		}
	case "dial":
		var name *uint16
		name, err = windows.UTF16PtrFromString(path)
		if err == nil {
			h, err = windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		}
	case "harden":
		err = Harden()
		if err == nil {
			err = checkHardenedProcessDACL()
		}
	default:
		err = fmt.Errorf("unknown helper mode")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("ready")
	if h != 0 {
		time.Sleep(5 * time.Second)
		_ = windows.CloseHandle(h)
	}
	os.Exit(0)
}

func TestWindowsHardenDACL(t *testing.T) {
	cmd := startWindowsHelper(t, os.Args[0], "harden", "unused")
	defer stopWindowsHelper(cmd)
}

func checkHardenedProcessDACL() error {
	sd, err := windows.GetSecurityInfo(windows.CurrentProcess(), windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 3 || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("process DACL is not protected with three entries: %s, %v", sd, err)
	}
	user, err := currentUserSID()
	if err != nil {
		return err
	}
	owner, err := windows.StringToSid("S-1-3-4")
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	want := []*windows.SID{user, owner, system}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		mask := uint32(ace.Mask)
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !want[i].Equals((*windows.SID)(unsafe.Pointer(&ace.SidStart))) ||
			mask&windows.PROCESS_VM_READ != 0 ||
			mask&(windows.PROCESS_VM_WRITE|windows.PROCESS_VM_OPERATION) != 0 || mask&windows.PROCESS_QUERY_LIMITED_INFORMATION == 0 {
			return fmt.Errorf("process DACL permits memory access or blocks identity queries: %s", sd)
		}
	}
	return nil
}

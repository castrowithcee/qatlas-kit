package background

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

const helperLimit = 1024

// helperCommand runs this test binary as the background process in the given mode.
func helperCommand(mode string, e localipc.Endpoint) Command {
	return Command{
		Args: []string{"-test.run=^TestBackgroundHelper$", "--", mode, e.SocketBase},
		Env:  append(os.Environ(), "BACKGROUND_HELPER=1"),
	}
}

func endpointFromBase(base string) localipc.Endpoint {
	e := testEndpointFixed
	e.SocketBase = base
	return e
}

// testEndpointFixed carries the platform-independent part of the endpoint into the helper process.
var testEndpointFixed = localipc.Endpoint{RuntimeDirName: "session", PipePrefix: "example-", ErrorName: "example"}

// TestBackgroundHelper is the background process the tests start.
func TestBackgroundHelper(t *testing.T) {
	if os.Getenv("BACKGROUND_HELPER") != "1" {
		return
	}
	time.AfterFunc(2*time.Minute, func() { os.Exit(3) }) // never outlives a test run that lost track of it
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	mode, base := args[0], args[1]
	child, ok := Inherit()
	if !ok {
		os.Exit(2)
	}
	defer child.Close()
	if mode == "silent" {
		time.Sleep(time.Minute)
		return
	}
	if mode == "fail" {
		child.Fail("synthetic\nfailure")
		return
	}
	data, err := child.ReadHandover(helperLimit)
	if err != nil {
		child.Fail(err.Error())
		return
	}
	e := endpointFromBase(base)
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		e.FallbackDir = dir
	}
	l, err := e.Listen()
	if errors.Is(err, localipc.ErrRunning) {
		child.Running()
		return
	}
	if err != nil {
		child.Fail(err.Error())
		return
	}
	child.Ready()
	for {
		conn, err := l.Accept()
		if err != nil {
			continue
		}
		_, _ = conn.Write(data)
		_ = conn.Close()
	}
}

// TestStarterHelper starts the background process and exits at once, as a short-lived command would.
func TestStarterHelper(t *testing.T) {
	if os.Getenv("BACKGROUND_STARTER") != "1" {
		return
	}
	base := os.Getenv("BACKGROUND_BASE")
	e := endpointFromBase(base)
	res, err := Start(context.Background(), helperCommand("serve", e), []byte("payload"), Options{})
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Stdout.WriteString(res.Report + " " + strconv.Itoa(res.PID) + "\n")
	os.Exit(0)
}

func killOnCleanup(t *testing.T, pid int) {
	t.Helper()
	t.Cleanup(func() {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	})
}

func read(t *testing.T, e localipc.Endpoint) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := e.Dial(ctx)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	data, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStartReportsReadyAndHandsOver(t *testing.T) {
	e := testEndpoint(t)
	res, err := Start(context.Background(), helperCommand("serve", e), []byte("payload"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	killOnCleanup(t, res.PID)
	if res.Report != ReportReady || res.PID <= 0 {
		t.Fatalf("Start() = %+v", res)
	}
	if got := read(t, e); got != "payload" {
		t.Fatalf("handover = %q", got)
	}
	if !processAlive(res.PID) {
		t.Fatal("process is not alive")
	}
	if !ownSession(res.PID) {
		t.Fatal("process does not lead its own session")
	}

	// The same endpoint again: the second process reports that one already runs and ends.
	second, err := Start(context.Background(), helperCommand("serve", e), nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	killOnCleanup(t, second.PID)
	if second.Report != ReportRunning {
		t.Fatalf("second Start() = %+v, want running", second)
	}
	if got := read(t, e); got != "payload" {
		t.Fatalf("first process lost its handover: %q", got)
	}
}

func TestStartFailureAndTimeout(t *testing.T) {
	e := testEndpoint(t)
	_, err := Start(context.Background(), helperCommand("fail", e), nil, Options{})
	var re *ReportError
	if !errors.As(err, &re) || re.Reason != "synthetic failure" {
		t.Fatalf("Start() = %v, want the reported reason on one line", err)
	}

	started := time.Now()
	_, err = Start(context.Background(), helperCommand("silent", e), nil, Options{Timeout: 700 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "in time") || time.Since(started) > 8*time.Second {
		t.Fatalf("Start() of a silent process = %v after %v", err, time.Since(started))
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	_, err = Start(ctx, helperCommand("silent", e), nil, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() with a canceled context = %v", err)
	}

	if _, err := Start(context.Background(), Command{Program: "/nonexistent/program"}, nil, Options{}); err == nil {
		t.Fatal("Start() of a missing program succeeded")
	}
}

func TestStartRejectsOversizeHandover(t *testing.T) {
	e := testEndpoint(t)
	_, err := Start(context.Background(), helperCommand("serve", e), bytes.Repeat([]byte("x"), helperLimit+1), Options{})
	var re *ReportError
	if !errors.As(err, &re) || !strings.Contains(re.Reason, "larger") {
		t.Fatalf("Start() = %v, want the size refusal", err)
	}
}

func TestBackgroundProcessSurvivesTheStarter(t *testing.T) {
	e := testEndpoint(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestStarterHelper$")
	cmd.Env = append(os.Environ(), "BACKGROUND_HELPER=1", "BACKGROUND_STARTER=1", "BACKGROUND_BASE="+e.SocketBase)
	// Output waits for the end of the pipe: it returns only if the background process holds none of the
	// starter's standard streams.
	done := make(chan struct{})
	var out []byte
	var runErr error
	go func() { out, runErr = cmd.Output(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the starter did not end")
	}
	fields := strings.Fields(string(out))
	if runErr != nil || len(fields) != 2 || fields[0] != ReportReady {
		t.Fatalf("starter: %q, %v", out, runErr)
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	killOnCleanup(t, pid)
	time.Sleep(300 * time.Millisecond)
	if !processAlive(pid) {
		t.Fatal("the background process ended with its starter")
	}
	if got := read(t, e); got != "payload" {
		t.Fatalf("answer after the starter ended = %q", got)
	}
}

func TestOutcomeAndReason(t *testing.T) {
	if outcome("ready\n", nil) != nil || outcome(" running ", nil) != nil {
		t.Fatal("success words rejected")
	}
	if err := outcome("", context.DeadlineExceeded); err == nil || !strings.Contains(err.Error(), "in time") {
		t.Fatalf("outcome() = %v", err)
	}
	if err := outcome("", nil); err == nil {
		t.Fatal("empty report accepted")
	}
	if got := cleanReason("a\r\nb\tc"); got != "a b c" {
		t.Fatalf("cleanReason() = %q", got)
	}
	if got := cleanReason(" \n"); got != "failed" {
		t.Fatalf("cleanReason() = %q", got)
	}
	if got := cleanReason(strings.Repeat("é", 600)); len(got) > 512 || !strings.HasPrefix(got, "é") {
		t.Fatalf("cleanReason() length %d", len(got))
	}
}

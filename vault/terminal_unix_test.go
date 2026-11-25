//go:build linux || darwin

package vault

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/ptytest"
)

// hiddenModeEnv makes the helper program run ReadHidden and names how it ends.
const hiddenModeEnv = "VAULT_TEST_HIDDEN"

const hiddenPrompt = "example secret \x1b[2J: "

// TestReadHiddenHelper is the program the terminal tests start; it does nothing in a normal run. In mode
// "cancel" SIGUSR1 ends the context.
func TestReadHiddenHelper(t *testing.T) {
	mode := os.Getenv(hiddenModeEnv)
	if mode == "" {
		return
	}
	ctx := context.Background()
	if mode == "cancel" {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, syscall.SIGUSR1)
		defer stop()
	}
	text, err := ReadHidden(ctx, hiddenPrompt)
	os.Stdout.WriteString("\nresult: " + text + "|" + errString(err) + "\n")
	if mode == "cancel" {
		// Stay alive so the test can look at the terminal; the test ends the program.
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func startHidden(t *testing.T, mode string) *ptytest.Terminal {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pty, err := ptytest.Start(ptytest.Command{Path: self, Args: []string{"-test.run=^TestReadHiddenHelper$"},
		Env: []string{hiddenModeEnv + "=" + mode, "TERM=xterm-256color"}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pty.Close() })
	return pty
}

func waitEcho(t *testing.T, pty *ptytest.Terminal, want bool) {
	t.Helper()
	err := pty.WaitFor(15*time.Second, func(string) bool {
		echo, err := pty.Echo()
		return err == nil && echo == want
	})
	if err != nil {
		t.Fatalf("echo never became %v: %v: %q", want, err, pty.Output())
	}
}

func TestReadHiddenReadsALineWithoutEcho(t *testing.T) {
	pty := startHidden(t, "line")
	if err := pty.WaitForText(15*time.Second, `: `); err != nil {
		t.Fatalf("%v: %q", err, pty.Output())
	}
	waitEcho(t, pty, false)
	if _, err := pty.Write([]byte("tidy words\r")); err != nil {
		t.Fatal(err)
	}
	if err := pty.WaitForText(15*time.Second, "result: tidy words|nil"); err != nil {
		t.Fatalf("%v: %q", err, pty.Output())
	}
	out := pty.Output()
	if strings.Contains(out, "tidy words\r") || strings.Count(out, "tidy words") != 1 {
		t.Fatalf("the typed line was echoed: %q", out)
	}
	if !strings.Contains(out, `example secret \x1b[2J: `) || strings.Contains(out, "\x1b[2J") {
		t.Fatalf("control characters of the prompt were not masked: %q", out)
	}
	if code, err := pty.Wait(15 * time.Second); err != nil || code != 0 {
		t.Fatalf("the program ended with %d, %v", code, err)
	}
}

func TestReadHiddenEndsWithTheContextAndRestoresEcho(t *testing.T) {
	pty := startHidden(t, "cancel")
	waitEcho(t, pty, false)
	if err := syscall.Kill(pty.Pid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	if err := pty.WaitForText(15*time.Second, "result: |context canceled"); err != nil {
		t.Fatalf("%v: %q", err, pty.Output())
	}
	waitEcho(t, pty, true)
}

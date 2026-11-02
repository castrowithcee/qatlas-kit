package ptytest

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"
)

const helperEnv = "PTYTEST_HELPER"

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		return
	}
	switch mode {
	case "prompt":
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			os.Exit(3)
		}
		if runtime.GOOS != "windows" {
			tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
			if err != nil {
				os.Exit(4)
			}
			_ = tty.Close()
		}
		fmt.Print("secret> ")
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			os.Exit(6)
		}
		fmt.Printf("\ngot %s\n", strings.ToUpper(string(secret)))
		os.Exit(0)
	case "size":
		columns, rows, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			os.Exit(6)
		}
		fmt.Printf("size %dx%d\n", columns, rows)
		os.Exit(0)
	case "env":
		fmt.Printf("probe [%s]\n", os.Getenv("PTYTEST_PROBE"))
		os.Exit(0)
	case "exit":
		fmt.Println("bye")
		os.Exit(5)
	case "hang":
		fmt.Println("hanging")
		time.Sleep(time.Hour)
	}
	os.Exit(2)
}

func helperCommand(t *testing.T, mode string) Command {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Command{Path: self, Args: []string{"-test.run=^TestHelperProcess$"}, Env: []string{helperEnv + "=" + mode}}
}

func startHelper(t *testing.T, c Command) *Terminal {
	t.Helper()
	tm, err := Start(c)
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	return tm
}

func TestSecretPromptWithEchoOff(t *testing.T) {
	tm := startHelper(t, helperCommand(t, "prompt"))
	// The prompt appears before echo is off; input typed in between would be echoed.
	err := tm.WaitFor(10*time.Second, func(output string) bool {
		if !strings.Contains(output, "secret>") {
			return false
		}
		echo, err := tm.Echo()
		if errors.Is(err, errors.ErrUnsupported) {
			return true
		}
		return err == nil && !echo
	})
	if err != nil {
		t.Fatalf("no prompt with echo off: %v: %q", err, tm.Output())
	}
	if _, err := tm.Write([]byte("hush\r")); err != nil {
		t.Fatal(err)
	}
	code, err := tm.Wait(30 * time.Second)
	if err != nil || code != 0 {
		t.Fatalf("Wait = %d, %v: %q", code, err, tm.Output())
	}
	out := tm.Output()
	if !strings.Contains(out, "got HUSH") {
		t.Fatalf("the answer is missing: %q", out)
	}
	if strings.Contains(out, "hush") {
		t.Fatalf("the secret was echoed: %q", out)
	}
}

func TestEchoIsOnByDefault(t *testing.T) {
	tm := startHelper(t, helperCommand(t, "hang"))
	if err := tm.WaitForText(10*time.Second, "hanging"); err != nil {
		t.Fatalf("%v: %q", err, tm.Output())
	}
	echo, err := tm.Echo()
	if runtime.GOOS == "windows" {
		if !errors.Is(err, errors.ErrUnsupported) {
			t.Fatalf("Echo on Windows = %v, %v, want ErrUnsupported", echo, err)
		}
		return
	}
	if err != nil || !echo {
		t.Fatalf("Echo = %v, %v, want true", echo, err)
	}
}

func TestExitCode(t *testing.T) {
	tm := startHelper(t, helperCommand(t, "exit"))
	code, err := tm.Wait(30 * time.Second)
	if err != nil || code != 5 {
		t.Fatalf("Wait = %d, %v: %q", code, err, tm.Output())
	}
	if !strings.Contains(tm.Output(), "bye") {
		t.Fatalf("output = %q", tm.Output())
	}
	if tm.Running() {
		t.Fatal("Running after Wait")
	}
}

func TestSize(t *testing.T) {
	c := helperCommand(t, "size")
	c.Size = Size{Columns: 100, Rows: 30}
	tm := startHelper(t, c)
	if _, err := tm.Wait(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tm.Output(), "size 100x30") {
		t.Fatalf("output = %q", tm.Output())
	}
}

func TestEnvironmentIsNotInherited(t *testing.T) {
	t.Setenv("PTYTEST_PROBE", "inherited")
	tm := startHelper(t, helperCommand(t, "env"))
	if _, err := tm.Wait(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tm.Output(), "probe []") {
		t.Fatalf("output = %q", tm.Output())
	}
}

func TestWaitForRefusesAnEndedProgram(t *testing.T) {
	tm := startHelper(t, helperCommand(t, "exit"))
	if err := tm.WaitForText(30*time.Second, "never written"); !errors.Is(err, ErrExited) {
		t.Fatalf("WaitForText = %v, want ErrExited", err)
	}
	if err := tm.WaitForText(time.Second, "bye"); err != nil {
		t.Fatalf("text written before the end: %v: %q", err, tm.Output())
	}
}

func TestWaitForTimesOut(t *testing.T) {
	tm := startHelper(t, helperCommand(t, "hang"))
	if err := tm.WaitForText(200*time.Millisecond, "never written"); !errors.Is(err, ErrTimeout) {
		t.Fatalf("WaitForText = %v, want ErrTimeout", err)
	}
	if !tm.Running() {
		t.Fatal("the program ended")
	}
	if err := tm.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.Wait(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if tm.Running() {
		t.Fatal("Running after Kill")
	}
}

func TestWaitKillsOnTimeout(t *testing.T) {
	tm := startHelper(t, helperCommand(t, "hang"))
	if _, err := tm.Wait(300 * time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Wait = %v, want ErrTimeout", err)
	}
	if tm.Running() {
		t.Fatal("the program still runs")
	}
}

func TestCloseEndsTheProgram(t *testing.T) {
	tm := startHelper(t, helperCommand(t, "hang"))
	if err := tm.WaitForText(10*time.Second, "hanging"); err != nil {
		t.Fatalf("%v: %q", err, tm.Output())
	}
	if err := tm.Close(); err != nil {
		t.Fatal(err)
	}
	if tm.Running() {
		t.Fatal("Running after Close")
	}
	if err := tm.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := tm.Write([]byte("x")); err == nil {
		t.Fatal("Write after Close succeeded")
	}
}

func TestStartRefusesInvalidCommands(t *testing.T) {
	valid := Command{Path: "program"}
	for name, change := range map[string]func(*Command){
		"no path":             func(c *Command) { c.Path = "" },
		"NUL in path":         func(c *Command) { c.Path = "pro\x00gram" },
		"NUL in argument":     func(c *Command) { c.Args = []string{"a\x00b"} },
		"NUL in directory":    func(c *Command) { c.Dir = "d\x00ir" },
		"NUL in environment":  func(c *Command) { c.Env = []string{"KEY=va\x00lue"} },
		"entry without '='":   func(c *Command) { c.Env = []string{"KEY"} },
		"zero rows":           func(c *Command) { c.Size = Size{Columns: 80} },
		"negative columns":    func(c *Command) { c.Size = Size{Columns: -1, Rows: 24} },
		"too many columns":    func(c *Command) { c.Size = Size{Columns: 32768, Rows: 24} },
		"too many rows":       func(c *Command) { c.Size = Size{Columns: 80, Rows: 1 << 16} },
		"zero columns":        func(c *Command) { c.Size = Size{Rows: 24} },
		"negative rows":       func(c *Command) { c.Size = Size{Columns: 80, Rows: -24} },
		"NUL in second entry": func(c *Command) { c.Env = []string{"A=1", "B=\x00"} },
	} {
		t.Run(name, func(t *testing.T) {
			c := valid
			change(&c)
			tm, err := Start(c)
			if !errors.Is(err, ErrInvalidCommand) {
				if tm != nil {
					_ = tm.Close()
				}
				t.Fatalf("Start = %v, want ErrInvalidCommand", err)
			}
		})
	}
}

func TestProcessAliveAndKillProcess(t *testing.T) {
	c := helperCommand(t, "hang")
	cmd := exec.Command(c.Path, c.Args...)
	cmd.Env = c.Env
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	pid := cmd.Process.Pid

	alive, err := ProcessAlive(pid)
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil || !alive {
		t.Fatalf("ProcessAlive of a running process = %v, %v", alive, err)
	}
	if err := KillProcess(pid); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if alive, err := ProcessAlive(pid); err != nil || alive {
		t.Fatalf("ProcessAlive of an ended process = %v, %v", alive, err)
	}
	if err := KillProcess(pid); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("KillProcess of an ended process = %v, want os.ErrProcessDone", err)
	}
}

func TestProcessFunctionsRefuseInvalidIDs(t *testing.T) {
	for _, pid := range []int{0, -1, -2} {
		if _, err := ProcessAlive(pid); !errors.Is(err, ErrInvalidProcess) {
			t.Errorf("ProcessAlive(%d) = %v, want ErrInvalidProcess", pid, err)
		}
		if err := KillProcess(pid); !errors.Is(err, ErrInvalidProcess) {
			t.Errorf("KillProcess(%d) = %v, want ErrInvalidProcess", pid, err)
		}
	}
}

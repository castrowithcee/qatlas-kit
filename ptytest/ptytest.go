package ptytest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrUnsupported is returned where the platform offers no pseudo-terminal.
var ErrUnsupported = fmt.Errorf("pseudo-terminals are unsupported on this platform: %w", errors.ErrUnsupported)

// ErrInvalidCommand is returned by Start for a command it refuses to run.
var ErrInvalidCommand = errors.New("invalid command")

// ErrTimeout is returned when a wait ends before its condition was met.
var ErrTimeout = errors.New("timed out")

// ErrExited is returned by WaitFor when the program ended before the condition was met.
var ErrExited = errors.New("the program ended")

// ErrInvalidProcess is returned for a process id that names no single process.
var ErrInvalidProcess = errors.New("invalid process id")

// outputGrace bounds the wait for the end of the output after the program ended, since a process the
// program started may still hold the terminal.
const outputGrace = 5 * time.Second

// maxDimension is the largest size a Windows pseudo console takes in either direction.
const maxDimension = 32767

// Size is the size of a terminal in character cells.
type Size struct {
	Columns, Rows int
}

// Command is a program to run at a terminal.
type Command struct {
	// Path is the program file. It is not looked up in PATH.
	Path string
	// Args are the arguments after the program name.
	Args []string
	// Env is the complete environment of the program; nil means an empty one. Windows adds SYSTEMROOT
	// when it is missing, since the system does not work without it.
	Env []string
	// Dir is the working directory; empty means the current one.
	Dir string
	// Size is the terminal size; zero means 80 columns and 24 rows.
	Size Size
}

func (c Command) check() (Size, error) {
	if c.Path == "" {
		return Size{}, fmt.Errorf("%w: no program path", ErrInvalidCommand)
	}
	values := append([]string{c.Path, c.Dir}, c.Args...)
	for _, v := range append(values, c.Env...) {
		if strings.IndexByte(v, 0) >= 0 {
			return Size{}, fmt.Errorf("%w: NUL byte in %q", ErrInvalidCommand, v)
		}
	}
	for _, kv := range c.Env {
		if !strings.Contains(kv, "=") {
			return Size{}, fmt.Errorf("%w: environment entry %q has no '='", ErrInvalidCommand, kv)
		}
	}
	size := c.Size
	if size == (Size{}) {
		size = Size{Columns: 80, Rows: 24}
	}
	if size.Columns < 1 || size.Rows < 1 || size.Columns > maxDimension || size.Rows > maxDimension {
		return Size{}, fmt.Errorf("%w: terminal size %dx%d is outside 1..%d", ErrInvalidCommand,
			size.Columns, size.Rows, maxDimension)
	}
	return size, nil
}

// console is the platform side of a terminal: the input the program reads and the output it writes.
type console interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	// echo reports whether the terminal echoes input.
	echo() (bool, error)
	// hangUp ends the output once everything the program wrote was read.
	hangUp()
	close() error
}

// Terminal is a program running at a pseudo-terminal of its own.
type Terminal struct {
	console console
	process *os.Process

	exited  chan struct{}
	state   *os.ProcessState
	waitErr error

	outputEnd chan struct{}
	mu        sync.Mutex
	output    bytes.Buffer

	hangUpOnce sync.Once
	closeOnce  sync.Once
	closeErr   error
}

// Start runs the command with a new pseudo-terminal as its terminal and standard streams.
func Start(c Command) (*Terminal, error) {
	size, err := c.check()
	if err != nil {
		return nil, err
	}
	con, process, err := start(c, size)
	if err != nil {
		return nil, err
	}
	t := &Terminal{console: con, process: process, exited: make(chan struct{}), outputEnd: make(chan struct{})}
	go t.read()
	go func() {
		t.state, t.waitErr = process.Wait()
		close(t.exited)
	}()
	return t, nil
}

func (t *Terminal) read() {
	defer close(t.outputEnd)
	buf := make([]byte, 4096)
	for {
		n, err := t.console.Read(buf)
		t.mu.Lock()
		t.output.Write(buf[:n])
		t.mu.Unlock()
		if err != nil {
			return
		}
	}
}

// Pid returns the process id of the program.
func (t *Terminal) Pid() int { return t.process.Pid }

// Output returns everything the program wrote to the terminal so far, with its escape sequences.
func (t *Terminal) Output() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.output.String()
}

// Write types p at the terminal. A terminal sends "\r" for the Enter key.
func (t *Terminal) Write(p []byte) (int, error) { return t.console.Write(p) }

// Echo reports whether the terminal echoes input. A program switches echo off before it reads a secret.
// Windows cannot report it and returns ErrUnsupported.
func (t *Terminal) Echo() (bool, error) { return t.console.echo() }

// Running reports whether the program still runs.
func (t *Terminal) Running() bool {
	select {
	case <-t.exited:
		return false
	default:
		return true
	}
}

// WaitFor waits until ready reports true for the output so far. It returns ErrExited when the program
// ended first and ErrTimeout when timeout passed first. ready may consult other state, such as Echo.
func (t *Terminal) WaitFor(timeout time.Duration, ready func(output string) bool) error {
	deadline := time.Now().Add(timeout)
	for {
		if ready(t.Output()) {
			return nil
		}
		if !t.Running() {
			t.finishOutput()
			if ready(t.Output()) {
				return nil
			}
			return ErrExited
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w after %v", ErrTimeout, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// WaitForText waits until the output contains text; see WaitFor.
func (t *Terminal) WaitForText(timeout time.Duration, text string) error {
	return t.WaitFor(timeout, func(output string) bool { return strings.Contains(output, text) })
}

// Wait waits for the program to end and returns its exit code, then waits a bounded time for the rest
// of its output. When timeout passes first, it kills the program and returns ErrTimeout.
func (t *Terminal) Wait(timeout time.Duration) (int, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var timedOut bool
	select {
	case <-t.exited:
	case <-timer.C:
		timedOut = true
		_ = t.process.Kill()
		<-t.exited
	}
	t.finishOutput()
	if t.waitErr != nil {
		return -1, t.waitErr
	}
	if timedOut {
		return t.state.ExitCode(), fmt.Errorf("%w after %v", ErrTimeout, timeout)
	}
	return t.state.ExitCode(), nil
}

// Kill ends the program at once. Processes it started are not affected.
func (t *Terminal) Kill() error { return t.process.Kill() }

// Close ends the program if it still runs and releases the terminal. It is safe to call more than once.
func (t *Terminal) Close() error {
	t.closeOnce.Do(func() {
		if t.Running() {
			_ = t.process.Kill()
		}
		<-t.exited
		t.finishOutput()
		t.closeErr = t.console.close()
	})
	return t.closeErr
}

// finishOutput hangs up the terminal of an ended program and waits a bounded time for the output to end.
func (t *Terminal) finishOutput() {
	t.hangUpOnce.Do(t.console.hangUp)
	timer := time.NewTimer(outputGrace)
	defer timer.Stop()
	select {
	case <-t.outputEnd:
	case <-timer.C:
	}
}

// ProcessAlive reports whether the process pid exists, such as one the program started in the
// background. On Linux and macOS, a child of the caller that ended but was not waited for still exists.
func ProcessAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("%w: %d", ErrInvalidProcess, pid)
	}
	return processAlive(pid)
}

// KillProcess ends the process pid at once. It returns os.ErrProcessDone when no such process exists.
func KillProcess(pid int) error {
	// On Unix, 0 and negative ids name process groups or every process the caller may signal.
	if pid <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidProcess, pid)
	}
	return killProcess(pid)
}

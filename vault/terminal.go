package vault

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"golang.org/x/term"
)

// ErrNoTerminal reports that no terminal is attached to ask a person for the passphrase.
var ErrNoTerminal = errors.New("no terminal is attached to ask for the vault passphrase")

// ReadPassphrase shows prompt on the controlling terminal and reads a line back without echo. It opens the
// terminal itself, so standard input may carry other data. A process none of whose standard streams is a
// terminal, such as an agent or a CI job, gets ErrNoTerminal and is never asked.
func ReadPassphrase(prompt string) (string, error) {
	in, out, closeTTY, err := openTTY()
	if err != nil {
		return "", ErrNoTerminal
	}
	defer closeTTY()
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", ErrNoTerminal
	}
	passphrase, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", ErrNoTerminal
	}
	return string(passphrase), nil
}

// interactive is a variable only so a test can decide it.
var interactive = func() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if term.IsTerminal(int(f.Fd())) {
			return true
		}
	}
	return false
}

// Interactive reports whether a person can be at this process's terminal, the rule ReadPassphrase applies
// before it opens one: at least one standard stream is a terminal.
func Interactive() bool { return interactive() }

// openTTY opens the controlling terminal apart from the process's own streams: the console on Windows,
// /dev/tty elsewhere, and standard input as a last resort if it is a terminal.
func openTTY() (in, out *os.File, closeAll func(), err error) {
	if !interactive() {
		return nil, nil, nil, errors.New("no standard stream is a terminal")
	}
	if runtime.GOOS == "windows" {
		in, err = os.OpenFile("CONIN$", os.O_RDWR, 0)
		if err != nil {
			return nil, nil, nil, err
		}
		out, err = os.OpenFile("CONOUT$", os.O_WRONLY, 0)
		if err != nil {
			_ = in.Close()
			return nil, nil, nil, err
		}
		return in, out, func() { _ = in.Close(); _ = out.Close() }, nil
	}
	if tty, ttyErr := os.OpenFile("/dev/tty", os.O_RDWR, 0); ttyErr == nil {
		return tty, tty, func() { _ = tty.Close() }, nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return os.Stdin, os.Stdin, func() {}, nil
	}
	return nil, nil, nil, errors.New("no terminal is attached")
}

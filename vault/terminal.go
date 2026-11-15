package vault

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unicode"

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

// confirmLimit is the most bytes of an answer ReadConfirm reads; a longer line counts as no.
const confirmLimit = 64

// ReadConfirm shows prompt on the controlling terminal and reads one line back. Only "y" or "yes", in any
// case and with surrounding space, answers yes; an empty line, a line longer than confirmLimit bytes and
// anything else answer no. A line that ends at end of input without a line break still counts. Like
// ReadPassphrase it opens the terminal itself and returns ErrNoTerminal, without asking, when none is
// attached or the input ends before any data. The prompt names the default itself, for example "[y/N]".
// Control characters in prompt other than a line break are shown as Go escapes, because prompts carry
// names and paths from configuration that someone else may have edited.
func ReadConfirm(prompt string) (bool, error) {
	in, out, closeTTY, err := openTTY()
	if err != nil {
		return false, ErrNoTerminal
	}
	defer closeTTY()
	return readConfirm(in, out, prompt)
}

// readConfirm is ReadConfirm over the given streams.
func readConfirm(in io.Reader, out io.Writer, prompt string) (bool, error) {
	if _, err := fmt.Fprint(out, escapePrompt(prompt)); err != nil {
		return false, ErrNoTerminal
	}
	r := bufio.NewReader(in)
	var line []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && len(line) > 0 {
				break
			}
			return false, ErrNoTerminal
		}
		if b == '\n' {
			break
		}
		if len(line) == confirmLimit {
			// Drop the rest of the line so it does not answer the next prompt.
			for b != '\n' && err == nil {
				b, err = r.ReadByte()
			}
			return false, nil
		}
		line = append(line, b)
	}
	answer := strings.TrimSpace(string(line))
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// escapePrompt shows every control or unprintable character of prompt but a line break as a Go escape.
func escapePrompt(prompt string) string {
	var b strings.Builder
	for _, r := range prompt {
		if r != '\n' && (unicode.IsControl(r) || !unicode.IsPrint(r)) {
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
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

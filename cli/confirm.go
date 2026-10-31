package cli

import (
	"bufio"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/castrowithcee/qatlas-kit/output"
)

// Confirmation describes the two distinct gates of a changing command.
type Confirmation struct {
	Agent           bool
	Yes             bool
	Confirm         bool
	RequiresConfirm bool
	Question        string
}

// ConfirmAction checks all confirmation gates before invoking action.
func ConfirmAction(options Confirmation, action func() error) error {
	if err := ConfirmRequest(options); err != nil {
		return err
	}
	return action()
}

// ConfirmRequest never reads stdin. It opens a terminal only when a question is needed.
func ConfirmRequest(options Confirmation) error {
	return confirmWith(options, Interactive, openTerminal)
}

type terminalPair struct {
	in    io.Reader
	out   io.Writer
	close func()
}

// Interactive reports whether at least one standard stream is a terminal.
func Interactive() bool {
	for _, stream := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if term.IsTerminal(int(stream.Fd())) {
			return true
		}
	}
	return false
}

func confirmWith(options Confirmation, interactive func() bool, open func() (terminalPair, error)) error {
	if options.RequiresConfirm && !options.Confirm {
		return &Failure{
			Code: output.CodeConfirmationRequired, Usage: true,
			Message:  "this request needs --confirm",
			NextStep: "Review the effects, then repeat this request with --confirm.",
		}
	}
	if options.Question == "" || options.Yes {
		return nil
	}
	if options.Agent || !interactive() {
		return &Failure{
			Code: output.CodeConfirmationRequired, Usage: true,
			Message:  "this request needs an explicit answer",
			NextStep: "Review the effects, then repeat this request with --yes.",
		}
	}
	tty, err := open()
	if err != nil {
		return &Failure{
			Code: output.CodeConfirmationRequired, Usage: true,
			Message:  "no terminal is available for confirmation",
			NextStep: "Repeat this request with --yes after reviewing its effects.",
			Cause:    err,
		}
	}
	defer tty.close()
	if _, err := io.WriteString(tty.out, options.Question+" [y/N] "); err != nil {
		return declined()
	}
	line, err := bufio.NewReader(io.LimitReader(tty.in, 64)).ReadString('\n')
	if err != nil {
		return declined()
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "y" && answer != "yes" {
		return declined()
	}
	return nil
}

func declined() error {
	return &Failure{
		Code: output.CodeConfirmationDeclined, Usage: true,
		Message:  "confirmation was declined",
		NextStep: "No change was made; repeat the request if you want to continue.",
	}
}

func openTerminal() (terminalPair, error) {
	if runtime.GOOS == "windows" {
		in, err := os.Open("CONIN$")
		if err != nil {
			return terminalPair{}, err
		}
		out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
		if err != nil {
			_ = in.Close()
			return terminalPair{}, err
		}
		return terminalPair{in: in, out: out, close: func() { _ = in.Close(); _ = out.Close() }}, nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return terminalPair{}, err
	}
	return terminalPair{in: tty, out: tty, close: func() { _ = tty.Close() }}, nil
}

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-kit/output"
)

const (
	ExitSuccess = 0
	ExitRuntime = 1
	ExitUsage   = 2
)

// Failure is a diagnostic with a stable code and an actionable next step.
type Failure struct {
	Code     output.Code
	Message  string
	NextStep string
	Detail   any
	Usage    bool
	Syntax   bool
	Cause    error
}

func (e *Failure) Error() string { return e.Message }
func (e *Failure) Unwrap() error { return e.Cause }

// NewFailure checks the public code before an application can emit it.
func NewFailure(code output.Code, message, nextStep string, usage bool) (*Failure, error) {
	if !output.ValidCode(code) {
		return nil, fmt.Errorf("invalid error code %q", code)
	}
	if message == "" || nextStep == "" {
		return nil, fmt.Errorf("error message and next step are required")
	}
	return &Failure{Code: code, Message: message, NextStep: nextStep, Usage: usage}, nil
}

// ExitCode maps a diagnostic to the public process exit contract.
func ExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	var failure *Failure
	if errors.As(err, &failure) && failure.Usage {
		return ExitUsage
	}
	return ExitRuntime
}

// WriteError writes the stable diagnostic to stderr. Only syntax errors carry usage text.
func WriteError(w io.Writer, program string, err error, usage string) error {
	if err == nil {
		return nil
	}
	var failure *Failure
	if !errors.As(err, &failure) {
		failure = &Failure{
			Code: output.CodeRuntime, Message: err.Error(),
			NextStep: "Check the cause and try again.",
		}
	}
	if program == "" {
		return fmt.Errorf("program name is required")
	}
	if !output.ValidCode(failure.Code) {
		return fmt.Errorf("invalid error code %q", failure.Code)
	}
	var detail []byte
	if failure.Detail != nil {
		var marshalErr error
		detail, marshalErr = json.Marshal(failure.Detail)
		if marshalErr != nil {
			return marshalErr
		}
	}
	if _, writeErr := fmt.Fprintf(w, "%s: %s: %s\n%s\n",
		escapeControl(program), failure.Code, escapeControl(failure.Message), escapeControl(failure.NextStep)); writeErr != nil {
		return writeErr
	}
	if detail != nil {
		if _, err := w.Write(append(detail, '\n')); err != nil {
			return err
		}
	}
	if failure.Syntax && usage != "" {
		_, err = io.WriteString(w, escapeUsage(usage))
		return err
	}
	return nil
}

// escapeControl makes untrusted text visible without letting it move the cursor or add diagnostic lines.
func escapeControl(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escapeUsage(usage string) string {
	lines := strings.Split(usage, "\n")
	for i := range lines {
		lines[i] = escapeControl(lines[i])
	}
	return strings.Join(lines, "\n")
}

const (
	AgentFlagHelp   = "agent mode: compact output by default, no progress, and no questions; use --yes for actions that ask"
	YesFlagHelp     = "answer this command's ordinary confirmation question with yes"
	ConfirmFlagHelp = "confirm this one data-changing request; required separately from --yes"
)

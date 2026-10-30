package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const (
	ExitSuccess = 0
	ExitRuntime = 1
	ExitUsage   = 2
)

const (
	CodeUsage                = "usage"
	CodeRuntime              = "runtime"
	CodeConfirmationRequired = "confirmation-required"
	CodeConfirmationDeclined = "confirmation-declined"
)

var codePattern = regexp.MustCompile("^[a-z]+(?:-[a-z]+)*$")

// Failure is a diagnostic with a stable code and an actionable next step.
type Failure struct {
	Code     string
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
func NewFailure(code, message, nextStep string, usage bool) (*Failure, error) {
	if !codePattern.MatchString(code) {
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
			Code: CodeRuntime, Message: err.Error(),
			NextStep: "Check the cause and try again.",
		}
	}
	if program == "" {
		return fmt.Errorf("program name is required")
	}
	if _, writeErr := fmt.Fprintf(w, "%s: %s: %s\n%s\n", program, failure.Code, failure.Message, failure.NextStep); writeErr != nil {
		return writeErr
	}
	if failure.Detail != nil {
		if err := json.NewEncoder(w).Encode(failure.Detail); err != nil {
			return err
		}
	}
	if failure.Syntax && usage != "" {
		_, err = io.WriteString(w, usage)
		return err
	}
	return nil
}

const (
	AgentFlagHelp   = "agent mode: compact output by default, no progress, and no questions; use --yes for actions that ask"
	YesFlagHelp     = "answer this command's ordinary confirmation question with yes"
	ConfirmFlagHelp = "confirm this one data-changing request; required separately from --yes"
)

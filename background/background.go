// Package background starts a program as a background process that is detached from the terminal and
// session of the starter, hands it optional bytes through an inherited pipe, and waits for the report the
// process gives once it runs.
//
// The caller supplies the program, its arguments and its environment; the arguments typically select a
// hidden subcommand of the same binary. The started process uses Inherit to read the handover and to
// report. Nothing is put on the command line or in the environment on the package's behalf, so a handover
// never appears there.
//
// The process gets the null device as its standard streams and a fixed working directory (the root
// directory on Unix, the system directory on Windows). On Linux and macOS it also leads a session of its
// own. On Windows it has no console and its own process group; its standard input and output are the
// handover and report pipes, and it leaves the job object of the starter where the job allows that.
//
// Limits: detaching from the session does not protect the process from a service manager that ends it
// with the user's login session; see LogoutBehavior. A Windows job that forbids breakaway still ends the
// process with the job. Neither side protects against a process of the same user or an administrator.
package background

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The words a started process reports with. Any other report is the reason it did not start.
const (
	ReportReady   = "ready"
	ReportRunning = "running"
)

// DefaultTimeout bounds how long Start waits for the report if Options.Timeout is zero.
const DefaultTimeout = 10 * time.Second

const maxReport = 4096

// ErrUnsupported means the platform cannot run detached background processes.
var ErrUnsupported = fmt.Errorf("background processes are unsupported on this platform: %w", errors.ErrUnsupported)

// Command describes the process to start. The zero value starts the running program without arguments.
type Command struct {
	// Program is the file to run. Empty means the running program (os.Executable).
	Program string
	// Args are the arguments after the program name.
	Args []string
	// Env is the environment as key=value pairs. Nil means the environment of the starter.
	Env []string
}

// Options tune Start. The zero value is valid.
type Options struct {
	// Timeout bounds the wait for the report; zero means DefaultTimeout. The context can end it sooner.
	Timeout time.Duration
}

// Result describes a started process.
type Result struct {
	// Report is ReportReady, or ReportRunning if another instance already runs and the process ended.
	Report string
	// PID is the process id of the started process.
	PID int
}

// ReportError means the started process reported a reason instead of ReportReady or ReportRunning.
type ReportError struct{ Reason string }

func (e *ReportError) Error() string { return "the background process did not start: " + e.Reason }

// outcome turns the report line read from the process into an error unless it is a success word. ctxErr
// is why the wait ended early, if it did.
func outcome(report string, ctxErr error) error {
	report = strings.TrimSpace(report)
	switch {
	case report == ReportReady, report == ReportRunning:
		return nil
	case report != "":
		return &ReportError{Reason: report}
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return errors.New("the background process did not report in time")
	case ctxErr != nil:
		return fmt.Errorf("the background process did not report: %w", ctxErr)
	default:
		return errors.New("the background process ended before it reported")
	}
}

// cleanReason makes reason fit a single report line and bounded in size.
func cleanReason(reason string) string {
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" {
		return "failed"
	}
	if len(reason) > 512 {
		reason = strings.ToValidUTF8(reason[:512], "")
	}
	return reason
}

func trimReport(report string) string { return strings.TrimSpace(report) }

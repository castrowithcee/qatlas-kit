package vaultproc

import (
	"errors"
	"fmt"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

var (
	// ErrNotRunning means no vault process answers: nothing listens, or the process is locking itself. It
	// is a state, not a failure: the vault is not unlocked in a process.
	ErrNotRunning = errors.New("no vault process is running")

	// ErrRunning means another vault process already serves the channel.
	ErrRunning = localipc.ErrRunning

	// ErrNotStarted means Run was called in a process that Start did not start, for example by hand.
	ErrNotStarted = errors.New("the vault process was not started with a handover")

	// ErrVersion means the other side speaks another protocol version, a process of another build.
	ErrVersion = errors.New("the vault process speaks another protocol version")

	// ErrUnproven means the process at the channel could not prove that it holds the vault's key. It wraps
	// localipc.ErrRefused.
	ErrUnproven = fmt.Errorf("%w: the process cannot prove that it holds the vault key", localipc.ErrRefused)

	// ErrProgramRefused means the vault process refused this program, as it refuses every program once its
	// own was replaced. It wraps localipc.ErrRefused.
	ErrProgramRefused = fmt.Errorf("%w: the vault process refused this program", localipc.ErrRefused)

	// ErrReplaced means the vault process hands, or handed, the vault over to a successor; a new connection
	// reaches it in a moment. Client calls ask again for a short while before they return it.
	ErrReplaced = errors.New("the vault process was replaced by its successor")

	// ErrHandover means a handover did not happen; the vault process locked itself. It wraps the reason.
	ErrHandover = errors.New("the vault process did not hand the vault over and locked itself")

	// ErrUnknownOperation means the vault process does not know the operation.
	ErrUnknownOperation = errors.New("the vault process does not know the operation")

	// ErrBadRequest means the vault process could not read the request.
	ErrBadRequest = errors.New("the vault process could not read the request")

	// ErrFailed means the vault process could not carry out the request; the reason stays in the process.
	ErrFailed = errors.New("the vault process could not carry out the request")

	// ErrTooLarge means a message exceeds MaxMessage.
	ErrTooLarge = fmt.Errorf("a vault process message exceeds %d bytes", MaxMessage)

	// ErrNoRecipient means a client has no recipient to challenge the process with: the vault is not
	// encrypted, so it has no process.
	ErrNoRecipient = errors.New("the vault has no recipient to check the vault process with")
)

// Error is an application's error code in an answer. A handler returns it to answer with Code; a client
// receives it. Code must match [a-z][a-z0-9-]* and must not be a code of the kit, or the answer carries the
// code of ErrFailed instead. Code is a fixed word: it never carries anything the request sent.
type Error struct{ Code string }

func (e *Error) Error() string { return "the vault process answered " + e.Code }

// answerError turns an answer's code into the error a client returns.
func answerError(code string) error {
	switch code {
	case codeRefused:
		return ErrProgramRefused
	case codeBadRequest:
		return ErrBadRequest
	case codeTooLarge:
		return ErrTooLarge
	case codeVersion:
		return ErrVersion
	case codeLocked:
		return ErrNotRunning
	case codeChallenge:
		return ErrUnproven
	case codeReplaced:
		return ErrReplaced
	case codeUnknownOp:
		return ErrUnknownOperation
	case codeFailed:
		return ErrFailed
	case codeUnverified:
		return fmt.Errorf("%w: it could not verify the release", ErrHandover)
	case codeRollback:
		return fmt.Errorf("%w: a rollback always locks", ErrHandover)
	case codeUnchanged:
		return fmt.Errorf("%w: its program was not replaced", ErrHandover)
	case codeMismatch:
		return fmt.Errorf("%w: the installed program is not the one the release holds", ErrHandover)
	case codeHandoverFailed:
		return fmt.Errorf("%w: no successor took the vault over", ErrHandover)
	case codeNoHandover:
		return fmt.Errorf("%w: this platform or process does not hand over", ErrHandover)
	}
	if wordPattern.MatchString(code) {
		return &Error{Code: code}
	}
	return ErrFailed
}

package vaultproc

// LockReason says why a vault process locked.
type LockReason string

// The reasons a process locks. The first one that happens is the reason.
const (
	// LockIdle: Config.IdleTimeout passed without a touch.
	LockIdle LockReason = "idle"
	// LockMaxUnlock: the maximum unlock time ended.
	LockMaxUnlock LockReason = "max-unlock"
	// LockCommand: a client sent OpLock.
	LockCommand LockReason = "lock-command"
	// LockClosed: Server.Close, or a stop signal or the end of the context of Run.
	LockClosed LockReason = "closed"
	// LockSocketGone: the process's socket is gone or replaced.
	LockSocketGone LockReason = "socket-gone"
	// LockProgramReplaced: the process's program was replaced and no handover was under way.
	LockProgramReplaced LockReason = "program-replaced"
	// LockHandedOver: the vault was handed over to a successor.
	LockHandedOver LockReason = "handed-over"
	// LockHandoverFailed: a handover failed; LockEvent.Handover tells why.
	LockHandoverFailed LockReason = "handover-failed"
	// LockHandoverDeclined: the user's setting asked an update to lock the vault.
	LockHandoverDeclined LockReason = "handover-declined"
	// LockListenerFailed: the listener failed.
	LockListenerFailed LockReason = "listener-failed"
)

// LockEvent is what OnLock is told.
type LockEvent struct {
	Reason LockReason
	// Handover is the reason of the failed handover when Reason is LockHandoverFailed, and empty otherwise.
	Handover HandoverReason
}

// HandoverReason says why a handover did not happen. Each reason is a fixed word; none quotes a request,
// a file or an error.
type HandoverReason string

// The reasons a handover fails. A handover that fails always locks the process.
const (
	// HandoverBadRequest: the request or its commit could not be read.
	HandoverBadRequest HandoverReason = "bad-request"
	// HandoverUnsupported: the process has no HandoverConfig, or the platform does not hand over.
	HandoverUnsupported HandoverReason = "unsupported"
	// HandoverRollback: the update is a rollback.
	HandoverRollback HandoverReason = "rollback"
	// HandoverNotVerified: the release did not verify.
	HandoverNotVerified HandoverReason = "not-verified"
	// HandoverTooLarge: a release file or the installed program is over its limit; for the archive and the
	// program that is HandoverConfig.MaxFileSize.
	HandoverTooLarge HandoverReason = "too-large"
	// HandoverDeclined: the user's setting asks an update to lock the vault.
	HandoverDeclined HandoverReason = "declined"
	// HandoverProgramUnchanged: the program was not replaced at the commit.
	HandoverProgramUnchanged HandoverReason = "program-unchanged"
	// HandoverProgramMismatch: the installed program is not the one of the release.
	HandoverProgramMismatch HandoverReason = "program-mismatch"
	// HandoverNoSuccessor: no successor took the vault over, or the connection ended first.
	HandoverNoSuccessor HandoverReason = "no-successor"
	// HandoverLocked: the process was locking when the handover reached its commit.
	HandoverLocked HandoverReason = "locked"
)

// HandoverResult is what OnHandover is told.
type HandoverResult struct {
	// HandedOver is true when a successor took the vault over.
	HandedOver bool
	// Reason is empty after a handover and otherwise says why none happened.
	Reason HandoverReason
}

// handoverReason is the reason of an answer code of a failed handover.
func handoverReason(code string) HandoverReason {
	switch code {
	case codeBadRequest:
		return HandoverBadRequest
	case codeNoHandover:
		return HandoverUnsupported
	case codeRollback:
		return HandoverRollback
	case codeUnverified:
		return HandoverNotVerified
	case codeUnchanged:
		return HandoverProgramUnchanged
	case codeMismatch:
		return HandoverProgramMismatch
	case codeLocked:
		return HandoverLocked
	}
	return HandoverNoSuccessor
}

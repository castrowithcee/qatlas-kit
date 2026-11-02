package vaultproc

import (
	"context"
	"errors"
	"fmt"

	"github.com/castrowithcee/qatlas-kit/selfupdate"
)

// Outcome is what an update did to the vault process.
type Outcome int

const (
	// OutcomeNone means no vault process was running.
	OutcomeNone Outcome = iota
	// OutcomeHandedOver means the successor holds the vault; ReplacementResult.PID names it.
	OutcomeHandedOver
	// OutcomeLocked means the vault process locked itself. ReplacementResult.Err says why when the
	// handover was refused or failed; it is nil when the user's setting, a rollback or the platform asked
	// for the lock.
	OutcomeLocked
	// OutcomeUnreached means a process answered but could not be asked to lock; ReplacementResult.Err says
	// why, with its process id where known. It locks itself once it finds its program replaced.
	OutcomeUnreached
)

// ReplacementResult is the outcome of one Replacement.
type ReplacementResult struct {
	Outcome Outcome
	PID     int
	Err     error
}

// Replacement carries a vault process through one selfupdate installation. Its BeforeReplace and
// AfterReplace are the hooks of selfupdate.Client: before the program is replaced, the process verifies
// the release and prepares its successor, or locks; once the program is replaced, it hands the vault over.
// A rollback and a platform without a handover always lock. Finish ends a handover the installation did not
// get to commit. A Replacement serves one installation.
type Replacement struct {
	client   *Client
	handover *Handover
	result   ReplacementResult
}

// NewReplacement returns a Replacement for the vault process client reaches.
func NewReplacement(client *Client) *Replacement { return &Replacement{client: client} }

// BeforeReplace prepares the handover or locks the vault process. It never stops the installation: a
// process that is left running locks itself once its program is replaced.
func (r *Replacement) BeforeReplace(ctx context.Context, files selfupdate.ReleaseFiles) error {
	r.result, r.handover = ReplacementResult{}, nil
	if files.Rollback || !handoverSupported {
		r.lock(ctx, nil)
		return nil
	}
	handover, err := r.client.PrepareHandover(ctx, files)
	switch {
	case errors.Is(err, ErrNotRunning):
	case errors.Is(err, ErrHandover):
		r.result = ReplacementResult{Outcome: OutcomeLocked, Err: err}
	case err != nil:
		r.lock(ctx, err)
	case handover == nil:
		r.result = ReplacementResult{Outcome: OutcomeLocked}
	default:
		r.handover = handover
	}
	return nil
}

// AfterReplace commits a prepared handover.
func (r *Replacement) AfterReplace(ctx context.Context, _ selfupdate.ReleaseFiles) {
	handover := r.handover
	if handover == nil {
		return
	}
	r.handover = nil
	pid, err := handover.Commit(ctx)
	if err != nil {
		r.result = ReplacementResult{Outcome: OutcomeLocked, Err: err}
		return
	}
	r.result = ReplacementResult{Outcome: OutcomeHandedOver, PID: pid}
}

// Finish ends a handover that was prepared but not committed, which locks the process, and returns the
// outcome. Call it once the installation returned, whatever it returned.
func (r *Replacement) Finish() ReplacementResult {
	if r.handover != nil {
		_ = r.handover.Close()
		r.handover = nil
		r.result = ReplacementResult{Outcome: OutcomeLocked,
			Err: fmt.Errorf("%w: the update did not replace the program", ErrHandover)}
	}
	return r.result
}

// lock locks the process; cause is why it was not handed over, if that failed.
func (r *Replacement) lock(ctx context.Context, cause error) {
	err := r.client.Lock(ctx)
	switch {
	case errors.Is(err, ErrNotRunning) && cause == nil:
		r.result = ReplacementResult{}
	case err != nil && !errors.Is(err, ErrNotRunning):
		r.result = ReplacementResult{Outcome: OutcomeUnreached, Err: err}
	default:
		r.result = ReplacementResult{Outcome: OutcomeLocked, Err: cause}
	}
}

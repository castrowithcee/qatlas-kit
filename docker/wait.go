package docker

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

// Bounds of WaitHealthy. A zero WaitOptions.Deadline or Interval selects the default; a shorter Interval
// is raised to MinWaitInterval.
const (
	DefaultWaitDeadline = 60 * time.Second
	DefaultWaitInterval = time.Second
	MinWaitInterval     = 100 * time.Millisecond
)

// WaitReason says why a container was not healthy when WaitHealthy ended.
type WaitReason string

// The reasons of a WaitResult that is not OK.
const (
	ReasonNotFound   WaitReason = "not-found"
	ReasonExited     WaitReason = "exited"      // exited or dead; not waited for any longer
	ReasonRestarting WaitReason = "restarting"  // still restarting at the deadline
	ReasonNotRunning WaitReason = "not-running" // created or paused at the deadline
	ReasonUnhealthy  WaitReason = "unhealthy"   // health check failing at the deadline
	ReasonStarting   WaitReason = "starting"    // health check not yet passed at the deadline
)

// WaitOptions configures WaitHealthy.
type WaitOptions struct {
	// Deadline bounds the whole wait. Zero means DefaultWaitDeadline; negative is output.CodeInvalidRequest.
	Deadline time.Duration
	// Interval is the pause between two polls. Zero means DefaultWaitInterval.
	Interval time.Duration
}

// WaitResult is the outcome for one container, in the order of the references passed to WaitHealthy.
type WaitResult struct {
	Ref string
	// Name is the container's name; empty if it was not found. It is the Engine's text and not trustworthy.
	Name string
	OK   bool
	// Reason is empty if OK.
	Reason WaitReason
}

// WaitHealthy polls the containers until each one runs and, if it has a health check, is healthy, or until
// the deadline. A container that is exited or dead, or not found, is final at once and not polled again;
// a failing health check is watched until the deadline because it can recover. The deadline is
// checked between polls, so a poll in flight finishes first (bounded by Options.Timeout), and one last poll
// runs at the deadline. A container that failed is reported in the results; the error is only for an
// invalid call, a failing Engine and a cancelled context. refs must be non-empty, valid, distinct as
// written and at most MaxContainers.
func (c *Client) WaitHealthy(ctx context.Context, refs []string, opts WaitOptions) ([]WaitResult, error) {
	if len(refs) == 0 || len(refs) > MaxContainers || opts.Deadline < 0 || opts.Interval < 0 {
		return nil, &Error{Code: output.CodeInvalidRequest, Message: "the wait request is not valid"}
	}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if _, err := checkRef(ref); err != nil {
			return nil, err
		}
		key := strings.TrimPrefix(ref, "/")
		if seen[key] {
			return nil, &Error{Code: output.CodeInvalidRequest, Message: "a container reference is listed twice"}
		}
		seen[key] = true
	}
	deadline, interval := opts.Deadline, opts.Interval
	if deadline == 0 {
		deadline = DefaultWaitDeadline
	}
	if interval == 0 {
		interval = DefaultWaitInterval
	}
	interval = max(interval, c.minWaitInterval)
	end := time.Now().Add(deadline)

	results := make([]WaitResult, len(refs))
	done := make([]bool, len(refs))
	for i, ref := range refs {
		results[i] = WaitResult{Ref: ref, Reason: ReasonNotRunning}
	}
	for {
		pending := 0
		for i, ref := range refs {
			if done[i] {
				continue
			}
			d, err := c.Inspect(ctx, ref)
			var failure *Error
			if errors.As(err, &failure) && failure.Code == output.CodeNotFound {
				results[i].Reason, done[i] = ReasonNotFound, true
				continue
			}
			if err != nil {
				return nil, err
			}
			r := &results[i]
			r.Name = d.Name
			r.Reason = judge(d.State, d.Health)
			r.OK = r.Reason == ""
			done[i] = r.OK || r.Reason == ReasonExited
			if !done[i] {
				pending++
			}
		}
		remaining := time.Until(end)
		if pending == 0 || remaining <= 0 {
			return results, nil
		}
		timer := time.NewTimer(min(interval, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, contextError(ctx.Err())
		case <-timer.C:
		}
	}
}

// judge maps a container's state and health to success (empty reason) or the reason it is not ready.
func judge(state, health string) WaitReason {
	switch state {
	case "running":
		switch health {
		case "", "healthy":
			return ""
		case "unhealthy":
			return ReasonUnhealthy
		}
		return ReasonStarting
	case "exited", "dead":
		return ReasonExited
	case "restarting":
		return ReasonRestarting
	}
	return ReasonNotRunning
}

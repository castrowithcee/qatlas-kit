package docker

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

const (
	// MaxGrace bounds the grace period of Stop and Restart.
	MaxGrace = time.Hour
	// engineDefaultGrace is what the Engine waits for a container that has no stop timeout of its own; it
	// is only used to size the request deadline when the caller passes no grace.
	engineDefaultGrace = 10 * time.Second
)

// Start starts a stopped container. A container that already runs is not an error.
func (c *Client) Start(ctx context.Context, ref string) error {
	return c.act(ctx, ref, "start", 0, false)
}

// Stop stops a container: the Engine signals it and kills it after grace. Zero grace selects the Engine's
// default, a negative or larger than MaxGrace one is output.CodeInvalidRequest, and a fraction of a second
// is rounded up. A container that is already stopped is not an error.
func (c *Client) Stop(ctx context.Context, ref string, grace time.Duration) error {
	return c.act(ctx, ref, "stop", grace, true)
}

// Restart stops a container as Stop does and starts it again.
func (c *Client) Restart(ctx context.Context, ref string, grace time.Duration) error {
	return c.act(ctx, ref, "restart", grace, true)
}

// act posts an action. The request may run for the grace period, so it is bounded by Options.Timeout plus
// the grace instead of by the Client's overall timeout.
func (c *Client) act(ctx context.Context, ref, verb string, grace time.Duration, graceAllowed bool) error {
	segment, err := checkRef(ref)
	if err != nil {
		return err
	}
	if grace < 0 || grace > MaxGrace {
		return &Error{Code: output.CodeInvalidRequest, Message: "the grace period is not valid"}
	}
	version, err := c.apiVersion(ctx)
	if err != nil {
		return err
	}
	target := "http://" + host + "/v" + version + "/containers/" + segment + "/" + verb
	wait := grace
	if graceAllowed {
		if grace == 0 {
			wait = engineDefaultGrace
		} else {
			seconds := (grace + time.Second - 1) / time.Second
			target += "?" + url.Values{"t": {strconv.FormatInt(int64(seconds), 10)}}.Encode()
			wait = seconds * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout+wait)
	defer cancel()
	resp, err := c.request(ctx, c.actions, http.MethodPost, target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	switch {
	case resp.StatusCode == http.StatusNoContent:
		return nil
	case resp.StatusCode == http.StatusNotModified && verb != "restart":
		return nil
	}
	return statusError(resp.StatusCode)
}

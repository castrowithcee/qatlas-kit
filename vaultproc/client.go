package vaultproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
)

// The pause and the overall limit within which a call asks again when the process answers replaced: a
// handover moves the socket to the successor within moments of the commit.
const (
	replacedPause = 50 * time.Millisecond
	replacedLimit = 2 * time.Second
)

// Client reaches the vault process of one vault. Every call opens a connection of its own. The process
// that listens must pass the kernel checks of localipc.CheckHardened and then prove that it holds the
// vault's key: the client sends a fresh nonce encrypted to Recipient, and the process answers with a hash
// of what it decrypted. Nothing else is sent before both passed.
type Client struct {
	// Endpoint is the channel; it must equal the process's Config.Endpoint.
	Endpoint localipc.Endpoint
	// Recipient is the vault's public recipient as age text (vault.Vault.Recipient).
	Recipient string
	// Version is the application's protocol version, as in Config.Version.
	Version int
	// ProofLabel is the proof prefix as in Config.ProofLabel; it must equal the process's. An empty label
	// fails every call before anything is dialed.
	ProofLabel string
	// Timeout bounds one call; zero means DefaultRequestTimeout. The context of a call bounds it too.
	Timeout time.Duration
}

// Status is what a running vault process reports. It holds no secret.
type Status struct {
	PID int
	// LocksAt is when the process locks itself unless a request touches it first.
	LocksAt time.Time
	// UnlockEndsAt is the end of the maximum unlock time; zero without a limit.
	UnlockEndsAt time.Time
	// MemoryLocked reports whether the system keeps the held document out of swap.
	MemoryLocked bool
}

// Status reports on the running vault process. ErrNotRunning means there is none.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var st statusResult
	if err := c.Call(ctx, OpStatus, nil, &st); err != nil {
		return Status{}, err
	}
	status := Status{PID: st.PID, LocksAt: st.LocksAt, MemoryLocked: st.MemoryLocked}
	if st.UnlockEndsAt != nil {
		status.UnlockEndsAt = *st.UnlockEndsAt
	}
	return status, nil
}

// Lock asks the vault process to overwrite what it holds and end, and waits briefly until it is gone.
// ErrNotRunning means there was none.
func (c *Client) Lock(ctx context.Context) error {
	if err := c.Call(ctx, OpLock, nil, nil); err != nil {
		return err
	}
	for deadline := time.Now().Add(replacedLimit); time.Now().Before(deadline); {
		if _, err := c.Status(ctx); errors.Is(err, ErrNotRunning) || ctx.Err() != nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// Call runs the application operation op with body encoded as JSON (nil for none) and decodes the result
// into result unless it is nil. An application's code is an *Error. A failure at a process that did listen
// is a *localipc.PeerError with its process id, so the application can name the process to end.
func (c *Client) Call(ctx context.Context, op string, body, result any) error {
	if !wordPattern.MatchString(op) || op == OpHandover {
		return fmt.Errorf("vaultproc: %q is not an operation a call can name", op)
	}
	var raw json.RawMessage
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return errors.New("vaultproc: cannot encode the request")
		}
		defer clear(data)
		raw = data
	}
	resp, err := c.call(ctx, request{Op: op, Body: raw})
	defer clear(resp.Result)
	if err != nil || result == nil || len(resp.Result) == 0 {
		return err
	}
	if json.Unmarshal(resp.Result, result) != nil {
		return errors.New("vaultproc: the answer cannot be read")
	}
	return nil
}

// call asks again on a new connection while the process answers replaced, for up to replacedLimit.
func (c *Client) call(ctx context.Context, req request) (response, error) {
	end := time.Now().Add(replacedLimit)
	for {
		resp, err := c.callOnce(ctx, req)
		if !errors.Is(err, ErrReplaced) || !time.Now().Add(replacedPause).Before(end) {
			return resp, err
		}
		select {
		case <-ctx.Done():
			return resp, err
		case <-time.After(replacedPause):
		}
	}
}

func (c *Client) callOnce(ctx context.Context, req request) (response, error) {
	ctx, cancel := context.WithDeadline(ctx, c.deadline(ctx, c.timeout()))
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return response{}, err
	}
	defer conn.Close()
	resp, err := c.exchange(ctx, conn, req)
	return resp, c.named(ctx, conn, err)
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultRequestTimeout
}

// deadline is the earlier of timeout from now and the end of ctx.
func (c *Client) deadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	return deadline
}

// dial connects and runs the kernel checks and the challenge.
func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	if c.ProofLabel == "" {
		return nil, ErrNoProofLabel
	}
	recipient, err := c.recipient()
	if err != nil {
		return nil, err
	}
	conn, err := c.Endpoint.DialWith(ctx, localipc.DialOptions{Server: localipc.CheckHardened,
		Challenge: func(ctx context.Context, conn net.Conn) error { return c.challenge(ctx, conn, recipient) }})
	switch {
	case err == nil:
		return conn, nil
	case notRunning(err), errors.Is(err, ErrNotRunning):
		return nil, ErrNotRunning
	case errors.Is(err, ErrReplaced):
		return nil, ErrReplaced
	case ctx.Err() != nil && !errors.Is(err, localipc.ErrRefused):
		return nil, ctx.Err()
	}
	return nil, err
}

func (c *Client) recipient() (age.Recipient, error) {
	text := strings.TrimSpace(c.Recipient)
	if text == "" {
		return nil, ErrNoRecipient
	}
	recipient, err := age.ParseX25519Recipient(text)
	if err != nil {
		return nil, errors.New("vaultproc: the vault recipient cannot be read; the vault process is not asked")
	}
	return recipient, nil
}

// challenge asks the process to prove that it holds the vault's key, with a nonce for this connection only.
func (c *Client) challenge(ctx context.Context, conn net.Conn, recipient age.Recipient) error {
	nonce, challenge, err := newChallenge(recipient)
	if err != nil {
		return err
	}
	defer clear(nonce)
	if err := writeMessage(conn, hello{V: ProtocolVersion, App: c.Version, Challenge: challenge}); err != nil {
		return failed(ctx, err)
	}
	resp, err := c.answer(ctx, conn)
	switch {
	case err != nil:
		return err
	case !checkProof(c.ProofLabel, nonce, resp.Proof):
		return ErrUnproven
	}
	return nil
}

// exchange sends the request on a checked connection and reads its answer.
func (c *Client) exchange(ctx context.Context, conn net.Conn, req request) (response, error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return response{}, err
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	req.V, req.App = ProtocolVersion, c.Version
	if err := writeMessage(conn, req); err != nil {
		return response{}, failed(ctx, err)
	}
	return c.answer(ctx, conn)
}

// answer reads one answer and turns its code into the client's error.
func (c *Client) answer(ctx context.Context, conn net.Conn) (response, error) {
	var resp response
	if err := readMessage(conn, &resp); err != nil {
		return response{}, failed(ctx, err)
	}
	if resp.V != ProtocolVersion {
		clear(resp.Result)
		return response{}, ErrVersion
	}
	if resp.Error != "" {
		return response{}, answerError(resp.Error)
	}
	if resp.App != c.Version {
		clear(resp.Result)
		return response{}, ErrVersion
	}
	return resp, nil
}

// named adds the peer's process id to an error it caused.
func (c *Client) named(ctx context.Context, conn net.Conn, err error) error {
	if err == nil || errors.Is(err, ErrNotRunning) || errors.Is(err, ErrReplaced) || ctx.Err() != nil {
		return err
	}
	var peer *localipc.PeerError
	if errors.As(err, &peer) {
		return err
	}
	if pid, pidErr := localipc.PeerPID(conn); pidErr == nil && pid > 0 {
		return &localipc.PeerError{PID: pid, Err: err}
	}
	return err
}

// failed says why an exchange broke off. A context that ended wins; a connection closed without an answer
// means the process is locking itself.
func failed(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, os.ErrDeadlineExceeded):
		return fmt.Errorf("the vault process did not answer in time: %w", err)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.EPIPE),
		errors.Is(err, syscall.ECONNRESET), errors.Is(err, net.ErrClosed):
		return ErrNotRunning
	case errors.Is(err, ErrTooLarge), errors.Is(err, errMalformed):
		return err
	}
	return fmt.Errorf("cannot talk to the vault process: %w", err)
}

// notRunning reports a dial error that means nobody serves the channel.
func notRunning(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOTSOCK)
}

// Handover is a handover prepared on one connection, which its commit uses as well.
type Handover struct {
	client *Client
	conn   net.Conn
}

// PrepareHandover asks the vault process to hand the vault over to a successor started from the program
// an update is about to install, which files describes as selfupdate.Client.BeforeReplace receives them.
// The process verifies the release itself and reads the user's update setting itself. It returns the
// Handover to commit once the program is replaced, or nil once the process locked itself as the setting
// asks. ErrHandover means it refused and locked itself; ErrNotRunning that there is nothing to hand over.
// The caller closes a Handover it does not commit, and the process locks itself then.
func (c *Client) PrepareHandover(ctx context.Context, files selfupdate.ReleaseFiles) (*Handover, error) {
	body, err := json.Marshal(handoverRequest{Phase: phasePrepare, Release: &releaseFiles{
		Checksums: files.Checksums, Signature: files.Signature, Archive: files.Archive,
		ArchiveName: files.ArchiveName, Rollback: files.Rollback}})
	if err != nil {
		return nil, errors.New("vaultproc: cannot encode the handover")
	}
	ctx, cancel := context.WithDeadline(ctx, c.deadline(ctx, HandoverTimeout))
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.exchange(ctx, conn, request{Op: OpHandover, Body: body})
	if err != nil {
		_ = conn.Close()
		return nil, c.named(ctx, conn, err)
	}
	var result handoverResult
	if json.Unmarshal(resp.Result, &result) != nil {
		_ = conn.Close()
		return nil, errors.New("vaultproc: the handover answer cannot be read")
	}
	if !result.HandOver {
		_ = conn.Close()
		return nil, nil
	}
	_ = conn.SetDeadline(time.Time{})
	return &Handover{client: c, conn: conn}, nil
}

// Commit tells the vault process that the program was replaced and waits until it handed the vault over:
// it checks that its program is now the one of the release, starts the successor from it and hands it
// the vault and then the socket. It returns the successor's process id. Any other outcome locked the
// process. The connection is closed either way.
func (h *Handover) Commit(ctx context.Context) (int, error) {
	defer h.conn.Close()
	body, _ := json.Marshal(handoverRequest{Phase: phaseCommit})
	ctx, cancel := context.WithDeadline(ctx, h.client.deadline(ctx, HandoverTimeout))
	defer cancel()
	resp, err := h.client.exchange(ctx, h.conn, request{Op: OpHandover, Body: body})
	if err != nil {
		return 0, h.client.named(ctx, h.conn, err)
	}
	var result handoverResult
	if json.Unmarshal(resp.Result, &result) != nil || result.PID <= 0 {
		return 0, fmt.Errorf("%w: the answer cannot be read", ErrHandover)
	}
	return result.PID, nil
}

// Close ends a handover that is not committed; the vault process locks itself.
func (h *Handover) Close() error { return h.conn.Close() }

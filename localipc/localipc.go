package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// Endpoint contains names supplied by the consuming application; the package
// documentation gives where the channel lives. RuntimeDirName and SocketBase
// must each be a single path component on Unix. RuntimeRoot, when set, must be
// an absolute, existing, private directory of the user. FallbackDir must be
// absolute, and may be empty only with RuntimeRoot. On Windows, PipePrefix and
// SocketBase form the pipe name with the user's SID, and RuntimeRoot and
// FallbackDir are ignored. Running needs every field except SocketBase.
type Endpoint struct {
	RuntimeDirName string
	SocketBase     string
	FallbackDir    string
	RuntimeRoot    string
	ErrorName      string
	PipePrefix     string
}

// ErrRunning means a listener already owns this endpoint.
var ErrRunning = errors.New("local channel is already running")

// ErrRefused means the peer or the channel's filesystem permissions failed a check.
var ErrRefused = errors.New("local channel refused")

// ErrProgramReplaced means the program file this process started from was
// removed or replaced since. Such a process refuses every peer unless it
// selected CrossVersion.
var ErrProgramReplaced = errors.New("this program was removed or replaced since it started")

// ErrNoChallenge means CheckHardened was chosen without a Challenge.
var ErrNoChallenge = errors.New("a hardened server check needs a challenge")

// ErrUnsupported means the platform has no channel implementation yet.
var ErrUnsupported = fmt.Errorf("local channel is unsupported on this platform: %w", errors.ErrUnsupported)

var errHandoverUnsupported = fmt.Errorf("handing a local channel over is unsupported on this platform: %w",
	errors.ErrUnsupported)

// PathTooLongError reports a socket path that exceeds the portable Unix limit.
type PathTooLongError struct{ Path string }

func (e *PathTooLongError) Error() string {
	return fmt.Sprintf("socket path %q is %d bytes long; maximum is %d", e.Path, len(e.Path), maxSocketPath)
}

const maxSocketPath = 103

// PeerError is a refused peer. PID is its process id as the kernel reported
// it, or 0 if it could not be told. Err wraps ErrRefused.
type PeerError struct {
	PID int
	Err error
}

func (e *PeerError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("%v (process %d)", e.Err, e.PID)
	}
	return e.Err.Error()
}

func (e *PeerError) Unwrap() error { return e.Err }

// ServerCheck selects how a client checks the process that listens.
type ServerCheck int

const (
	// CheckProgram requires the same user and the same program file.
	CheckProgram ServerCheck = iota
	// CheckHardened is for a server that called Harden. Where Harden hides its
	// program file (Linux), the program check is replaced by the user check
	// plus the mandatory Challenge; elsewhere the program is checked as well.
	CheckHardened
)

// Challenge lets the listening process prove that it is the application's
// own server. It runs after the kernel checks and before the dial returns,
// bounded by the dial's context. It returns nil only for a peer that proved
// itself; the client must send nothing of its own before it succeeds.
type Challenge func(ctx context.Context, conn net.Conn) error

// DialOptions tune DialWith. The zero value is the strict default.
type DialOptions struct {
	Server ServerCheck
	// Challenge is required with CheckHardened and optional otherwise. It
	// runs after the CrossVersion challenge.
	Challenge Challenge
	// CrossVersion, if set, accepts a listener started from another program
	// file; see CrossVersion.
	CrossVersion *CrossVersion
}

// ListenOptions tune ListenWith. The zero value is the strict default.
type ListenOptions struct {
	// Successor listens beside the endpoint's channel, where clients do not
	// reach it until the running listener calls HandOver and AwaitHandover
	// returns. Windows does not support it.
	Successor bool
	// Earlier, valid only with Successor, is an endpoint the application used
	// before, such as one with another RuntimeDirName or SocketBase. Where a
	// listener serves Earlier and none serves the endpoint itself, the
	// successor listens at the endpoint's channel at once and places beside
	// the earlier socket what that listener moves in its handover; Accept fails
	// until AwaitHandover returns. Otherwise it listens as Successor alone
	// does. Windows does not support it.
	Earlier *Endpoint
	// Refused, if set, receives each connection Accept refuses before Accept
	// closes it, with the *PeerError. The peer is unverified: the function may
	// send one short refusal and must treat anything read as untrusted. It
	// runs under a short deadline and blocks Accept while it runs.
	Refused func(conn net.Conn, err error)
	// CrossVersion, if set, accepts clients started from another program
	// file; see CrossVersion. Accept runs its exchange under a short deadline
	// and blocks while it runs.
	CrossVersion *CrossVersion
}

// refusalWait bounds the Refused callback's writes and reads.
const refusalWait = time.Second

// handoverPoll is how often AwaitHandover looks for the moved socket.
const handoverPoll = 10 * time.Millisecond

// Path returns the platform's channel path without creating it.
func (e Endpoint) Path() (string, error) { return endpointPath(e) }

// Running returns the SocketBase of every channel of the application that
// currently has a private socket (Unix) or an open pipe (Windows), sorted and
// without duplicates. It validates the fields as Path does, except that
// SocketBase is not needed, and fails with ErrRefused for a runtime directory
// that is not private. It neither connects nor locks: a listed channel is not
// proven alive, and a crashed Unix process leaves its name until a Listen of
// the same name takes it over. Dial checks.
func (e Endpoint) Running() ([]string, error) { return running(e) }

// Listen owns one endpoint until the returned listener is closed. Accept checks
// the user and program of every peer and closes rejected connections.
func (e Endpoint) Listen() (net.Listener, error) { return listen(e, ListenOptions{}) }

// ListenWith is Listen with options.
func (e Endpoint) ListenWith(opts ListenOptions) (net.Listener, error) { return listen(e, opts) }

// Dial connects to an endpoint and checks the user and program before returning.
func (e Endpoint) Dial(ctx context.Context) (net.Conn, error) { return e.DialWith(ctx, DialOptions{}) }

// DialWith connects to an endpoint and checks the listening process as opts
// select. A refusal is a *PeerError; the connection is closed then.
func (e Endpoint) DialWith(ctx context.Context, opts DialOptions) (net.Conn, error) {
	program, err := opts.programCheck()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	secret, err := opts.CrossVersion.secret()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	defer clear(secret)
	check := func(c net.Conn) (int, error) { return checkPeer(c, program) }
	if secret != nil {
		check = func(c net.Conn) (int, error) {
			pid, err := checkPeer(c, false)
			if err == nil {
				err = proveAsDialer(ctx, c, secret)
			}
			return pid, err
		}
	}
	conn, err := dial(ctx, e)
	if err != nil {
		return nil, err
	}
	return establish(ctx, conn, e.label(), check, opts.Challenge)
}

// HandOver moves the channel of l, a listener from Listen, to the successor
// that listens beside it: first its lock, then its socket, each atomically.
// The successor must hold its lock and own a private socket. Afterwards new
// connections reach the successor, and closing l leaves its channel alone.
func HandOver(l net.Listener) error { return handOver(l) }

// AwaitHandover waits until the running listener moved l, a listener from
// ListenWith with Successor, onto the endpoint's channel. With Earlier, it
// waits until the earlier listener moved what l placed beside its socket onto
// that socket, then removes it, and l owns the endpoint's channel. ctx bounds
// the wait; without a handover l keeps its place.
func AwaitHandover(ctx context.Context, l net.Listener) error { return awaitHandover(ctx, l) }

// ProgramReplaced returns the path this process's program had at its start and
// whether that file was removed or replaced since. An error means it cannot be
// told; callers treat that as replaced.
func ProgramReplaced() (path string, replaced bool, err error) { return programReplaced() }

func (o DialOptions) programCheck() (bool, error) {
	switch o.Server {
	case CheckProgram:
		return true, nil
	case CheckHardened:
		if o.Challenge == nil {
			return false, ErrNoChallenge
		}
		return hardenedProgramCheck, nil
	}
	return false, fmt.Errorf("unknown server check %d", o.Server)
}

// establish checks a fresh client connection and runs the challenge. It closes
// conn on any failure.
func establish(ctx context.Context, conn net.Conn, label string, check func(net.Conn) (int, error),
	challenge Challenge) (net.Conn, error) {
	pid, err := check(conn)
	if err == nil && challenge != nil {
		err = runChallenge(ctx, conn, challenge)
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s: %w", label, &PeerError{PID: pid, Err: err})
	}
	return conn, nil
}

func runChallenge(ctx context.Context, conn net.Conn, challenge Challenge) error {
	return bounded(ctx, conn, "the challenge failed", func() error { return challenge(ctx, conn) })
}

// bounded ties the connection's deadline to ctx so a silent peer cannot hold
// an exchange longer than the caller allows. failed names the exchange in a
// refusal.
func bounded(ctx context.Context, conn net.Conn, failed string, exchange func() error) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("%w: cannot bound the challenge: %v", ErrRefused, err)
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	err := exchange()
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrRefused, failed, err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("%w: cannot reset the deadline: %v", ErrRefused, err)
	}
	return nil
}

// refuse hands a refused connection to the Refused callback and closes it.
func refuse(conn net.Conn, pid int, reason error, refused func(net.Conn, error)) error {
	err := &PeerError{PID: pid, Err: reason}
	if refused != nil {
		_ = conn.SetDeadline(time.Now().Add(refusalWait))
		refused(conn, err)
	}
	_ = conn.Close()
	return err
}

func (e Endpoint) label() string {
	if e.ErrorName != "" {
		return e.ErrorName
	}
	return "local channel"
}

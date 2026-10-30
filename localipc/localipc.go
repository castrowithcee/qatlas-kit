// Package localipc provides a local process channel with checked peer identity.
//
// Linux checks the peer's user and program file. macOS and Windows check the
// peer's user and the file currently at its reported image path; this cannot
// prove which file a peer loaded if that path changed after start.
// Unix uses a private runtime directory, socket permissions, and a lock.
// Windows uses a protected named pipe that rejects network clients and another
// first instance. These checks limit accidental access by other same-user
// processes. They cannot prevent deliberate impersonation or access by root or
// an administrator. Harden is optional. Linux Harden hides the executable from
// same-user processes, so connections to a hardened listener fail closed.
// Windows Harden limits process
// access to identity queries, waiting, and termination while denying memory
// access through the process DACL; debug privilege and existing handles bypass it.
package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// Endpoint contains names supplied by the consuming application. FallbackDir
// is an absolute directory used when no private user runtime directory exists.
// RuntimeDirName and SocketBase must each be a single path component on Unix.
// On Windows, PipePrefix and SocketBase form the pipe name with the user's SID.
type Endpoint struct {
	RuntimeDirName string
	SocketBase     string
	FallbackDir    string
	ErrorName      string
	PipePrefix     string
}

// ErrRunning means a listener already owns this endpoint.
var ErrRunning = errors.New("local channel is already running")

// ErrRefused means the peer or the channel's filesystem permissions failed a check.
var ErrRefused = errors.New("local channel refused")

// ErrUnsupported means the platform has no channel implementation yet.
var ErrUnsupported = fmt.Errorf("local channel is unsupported on this platform: %w", errors.ErrUnsupported)

// PathTooLongError reports a socket path that exceeds the portable Unix limit.
type PathTooLongError struct{ Path string }

func (e *PathTooLongError) Error() string {
	return fmt.Sprintf("socket path %q is %d bytes long; maximum is %d", e.Path, len(e.Path), maxSocketPath)
}

const maxSocketPath = 103

// Path returns the platform's channel path without creating it.
func (e Endpoint) Path() (string, error) { return endpointPath(e) }

// Listen owns one endpoint until the returned listener is closed. Accept checks
// the user and program of every peer and closes rejected connections.
func (e Endpoint) Listen() (net.Listener, error) { return listen(e) }

// Dial connects to an endpoint and checks the user and program before returning.
func (e Endpoint) Dial(ctx context.Context) (net.Conn, error) { return dial(ctx, e) }

func (e Endpoint) label() string {
	if e.ErrorName != "" {
		return e.ErrorName
	}
	return "local channel"
}

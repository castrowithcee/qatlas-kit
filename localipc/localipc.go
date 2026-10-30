// Package localipc provides a local process channel with checked peer identity.
//
// On Linux and macOS, both endpoints must have the same user ID and run the same program
// file. A private runtime directory, socket permissions, and a lock also limit
// accidental access and duplicate listeners. This limits access by other
// same-user processes to what the peer check can identify; it cannot prevent
// deliberate impersonation or access by root. Harden is optional. On Linux
// it hides the executable from other same-user processes, so connections to
// a hardened listener fail closed. On macOS it disables core dumps and
// debugger attachment without hiding the executable identity. macOS reports
// the path used at exec, so a peer whose program file was replaced after exec
// can be identified only by the file currently at that path.
package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// Endpoint contains names supplied by the consuming application. FallbackDir
// is an absolute directory used when no private user runtime directory exists.
// RuntimeDirName and SocketBase must each be a single path component.
type Endpoint struct {
	RuntimeDirName string
	SocketBase     string
	FallbackDir    string
	ErrorName      string
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

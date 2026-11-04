//go:build !linux && !darwin && !windows

package localipc

import (
	"context"
	"net"
)

const hardenedProgramCheck = true

func endpointPath(Endpoint) (string, error)                { return "", ErrUnsupported }
func running(Endpoint) ([]string, error)                   { return nil, ErrUnsupported }
func listen(Endpoint, ListenOptions) (net.Listener, error) { return nil, ErrUnsupported }
func dial(context.Context, Endpoint) (net.Conn, error)     { return nil, ErrUnsupported }
func checkPeer(net.Conn, bool) (int, error)                { return 0, ErrUnsupported }
func handOver(net.Listener) error                          { return ErrUnsupported }
func awaitHandover(context.Context, net.Listener) error    { return ErrUnsupported }
func programReplaced() (string, bool, error)               { return "", false, ErrUnsupported }

// VerifyPeer is unavailable on this platform.
func VerifyPeer(net.Conn) error { return ErrUnsupported }

// PeerPID is unavailable on this platform.
func PeerPID(net.Conn) (int, error) { return 0, ErrUnsupported }

// PeerUID is unavailable on this platform.
func PeerUID(net.Conn) (uint32, error) { return 0, ErrUnsupported }

// Harden is unavailable on this platform.
func Harden() error { return ErrUnsupported }

//go:build !linux

package localipc

import (
	"context"
	"net"
)

func endpointPath(Endpoint) (string, error)            { return "", ErrUnsupported }
func listen(Endpoint) (net.Listener, error)            { return nil, ErrUnsupported }
func dial(context.Context, Endpoint) (net.Conn, error) { return nil, ErrUnsupported }

// VerifyPeer is unavailable on this platform.
func VerifyPeer(net.Conn) error { return ErrUnsupported }

// Harden is unavailable on this platform.
func Harden() error { return ErrUnsupported }

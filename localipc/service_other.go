//go:build !linux

package localipc

import (
	"context"
	"net"
)

func servicePath(ServiceEndpoint) (string, error) { return "", ErrUnsupported }

func listenService(ServiceEndpoint, ListenOptions) (net.Listener, error) { return nil, ErrUnsupported }

func dialService(context.Context, ServiceEndpoint) (net.Conn, error) { return nil, ErrUnsupported }

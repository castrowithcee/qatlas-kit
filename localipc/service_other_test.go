//go:build !linux

package localipc

import (
	"context"
	"errors"
	"testing"
)

func TestServiceUnsupported(t *testing.T) {
	e := ServiceEndpoint{Dir: "/run/example", SocketBase: "control", ServiceUser: "example", Group: "example"}
	if _, err := e.Path(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Path() = %v", err)
	}
	if _, err := e.Listen(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Listen() = %v", err)
	}
	if _, err := e.Dial(context.Background()); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Dial() = %v", err)
	}
}

func TestServiceCrossVersionUnsupported(t *testing.T) {
	e := ServiceEndpoint{Dir: "/run/example", SocketBase: "control", ServiceUser: "example", Group: "example"}
	if _, err := e.ListenWith(ListenOptions{CrossVersion: &CrossVersion{Secret: testSecret(1)}}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ListenWith() = %v", err)
	}
}

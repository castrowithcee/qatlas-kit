//go:build windows

package docker

import (
	"errors"
	"testing"

	"github.com/castrowithcee/qatlas-kit/output"
)

func TestNewIsUnsupported(t *testing.T) {
	_, err := New(Options{Socket: `C:\x\docker.sock`})
	var failure *Error
	if !errors.As(err, &failure) || !errors.Is(err, errors.ErrUnsupported) || failure.Code != output.CodeRuntime {
		t.Fatalf("New() error = %v", err)
	}
}

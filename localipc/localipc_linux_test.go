//go:build linux

package localipc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnreadableProgramFailsClosed(t *testing.T) {
	e := testEndpoint(t)
	if err := os.Mkdir(filepath.Dir(mustPath(t, e)), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := startHelper(t, os.Args[0], "harden-listen", mustPath(t, e))
	defer stopHelper(cmd)
	if _, err := e.Dial(context.Background()); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("client accepted a hardened peer: %v", err)
	}
}

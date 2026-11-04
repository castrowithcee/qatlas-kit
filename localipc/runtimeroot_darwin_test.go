package localipc

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRuntimeRootTooLongIgnoresTmpdir(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "ipct-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	t.Setenv("TMPDIR", tmp)
	e := Endpoint{RuntimeDirName: "session", SocketBase: strings.Repeat("x", maxSocketPath), RuntimeRoot: shortRoot(t, 0o700), ErrorName: "example"}
	var tooLong *PathTooLongError
	if _, err := e.Path(); !errors.As(err, &tooLong) {
		t.Fatalf("error = %v, want PathTooLongError", err)
	}
}

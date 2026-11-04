//go:build windows

package localipc

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"testing"
)

func uniquePrefix(t *testing.T) string {
	t.Helper()
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return "ipcrun-" + hex.EncodeToString(nonce[:]) + "-"
}

func listenPipe(t *testing.T, e Endpoint, base string) func() {
	t.Helper()
	e.SocketBase = base
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return func() { _ = l.Close() }
}

func TestWindowsRunning(t *testing.T) {
	// Packages run in parallel; unique prefixes keep other pipes out of the list.
	e := windowsEndpoint(t)
	e.PipePrefix = uniquePrefix(t)
	e.SocketBase = ""
	closeA := listenPipe(t, e, "ipcrun-b")
	listenPipe(t, e, "ipcrun-a")
	foreign := e
	foreign.PipePrefix = uniquePrefix(t)
	listenPipe(t, foreign, "ipcrun-c")
	want := []string{"ipcrun-a", "ipcrun-b"}
	if got, err := e.Running(); err != nil || !slices.Equal(got, want) {
		t.Fatalf("running = %v, %v", got, err)
	}
	closeA()
	want = []string{"ipcrun-a"}
	if got, err := e.Running(); err != nil || !slices.Equal(got, want) {
		t.Fatalf("running after close = %v, %v", got, err)
	}
}

func TestWindowsRunningRejectsInvalidPrefix(t *testing.T) {
	for _, prefix := range []string{"", `a\b`} {
		if _, err := (Endpoint{PipePrefix: prefix}).Running(); err == nil {
			t.Fatalf("prefix %q accepted", prefix)
		}
	}
}

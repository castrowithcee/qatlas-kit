//go:build windows

package vaultproc

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

func testEndpoint(t *testing.T) localipc.Endpoint {
	t.Helper()
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return localipc.Endpoint{PipePrefix: "example-", SocketBase: hex.EncodeToString(nonce[:]), ErrorName: "example"}
}

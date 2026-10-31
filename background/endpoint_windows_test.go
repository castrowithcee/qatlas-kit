//go:build windows

package background

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"golang.org/x/sys/windows"

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

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	event, err := windows.WaitForSingleObject(h, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}

// ownSession has no Windows counterpart; the process has no console instead.
func ownSession(int) bool { return true }

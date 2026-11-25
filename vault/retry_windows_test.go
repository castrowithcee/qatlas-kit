//go:build windows

package vault

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const retryDocument = `{"schema":1}`

// releaseAfter calls release after d, and at cleanup in case the test ends first.
func releaseAfter(t *testing.T, d time.Duration, release func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		time.Sleep(d)
		release()
		close(done)
	}()
	t.Cleanup(func() { <-done })
}

func retryHeldVault(t *testing.T, retry bool) *Vault {
	t.Helper()
	v := retryVault(t, retry)
	if err := v.CreateUnencrypted([]byte(retryDocument)); err != nil {
		t.Fatal(err)
	}
	return v
}

// os.Open shares no delete access, so a replace of the file meets a sharing violation while it is open.
func TestWindowsWriteWaitsForOpenFile(t *testing.T) {
	v := retryHeldVault(t, true)
	f, err := os.Open(v.plainPath())
	if err != nil {
		t.Fatal(err)
	}
	releaseAfter(t, 200*time.Millisecond, func() { _ = f.Close() })
	if err := v.Write([]byte(retryDocument)); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func TestWindowsReadWaitsForExclusiveHandle(t *testing.T) {
	v := retryHeldVault(t, true)
	p, err := windows.UTF16PtrFromString(v.plainPath())
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	releaseAfter(t, 200*time.Millisecond, func() { _ = windows.CloseHandle(h) })
	if _, err := v.readFile(v.plainPath()); err != nil {
		t.Fatalf("readFile: %v", err)
	}
}

func TestWindowsWriteFailsWithoutOption(t *testing.T) {
	v := retryHeldVault(t, false)
	f, err := os.Open(v.plainPath())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := v.Write([]byte(retryDocument)); err == nil {
		t.Fatal("Write succeeded although the file stayed open")
	}
}

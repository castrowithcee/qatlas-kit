//go:build windows

package configfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestExistingOpenLockACLIsRejectedWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	lock := path + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	open, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := open.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(lock, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	store := numberStore(t, path)
	if _, err := store.SaveIfUnchanged(1, RevisionAbsent); err == nil {
		t.Fatal("accepted a lock file open to everyone")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration was written: %v", err)
	}
	got, err := windows.GetNamedSecurityInfo(lock, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	actual, _, err := got.DACL()
	if err != nil {
		t.Fatal(err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if actual == nil || actual.AceCount != 1 {
		t.Fatalf("existing ACL changed: %v", actual)
	}
	if err := windows.GetAce(actual, 0, &ace); err != nil {
		t.Fatal(err)
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	if !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(everyone) {
		t.Fatal("existing ACL was changed")
	}
}

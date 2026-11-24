//go:build windows

package configfile

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func widenACL(t *testing.T, path string) {
	t.Helper()
	open, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := open.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func assertPrivateACL(t *testing.T, path string) { assertUserOnlyACL(t, path) }

// assertWideACL checks that the ACL is still the one widenACL set: a single entry for Everyone.
func assertWideACL(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("%s ACL = %v, %v", path, dacl, err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	if !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(everyone) {
		t.Fatalf("%s ACL was not kept", path)
	}
}

//go:build windows

package configfile

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openLockFile(path string) (*os.File, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	want, err := windows.SecurityDescriptorFromString(fmt.Sprintf(privateFileSDDL, user.User.Sid.String()))
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: want}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, &sa, windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(handle), path)
	var info windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(handle, &info)
	if err == nil && info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		err = errors.New("lock file is not a regular file")
	}
	if err == nil {
		err = checkPrivateLockACL(handle, want, user.User.Sid)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func checkPrivateLockACL(handle windows.Handle, want *windows.SECURITY_DESCRIPTOR, user *windows.SID) error {
	got, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	control, _, err := got.Control()
	if err != nil {
		return err
	}
	if control&(windows.SE_DACL_PRESENT|windows.SE_DACL_PROTECTED) != windows.SE_DACL_PRESENT|windows.SE_DACL_PROTECTED {
		return errors.New("lock file has an unprotected or absent ACL")
	}
	actual, _, err := got.DACL()
	if err != nil {
		return err
	}
	expected, _, err := want.DACL()
	if err != nil {
		return err
	}
	if actual == nil || expected == nil || actual.AceCount != 1 || expected.AceCount != 1 {
		return errors.New("lock file ACL does not contain exactly one user entry")
	}
	var ace, wantACE *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(actual, 0, &ace); err != nil {
		return err
	}
	if err := windows.GetAce(expected, 0, &wantACE); err != nil {
		return err
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != 0 ||
		ace.Mask != wantACE.Mask || !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user) {
		return errors.New("lock file ACL grants access beyond the current user")
	}
	return nil
}

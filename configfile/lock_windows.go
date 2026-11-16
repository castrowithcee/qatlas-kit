//go:build windows

package configfile

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"
)

// lockIdentity names the user a lock file must be private to and the owners that mark an existing lock
// file as that user's own.
type lockIdentity struct {
	user   *windows.SID
	owners []*windows.SID
}

func openLockFile(path string) (*os.File, error) {
	id, err := currentLockIdentity()
	if err != nil {
		return nil, err
	}
	return openLockFileAs(path, id, setLockDACL)
}

func currentLockIdentity() (lockIdentity, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return lockIdentity{}, err
	}
	// Files created by an elevated administrator are owned by the token's default owner, often the
	// Administrators group, instead of the user. Only the token's own SIDs can be such an owner.
	owner, err := tokenOwner(token)
	if err != nil {
		return lockIdentity{}, err
	}
	return lockIdentity{user: user.User.Sid, owners: []*windows.SID{user.User.Sid, owner}}, nil
}

func tokenOwner(token windows.Token) (*windows.SID, error) {
	var n uint32
	err := windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &n)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, fmt.Errorf("query token owner: %w", err)
	}
	buf := make([]byte, n)
	if err := windows.GetTokenInformation(token, windows.TokenOwner, &buf[0], n, &n); err != nil {
		return nil, fmt.Errorf("query token owner: %w", err)
	}
	owner := (*struct{ Owner *windows.SID })(unsafe.Pointer(&buf[0])).Owner
	if owner == nil {
		return nil, errors.New("token has no default owner")
	}
	return owner.Copy()
}

// openLockFileAs opens or creates the lock file for id. setDACL is a parameter only so that tests can
// make it fail.
func openLockFileAs(path string, id lockIdentity, setDACL func(windows.Handle, *windows.ACL) error) (*os.File, error) {
	want, err := windows.SecurityDescriptorFromString(fmt.Sprintf(privateFileSDDL, id.user.String()))
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: want}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.WRITE_DAC,
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
	if err == nil && checkPrivateLockACL(handle, want, id.user) != nil {
		err = adoptLockFile(handle, want, id, setDACL)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// adoptLockFile makes an existing lock file of the user private, so that a lock file left by a program
// that did not restrict it does not block every write. Every step uses the open handle, so the file that
// was checked is the file that is changed.
func adoptLockFile(handle windows.Handle, want *windows.SECURITY_DESCRIPTOR, id lockIdentity,
	setDACL func(windows.Handle, *windows.ACL) error) error {
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !slices.ContainsFunc(id.owners, owner.Equals) {
		return errors.New("lock file is not private and belongs to another owner")
	}
	dacl, _, err := want.DACL()
	if err != nil {
		return err
	}
	if err := setDACL(handle, dacl); err != nil {
		return fmt.Errorf("make lock file private: %w", err)
	}
	return checkPrivateLockACL(handle, want, id.user)
}

func setLockDACL(handle windows.Handle, dacl *windows.ACL) error {
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
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

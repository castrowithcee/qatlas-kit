//go:build windows

package configfile

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const (
	privateFileSDDL = "D:P(A;;FA;;;%s)"
	privateDirSDDL  = "D:P(A;OICI;FA;;;%s)"
)

func restrictFile(path string) error {
	if err := os.Chmod(path, fileMode); err != nil {
		return err
	}
	return setPrivateACL(path, privateFileSDDL)
}

func restrictDir(path string) error { return setPrivateACL(path, privateDirSDDL) }

func setPrivateACL(path, sddl string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("determine current user: %w", err)
	}
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf(sddl, user.User.Sid.String()))
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func preservePermissions(source, target string) error {
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(target, info.Mode().Perm()); err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(source, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(target, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

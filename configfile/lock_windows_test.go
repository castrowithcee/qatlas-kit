//go:build windows

package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestOwnLockFileWithInheritedACLIsMadePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app", "settings")
	lock := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("4242"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertNotPrivateLock(t, lock)
	assertLockAdopted(t, path)
}

func TestOwnLockFileOpenToEveryoneIsMadePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	lock := path + ".lock"
	if err := os.WriteFile(lock, []byte("4242"), 0o600); err != nil {
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
	assertNotPrivateLock(t, lock)
	assertLockAdopted(t, path)
}

func TestOwnLockFileOwnedByAdministratorsIsMadePrivate(t *testing.T) {
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	id, err := currentLockIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if !windows.GetCurrentProcessToken().IsElevated() || !id.owners[1].Equals(admins) {
		// The hosted CI runs elevated with Administrators as default owner and must cover this case.
		if os.Getenv("CI") != "" {
			t.Fatal("CI token is not elevated with Administrators as default owner")
		}
		t.Skip("needs an elevated token whose default owner is Administrators")
	}
	path := filepath.Join(t.TempDir(), "settings")
	lock := path + ".lock"
	if err := os.WriteFile(lock, []byte("4242"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(lock, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION,
		admins, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := securityDescriptor(t, lock, windows.OWNER_SECURITY_INFORMATION); got != "O:BA" {
		t.Fatalf("prepared lock owner = %s", got)
	}
	assertNotPrivateLock(t, lock)
	assertLockAdopted(t, path)
}

// assertNotPrivateLock proves that the lock file fails the private ACL check, so a later success can
// only come from making it private.
func assertNotPrivateLock(t *testing.T, lock string) {
	t.Helper()
	id, err := currentLockIdentity()
	if err != nil {
		t.Fatal(err)
	}
	want, err := windows.SecurityDescriptorFromString(fmt.Sprintf(privateFileSDDL, id.user.String()))
	if err != nil {
		t.Fatal(err)
	}
	h := openForSecurity(t, lock)
	defer windows.CloseHandle(h)
	if err := checkPrivateLockACL(h, want, id.user); err == nil {
		t.Fatal("prepared lock file already passes the private ACL check")
	}
}

func assertLockAdopted(t *testing.T, path string) {
	t.Helper()
	lock := path + ".lock"
	owner := securityDescriptor(t, lock, windows.OWNER_SECURITY_INFORMATION)
	store := numberStore(t, path)
	if _, err := store.SaveIfUnchanged(1, RevisionAbsent); err != nil {
		t.Fatalf("own lock file was not taken over: %v", err)
	}
	if got := readNumberFile(t, path); string(got) != "1" {
		t.Fatalf("configuration = %q", got)
	}
	assertUserOnlyACL(t, lock)
	if got := securityDescriptor(t, lock, windows.DACL_SECURITY_INFORMATION); !strings.HasPrefix(got, "D:P") {
		t.Fatalf("lock ACL is not protected: %s", got)
	}
	if got := securityDescriptor(t, lock, windows.OWNER_SECURITY_INFORMATION); got != owner {
		t.Fatalf("lock owner changed from %s to %s", owner, got)
	}
	if got, err := os.ReadFile(lock); err != nil || string(got) != "4242" {
		t.Fatalf("lock content = %q, %v", got, err)
	}
}

func TestLockDirectoryIsRejectedWithoutChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	lock := path + ".lock"
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	before := securityDescriptor(t, lock, allSecurityInfo)
	assertSaveRejected(t, path)
	if got := securityDescriptor(t, lock, allSecurityInfo); got != before {
		t.Fatalf("directory security changed from %s to %s", before, got)
	}
}

func TestLockSymlinkIsRejectedWithoutChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings")
	lock := path + ".lock"
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("4242"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, lock); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	link, dest := securityDescriptor(t, lock, allSecurityInfo), securityDescriptor(t, target, allSecurityInfo)
	assertSaveRejected(t, path)
	if got := securityDescriptor(t, lock, allSecurityInfo); got != link {
		t.Fatalf("link security changed from %s to %s", link, got)
	}
	if got := securityDescriptor(t, target, allSecurityInfo); got != dest {
		t.Fatalf("link target security changed from %s to %s", dest, got)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "4242" {
		t.Fatalf("link target content = %q, %v", got, err)
	}
}

func TestForeignOwnedLockFileIsRejectedWithoutChange(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "settings.lock")
	if err := os.WriteFile(lock, []byte("4242"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Creating a file owned by someone else needs privileges, so the lock is opened as another user.
	other, err := windows.CreateWellKnownSid(windows.WinLocalServiceSid)
	if err != nil {
		t.Fatal(err)
	}
	id := lockIdentity{user: other, owners: []*windows.SID{other}}
	before := securityDescriptor(t, lock, allSecurityInfo)
	f, err := openLockFileAs(lock, id, setLockDACL)
	if err == nil {
		_ = f.Close()
		t.Fatal("took over a lock file owned by another user")
	}
	assertFileUnchanged(t, lock, before)
}

func TestLockDACLFailureIsRejectedWithoutChange(t *testing.T) {
	for name, set := range map[string]func(windows.Handle, *windows.ACL) error{
		"set fails":     func(windows.Handle, *windows.ACL) error { return errors.New("access denied") },
		"set no effect": func(windows.Handle, *windows.ACL) error { return nil },
	} {
		t.Run(name, func(t *testing.T) {
			lock := filepath.Join(t.TempDir(), "settings.lock")
			if err := os.WriteFile(lock, []byte("4242"), 0o600); err != nil {
				t.Fatal(err)
			}
			id, err := currentLockIdentity()
			if err != nil {
				t.Fatal(err)
			}
			before := securityDescriptor(t, lock, allSecurityInfo)
			f, err := openLockFileAs(lock, id, set)
			if err == nil {
				_ = f.Close()
				t.Fatal("used a lock file that was not made private")
			}
			assertFileUnchanged(t, lock, before)
		})
	}
}

const allSecurityInfo = windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION |
	windows.DACL_SECURITY_INFORMATION

func assertSaveRejected(t *testing.T, path string) {
	t.Helper()
	store := numberStore(t, path)
	if _, err := store.SaveIfUnchanged(1, RevisionAbsent); err == nil {
		t.Fatal("accepted a lock that is not a regular file")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration was written: %v", err)
	}
}

func assertFileUnchanged(t *testing.T, name, security string) {
	t.Helper()
	if got := securityDescriptor(t, name, allSecurityInfo); got != security {
		t.Fatalf("security changed from %s to %s", security, got)
	}
	if got, err := os.ReadFile(name); err != nil || string(got) != "4242" {
		t.Fatalf("content = %q, %v", got, err)
	}
}

// openForSecurity opens the entry itself, not a link target, with just enough access to read its security.
func openForSecurity(t *testing.T, name string) windows.Handle {
	t.Helper()
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func securityDescriptor(t *testing.T, name string, info windows.SECURITY_INFORMATION) string {
	t.Helper()
	h := openForSecurity(t, name)
	defer windows.CloseHandle(h)
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, info)
	if err != nil {
		t.Fatal(err)
	}
	s := sd.String()
	if s == "" {
		t.Fatalf("cannot format security of %s", name)
	}
	return s
}

func TestNewPrivateFileAndBackupCopyHaveUserOnlyACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export")
	if err := WriteNewPrivateFile(path, []byte("data")); err != nil {
		t.Fatal(err)
	}
	store := numberStore(t, filepath.Join(dir, "settings"))
	copied, err := store.BackupCopy(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, copied} {
		assertUserOnlyACL(t, name)
	}
}

func assertUserOnlyACL(t *testing.T, name string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(name, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("%s ACL = %v, %v", name, dacl, err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	if !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid) {
		t.Fatalf("%s ACL does not belong to current user", name)
	}
}

func TestNewBackupAndDirectoryHaveUserOnlyACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	store := numberStore(t, path)
	mustSaveNumber(t, store, 1)
	mustSaveNumber(t, store, 2)
	backups, err := store.Backups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, %v", backups, err)
	}
	for _, name := range []string{store.backupDir(), filepath.Join(store.backupDir(), backups[0].Name), path + ".lock"} {
		assertUserOnlyACL(t, name)
	}
}

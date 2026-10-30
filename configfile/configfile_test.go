package configfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestResolvePathPrecedence(t *testing.T) {
	t.Setenv("TEST_CONFIG_FILE", "env-file")
	t.Setenv("TEST_CONFIG_DIR", "env-dir")
	for _, tc := range []struct {
		flag, file, dir, want string
	}{
		{"flag-file", "TEST_CONFIG_FILE", "TEST_CONFIG_DIR", "flag-file"},
		{"", "TEST_CONFIG_FILE", "TEST_CONFIG_DIR", "env-file"},
		{"", "", "TEST_CONFIG_DIR", filepath.Join("env-dir", "settings.json")},
		{"", "", "", filepath.Join("default-dir", "settings.json")},
	} {
		got, err := ResolvePath(tc.flag, tc.file, tc.dir, "default-dir", "settings.json")
		if err != nil || got != tc.want {
			t.Fatalf("ResolvePath(%q, %q, %q) = %q, %v; want %q", tc.flag, tc.file, tc.dir, got, err, tc.want)
		}
	}
	if _, err := ResolvePath("", "", "", "default-dir", "../escape"); err == nil {
		t.Fatal("accepted a path in place of a file name")
	}
}

func TestHasCurrentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	if err := os.WriteFile(path, []byte("2"), 0o600); err != nil {
		t.Fatal(err)
	}
	parse := func(data []byte) (int, error) { return strconv.Atoi(string(data)) }
	if !HasCurrentSchema(path, 2, parse) || HasCurrentSchema(path, 3, parse) {
		t.Fatal("schema version comparison failed")
	}
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if HasCurrentSchema(path, 2, parse) || HasCurrentSchema(path+"-missing", 2, parse) {
		t.Fatal("unreadable or malformed file counted as current")
	}
}

func TestPrepareRejectsBeforeReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	before := []byte("original")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	validate := func(value string) error {
		if value == "bad" {
			return errors.New("invalid")
		}
		return nil
	}
	decode := func(data []byte) (string, error) { return string(data), nil }
	for _, tc := range []struct {
		name  string
		value string
		enc   func(string) ([]byte, error)
		dec   func([]byte) (string, error)
	}{
		{"invalid input", "bad", func(s string) ([]byte, error) { return []byte(s), nil }, decode},
		{"encode error", "good", func(string) ([]byte, error) { return nil, errors.New("encode") }, decode},
		{"decode error", "good", func(s string) ([]byte, error) { return []byte(s), nil }, func([]byte) (string, error) { return "", errors.New("decode") }},
		{"invalid encoded value", "good", func(string) ([]byte, error) { return []byte("bad"), nil }, decode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := prepare(tc.value, validate, tc.enc, tc.dec)
			if err == nil || data != nil {
				t.Fatal("invalid encoding accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("target changed: %q, %v", after, err)
			}
		})
	}
}

func TestReplaceFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := replace(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced: %v, %v", info, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatalf("target = %q, %v", data, err)
	}
}

func TestReplaceKeepsExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions are ACLs")
	}
	path := filepath.Join(t.TempDir(), "settings")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := replace(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("mode = %#o; want 0640", got)
	}
}

func TestReplaceCreatesPrivateFileAndDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "one", "two")
	path := filepath.Join(dir, "settings")
	if err := replace(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return // Windows access is controlled by the ACL, not the Go mode bits.
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %s mode = %v", path, info.Mode().Perm())
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v", info.Mode().Perm())
	}
}

func TestReplaceFinalCheckKeepsTargetUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	stopped := errors.New("changed before rename")
	if err := replaceResolved(path, []byte("new"), func() error { return stopped }); !errors.Is(err, stopped) {
		t.Fatalf("replace = %v; want final check failure", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("target = %q, %v; want old", data, err)
	}
}

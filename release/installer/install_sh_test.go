package installer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/installer"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

// shEnv runs a rendered install.sh against a local release server with a temporary HOME.
type shEnv struct {
	t      *testing.T
	server *releasetest.Server
	signer ssh.Signer
	script string
	home   string
	prefix string
	shell  string
	path   string
}

func newShEnv(t *testing.T) *shEnv {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh supports Linux and macOS only")
	}
	target := hostTarget(t)
	for _, tool := range []string{"sh", "curl", "tar", "ssh-keygen"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	e := &shEnv{t: t, server: releasetest.NewServer(t), signer: releasetest.NewSigner(t), shell: "/bin/bash", path: os.Getenv("PATH")}
	root := t.TempDir()
	e.home = filepath.Join(root, "home")
	e.prefix = filepath.Join(e.home, ".local")
	if err := os.MkdirAll(e.home, 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := installer.Shell(testParams(t, e.signer, target, "windows/amd64"))
	if err != nil {
		t.Fatal(err)
	}
	e.script = filepath.Join(root, "install.sh")
	if err := os.WriteFile(e.script, script, 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *shEnv) publish(tag, program string, mutate func(*release)) {
	e.t.Helper()
	publish(e.t, e.server, e.signer, tag, archiveName(tag), program, mutate)
}

// run executes install.sh with a clean environment; env entries are added to it.
func (e *shEnv) run(stdinScript bool, env []string, args ...string) (string, error) {
	e.t.Helper()
	full := []string{
		"PATH=" + e.path,
		"HOME=" + e.home,
		"SHELL=" + e.shell,
		"TMPDIR=" + e.t.TempDir(),
		testEnv + "_INSTALL_BASE_URL=" + e.server.URL,
		testEnv + "_INSTALL_PREFIX=" + e.prefix,
	}
	var cmd *exec.Cmd
	if stdinScript {
		script, err := os.Open(e.script)
		if err != nil {
			e.t.Fatal(err)
		}
		defer script.Close()
		cmd = exec.Command("sh", append([]string{"-s", "--"}, args...)...)
		cmd.Stdin = script
	} else {
		cmd = exec.Command("sh", append([]string{e.script}, args...)...)
	}
	cmd.Env = append(full, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *shEnv) mustRun(env []string, args ...string) string {
	e.t.Helper()
	out, err := e.run(false, env, args...)
	if err != nil {
		e.t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	return out
}

func (e *shEnv) binary() string { return filepath.Join(e.prefix, "bin", testProgram) }

func TestShellFreshAndReinstall(t *testing.T) {
	e := newShEnv(t)
	e.publish("v1.0.0", "program one", nil)

	out := e.mustRun(nil)
	for _, want := range []string{"Sample Tool v1.0.0", e.binary(), "new terminal", testNextStep} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	info, err := os.Stat(e.binary())
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("binary: %v, %v", info, err)
	}
	if got := readFile(t, e.binary()); got != "program one" {
		t.Errorf("binary = %q", got)
	}
	if got := readFile(t, filepath.Join(e.prefix, "share/man/man1", testProgram+".1")); !strings.Contains(got, "v1.0.0") {
		t.Errorf("manpage = %q", got)
	}
	license := filepath.Join(e.prefix, "share/doc", testProgram, "LICENSE")
	if got := readFile(t, license); got != "license v1.0.0\n" {
		t.Errorf("license = %q", got)
	}
	profile := filepath.Join(e.home, ".profile")
	if got := readFile(t, profile); strings.Count(got, `export PATH="$HOME/.local/bin:$PATH"`) != 1 {
		t.Errorf("profile = %q", got)
	}

	e.publish("v1.0.1", "program two", nil)
	if out := e.mustRun(nil); !strings.Contains(out, "v1.0.1") {
		t.Errorf("reinstall output:\n%s", out)
	}
	if got := readFile(t, e.binary()); got != "program two" {
		t.Errorf("binary after reinstall = %q", got)
	}
	if got := readFile(t, license); got != "license v1.0.1\n" {
		t.Errorf("license after reinstall = %q", got)
	}
	if got := readFile(t, profile); strings.Count(got, "installer") != 1 || strings.Count(got, "export PATH") != 1 {
		t.Errorf("profile after reinstall = %q", got)
	}
	if left := stageLeftovers(e.prefix); len(left) != 0 {
		t.Errorf("staging left behind: %v", left)
	}
}

func TestShellLeavesOtherFilesUntouched(t *testing.T) {
	e := newShEnv(t)
	config := filepath.Join(e.home, "."+testProgram, "config.json")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.publish("v1.0.0", "program", nil)
	e.mustRun(nil)
	e.mustRun(nil)
	if got := readFile(t, config); got != "secret" {
		t.Errorf("config = %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(config)); len(entries) != 1 {
		t.Errorf("config directory changed: %v", entries)
	}
}

func TestShellRejectsBadReleaseWithoutChange(t *testing.T) {
	for name, tc := range rejections(t) {
		t.Run(name, func(t *testing.T) {
			e := newShEnv(t)
			e.publish("v1.0.0", "old program", nil)
			e.mustRun(nil)
			profile := readFile(t, filepath.Join(e.home, ".profile"))

			e.publish("v1.0.1", "new program", tc.mutate)
			out, err := e.run(false, nil)
			if err == nil {
				t.Fatalf("install succeeded:\n%s", out)
			}
			if !strings.Contains(out, tc.message) || !strings.Contains(out, "nothing was installed") {
				t.Errorf("output lacks %q:\n%s", tc.message, out)
			}
			if got := readFile(t, e.binary()); got != "old program" {
				t.Errorf("binary changed to %q", got)
			}
			if got := readFile(t, filepath.Join(e.prefix, "share/doc", testProgram, "LICENSE")); got != "license v1.0.0\n" {
				t.Errorf("license changed to %q", got)
			}
			if got := readFile(t, filepath.Join(e.home, ".profile")); got != profile {
				t.Errorf("profile changed to %q", got)
			}
			if left := stageLeftovers(e.prefix); len(left) != 0 {
				t.Errorf("staging left behind: %v", left)
			}
		})
	}
}

func TestShellDownloadsOnlyTheConfiguredSignature(t *testing.T) {
	e := newShEnv(t)
	e.publish("v1.0.0", "program", func(r *release) { r.signature = "checksums.txt.sig" })
	out, err := e.run(false, nil)
	if err == nil || !strings.Contains(out, "download of "+testSignature+" failed") {
		t.Fatalf("install with another signature name: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(e.binary()); statErr == nil {
		t.Error("program installed")
	}
}

func TestShellFailureOnFreshPrefixCreatesNothing(t *testing.T) {
	e := newShEnv(t)
	e.publish("v1.0.0", "program", rejections(t)["wrong checksum"].mutate)
	if out, err := e.run(false, nil); err == nil {
		t.Fatalf("install succeeded:\n%s", out)
	}
	if entries, _ := os.ReadDir(e.home); len(entries) != 0 {
		t.Errorf("home changed: %v", entries)
	}
}

func TestShellRequiresSSHKeygen(t *testing.T) {
	e := newShEnv(t)
	e.publish("v1.0.0", "program", nil)
	// A PATH with every tool the installer uses except ssh-keygen.
	bin := t.TempDir()
	for _, tool := range []string{"sh", "curl", "tar", "gzip", "awk", "grep", "cut", "uname", "mktemp", "mkdir", "chmod", "mv", "rm", "cat", "sha256sum", "shasum", "perl"} {
		if path, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	e.path = bin
	out, err := e.run(false, nil)
	if err == nil || !strings.Contains(out, "ssh-keygen is required") {
		t.Fatalf("install without ssh-keygen: %v\n%s", err, out)
	}
	if entries, _ := os.ReadDir(e.home); len(entries) != 0 {
		t.Errorf("home changed: %v", entries)
	}
}

func TestShellRejectsUnbuiltPlatform(t *testing.T) {
	e := newShEnv(t)
	other := "linux/amd64"
	if hostTarget(t) == other {
		other = "linux/arm64"
	}
	script, err := installer.Shell(testParams(t, e.signer, other))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.script, script, 0o644); err != nil {
		t.Fatal(err)
	}
	e.publish("v1.0.0", "program", nil)
	if out, err := e.run(false, nil); err == nil || !strings.Contains(out, "no release for "+hostTarget(t)) {
		t.Fatalf("install on an unbuilt platform: %v\n%s", err, out)
	}
}

func TestShellPinnedVersions(t *testing.T) {
	e := newShEnv(t)
	e.publish("v1.2.3-rc.1", "release candidate", nil)
	e.publish("v1.0.0", "stable", nil)

	if out, err := e.run(true, nil, "v1.2.3-rc.1"); err != nil {
		t.Fatalf("argument: %v\n%s", err, out)
	}
	if got := readFile(t, e.binary()); got != "release candidate" {
		t.Errorf("binary = %q", got)
	}
	e.mustRun(nil)
	if got := readFile(t, e.binary()); got != "stable" {
		t.Errorf("latest binary = %q", got)
	}
	e.mustRun([]string{testEnv + "_VERSION=v1.2.3-rc.1"})
	if got := readFile(t, e.binary()); got != "release candidate" {
		t.Errorf("version variable binary = %q", got)
	}
	for _, version := range []string{"latest", "v1.2", "v01.2.3", "v1.2.3;id"} {
		if out, err := e.run(false, nil, version); err == nil || !strings.Contains(out, "invalid version") {
			t.Errorf("version %q: %v\n%s", version, err, out)
		}
	}
}

func TestShellPathHandling(t *testing.T) {
	t.Run("already on PATH", func(t *testing.T) {
		e := newShEnv(t)
		e.publish("v1.0.0", "program", nil)
		e.path = filepath.Join(e.prefix, "bin") + string(os.PathListSeparator) + e.path
		if out := e.mustRun(nil); strings.Contains(out, "new terminal") {
			t.Errorf("unexpected PATH hint:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(e.home, ".profile")); !os.IsNotExist(err) {
			t.Errorf("profile written: %v", err)
		}
	})
	t.Run("zsh", func(t *testing.T) {
		e := newShEnv(t)
		e.shell = "/usr/bin/zsh"
		e.publish("v1.0.0", "program", nil)
		e.mustRun(nil)
		e.mustRun(nil)
		if got := readFile(t, filepath.Join(e.home, ".zprofile")); strings.Count(got, "export PATH") != 1 {
			t.Errorf(".zprofile = %q", got)
		}
		if _, err := os.Stat(filepath.Join(e.home, ".profile")); !os.IsNotExist(err) {
			t.Errorf(".profile written: %v", err)
		}
	})
	t.Run("existing profile is appended to", func(t *testing.T) {
		e := newShEnv(t)
		profile := filepath.Join(e.home, ".profile")
		if err := os.WriteFile(profile, []byte("# mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		e.publish("v1.0.0", "program", nil)
		e.mustRun(nil)
		if got := readFile(t, profile); !strings.HasPrefix(got, "# mine\n") || strings.Count(got, "export PATH") != 1 {
			t.Errorf("profile = %q", got)
		}
	})
}

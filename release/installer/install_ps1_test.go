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

// psShells lists the PowerShell flavours available for running install.ps1: Windows PowerShell 5.1 on
// Windows, and PowerShell 7 wherever pwsh is installed.
func psShells() []string {
	var shells []string
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("powershell.exe"); err == nil {
			shells = append(shells, p)
		}
	}
	if p, err := exec.LookPath("pwsh"); err == nil {
		shells = append(shells, p)
	}
	return shells
}

func shellName(shell string) string {
	return strings.TrimSuffix(strings.ToLower(filepath.Base(shell)), ".exe")
}

// psEnv runs a rendered install.ps1 against a local release server with temporary USERPROFILE,
// LOCALAPPDATA and a user PATH stand-in, so the real machine is never touched.
type psEnv struct {
	t        *testing.T
	shell    string
	server   *releasetest.Server
	signer   ssh.Signer
	script   string
	home     string
	prefix   string
	pathFile string
}

func newPSEnv(t *testing.T, shell string) *psEnv {
	t.Helper()
	e := &psEnv{t: t, shell: shell, server: releasetest.NewServer(t), signer: releasetest.NewSigner(t)}
	root := t.TempDir()
	e.home = filepath.Join(root, "home")
	e.prefix = filepath.Join(root, "local", "Programs", testProgram)
	e.pathFile = filepath.Join(root, "user-path.txt")
	if err := os.MkdirAll(e.home, 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := installer.PowerShell(testParams(t, e.signer, "linux/amd64", hostTarget(t)))
	if err != nil {
		t.Fatal(err)
	}
	e.script = filepath.Join(root, "install.ps1")
	if err := os.WriteFile(e.script, script, 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *psEnv) publish(tag, program string, mutate func(*release)) {
	e.t.Helper()
	publish(e.t, e.server, e.signer, tag, archiveName(tag), program, mutate)
}

// run pipes install.ps1 into Invoke-Expression, like `irm ... | iex`, with errors made fatal so a failed
// installation yields a non-zero exit code. env entries are added to the environment. The output comes
// back with all whitespace collapsed to single spaces.
func (e *psEnv) run(env ...string) (string, error) {
	e.t.Helper()
	command := "$ErrorActionPreference = 'Stop'; Get-Content -Raw -LiteralPath '" + e.script + "' | Invoke-Expression"
	cmd := exec.Command(e.shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", command)
	cmd.Env = append(os.Environ(),
		"USERPROFILE="+e.home,
		"HOME="+e.home,
		"LOCALAPPDATA="+filepath.Join(filepath.Dir(filepath.Dir(e.prefix)), "local"),
		"TEMP="+e.t.TempDir(),
		"TMP="+e.t.TempDir(),
		testEnv+"_VERSION=",
		testEnv+"_INSTALL_BASE_URL="+e.server.URL,
		testEnv+"_INSTALL_PREFIX="+e.prefix,
		testEnv+"_INSTALL_USER_PATH_FILE="+e.pathFile,
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	// Windows PowerShell wraps error records at the console width, so words are compared, not lines.
	return strings.Join(strings.Fields(string(out)), " "), err
}

func (e *psEnv) mustRun(env ...string) string {
	e.t.Helper()
	out, err := e.run(env...)
	if err != nil {
		e.t.Fatalf("install.ps1 failed: %v\n%s", err, out)
	}
	return out
}

func (e *psEnv) exe() string { return filepath.Join(e.prefix, "bin", testProgram+".exe") }

func (e *psEnv) license() string {
	return filepath.Join(e.prefix, "share", "doc", testProgram, "LICENSE")
}

// userPathRegistry returns the real HKCU PATH so tests can prove it stays unchanged.
func userPathRegistry() string {
	out, _ := exec.Command("reg", "query", `HKCU\Environment`, "/v", "Path").CombinedOutput()
	return string(out)
}

// forEachShell runs body once per available PowerShell, with ssh-keygen required.
func forEachShell(t *testing.T, body func(t *testing.T, e *psEnv)) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("install.ps1 runs on Windows only")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not installed")
	}
	shells := psShells()
	if len(shells) == 0 {
		t.Skip("no PowerShell found")
	}
	for _, shell := range shells {
		t.Run(shellName(shell), func(t *testing.T) {
			realPath := userPathRegistry()
			body(t, newPSEnv(t, shell))
			if got := userPathRegistry(); got != realPath {
				t.Errorf("the real user PATH changed:\n%s\n-->\n%s", realPath, got)
			}
		})
	}
}

func TestPowerShellSyntax(t *testing.T) {
	shells := psShells()
	if len(shells) == 0 {
		t.Skip("no PowerShell found")
	}
	signer := releasetest.NewSigner(t)
	script, err := installer.PowerShell(testParams(t, signer, "windows/amd64", "windows/arm64"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "install.ps1")
	if err := os.WriteFile(path, script, 0o644); err != nil {
		t.Fatal(err)
	}
	command := "$errs = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('" + path +
		"', [ref]$null, [ref]$errs); if ($errs -and $errs.Count) { $errs | ForEach-Object { $_.ToString() }; exit 1 }"
	for _, shell := range shells {
		t.Run(shellName(shell), func(t *testing.T) {
			if out, err := exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput(); err != nil {
				t.Fatalf("install.ps1 does not parse: %v\n%s", err, out)
			}
		})
	}
}

func TestPowerShellFreshAndReinstall(t *testing.T) {
	forEachShell(t, func(t *testing.T, e *psEnv) {
		e.publish("v1.0.0", "program one", nil)
		out := e.mustRun()
		for _, want := range []string{"Sample Tool v1.0.0", e.exe(), "new terminal", testNextStep} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
		if got := readFile(t, e.exe()); got != "program one" {
			t.Errorf("binary = %q", got)
		}
		if got := readFile(t, e.license()); got != "license v1.0.0\n" {
			t.Errorf("license = %q", got)
		}
		binDir := filepath.Join(e.prefix, "bin")
		if got := readFile(t, e.pathFile); got != binDir {
			t.Errorf("user PATH = %q, want %q", got, binDir)
		}

		e.publish("v1.0.1", "program two", nil)
		out = e.mustRun()
		if !strings.Contains(out, "v1.0.1") || strings.Contains(out, "new terminal") {
			t.Errorf("reinstall output:\n%s", out)
		}
		if got := readFile(t, e.exe()); got != "program two" {
			t.Errorf("binary after reinstall = %q", got)
		}
		if got := readFile(t, e.exe()+".old"); got != "program one" {
			t.Errorf(".old = %q", got)
		}
		if got := readFile(t, e.license()); got != "license v1.0.1\n" {
			t.Errorf("license after reinstall = %q", got)
		}

		// A third run replaces an existing .old as well.
		e.publish("v1.0.2", "program three", nil)
		e.mustRun()
		if got := readFile(t, e.exe()+".old"); got != "program two" {
			t.Errorf(".old after third run = %q", got)
		}
		if left := stageLeftovers(e.prefix); len(left) != 0 {
			t.Errorf("staging left behind: %v", left)
		}
	})
}

func TestPowerShellPathHandling(t *testing.T) {
	forEachShell(t, func(t *testing.T, e *psEnv) {
		binDir := filepath.Join(e.prefix, "bin")
		if err := os.WriteFile(e.pathFile, []byte(`C:\Tools;`), 0o644); err != nil {
			t.Fatal(err)
		}
		e.publish("v1.0.0", "program", nil)
		e.mustRun()
		if got := readFile(t, e.pathFile); got != `C:\Tools;`+binDir {
			t.Errorf("user PATH = %q", got)
		}
		// Already present, in a different case and with a trailing backslash.
		if err := os.WriteFile(e.pathFile, []byte(strings.ToUpper(binDir)+`\;C:\Tools`), 0o644); err != nil {
			t.Fatal(err)
		}
		out := e.mustRun()
		if got := readFile(t, e.pathFile); got != strings.ToUpper(binDir)+`\;C:\Tools` {
			t.Errorf("user PATH changed to %q", got)
		}
		if strings.Contains(out, "new terminal") {
			t.Errorf("unexpected PATH hint:\n%s", out)
		}
		// Unexpanded entries survive, and an entry that only expands to the directory counts as present.
		raw := `%USERPROFILE%\tools;C:\Tools`
		if err := os.WriteFile(e.pathFile, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		e.mustRun()
		if got := readFile(t, e.pathFile); got != raw+";"+binDir {
			t.Errorf("user PATH = %q", got)
		}
		if err := os.WriteFile(e.pathFile, []byte(`%SAMPLE_TEST_BIN%\`), 0o644); err != nil {
			t.Fatal(err)
		}
		out = e.mustRun("SAMPLE_TEST_BIN=" + binDir)
		if got := readFile(t, e.pathFile); got != `%SAMPLE_TEST_BIN%\` || strings.Contains(out, "new terminal") {
			t.Errorf("user PATH = %q, output:\n%s", got, out)
		}
	})
}

func TestPowerShellRejectsBadReleaseWithoutChange(t *testing.T) {
	for name, tc := range rejections(t) {
		t.Run(name, func(t *testing.T) {
			forEachShell(t, func(t *testing.T, e *psEnv) {
				e.publish("v1.0.0", "old program", nil)
				e.mustRun()
				pathBefore := readFile(t, e.pathFile)

				e.publish("v1.0.1", "new program", tc.mutate)
				out, err := e.run()
				if err == nil {
					t.Fatalf("install succeeded:\n%s", out)
				}
				if !strings.Contains(out, tc.message) || !strings.Contains(out, "nothing was installed") {
					t.Errorf("output lacks %q:\n%s", tc.message, out)
				}
				if got := readFile(t, e.exe()); got != "old program" {
					t.Errorf("binary changed to %q", got)
				}
				if _, err := os.Stat(e.exe() + ".old"); !os.IsNotExist(err) {
					t.Errorf(".old exists: %v", err)
				}
				if got := readFile(t, e.license()); got != "license v1.0.0\n" {
					t.Errorf("license changed to %q", got)
				}
				if got := readFile(t, e.pathFile); got != pathBefore {
					t.Errorf("user PATH changed to %q", got)
				}
				if left := stageLeftovers(e.prefix); len(left) != 0 {
					t.Errorf("staging left behind: %v", left)
				}
			})
		})
	}
}

func TestPowerShellFailureOnFreshPrefixCreatesNothing(t *testing.T) {
	forEachShell(t, func(t *testing.T, e *psEnv) {
		e.publish("v1.0.0", "program", rejections(t)["wrong checksum"].mutate)
		if out, err := e.run(); err == nil {
			t.Fatalf("install succeeded:\n%s", out)
		}
		for _, path := range []string{e.prefix, e.pathFile, filepath.Dir(e.prefix)} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("%s exists: %v", path, err)
			}
		}
	})
}

func TestPowerShellRequiresSSHKeygen(t *testing.T) {
	forEachShell(t, func(t *testing.T, e *psEnv) {
		e.publish("v1.0.0", "program", nil)
		// Windows itself without the OpenSSH folder or any other tool directory.
		system := filepath.Join(os.Getenv("SystemRoot"), "System32")
		path := strings.Join([]string{system, os.Getenv("SystemRoot"), filepath.Join(system, "WindowsPowerShell", "v1.0")}, string(os.PathListSeparator))
		out, err := e.run("PATH="+path, "Path="+path)
		if err == nil || !strings.Contains(out, "ssh-keygen is required") {
			t.Fatalf("install without ssh-keygen: %v\n%s", err, out)
		}
		if _, err := os.Stat(e.prefix); !os.IsNotExist(err) {
			t.Errorf("prefix exists: %v", err)
		}
	})
}

func TestPowerShellPinnedVersions(t *testing.T) {
	forEachShell(t, func(t *testing.T, e *psEnv) {
		e.publish("v1.2.3-rc.1", "release candidate", nil)
		e.publish("v1.0.0", "stable", nil)

		e.mustRun()
		if got := readFile(t, e.exe()); got != "stable" {
			t.Errorf("latest binary = %q", got)
		}
		e.mustRun(testEnv + "_VERSION=v1.2.3-rc.1")
		if got := readFile(t, e.exe()); got != "release candidate" {
			t.Errorf("version variable binary = %q", got)
		}
		if out, err := e.run(testEnv + "_VERSION=latest"); err == nil || !strings.Contains(out, "invalid version") {
			t.Errorf("invalid version: %v\n%s", err, out)
		}
	})
}

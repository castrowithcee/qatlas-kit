//go:build linux

package installer_test

import (
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/installer"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

// systemStubs stand in for the account and service commands. Commands that would change the host only
// append their arguments to $STUB_LOG; the fake account database lives in $STUB_STATE as group.<name>,
// user.<name> and groups.<user> (one group per line), so the tests never touch the host.
var systemStubs = map[string]string{
	"id": `if [ "$#" = 1 ] && [ "$1" = -u ]; then echo "$STUB_UID"; exit 0; fi
if [ "$#" = 2 ] && [ "$1" = -nG ]; then
	[ -f "$STUB_STATE/user.$2" ] || exit 1
	tr '\n' ' ' <"$STUB_STATE/groups.$2"; echo; exit 0
fi
[ "$#" = 1 ] && [ -f "$STUB_STATE/user.$1" ]`,
	"getent": `[ "$#" = 2 ] && [ "$1" = group ] && [ -f "$STUB_STATE/group.$2" ] || exit 2
echo "$2:x:990:"`,
	"groupadd": `echo "groupadd $*" >>"$STUB_LOG"
for a; do name=$a; done
touch "$STUB_STATE/group.$name"`,
	"useradd": `echo "useradd $*" >>"$STUB_LOG"
for a; do name=$a; done
touch "$STUB_STATE/user.$name"
echo "$name" >"$STUB_STATE/groups.$name"`,
	"usermod": `echo "usermod $*" >>"$STUB_LOG"
[ "$#" = 3 ] && [ "$1" = -aG ] || exit 2
echo "$2" >>"$STUB_STATE/groups.$3"`,
	"chown":     `echo "chown $*" >>"$STUB_LOG"`,
	"systemctl": `echo "systemctl $*" >>"$STUB_LOG"`,
}

const installingUser = "alice"

var serviceGroups = []string{"sample-data", "sample-dev"}

// sysEnv runs a rendered system install.sh against a local release server, a test root and stubs.
type sysEnv struct {
	t        *testing.T
	server   *releasetest.Server
	signer   ssh.Signer
	service  installer.Service
	script   string
	root     string
	stubs    string
	state    string
	log      string
	tmp      string
	path     string
	uid      string
	sudoUser string
}

func newSysEnv(t *testing.T, service installer.Service) *sysEnv {
	t.Helper()
	hostTarget(t)
	for _, tool := range []string{"sh", "curl", "tar", "gzip", "ssh-keygen"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	dir := t.TempDir()
	e := &sysEnv{t: t, server: releasetest.NewServer(t), signer: releasetest.NewSigner(t), service: service,
		root: filepath.Join(dir, "root"), stubs: filepath.Join(dir, "stubs"), state: filepath.Join(dir, "state"),
		log: filepath.Join(dir, "calls.log"), tmp: filepath.Join(dir, "tmp"), script: filepath.Join(dir, "install.sh"),
		path: os.Getenv("PATH"), uid: "0", sudoUser: installingUser}
	for _, d := range []string{filepath.Join(e.root, "run/systemd/system"), e.stubs, e.state, e.tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range systemStubs {
		writeFile(t, filepath.Join(e.stubs, name), "#!/bin/sh\n"+body+"\n", 0o755)
	}
	writeFile(t, e.log, "", 0o644)
	for _, g := range serviceGroups {
		writeFile(t, filepath.Join(e.state, "group."+g), "", 0o644)
	}
	writeFile(t, filepath.Join(e.state, "user."+installingUser), "", 0o644)
	writeFile(t, filepath.Join(e.state, "groups."+installingUser), installingUser+"\n", 0o644)
	e.render(hostTarget(t))
	return e
}

func (e *sysEnv) render(targets ...string) {
	e.t.Helper()
	p := testParams(e.t, e.signer, targets...)
	service := e.service
	p.Service = &service
	script, err := installer.Shell(p)
	if err != nil {
		e.t.Fatal(err)
	}
	writeFile(e.t, e.script, string(script), 0o644)
}

// publish adds tag with the unit of the service unless withoutUnit is set.
func (e *sysEnv) publish(tag, program string, mutate func(*release), withoutUnit bool) {
	e.t.Helper()
	var extra []releasetest.File
	if !withoutUnit {
		extra = append(extra, releasetest.File{Name: "lib/systemd/system/" + testProgram + ".service", Body: []byte(unitText(tag))})
	}
	publish(e.t, e.server, e.signer, tag, archiveName(tag), program, mutate, extra...)
}

func unitText(tag string) string {
	return "[Unit]\nDescription=sample service " + tag + "\n"
}

// run executes install.sh with a clean environment that always sets the test root and the stubs.
func (e *sysEnv) run(stdinScript bool, args ...string) (string, error) {
	e.t.Helper()
	if !filepath.IsAbs(e.root) || e.root == "/" {
		e.t.Fatalf("invalid test root %q", e.root)
	}
	env := []string{
		"PATH=" + e.stubs + string(os.PathListSeparator) + e.path,
		"HOME=" + filepath.Join(e.tmp, "home"),
		"TMPDIR=" + e.tmp,
		testEnv + "_INSTALL_BASE_URL=" + e.server.URL,
		testEnv + "_INSTALL_ROOT=" + e.root,
		"STUB_LOG=" + e.log,
		"STUB_STATE=" + e.state,
		"STUB_UID=" + e.uid,
	}
	if e.sudoUser != "" {
		env = append(env, "SUDO_USER="+e.sudoUser)
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
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if left, _ := os.ReadDir(e.tmp); len(left) != 0 {
		e.t.Errorf("temporary files left behind: %v", left)
	}
	return string(out), err
}

func (e *sysEnv) mustRun(args ...string) string {
	e.t.Helper()
	out, err := e.run(false, args...)
	if err != nil {
		e.t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	return out
}

// calls returns the logged stub calls with the random suffix of staged files removed.
func (e *sysEnv) calls() []string {
	e.t.Helper()
	stage := regexp.MustCompile(`\.` + testProgram + `-stage\.[A-Za-z0-9]+$`)
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(readFile(e.t, e.log), "\n"), "\n") {
		if line != "" {
			out = append(out, stage.ReplaceAllString(strings.ReplaceAll(line, e.root, "<root>"), ".stage"))
		}
	}
	return out
}

func (e *sysEnv) at(p string) string { return filepath.Join(e.root, p) }

// snapshot records every path below dir with its type, mode and content or link target.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		entry := info.Mode().String()
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry += " -> " + target
		case info.Mode().IsRegular():
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry += " " + string(body)
		}
		out[path] = entry
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func checkFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		t.Errorf("%s: mode %v, want regular %v", path, info.Mode(), mode)
	}
	if got := readFile(t, path); got != body {
		t.Errorf("%s = %q, want %q", path, got, body)
	}
}

func checkNoStaging(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{"usr/local/bin", "usr/local/lib/systemd/system", "usr/local/share/man/man1", "usr/local/share/doc/" + testProgram} {
		if left, _ := filepath.Glob(filepath.Join(root, dir, "."+testProgram+"-stage.*")); len(left) != 0 {
			t.Errorf("staged files left behind: %v", left)
		}
	}
}

func (e *sysEnv) checkInstalled(tag, program string) {
	e.t.Helper()
	checkFile(e.t, e.at("usr/local/bin/"+testProgram), program, 0o755)
	checkFile(e.t, e.at("usr/local/lib/systemd/system/"+testProgram+".service"), unitText(tag), 0o644)
	checkFile(e.t, e.at("usr/local/share/man/man1/"+testProgram+".1"), ".TH SAMPLE 1 "+tag+"\n", 0o644)
	checkFile(e.t, e.at("usr/local/share/doc/"+testProgram+"/LICENSE"), "license "+tag+"\n", 0o644)
	checkNoStaging(e.t, e.root)
}

// fileCalls are the stub calls that place unit, manpage, license and program, in this order.
var fileCalls = []string{
	"chown root:root <root>/usr/local/lib/systemd/system/.stage",
	"chown root:root <root>/usr/local/share/man/man1/.stage",
	"chown root:root <root>/usr/local/share/doc/" + testProgram + "/.stage",
	"chown root:root <root>/usr/local/bin/.stage",
}

func TestSystemFreshInstall(t *testing.T) {
	e := newSysEnv(t, installer.Service{Groups: serviceGroups, AddInstallingUser: true})
	writeFile(t, e.at("sbin/nologin"), "", 0o755)
	e.publish("v1.0.0", "program one", nil, false)

	out := e.mustRun()
	want := append([]string{
		"groupadd --system " + testProgram,
		"useradd --system --gid " + testProgram + " --no-create-home --home-dir /var/lib/" + testProgram + " --shell /sbin/nologin " + testProgram,
		"usermod -aG sample-data " + testProgram,
		"usermod -aG sample-dev " + testProgram,
		"usermod -aG " + testProgram + " " + installingUser,
		"chown root:" + testProgram + " <root>/etc/" + testProgram,
	}, fileCalls...)
	want = append(want, "systemctl daemon-reload", "systemctl enable --now "+testProgram)
	if got := e.calls(); !slices.Equal(got, want) {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	e.checkInstalled("v1.0.0", "program one")
	config, err := os.Lstat(e.at("etc/" + testProgram))
	if err != nil || !config.IsDir() || config.Mode().Perm() != 0o750 {
		t.Errorf("config directory: %v, %v", config, err)
	}
	for _, want := range []string{
		"Sample Tool v1.0.0", "/usr/local/bin/" + testProgram, "/usr/local/lib/systemd/system/" + testProgram + ".service",
		"/etc/" + testProgram, "/var/lib/" + testProgram, "user:    " + testProgram, "Added " + installingUser, testNextStep,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "warning") {
		t.Errorf("unexpected warning:\n%s", out)
	}
}

func TestSystemUpdate(t *testing.T) {
	e := newSysEnv(t, installer.Service{Groups: serviceGroups, AddInstallingUser: true, WritableConfig: true})
	e.publish("v1.0.0", "program one", nil, false)
	e.mustRun()
	if !strings.Contains(strings.Join(e.calls(), "\n"), "--shell /bin/false") {
		t.Errorf("useradd without a nologin shell does not use /bin/false:\n%s", strings.Join(e.calls(), "\n"))
	}

	// The administrator owns /etc: a drop-in, a full unit that hides the installed one and the config.
	writeFile(t, e.at("etc/systemd/system/"+testProgram+".service.d/override.conf"), "[Service]\nEnvironment=SAMPLE=1\n", 0o644)
	writeFile(t, e.at("etc/systemd/system/"+testProgram+".service"), "[Unit]\nDescription=local copy\n", 0o644)
	writeFile(t, e.at("etc/"+testProgram+"/config.toml"), "sample = true\n", 0o600)
	if err := os.Chmod(e.at("etc/"+testProgram), 0o700); err != nil {
		t.Fatal(err)
	}
	// The service user has lost one supplementary group since the first install.
	writeFile(t, filepath.Join(e.state, "groups."+testProgram), testProgram+"\nsample-data\n", 0o644)
	etc := snapshot(t, e.at("etc"))
	before := len(e.calls())

	e.publish("v1.0.1", "program two", nil, false)
	out, err := e.run(true, "v1.0.1")
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	want := append([]string{"usermod -aG sample-dev " + testProgram}, fileCalls...)
	want = append(want, "systemctl daemon-reload", "systemctl enable "+testProgram, "systemctl restart "+testProgram)
	if got := e.calls()[before:]; !slices.Equal(got, want) {
		t.Errorf("update calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	e.checkInstalled("v1.0.1", "program two")
	if got := snapshot(t, e.at("etc")); !maps.Equal(got, etc) {
		t.Errorf("/etc changed:\n%v\nwant:\n%v", got, etc)
	}
	if !strings.Contains(out, "warning: /etc/systemd/system/"+testProgram+".service overrides the installed unit") {
		t.Errorf("output lacks the override warning:\n%s", out)
	}
	if strings.Contains(out, "Added ") || strings.Contains(out, "No user was added") {
		t.Errorf("update reports a group membership:\n%s", out)
	}
}

func TestSystemInstallingUser(t *testing.T) {
	tests := map[string]struct {
		add      bool
		sudoUser string
		added    bool
		hint     bool
	}{
		"on":                   {add: true, sudoUser: installingUser, added: true},
		"off":                  {add: false, sudoUser: installingUser},
		"on without SUDO_USER": {add: true, hint: true},
		"on with root":         {add: true, sudoUser: "root", hint: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			e := newSysEnv(t, installer.Service{AddInstallingUser: tc.add})
			e.sudoUser = tc.sudoUser
			e.publish("v1.0.0", "program", nil, false)
			out := e.mustRun()
			calls := strings.Join(e.calls(), "\n")
			if got := strings.Contains(calls, "usermod -aG "+testProgram+" "); got != tc.added {
				t.Errorf("installing user added = %v, want %v:\n%s", got, tc.added, calls)
			}
			if got := strings.Contains(out, "No user was added to the group "+testProgram); got != tc.hint {
				t.Errorf("hint shown = %v, want %v:\n%s", got, tc.hint, out)
			}
			if strings.Contains(calls, "usermod -aG sample-d") {
				t.Errorf("supplementary group added without being requested:\n%s", calls)
			}
		})
	}
}

func TestSystemWritableConfig(t *testing.T) {
	for _, writable := range []bool{false, true} {
		e := newSysEnv(t, installer.Service{WritableConfig: writable})
		e.publish("v1.0.0", "program", nil, false)
		e.mustRun()
		owner := "root"
		if writable {
			owner = testProgram
		}
		want := "chown " + owner + ":" + testProgram + " <root>/etc/" + testProgram
		if calls := e.calls(); !slices.Contains(calls, want) {
			t.Errorf("writable %v: calls lack %q:\n%s", writable, want, strings.Join(calls, "\n"))
		}
	}
}

func TestSystemPinnedVersion(t *testing.T) {
	e := newSysEnv(t, installer.Service{})
	e.publish("v1.2.3-rc.1", "release candidate", nil, false)
	e.publish("v1.0.0", "stable", nil, false)
	if out, err := e.run(true, "v1.2.3-rc.1"); err != nil {
		t.Fatalf("argument: %v\n%s", err, out)
	}
	checkFile(t, e.at("usr/local/bin/"+testProgram), "release candidate", 0o755)
	e.mustRun()
	checkFile(t, e.at("usr/local/bin/"+testProgram), "stable", 0o755)
	for _, version := range []string{"latest", "v1.2", "v1.2.3;id"} {
		if out, err := e.run(false, version); err == nil || !strings.Contains(out, "invalid version") {
			t.Errorf("version %q: %v\n%s", version, err, out)
		}
	}
}

func TestSystemAbortsWithoutChange(t *testing.T) {
	type abort struct {
		setup     func(e *sysEnv)
		mutate    func(*release)
		noUnit    bool
		installed bool // run over an existing installation instead of a fresh host
		message   string
	}
	cases := map[string]abort{
		"not root":      {setup: func(e *sysEnv) { e.uid = "1000" }, message: "must run as root"},
		"no systemd":    {setup: func(e *sysEnv) { removeAll(e.t, e.at("run")) }, message: "systemd is not running"},
		"missing group": {setup: func(e *sysEnv) { removeAll(e.t, filepath.Join(e.state, "group.sample-dev")) }, message: "group sample-dev does not exist"},
		"no ssh-keygen": {setup: func(e *sysEnv) { e.path = toolsWithout(e.t, "ssh-keygen") }, message: "ssh-keygen is required"},
		"unbuilt architecture": {setup: func(e *sysEnv) {
			other := "linux/amd64"
			if hostTarget(e.t) == other {
				other = "linux/arm64"
			}
			e.render(other)
		}, message: "no release for " + hostTarget(t)},
		"unknown installing user": {setup: func(e *sysEnv) { e.sudoUser = "mallory" }, message: "mallory from SUDO_USER does not exist"},
		"invalid installing user": {setup: func(e *sysEnv) { e.sudoUser = "-o" }, message: "invalid user name"},
		"config is a file": {setup: func(e *sysEnv) {
			writeFile(e.t, e.at("etc/"+testProgram), "", 0o644)
		}, message: "is not a directory"},
	}
	for name, r := range rejections(t) {
		cases[name] = abort{mutate: r.mutate, message: r.message}
		cases[name+" over installation"] = abort{mutate: r.mutate, message: r.message, installed: true}
	}
	cases["archive without unit"] = abort{noUnit: true, message: "does not contain lib/systemd/system/" + testProgram + ".service"}
	cases["archive without unit over installation"] = abort{noUnit: true, installed: true, message: "does not contain lib/systemd/system/"}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSysEnv(t, installer.Service{Groups: serviceGroups, AddInstallingUser: true})
			if tc.installed {
				e.publish("v1.0.0", "old program", nil, false)
				e.mustRun()
			}
			e.publish("v1.0.1", "new program", tc.mutate, tc.noUnit)
			if tc.setup != nil {
				tc.setup(e)
			}
			root, log, state := snapshot(t, e.root), readFile(t, e.log), snapshot(t, e.state)

			out, err := e.run(false)
			if err == nil {
				t.Fatalf("install succeeded:\n%s", out)
			}
			if !strings.Contains(out, testProgram+"-install: ") || !strings.Contains(out, tc.message) {
				t.Errorf("output lacks %q:\n%s", tc.message, out)
			}
			if got := snapshot(t, e.root); !maps.Equal(got, root) {
				t.Errorf("test root changed:\n%v\nwant:\n%v", got, root)
			}
			if got := readFile(t, e.log); got != log {
				t.Errorf("stub log changed:\n%s", got)
			}
			if got := snapshot(t, e.state); !maps.Equal(got, state) {
				t.Errorf("account database changed:\n%v", got)
			}
		})
	}
}

func removeAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

// toolsWithout returns a PATH directory with every tool the installer and the stubs use except skip.
func toolsWithout(t *testing.T, skip string) string {
	t.Helper()
	bin := t.TempDir()
	for _, tool := range []string{"sh", "curl", "tar", "gzip", "awk", "grep", "cut", "tr", "cp", "mv", "rm", "mkdir", "mktemp",
		"chmod", "uname", "cat", "touch", "sha256sum", "shasum", "perl", "ssh-keygen"} {
		if tool == skip {
			continue
		}
		if path, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return bin
}

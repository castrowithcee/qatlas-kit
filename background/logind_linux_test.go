//go:build linux

package background

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogoutBehavior(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check := func(name string, want LogoutOutcome) {
		t.Helper()
		if got := logoutBehavior(root, name); got != want {
			t.Fatalf("logoutBehavior(%q) = %v, want %v", name, got, want)
		}
	}

	check("alex", LogoutUnknown) // no systemd
	write("run/systemd/system/.keep", "")
	check("alex", LogoutSpares) // logind default
	write("etc/systemd/logind.conf", "[Login]\n#KillUserProcesses=yes\n")
	check("alex", LogoutSpares)
	write("usr/lib/systemd/logind.conf.d/10-kill.conf", "[Login]\nKillUserProcesses=yes\n")
	check("alex", LogoutKills)
	check("", LogoutKills)
	write("etc/systemd/logind.conf.d/20-exclude.conf", "[Login]\nKillExcludeUsers=root alex\n")
	check("alex", LogoutSpares)
	check("sam", LogoutKills)
	write("etc/systemd/logind.conf.d/20-exclude.conf", "[Other]\nKillExcludeUsers=alex\n")
	check("alex", LogoutKills)
	write("etc/systemd/logind.conf.d/30-only.conf", "[Login]\nKillOnlyUsers=sam\n")
	check("alex", LogoutSpares)
	check("sam", LogoutKills)
	write("etc/systemd/logind.conf.d/10-kill.conf", "[Login]\nKillUserProcesses=no\n") // replaces the /usr drop-in
	check("sam", LogoutSpares)
	write("etc/systemd/logind.conf.d/10-kill.conf", "[Login]\nKillUserProcesses=maybe\n")
	check("sam", LogoutUnknown)
}

func TestLogoutBehaviorIsTriState(t *testing.T) {
	switch LogoutBehavior() {
	case LogoutUnknown, LogoutKills, LogoutSpares:
	default:
		t.Fatal("not a LogoutOutcome")
	}
}

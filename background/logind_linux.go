//go:build linux

package background

import (
	"bufio"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// LogoutBehavior reports whether systemd-logind would end a background process when the user's session
// closes (KillUserProcesses=yes, minus the user exceptions). It only reads the files logind reads. It is
// LogoutUnknown without systemd or with a setting it cannot parse. It does not cover a process started
// from a service unit.
func LogoutBehavior() LogoutOutcome {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return logoutBehavior("/", name)
}

func logoutBehavior(root, name string) LogoutOutcome {
	if _, err := os.Stat(filepath.Join(root, "run", "systemd", "system")); err != nil {
		return LogoutUnknown
	}
	settings := map[string]string{}
	for _, path := range logindFiles(root) {
		readLogindFile(path, settings)
	}
	kill, ok := parseSystemdBool(settings["KillUserProcesses"])
	switch {
	case settings["KillUserProcesses"] == "":
		return LogoutSpares // logind's default
	case !ok:
		return LogoutUnknown
	case !kill:
		return LogoutSpares
	case name == "":
		return LogoutKills
	case slices.Contains(strings.Fields(settings["KillExcludeUsers"]), name):
		return LogoutSpares
	}
	if only := strings.Fields(settings["KillOnlyUsers"]); len(only) > 0 && !slices.Contains(only, name) {
		return LogoutSpares
	}
	return LogoutKills
}

// logindFiles lists logind.conf and its drop-ins in the order systemd reads them: the main files, then the
// drop-ins sorted by name, where a drop-in in a later directory replaces one of the same name.
func logindFiles(root string) []string {
	var files []string
	for _, main := range []string{"usr/lib/systemd/logind.conf", "etc/systemd/logind.conf"} {
		files = append(files, filepath.Join(root, main))
	}
	dropIns := map[string]string{}
	for _, dir := range []string{"usr/lib/systemd/logind.conf.d", "usr/local/lib/systemd/logind.conf.d",
		"run/systemd/logind.conf.d", "etc/systemd/logind.conf.d"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".conf") {
				dropIns[entry.Name()] = filepath.Join(root, dir, entry.Name())
			}
		}
	}
	names := make([]string, 0, len(dropIns))
	for name := range dropIns {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		files = append(files, dropIns[name])
	}
	return files
}

// readLogindFile adds the [Login] settings of one file to settings. A file that cannot be read adds none.
func readLogindFile(path string, settings map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "", strings.HasPrefix(line, "#"), strings.HasPrefix(line, ";"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = line
		case section == "[Login]":
			if key, value, ok := strings.Cut(line, "="); ok {
				settings[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	}
}

func parseSystemdBool(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "1", "yes", "y", "true", "t", "on":
		return true, true
	case "0", "no", "n", "false", "f", "off":
		return false, true
	}
	return false, false
}

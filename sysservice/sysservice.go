package sysservice

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

var (
	// The service user name is the program name, so the program name must also be a valid system user name.
	programPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	groupPattern   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	forbiddenGroup = []string{"root", "sudo", "wheel", "admin"}
	execKeys       = []string{"ExecStart", "ExecStartPre", "ExecStartPost", "ExecReload", "ExecStop", "ExecStopPost", "ExecCondition"}
)

// Paths are the absolute POSIX installation paths of a system service.
type Paths struct {
	Program string // executable
	Unit    string // systemd unit file
	Config  string // configuration directory
	State   string // state directory
}

// UnitRules selects what ValidateUnit checks the unit against.
type UnitRules struct {
	Program        string   // program and service user name
	Groups         []string // exact set of SupplementaryGroups
	WritableConfig bool     // ReadWritePaths must release the configuration directory
}

// UnitEntry returns the archive path of the unit of program.
func UnitEntry(program string) string {
	return path.Join("lib/systemd/system", program+".service")
}

// InstallPaths returns the installation paths of program. It does not validate program.
func InstallPaths(program string) Paths {
	return Paths{
		Program: path.Join("/usr/local/bin", program),
		Unit:    path.Join("/usr/local/lib/systemd/system", program+".service"),
		Config:  path.Join("/etc", program),
		State:   path.Join("/var/lib", program),
	}
}

// ValidateGroups checks the supplementary groups of the service user of program.
func ValidateGroups(program string, groups []string) error {
	if err := validateProgram(program); err != nil {
		return err
	}
	var errs []error
	seen := map[string]bool{}
	for _, g := range groups {
		switch {
		case !groupPattern.MatchString(g):
			errs = append(errs, fmt.Errorf("group %q must match %s", g, groupPattern))
		case g == program:
			errs = append(errs, fmt.Errorf("group %q is the service group itself", g))
		case slices.Contains(forbiddenGroup, g):
			errs = append(errs, fmt.Errorf("group %q is privileged and not allowed", g))
		case seen[g]:
			errs = append(errs, fmt.Errorf("group %q is listed twice", g))
		}
		seen[g] = true
	}
	return errors.Join(errs...)
}

func validateProgram(program string) error {
	if !programPattern.MatchString(program) {
		return fmt.Errorf("program name %q must match %s", program, programPattern)
	}
	return nil
}

// ValidateUnit checks unit against rules. It validates the program name and groups first and then reports
// every violated unit rule.
func ValidateUnit(unit []byte, rules UnitRules) error {
	if err := validateProgram(rules.Program); err != nil {
		return err
	}
	if err := ValidateGroups(rules.Program, rules.Groups); err != nil {
		return err
	}
	u, err := parseUnit(string(unit))
	if err != nil {
		return err
	}
	if !u.hasService {
		return errors.New("unit has no [Service] section")
	}
	p := InstallPaths(rules.Program)
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if v := u.last("User"); v != rules.Program {
		fail("User must be %q, is %q", rules.Program, v)
	}
	if v := u.last("Group"); v != rules.Program {
		fail("Group must be %q, is %q", rules.Program, v)
	}
	if v := u.last("NoNewPrivileges"); !isTrue(v) {
		fail("NoNewPrivileges must be yes, is %q", v)
	}
	if v := u.last("PrivateTmp"); !isTrue(v) {
		fail("PrivateTmp must be yes, is %q", v)
	}
	if v := u.last("ProtectSystem"); v != "full" && v != "strict" {
		fail("ProtectSystem must be full or strict, is %q", v)
	}
	for _, key := range []string{"ConfigurationDirectory", "StateDirectory"} {
		if w := u.words(key); !slices.Equal(w, []string{rules.Program}) {
			fail("%s must be exactly %q, is %q", key, rules.Program, strings.Join(w, " "))
		}
	}
	starts := u.list("ExecStart")
	if len(starts) == 0 {
		fail("ExecStart is missing")
	}
	for _, s := range starts {
		if !startsWithWord(s, p.Program) {
			fail("ExecStart must begin with %s, is %q", p.Program, s)
		}
	}
	got := u.words("SupplementaryGroups")
	if !sameSet(got, rules.Groups) {
		fail("SupplementaryGroups must be exactly %q, is %q", strings.Join(rules.Groups, " "), strings.Join(got, " "))
	}
	if rules.WritableConfig && !slices.Contains(u.words("ReadWritePaths"), p.Config) {
		fail("ReadWritePaths must release %s", p.Config)
	}

	for _, e := range u.entries {
		switch e.key {
		case "User", "Group":
			if isRoot(e.value) {
				fail("%s must not be root or ID 0 (section [%s] line %d)", e.key, e.section, e.line)
			}
		case "DynamicUser", "PermissionsStartOnly":
			// Anything but an explicit false or a reset is rejected, so an unparsable value cannot hide a true one.
			if e.value != "" && !isFalse(e.value) {
				fail("%s is not allowed (section [%s] line %d)", e.key, e.section, e.line)
			}
		case "AmbientCapabilities":
			if e.value != "" {
				fail("AmbientCapabilities is not allowed (section [%s] line %d)", e.section, e.line)
			}
		}
		if slices.Contains(execKeys, e.key) && hasPrivilegedPrefix(e.value) {
			fail("%s must not use the + or ! prefix (section [%s] line %d)", e.key, e.section, e.line)
		}
	}
	return errors.Join(errs...)
}

type entry struct {
	section, key, value string
	line                int
}

type unit struct {
	entries    []entry
	hasService bool
}

func parseUnit(text string) (*unit, error) {
	u := &unit{}
	var logical strings.Builder
	var section string
	continued := false
	startLine := 0
	flush := func() error {
		line := strings.TrimSpace(logical.String())
		logical.Reset()
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") || len(line) < 3 {
				return fmt.Errorf("line %d: malformed section header %q", startLine, line)
			}
			section = line[1 : len(line)-1]
			if section == "Service" {
				u.hasService = true
			}
			return nil
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return fmt.Errorf("line %d: not a key=value assignment", startLine)
		}
		if section == "" {
			return fmt.Errorf("line %d: assignment outside a section", startLine)
		}
		u.entries = append(u.entries, entry{section: section, key: key, value: strings.TrimSpace(value), line: startLine})
		return nil
	}
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || line[0] == '#' || line[0] == ';' {
			// An empty line ends a continuation; a comment inside one is skipped.
			if continued && line == "" {
				continued = false
				if err := flush(); err != nil {
					return nil, err
				}
			}
			continue
		}
		if !continued {
			startLine = i + 1
		}
		if strings.HasSuffix(line, `\`) {
			logical.WriteString(strings.TrimSuffix(line, `\`) + " ")
			continued = true
			continue
		}
		logical.WriteString(line)
		continued = false
		if err := flush(); err != nil {
			return nil, err
		}
	}
	if continued {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// list returns the effective values of a list-valued key of [Service]: an empty assignment resets it.
func (u *unit) list(key string) []string {
	var out []string
	for _, e := range u.entries {
		if e.section != "Service" || e.key != key {
			continue
		}
		if e.value == "" {
			out = nil
			continue
		}
		out = append(out, e.value)
	}
	return out
}

func (u *unit) words(key string) []string {
	var out []string
	for _, v := range u.list(key) {
		out = append(out, strings.Fields(v)...)
	}
	return out
}

// last returns the effective value of a single-valued key of [Service]: the last assignment wins.
func (u *unit) last(key string) string {
	v := ""
	for _, e := range u.entries {
		if e.section == "Service" && e.key == key {
			v = e.value
		}
	}
	return v
}

func isTrue(v string) bool {
	switch strings.ToLower(v) {
	case "1", "yes", "true", "on":
		return true
	}
	return false
}

func isFalse(v string) bool {
	switch strings.ToLower(v) {
	case "0", "no", "false", "off":
		return true
	}
	return false
}

func isRoot(v string) bool {
	if v == "root" {
		return true
	}
	return v != "" && strings.Trim(v, "0") == ""
}

func startsWithWord(s, prefix string) bool {
	rest, ok := strings.CutPrefix(s, prefix)
	return ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t')
}

// hasPrivilegedPrefix reports whether an Exec line carries + or ! among its combinable prefix characters.
func hasPrivilegedPrefix(v string) bool {
	for _, c := range v {
		switch c {
		case '+', '!':
			return true
		case '@', '-', ':':
		default:
			return false
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

type semver struct {
	major, minor, patch uint64
	pre                 []string
}

func parseSemver(value string) (semver, error) {
	if !strings.HasPrefix(value, "v") {
		return semver{}, fmt.Errorf("version %q must start with v", value)
	}
	parts := strings.SplitN(value[1:], "+", 2)
	if len(parts) == 2 && !validIdentifiers(parts[1], false) {
		return semver{}, fmt.Errorf("invalid build metadata in %q", value)
	}
	core := strings.SplitN(parts[0], "-", 2)
	var pre []string
	if len(core) == 2 {
		if !validIdentifiers(core[1], true) {
			return semver{}, fmt.Errorf("invalid prerelease in %q", value)
		}
		pre = strings.Split(core[1], ".")
	}
	numbers := strings.Split(core[0], ".")
	if len(numbers) != 3 {
		return semver{}, fmt.Errorf("version %q needs major, minor, and patch", value)
	}
	var parsed [3]uint64
	for i, n := range numbers {
		if n == "" || len(n) > 1 && n[0] == '0' || !digits(n) {
			return semver{}, fmt.Errorf("invalid version number %q", n)
		}
		var err error
		parsed[i], err = strconv.ParseUint(n, 10, 64)
		if err != nil {
			return semver{}, fmt.Errorf("invalid version number %q: %w", n, err)
		}
	}
	return semver{parsed[0], parsed[1], parsed[2], pre}, nil
}

func validIdentifiers(value string, prerelease bool) bool {
	if value == "" {
		return false
	}
	for _, id := range strings.Split(value, ".") {
		if id == "" || prerelease && len(id) > 1 && id[0] == '0' && digits(id) {
			return false
		}
		for _, c := range id {
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-') {
				return false
			}
		}
	}
	return true
}

func digits(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (v semver) compare(other semver) int {
	for _, pair := range [][2]uint64{{v.major, other.major}, {v.minor, other.minor}, {v.patch, other.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(v.pre) == 0 && len(other.pre) > 0 {
		return 1
	}
	if len(other.pre) == 0 && len(v.pre) > 0 {
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(other.pre); i++ {
		a, b := v.pre[i], other.pre[i]
		if a == b {
			continue
		}
		an, bn := digits(a), digits(b)
		if an != bn {
			if an {
				return -1
			}
			return 1
		}
		if an && len(a) != len(b) {
			if len(a) < len(b) {
				return -1
			}
			return 1
		}
		if a < b {
			return -1
		}
		return 1
	}
	if len(v.pre) < len(other.pre) {
		return -1
	}
	if len(v.pre) > len(other.pre) {
		return 1
	}
	return 0
}

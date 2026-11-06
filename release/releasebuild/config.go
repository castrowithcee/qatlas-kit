package releasebuild

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-kit/sysservice"
	"golang.org/x/crypto/ssh"
)

// Config describes the release build of an application. It is kept as JSON in the application
// repository; LoadConfig reads it.
type Config struct {
	// Package is the main package to build, "." or a path starting with "./". Empty means ".".
	Package string `json:"package"`
	// VersionVariable is the string variable that receives the version, such as
	// "example.com/tool/internal/cli.version".
	VersionVariable string `json:"versionVariable"`
	// Targets are the platforms to build as "goos/goarch", with goos linux, darwin or windows and goarch
	// amd64 or arm64.
	Targets []string `json:"targets"`
	// ManpageCommand is the command that writes the manpages. The arguments "{version}" and "{dir}" are
	// replaced by the version and the output directory.
	ManpageCommand []string `json:"manpageCommand"`
	// Files are extra regular files for every archive.
	Files []File `json:"files"`
	// Installer holds the texts of the installers.
	Installer Installer `json:"installer"`
	// Service, if set, builds the system service variant; see Service.
	Service *Service `json:"service"`
}

// Service selects the system service variant: every archive carries the unit and install.sh installs the
// program as a system service. It allows only linux/amd64 and linux/arm64 targets and therefore builds no
// install.ps1. The unit must pass sysservice.ValidateUnit with the program, Groups and WritableConfig.
type Service struct {
	// Unit is the unit file, relative to the module directory.
	Unit string `json:"unit"`
	// Groups are the supplementary groups of the service user.
	Groups []string `json:"groups"`
	// AddInstallingUser lets a fresh installation add the installing user to the group of the program.
	AddInstallingUser bool `json:"addInstallingUser"`
	// WritableConfig makes the configuration directory writable for the service user.
	WritableConfig bool `json:"writableConfig"`
}

// File copies Source, relative to the module directory, to Path inside each archive.
type File struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

// Installer holds the texts of install.sh and install.ps1; see the installer package.
type Installer struct {
	DisplayName string `json:"displayName"`
	NextStep    string `json:"nextStep"`
}

// LoadConfig reads a Config from a JSON file. Unknown fields and trailing data are errors.
func LoadConfig(file string) (Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Config{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var c Config
	if err := decoder.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", file, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode %s: trailing data", file)
	}
	return c, nil
}

var (
	versionPattern     = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
	programPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	variablePattern    = regexp.MustCompile(`^[A-Za-z0-9_.~/-]+\.[A-Za-z_][A-Za-z0-9_]*$`)
	archivePathPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	// Same pattern as the installers accept for a namespace.
	previousNamespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	assetPattern             = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	manpagePattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.1$`)
)

// ArchiveName returns the archive name of program at version for one target.
func ArchiveName(program, version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return program + "_" + version + "_" + goos + "_" + goarch + ext
}

type target struct{ goos, goarch string }

func (c Config) targets() ([]target, error) {
	if len(c.Targets) == 0 {
		return nil, errors.New("at least one target is required")
	}
	sorted := slices.Sorted(slices.Values(c.Targets))
	if len(slices.Compact(slices.Clone(sorted))) != len(sorted) {
		return nil, errors.New("targets must be unique")
	}
	var out []target
	for _, raw := range sorted {
		goos, goarch, _ := strings.Cut(raw, "/")
		if (goos != "linux" && goos != "darwin" && goos != "windows") || (goarch != "amd64" && goarch != "arm64") {
			return nil, fmt.Errorf("unsupported target %q", raw)
		}
		out = append(out, target{goos, goarch})
	}
	return out, nil
}

func (c Config) mainPackage() (string, error) {
	p := c.Package
	if p == "" {
		return ".", nil
	}
	if p != "." && !(strings.HasPrefix(p, "./") && archivePath(p[2:])) {
		return "", fmt.Errorf("package %q must be \".\" or a clean path starting with \"./\"", p)
	}
	return p, nil
}

// archivePath reports whether p is a clean relative slash path without "." or ".." elements.
func archivePath(p string) bool {
	if !archivePathPattern.MatchString(p) {
		return false
	}
	for _, element := range strings.Split(p, "/") {
		if element == "." || element == ".." {
			return false
		}
	}
	return true
}

func (c Config) files() error {
	seen := map[string]bool{}
	for _, f := range c.Files {
		if !archivePath(f.Path) {
			return fmt.Errorf("invalid archive path %q", f.Path)
		}
		if f.Path == "bin" || strings.HasPrefix(f.Path, "bin/") || f.Path == "share/man/man1" || strings.HasPrefix(f.Path, "share/man/man1/") {
			return fmt.Errorf("archive path %q is reserved for the program and its manpages", f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("duplicate archive path %q", f.Path)
		}
		seen[f.Path] = true
		if !archivePath(f.Source) {
			return fmt.Errorf("source %q must be a clean path inside the module", f.Source)
		}
	}
	for p := range seen {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if seen[dir] {
				return fmt.Errorf("archive path %q is both a file and a directory", dir)
			}
		}
	}
	return nil
}

// service checks the parts of the Service that need no file access.
func (c Config) service(program string) error {
	s := c.Service
	if s.Unit == "" {
		return errors.New("service unit is required")
	}
	if !archivePath(s.Unit) {
		return fmt.Errorf("service unit %q must be a clean path inside the module", s.Unit)
	}
	for _, t := range c.Targets {
		if t != "linux/amd64" && t != "linux/arm64" {
			return fmt.Errorf("service allows only linux/amd64 and linux/arm64 targets, not %q", t)
		}
	}
	entry := sysservice.UnitEntry(program)
	for _, f := range c.Files {
		if f.Path == entry || strings.HasPrefix(entry, f.Path+"/") || strings.HasPrefix(f.Path, entry+"/") {
			return fmt.Errorf("archive path %q conflicts with the service unit %q", f.Path, entry)
		}
	}
	return nil
}

// ParseAllowedSigners returns the keys of an allowed signers file. Every line that is neither empty nor
// a comment must name exactly namespace as principal, carry no options, and hold an Ed25519 key.
func ParseAllowedSigners(data []byte, namespace string) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	seen := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for number := 1; scanner.Scan(); number++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != namespace || fields[1] != ssh.KeyAlgoED25519 {
			return nil, fmt.Errorf("allowed signers line %d must be %q followed by an ssh-ed25519 key", number, namespace)
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(fields[1] + " " + fields[2]))
		if err != nil || key.Type() != ssh.KeyAlgoED25519 {
			return nil, fmt.Errorf("allowed signers line %d holds no valid Ed25519 key", number)
		}
		if seen[string(key.Marshal())] {
			return nil, fmt.Errorf("allowed signers line %d repeats a key", number)
		}
		seen[string(key.Marshal())] = true
		keys = append(keys, key)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errors.New("allowed signers file lists no key")
	}
	return keys, nil
}

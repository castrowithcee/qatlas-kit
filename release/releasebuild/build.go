package releasebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/release"
	"github.com/castrowithcee/qatlas-kit/release/installer"
)

// Options holds what Build needs besides the Config.
type Options struct {
	// Dir is the module directory to build in. Empty means the current directory.
	Dir string
	// Program is the program file name without ".exe".
	Program string
	// Namespace is the signature namespace; it must be Program + "-release".
	Namespace string
	// AllowedSigners is the allowed signers file, relative to Dir, whose keys the installers trust.
	AllowedSigners string
	// ReleasesURL is the https release page the installers download from.
	ReleasesURL string
	// Version is the SemVer tag of the release.
	Version string
	// Output is the directory for the assets; it must be missing or empty.
	Output string
	// SourceDate is the time recorded in archives and passed on as SOURCE_DATE_EPOCH.
	SourceDate time.Time
	// Log receives the output of the manpage command and of go build. Nil discards it.
	Log io.Writer
}

var minimumSourceDate = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// Build writes the release assets to o.Output and returns their names, sorted.
func Build(ctx context.Context, c Config, o Options) ([]string, error) {
	plan, err := prepare(c, o)
	if err != nil {
		return nil, err
	}
	if err := emptyOutput(o.Output); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "release-build-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	manpages, err := plan.manpages(ctx, work)
	if err != nil {
		return nil, err
	}
	assets := map[string][]byte{}
	for _, t := range plan.targets {
		name, data, err := plan.archive(ctx, work, t, manpages)
		if err != nil {
			return nil, err
		}
		assets[name] = data
	}
	maps.Copy(assets, plan.installers)
	names := slices.Sorted(maps.Keys(assets))
	var checksums strings.Builder
	for _, name := range names {
		sum := sha256.Sum256(assets[name])
		checksums.WriteString(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	}
	assets["checksums.txt"] = []byte(checksums.String())
	for _, name := range names {
		want, err := release.ChecksumFor(assets["checksums.txt"], name)
		if sum := sha256.Sum256(assets[name]); err != nil || want != hex.EncodeToString(sum[:]) {
			return nil, fmt.Errorf("checksums.txt does not list %s correctly", name)
		}
	}
	names = append(names, "checksums.txt")
	slices.Sort(names)
	if err := os.MkdirAll(o.Output, 0o755); err != nil {
		return nil, err
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(o.Output, name), assets[name], 0o644); err != nil {
			return nil, err
		}
	}
	return names, nil
}

type plan struct {
	Config
	Options
	pkg        string
	targets    []target
	installers map[string][]byte
	log        io.Writer
}

// prepare checks every input before anything is built or written.
func prepare(c Config, o Options) (*plan, error) {
	p := &plan{Config: c, Options: o, log: o.Log}
	if p.log == nil {
		p.log = io.Discard
	}
	if p.Dir == "" {
		p.Dir = "."
	}
	if !programPattern.MatchString(o.Program) {
		return nil, fmt.Errorf("invalid program name %q", o.Program)
	}
	if o.Namespace != o.Program+"-release" {
		return nil, fmt.Errorf("signature namespace must be %q", o.Program+"-release")
	}
	if !versionPattern.MatchString(o.Version) {
		return nil, fmt.Errorf("version %q must be a SemVer tag such as v1.2.3 or v1.2.3-rc.1", o.Version)
	}
	if o.Output == "" {
		return nil, errors.New("output directory is required")
	}
	if o.SourceDate.Before(minimumSourceDate) {
		return nil, errors.New("source date must not be before 1980")
	}
	p.SourceDate = o.SourceDate.UTC().Truncate(time.Second)
	var err error
	if p.pkg, err = c.mainPackage(); err != nil {
		return nil, err
	}
	if !variablePattern.MatchString(c.VersionVariable) {
		return nil, fmt.Errorf("invalid version variable %q", c.VersionVariable)
	}
	if p.targets, err = c.targets(); err != nil {
		return nil, err
	}
	if len(c.ManpageCommand) == 0 || c.ManpageCommand[0] == "" || !slices.Contains(c.ManpageCommand, "{dir}") {
		return nil, errors.New(`manpage command is required and must take the output directory "{dir}"`)
	}
	if err := c.files(); err != nil {
		return nil, err
	}
	for _, f := range c.Files {
		info, err := os.Lstat(filepath.Join(p.Dir, filepath.FromSlash(f.Source)))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("source %s is not a regular file", f.Source)
		}
	}
	if o.AllowedSigners == "" {
		return nil, errors.New("allowed signers file is required")
	}
	signers, err := os.ReadFile(filepath.Join(p.Dir, o.AllowedSigners))
	if err != nil {
		return nil, err
	}
	keys, err := ParseAllowedSigners(signers, o.Namespace)
	if err != nil {
		return nil, err
	}
	params := installer.Params{
		Program: o.Program, DisplayName: c.Installer.DisplayName, ReleasesURL: o.ReleasesURL,
		Namespace: o.Namespace, TrustedKeys: keys, Targets: c.Targets, NextStep: c.Installer.NextStep,
	}
	p.installers = map[string][]byte{}
	if slices.ContainsFunc(p.targets, func(t target) bool { return t.goos != "windows" }) {
		if p.installers["install.sh"], err = installer.Shell(params); err != nil {
			return nil, err
		}
	}
	if slices.ContainsFunc(p.targets, func(t target) bool { return t.goos == "windows" }) {
		if p.installers["install.ps1"], err = installer.PowerShell(params); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func emptyOutput(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("output directory %s is not empty", dir)
	}
	return nil
}

func (p *plan) env(extra ...string) []string {
	return append(os.Environ(), append([]string{"SOURCE_DATE_EPOCH=" + strconv.FormatInt(p.SourceDate.Unix(), 10)}, extra...)...)
}

// manpages runs the manpage command and returns the pages by file name.
func (p *plan) manpages(ctx context.Context, work string) (map[string][]byte, error) {
	dir := filepath.Join(work, "man")
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, err
	}
	args := make([]string, len(p.ManpageCommand))
	for i, arg := range p.ManpageCommand {
		args[i] = strings.NewReplacer("{version}", p.Version, "{dir}", dir).Replace(arg)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = p.Dir, p.env(), p.log, p.log
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("manpage command: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	pages := map[string][]byte{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !manpagePattern.MatchString(entry.Name()) {
			return nil, fmt.Errorf("manpage command wrote %s, not a regular <name>.1 file", entry.Name())
		}
		if pages[entry.Name()], err = os.ReadFile(filepath.Join(dir, entry.Name())); err != nil {
			return nil, err
		}
	}
	if len(pages) == 0 {
		return nil, errors.New("manpage command wrote no manpage")
	}
	return pages, nil
}

// archive builds the program for t and returns the checked archive.
func (p *plan) archive(ctx context.Context, work string, t target, manpages map[string][]byte) (string, []byte, error) {
	program := p.Program
	if t.goos == "windows" {
		program += ".exe"
	}
	binary := filepath.Join(work, t.goos+"_"+t.goarch, program)
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false",
		"-ldflags", "-s -w -X "+p.VersionVariable+"="+p.Version, "-o", binary, p.pkg)
	cmd.Dir, cmd.Stdout, cmd.Stderr = p.Dir, p.log, p.log
	cmd.Env = p.env("CGO_ENABLED=0", "GOOS="+t.goos, "GOARCH="+t.goarch)
	if err := cmd.Run(); err != nil {
		return "", nil, fmt.Errorf("build %s/%s: %w", t.goos, t.goarch, err)
	}
	body, err := os.ReadFile(binary)
	if err != nil {
		return "", nil, err
	}
	entries := []entry{{name: "bin/" + program, body: body, executable: true}}
	if t.goos != "windows" {
		for name, page := range manpages {
			entries = append(entries, entry{name: "share/man/man1/" + name, body: page})
		}
	}
	for _, f := range p.Files {
		data, err := os.ReadFile(filepath.Join(p.Dir, filepath.FromSlash(f.Source)))
		if err != nil {
			return "", nil, err
		}
		entries = append(entries, entry{name: f.Path, body: data})
	}
	name := ArchiveName(p.Program, p.Version, t.goos, t.goarch)
	data, err := writeArchive(name, entries, p.SourceDate)
	if err != nil {
		return "", nil, err
	}
	got, err := release.ProgramSHA256(name, data, "bin/"+program)
	if sum := sha256.Sum256(body); err != nil || got != hex.EncodeToString(sum[:]) {
		return "", nil, fmt.Errorf("check %s: program does not verify: %v", name, err)
	}
	return name, data, nil
}

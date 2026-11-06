package installer

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/castrowithcee/qatlas-kit/sysservice"
	"golang.org/x/crypto/ssh"
)

// Params describes the program an installer installs.
type Params struct {
	// Program is the program file name without ".exe", such as "tool".
	Program string
	// DisplayName names the program in messages. Empty means Program.
	DisplayName string
	// ReleasesURL is the https URL of the release page, such as "https://github.com/owner/repo/releases".
	// The installers download from <ReleasesURL>/latest and <ReleasesURL>/download/<tag>/<asset>.
	ReleasesURL string
	// Namespace is the SSH signature namespace of checksums.txt; it is also the principal of the keys.
	Namespace string
	// SignatureAsset is the name of the release file that holds the signature over checksums.txt. It is
	// required and has no default.
	SignatureAsset string
	// TrustedKeys are the Ed25519 keys whose signature over checksums.txt is accepted.
	TrustedKeys []ssh.PublicKey
	// Targets are the built platforms as "goos/goarch", such as "linux/amd64".
	Targets []string
	// NextStep is an optional line of printable ASCII shown after a successful installation.
	NextStep string
	// Service, if set, selects the system service variant of install.sh. It allows only linux/amd64 and
	// linux/arm64 targets, and PowerShell refuses it.
	Service *Service
}

// Service selects the system service variant of install.sh.
type Service struct {
	Groups            []string // supplementary groups of the service user
	AddInstallingUser bool     // a fresh install adds SUDO_USER to the group <program>
	WritableConfig    bool     // /etc/<program> is owned by the service user
}

var (
	//go:embed install.sh.tmpl
	shellSource string
	//go:embed install.ps1.tmpl
	powerShellSource string
	//go:embed install_system.sh.tmpl
	systemSource string

	shellTemplate      = template.Must(template.New("install.sh").Option("missingkey=error").Funcs(quoting).Parse(shellSource))
	systemTemplate     = template.Must(template.New("install.sh").Option("missingkey=error").Funcs(quoting).Parse(systemSource))
	powerShellTemplate = template.Must(template.New("install.ps1").Option("missingkey=error").Funcs(quoting).Parse(powerShellSource))

	quoting = template.FuncMap{
		"sh":   func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" },
		"ps":   func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" },
		"join": strings.Join,
	}

	programPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	displayPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)
	namespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	assetPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type values struct {
	Program, DisplayName, ReleasesURL, Namespace, SignatureAsset, Signers, NextStep, Env string
	Unix, Windows                                                                        []string

	// System service variant only.
	Groups                            []string
	AddInstallingUser, WritableConfig bool
	Paths                             sysservice.Paths
	UnitEntry                         string
}

// Shell renders install.sh. Params must name at least one linux or darwin target. With Params.Service it
// renders the system service variant.
func Shell(p Params) ([]byte, error) {
	v, err := p.values()
	if err != nil {
		return nil, err
	}
	if len(v.Unix) == 0 {
		return nil, errors.New("install.sh needs a linux or darwin target")
	}
	if p.Service != nil {
		return render(systemTemplate, v)
	}
	return render(shellTemplate, v)
}

// PowerShell renders install.ps1. Params must name at least one windows target and no Service.
func PowerShell(p Params) ([]byte, error) {
	if p.Service != nil {
		return nil, errors.New("install.ps1 cannot install a system service")
	}
	v, err := p.values()
	if err != nil {
		return nil, err
	}
	if len(v.Windows) == 0 {
		return nil, errors.New("install.ps1 needs a windows target")
	}
	return render(powerShellTemplate, v)
}

func render(t *template.Template, v values) ([]byte, error) {
	var out bytes.Buffer
	if err := t.Execute(&out, v); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (p Params) values() (values, error) {
	if !programPattern.MatchString(p.Program) {
		return values{}, fmt.Errorf("invalid program name %q", p.Program)
	}
	v := values{Program: p.Program, DisplayName: p.DisplayName, Namespace: p.Namespace, NextStep: p.NextStep}
	if v.DisplayName == "" {
		v.DisplayName = p.Program
	}
	if !displayPattern.MatchString(v.DisplayName) {
		return values{}, fmt.Errorf("invalid display name %q", v.DisplayName)
	}
	v.Env = strings.ToUpper(strings.ReplaceAll(p.Program, "-", "_"))
	base, err := url.Parse(p.ReleasesURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		base.ForceQuery || base.Opaque != "" || strings.HasSuffix(base.Path, "/") || base.String() != p.ReleasesURL || strings.ContainsAny(p.ReleasesURL, "'\"`$\\ ") {
		return values{}, fmt.Errorf("releases URL %q must be an https URL without query or trailing slash", p.ReleasesURL)
	}
	v.ReleasesURL = p.ReleasesURL
	if !namespacePattern.MatchString(p.Namespace) {
		return values{}, fmt.Errorf("invalid signature namespace %q", p.Namespace)
	}
	// The name must not collide with a file the scripts write next to the downloads.
	if !assetPattern.MatchString(p.SignatureAsset) || p.SignatureAsset == "checksums.txt" || p.SignatureAsset == "allowed-signers" ||
		p.SignatureAsset == "verify.out" || p.SignatureAsset == "verify.err" || strings.HasPrefix(p.SignatureAsset, p.Program+"_") {
		return values{}, fmt.Errorf("invalid signature asset name %q", p.SignatureAsset)
	}
	v.SignatureAsset = p.SignatureAsset
	if len(p.TrustedKeys) == 0 {
		return values{}, errors.New("at least one trusted key is required")
	}
	var signers []string
	for _, key := range p.TrustedKeys {
		if key == nil || key.Type() != ssh.KeyAlgoED25519 {
			return values{}, errors.New("trusted keys must be Ed25519 keys")
		}
		signers = append(signers, p.Namespace+" "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))))
	}
	v.Signers = strings.Join(signers, "\n")
	for _, r := range p.NextStep {
		if r < 0x20 || r > 0x7e {
			return values{}, errors.New("next step must be a single line of printable ASCII")
		}
	}
	for _, target := range p.Targets {
		goos, goarch, _ := strings.Cut(target, "/")
		if goarch != "amd64" && goarch != "arm64" {
			return values{}, fmt.Errorf("unsupported target %q", target)
		}
		switch goos {
		case "linux", "darwin":
			v.Unix = append(v.Unix, target)
		case "windows":
			v.Windows = append(v.Windows, goarch)
		default:
			return values{}, fmt.Errorf("unsupported target %q", target)
		}
	}
	if len(v.Unix)+len(v.Windows) != len(slices.Compact(slices.Sorted(slices.Values(p.Targets)))) {
		return values{}, errors.New("targets must be unique")
	}
	if p.Service != nil {
		if err := v.service(p.Program, p.Service, p.Targets); err != nil {
			return values{}, err
		}
	}
	return v, nil
}

func (v *values) service(program string, s *Service, targets []string) error {
	if err := sysservice.ValidateGroups(program, s.Groups); err != nil {
		return err
	}
	for _, target := range targets {
		if target != "linux/amd64" && target != "linux/arm64" {
			return fmt.Errorf("a system service supports only linux/amd64 and linux/arm64 targets, not %q", target)
		}
	}
	v.Groups = slices.Clone(s.Groups)
	v.AddInstallingUser, v.WritableConfig = s.AddInstallingUser, s.WritableConfig
	v.Paths = sysservice.InstallPaths(program)
	v.UnitEntry = sysservice.UnitEntry(program)
	return nil
}

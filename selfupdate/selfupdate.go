package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	pageSize         = 100
	maxPages         = 20
	maxMetadataBytes = 1 << 20
	checksumsAsset   = "checksums.txt"
)

// ErrDevelopmentBuild reports a running build without a release version.
var ErrDevelopmentBuild = errors.New("self-update is unavailable in a development build")

// TargetReason says why a release cannot be chosen.
type TargetReason int

const (
	// TargetInvalidVersion is a version that is not a v-prefixed semantic version.
	TargetInvalidVersion TargetReason = iota + 1
	// TargetBelowMinimum is a version older than Client.MinimumVersion.
	TargetBelowMinimum
	// TargetNotFound is a version the repository has no published release for.
	TargetNotFound
	// TargetIneligible is a release that is a draft, unsigned, incomplete, or has assets outside the
	// repository.
	TargetIneligible
	// TargetRunning is the running version, which has nothing to install.
	TargetRunning
)

// TargetError reports a release that cannot be chosen. The application words its message from Reason.
type TargetError struct {
	Version string
	Reason  TargetReason
}

func (e *TargetError) Error() string {
	switch e.Reason {
	case TargetInvalidVersion:
		return fmt.Sprintf("%q is not a release version", e.Version)
	case TargetBelowMinimum:
		return "release " + e.Version + " is older than the minimum version"
	case TargetNotFound:
		return "release " + e.Version + " does not exist"
	case TargetRunning:
		return "release " + e.Version + " is the running version"
	default:
		return "release " + e.Version + " is unpublished, unsigned, or incomplete"
	}
}

// UnsupportedInstallationError reports an installed file that cannot be replaced safely in place.
type UnsupportedInstallationError struct {
	Path   string
	Reason string
}

func (e *UnsupportedInstallationError) Error() string {
	return "unsupported installation at " + e.Path + ": " + e.Reason
}

// Client holds the release source and policy supplied by the application.
type Client struct {
	RepositoryAPI  string
	CurrentVersion string
	MinimumVersion string
	ArchiveName    func(version string) string
	UserAgent      string
	HTTPClient     *http.Client
	// Token, when set, is sent as a bearer token to the host of RepositoryAPI and to no other host.
	Token     string
	Namespace string
	// SignatureAsset is the name of the release asset that holds the SSH signature over checksums.txt,
	// verified under Namespace. It is required and has no default: a plain file name that is neither
	// checksums.txt nor the archive name.
	SignatureAsset string
	TrustedKeys    []ssh.PublicKey
	// PreviousSignature, when set, makes releases up to and including its LastVersion verify only under
	// the previous signature set instead of Namespace and SignatureAsset.
	PreviousSignature *PreviousSignature
	ProgramName       string
	// Companions are installed together with the program and restored if the program cannot be replaced.
	Companions []Companion
	Executable string
	// ValidateRollback must check the prepared program against an isolated copy of the application's
	// configuration, for example with Prepared.ReadsConfig. Installing an older release requires it.
	ValidateRollback func(context.Context, *Prepared) error
	// BeforeReplace runs once everything is verified and staged, right before the first installed file is
	// replaced. An error ends the installation with nothing replaced.
	BeforeReplace func(context.Context, ReleaseFiles) error
	// AfterReplace runs once the program is replaced. The installation is complete by then.
	AfterReplace func(context.Context, ReleaseFiles)
	// Progress is called while the archive downloads, with the bytes received and the announced size, or -1
	// when the server does not announce it.
	Progress func(done, total int64)
}

// PreviousSignature describes the signature set of the oldest releases after an application renamed its
// signature namespace and signature file. A release up to and including LastVersion is verified only with
// this namespace and signature file, every later release only with the current Client.Namespace and
// Client.SignatureAsset; a later release that still carries the previous file is not verified with it. The
// same Client.TrustedKeys apply to both sets, and the installers of the release package always use the
// current set.
type PreviousSignature struct {
	// LastVersion is the newest release signed only with the previous set, such as "v1.4.0".
	LastVersion string
	// Namespace is the signature namespace of those releases.
	Namespace string
	// SignatureAsset is the name of the release asset that holds their signature over checksums.txt, with
	// the same rules as Client.SignatureAsset.
	SignatureAsset string
}

// Companion is a file that the release archive carries next to the program, such as a manpage.
type Companion struct {
	// Entry is the canonical slash-separated path in the archive, for example "share/man/man1/tool.1".
	Entry string
	// Path is the absolute installation path. Its base name must equal the base name of Entry.
	Path string
}

// Result describes the newest eligible stable release or, from InstallPrepared, the installed release.
type Result struct {
	Current         string
	Latest          string
	UpdateAvailable bool
	Updated         bool
	// Companions lists the companion paths that were written; a companion whose content is unchanged is
	// left alone.
	Companions []string
}

// Entry is a version offered in the release list.
type Entry struct {
	Version    string
	Prerelease bool
	Current    bool
	Older      bool
}

// Target is a selected release. Its metadata is retained for later verification.
type Target struct {
	Entry
	rel githubRelease
}

type githubRelease struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	APIURL string `json:"url"`
}

type policy struct {
	base    *url.URL
	current semver
	minimum semver
}

type statusError struct {
	code   int
	status string
}

func (e *statusError) Error() string { return "release API returned " + e.status }

func (c *Client) validate() (policy, error) {
	if c == nil || c.ArchiveName == nil || c.UserAgent == "" || strings.ContainsAny(c.UserAgent, "\r\n") {
		return policy{}, errors.New("archive name and user agent are required")
	}
	if !validAssetName(c.SignatureAsset) || c.SignatureAsset == checksumsAsset {
		return policy{}, errors.New("a valid signature asset name is required")
	}
	if prev := c.PreviousSignature; prev != nil {
		if _, err := parseSemver(prev.LastVersion); err != nil {
			return policy{}, errors.New("previous signature needs a valid last version")
		}
		if prev.Namespace == "" || prev.Namespace == c.Namespace {
			return policy{}, errors.New("previous signature needs a namespace that differs from the current one")
		}
		if !validAssetName(prev.SignatureAsset) || prev.SignatureAsset == checksumsAsset || prev.SignatureAsset == c.SignatureAsset {
			return policy{}, errors.New("previous signature needs a valid signature asset name that differs from the current one")
		}
	}
	if strings.ContainsAny(c.Token, "\r\n") {
		return policy{}, errors.New("invalid token")
	}
	base, err := url.Parse(c.RepositoryAPI)
	if err != nil || base == nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		(base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1" || base.Hostname() == "::1"))) {
		return policy{}, errors.New("invalid repository API URL")
	}
	parts := strings.Split(strings.Trim(base.EscapedPath(), "/"), "/")
	if len(parts) != 3 || parts[0] != "repos" || parts[1] == "" || parts[2] == "" || strings.Contains(base.EscapedPath(), "%") || strings.HasSuffix(base.Path, "/") {
		return policy{}, errors.New("repository API URL must name /repos/owner/repository")
	}
	if c.CurrentVersion == "" || c.CurrentVersion == "dev" {
		return policy{}, ErrDevelopmentBuild
	}
	current, err := parseSemver(c.CurrentVersion)
	if err != nil {
		return policy{}, fmt.Errorf("invalid current version: %w", err)
	}
	minimum, err := parseSemver(c.MinimumVersion)
	if err != nil || len(minimum.pre) != 0 {
		return policy{}, errors.New("minimum version must be a stable release version")
	}
	return policy{base, current, minimum}, nil
}

// validAssetName reports whether name is a plain release file name without separators or control bytes.
func validAssetName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// Check reports the newest eligible stable release without downloading artifacts.
func (c *Client) Check(ctx context.Context) (Result, error) {
	result, _, err := c.newest(ctx)
	return result, err
}

// Update installs the newest eligible stable release when it is newer than the running version. It never
// installs an older release; a rollback goes through Target, Prepare and InstallPrepared.
func (c *Client) Update(ctx context.Context) (Result, error) {
	result, best, err := c.newest(ctx)
	if err != nil || !result.UpdateAvailable {
		return result, err
	}
	prepared, err := c.Prepare(ctx, Target{Entry: Entry{Version: best.TagName, Prerelease: best.Prerelease}, rel: *best})
	if err != nil {
		return result, err
	}
	return c.InstallPrepared(ctx, prepared)
}

func (c *Client) newest(ctx context.Context) (Result, *githubRelease, error) {
	p, err := c.validate()
	if err != nil {
		return Result{}, nil, err
	}
	releases, err := c.releases(ctx, p)
	if err != nil {
		return Result{}, nil, err
	}
	best := c.stable(releases, p)
	result := Result{Current: c.CurrentVersion, Latest: c.CurrentVersion}
	if best != nil {
		result.Latest = best.TagName
		v, _ := parseSemver(best.TagName)
		result.UpdateAvailable = v.compare(p.current) > 0
	}
	return result, best, nil
}

// List shows ten newest stable releases, newer prereleases, and the running version if eligible.
func (c *Client) List(ctx context.Context) ([]Entry, error) {
	p, err := c.validate()
	if err != nil {
		return nil, err
	}
	releases, err := c.releases(ctx, p)
	if err != nil {
		return nil, err
	}
	type item struct {
		entry Entry
		ver   semver
	}
	var items []item
	seen := make(map[string]bool)
	for _, r := range releases {
		v, ok := c.eligible(r, p)
		if !ok || seen[r.TagName] {
			continue
		}
		seen[r.TagName] = true
		cmp := v.compare(p.current)
		items = append(items, item{Entry{r.TagName, r.Prerelease, cmp == 0, cmp < 0}, v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ver.compare(items[j].ver) > 0 })
	var out []Entry
	stable := 0
	for _, item := range items {
		if !item.entry.Current {
			if item.entry.Prerelease && stable > 0 || !item.entry.Prerelease && stable >= 10 {
				continue
			}
		}
		if !item.entry.Prerelease {
			stable++
		}
		out = append(out, item.entry)
	}
	return out, nil
}

// Target selects an exact release version, including an older version for rollback. A release that
// cannot be chosen is a *TargetError.
func (c *Client) Target(ctx context.Context, tag string) (Target, error) {
	p, err := c.validate()
	if err != nil {
		return Target{}, err
	}
	v, err := parseSemver(tag)
	if err != nil {
		return Target{}, &TargetError{tag, TargetInvalidVersion}
	}
	if v.compare(p.minimum) < 0 {
		return Target{}, &TargetError{tag, TargetBelowMinimum}
	}
	var r githubRelease
	if err := c.getJSON(ctx, p, "/releases/tags/"+url.PathEscape(tag), &r); err != nil {
		var status *statusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return Target{}, &TargetError{tag, TargetNotFound}
		}
		return Target{}, err
	}
	if r.TagName != tag {
		return Target{}, &TargetError{tag, TargetNotFound}
	}
	if _, ok := c.eligible(r, p); !ok {
		return Target{}, &TargetError{tag, TargetIneligible}
	}
	cmp := v.compare(p.current)
	return Target{Entry: Entry{tag, r.Prerelease, cmp == 0, cmp < 0}, rel: r}, nil
}

func (c *Client) stable(releases []githubRelease, p policy) *githubRelease {
	var best *githubRelease
	var bestVersion semver
	for i := range releases {
		r := &releases[i]
		v, ok := c.eligible(*r, p)
		if ok && !r.Prerelease && (best == nil || v.compare(bestVersion) > 0) {
			best, bestVersion = r, v
		}
	}
	return best
}

// signatureSet returns the signature namespace and asset name that verify the release with the given
// version: the previous set up to and including PreviousSignature.LastVersion, the current set otherwise.
// The version is always the release tag, never inferred from the files a release offers.
func (c *Client) signatureSet(v semver) (namespace, assetName string) {
	if prev := c.PreviousSignature; prev != nil {
		if last, err := parseSemver(prev.LastVersion); err == nil && v.compare(last) <= 0 {
			return prev.Namespace, prev.SignatureAsset
		}
	}
	return c.Namespace, c.SignatureAsset
}

func (c *Client) eligible(r githubRelease, p policy) (semver, bool) {
	v, err := parseSemver(r.TagName)
	if err != nil || r.Draft || r.Prerelease != (len(v.pre) > 0) || v.compare(p.minimum) < 0 {
		return semver{}, false
	}
	_, signatureAsset := c.signatureSet(v)
	names := []string{c.ArchiveName(r.TagName), checksumsAsset, signatureAsset}
	if names[0] == "" || names[0] == names[1] || names[0] == names[2] || strings.ContainsAny(names[0], "/\\") {
		return semver{}, false
	}
	for _, name := range names {
		count := 0
		for _, a := range r.Assets {
			if a.Name == name {
				if !validAssetURL(p.base, a.APIURL) {
					return semver{}, false
				}
				count++
			}
		}
		if count != 1 {
			return semver{}, false
		}
	}
	return v, true
}

func validAssetURL(base *url.URL, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	id := strings.TrimPrefix(u.Path, base.Path+"/releases/assets/")
	return id != u.Path && id != "" && digits(id)
}

func (c *Client) releases(ctx context.Context, p policy) ([]githubRelease, error) {
	var all []githubRelease
	for page := 1; page <= maxPages; page++ {
		var batch []githubRelease
		path := "/releases?per_page=" + strconv.Itoa(pageSize) + "&page=" + strconv.Itoa(page)
		if err := c.getJSON(ctx, p, path, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			return all, nil
		}
	}
	return nil, errors.New("release list exceeds page limit")
}

// newRequest sets the headers of every request; the token goes only to the repository's API origin.
func (c *Client) newRequest(ctx context.Context, p policy, raw, accept string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", c.UserAgent)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" && request.URL.Scheme == p.base.Scheme && request.URL.Host == p.base.Host {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return request, nil
}

func (c *Client) getJSON(ctx context.Context, p policy, path string, out any) error {
	request, err := c.newRequest(ctx, p, p.base.String()+path, "application/vnd.github+json")
	if err != nil {
		return err
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	} else {
		copy := *client
		client = &copy
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &statusError{response.StatusCode, response.Status}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxMetadataBytes {
		return errors.New("release metadata exceeds size limit")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode release metadata: %w", err)
	}
	return nil
}

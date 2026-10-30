// Package selfupdate selects signed GitHub releases for a caller-supplied program.
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
)

const (
	pageSize         = 100
	maxPages         = 20
	maxMetadataBytes = 1 << 20
)

// Client holds the release source and policy supplied by the application.
type Client struct {
	RepositoryAPI  string
	CurrentVersion string
	MinimumVersion string
	ArchiveName    func(version string) string
	UserAgent      string
	HTTPClient     *http.Client
}

// Result describes the newest eligible stable release.
type Result struct {
	Current         string
	Latest          string
	UpdateAvailable bool
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
	rel release
}

type release struct {
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

func (c *Client) validate() (policy, error) {
	if c == nil || c.ArchiveName == nil || c.UserAgent == "" || strings.ContainsAny(c.UserAgent, "\r\n") {
		return policy{}, errors.New("archive name and user agent are required")
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

// Check reports the newest eligible stable release without downloading artifacts.
func (c *Client) Check(ctx context.Context) (Result, error) {
	p, err := c.validate()
	if err != nil {
		return Result{}, err
	}
	releases, err := c.releases(ctx, p)
	if err != nil {
		return Result{}, err
	}
	best := c.stable(releases, p)
	result := Result{Current: c.CurrentVersion, Latest: c.CurrentVersion}
	if best != nil {
		result.Latest = best.TagName
		v, _ := parseSemver(best.TagName)
		result.UpdateAvailable = v.compare(p.current) > 0
	}
	return result, nil
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

// Target selects an exact release version, including an older version for rollback.
func (c *Client) Target(ctx context.Context, tag string) (Target, error) {
	p, err := c.validate()
	if err != nil {
		return Target{}, err
	}
	v, err := parseSemver(tag)
	if err != nil {
		return Target{}, fmt.Errorf("invalid target version: %w", err)
	}
	if v.compare(p.minimum) < 0 {
		return Target{}, fmt.Errorf("release %s is older than minimum %s", tag, c.MinimumVersion)
	}
	var r release
	if err := c.getJSON(ctx, p, "/releases/tags/"+url.PathEscape(tag), &r); err != nil {
		return Target{}, err
	}
	if r.TagName != tag {
		return Target{}, errors.New("release tag does not match requested version")
	}
	if _, ok := c.eligible(r, p); !ok {
		return Target{}, fmt.Errorf("release %s is unpublished, unsigned, or incomplete", tag)
	}
	cmp := v.compare(p.current)
	return Target{Entry: Entry{tag, r.Prerelease, cmp == 0, cmp < 0}, rel: r}, nil
}

func (c *Client) stable(releases []release, p policy) *release {
	var best *release
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

func (c *Client) eligible(r release, p policy) (semver, bool) {
	v, err := parseSemver(r.TagName)
	if err != nil || r.Draft || r.Prerelease != (len(v.pre) > 0) || v.compare(p.minimum) < 0 {
		return semver{}, false
	}
	names := []string{c.ArchiveName(r.TagName), "checksums.txt", "checksums.txt.sig"}
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

func (c *Client) releases(ctx context.Context, p policy) ([]release, error) {
	var all []release
	for page := 1; page <= maxPages; page++ {
		var batch []release
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

func (c *Client) getJSON(ctx context.Context, p policy, path string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base.String()+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", c.UserAgent)
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
		return fmt.Errorf("release API returned %s", response.Status)
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

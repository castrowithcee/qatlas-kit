package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testRelease(base, tag string, signed bool) githubRelease {
	r := githubRelease{TagName: tag, Prerelease: strings.Contains(tag, "-")}
	for i, name := range []string{"sample_" + tag + ".zip", "checksums.txt", "checksums.txt.sig"} {
		if i == 2 && !signed {
			break
		}
		r.Assets = append(r.Assets, asset{Name: name, APIURL: fmt.Sprintf("%s/releases/assets/%d", base, i+1)})
	}
	return r
}

func testClient(t *testing.T, releases []githubRelease) (*Client, *int) {
	t.Helper()
	requests := new(int)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		if r.Header.Get("User-Agent") != "sample/1" {
			t.Errorf("user agent: %q", r.Header.Get("User-Agent"))
		}
		var response any
		switch {
		case r.URL.Path == "/repos/example/sample/releases":
			if r.URL.Query().Get("page") != "1" {
				response = []githubRelease{}
			} else {
				response = releases
			}
		case strings.HasPrefix(r.URL.Path, "/repos/example/sample/releases/tags/"):
			tag := strings.TrimPrefix(r.URL.Path, "/repos/example/sample/releases/tags/")
			for _, rel := range releases {
				if rel.TagName == tag {
					response = rel
					break
				}
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
		if response == nil {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	base := server.URL + "/repos/example/sample"
	for i := range releases {
		for j := range releases[i].Assets {
			releases[i].Assets[j].APIURL = fmt.Sprintf("%s/releases/assets/%d", base, j+1)
		}
	}
	return &Client{RepositoryAPI: base, CurrentVersion: "v1.0.0", MinimumVersion: "v1.0.0", ArchiveName: func(v string) string { return "sample_" + v + ".zip" }, UserAgent: "sample/1"}, requests
}

func TestCheckAndList(t *testing.T) {
	var releases []githubRelease
	for i := 13; i >= 1; i-- {
		releases = append(releases, testRelease("", fmt.Sprintf("v1.%d.0", i), true))
	}
	releases = append(releases,
		testRelease("", "v1.14.0-rc.1", true),
		testRelease("", "v1.13.0-rc.1", true),
		testRelease("", "v1.14.0-rc.2", false),
		testRelease("", "v1.0.0", true),
	)
	c, _ := testClient(t, releases)
	result, err := c.Check(context.Background())
	if err != nil || result.Latest != "v1.13.0" || !result.UpdateAvailable {
		t.Fatalf("check: %+v, %v", result, err)
	}
	list, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 12 || list[0].Version != "v1.14.0-rc.1" || list[1].Version != "v1.13.0" || list[len(list)-1].Version != "v1.0.0" || !list[len(list)-1].Current {
		t.Fatalf("unexpected list: %+v", list)
	}
	for _, entry := range list {
		if entry.Version == "v1.13.0-rc.1" || entry.Version == "v1.14.0-rc.2" {
			t.Fatalf("ineligible prerelease: %+v", entry)
		}
	}
}

func TestTargetRejectsIneligibleWithoutWriting(t *testing.T) {
	releases := []githubRelease{
		testRelease("", "v1.2.0", true),
		testRelease("", "v1.1.0", false),
		testRelease("", "v1.0.0", true),
	}
	c, requests := testClient(t, releases)
	c.CurrentVersion = "v1.2.0"
	if _, err := c.Target(context.Background(), "v0.9.0"); err == nil || *requests != 0 {
		t.Fatalf("under minimum: %v, requests: %d", err, *requests)
	}
	if _, err := c.Target(context.Background(), "v1.1.0"); err == nil {
		t.Fatal("unsigned release accepted")
	}
	target, err := c.Target(context.Background(), "v1.0.0")
	if err != nil || !target.Older || target.Current || target.Version != "v1.0.0" {
		t.Fatalf("rollback: %+v, %v", target, err)
	}
	c.CurrentVersion = "v1.0.0"
	current, err := c.Target(context.Background(), "v1.0.0")
	if err != nil || !current.Current {
		t.Fatalf("current: %+v, %v", current, err)
	}
}

func TestReleaseMetadataCannotRedirectAssets(t *testing.T) {
	r := testRelease("", "v1.1.0", true)
	c, _ := testClient(t, []githubRelease{r})
	for i := range r.Assets {
		r.Assets[i].APIURL = fmt.Sprintf("%s/releases/assets/%d", c.RepositoryAPI, i+1)
	}
	r.Assets[0].APIURL = "https://example.org/archive.zip"
	p, err := c.validate()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.eligible(r, p); ok {
		t.Fatal("foreign asset URL accepted")
	}
	r.Assets[0].APIURL = c.RepositoryAPI + "/releases/assets/1"
	r.Assets = append(r.Assets, r.Assets[0])
	if _, ok := c.eligible(r, p); ok {
		t.Fatal("duplicate archive accepted")
	}
}

func TestSemver(t *testing.T) {
	for _, bad := range []string{"1.2.3", "v1.2", "v01.2.3", "v1.2.3-01", "v1.2.3+", "v1.2.3+bad..meta", "v1.2.3-rc_1"} {
		if _, err := parseSemver(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, pair := range [][2]string{{"v1.2.3-rc.2", "v1.2.3-rc.10"}, {"v1.2.3-rc.10", "v1.2.3"}, {"v1.2.3", "v1.3.0"}} {
		a, _ := parseSemver(pair[0])
		b, _ := parseSemver(pair[1])
		if a.compare(b) >= 0 {
			t.Errorf("%s should precede %s", pair[0], pair[1])
		}
	}
}

func TestTargetErrorsNameTheReason(t *testing.T) {
	c, _ := testClient(t, []githubRelease{testRelease("", "v1.2.0", false)})
	for tag, want := range map[string]TargetReason{"1.2.0": TargetInvalidVersion, "v0.9.0": TargetBelowMinimum, "v1.3.0": TargetNotFound, "v1.2.0": TargetIneligible} {
		var te *TargetError
		if _, err := c.Target(context.Background(), tag); !errors.As(err, &te) || te.Reason != want || te.Version != tag {
			t.Errorf("%s: %v", tag, err)
		}
	}
}

func TestDevelopmentBuildCannotUpdate(t *testing.T) {
	c, requests := testClient(t, nil)
	for _, version := range []string{"", "dev"} {
		c.CurrentVersion = version
		if _, err := c.Check(context.Background()); !errors.Is(err, ErrDevelopmentBuild) {
			t.Errorf("%q: %v", version, err)
		}
		if _, err := c.Update(context.Background()); !errors.Is(err, ErrDevelopmentBuild) {
			t.Errorf("%q: %v", version, err)
		}
	}
	if *requests != 0 {
		t.Fatalf("development build sent %d requests", *requests)
	}
}

func TestTokenGoesOnlyToRepositoryHost(t *testing.T) {
	c := &Client{RepositoryAPI: "https://api.github.com/repos/example/sample", CurrentVersion: "v1.0.0", MinimumVersion: "v1.0.0",
		ArchiveName: func(string) string { return "sample.zip" }, UserAgent: "sample/1", Token: "test-token"}
	p, err := c.validate()
	if err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[string]string{
		"https://api.github.com/repos/example/sample/releases/assets/1": "Bearer test-token",
		"https://uploads.github.com/repos/example/sample":               "",
		"http://api.github.com/repos/example/sample":                    "",
		"https://api.github.com.example.org/repos/example/sample":       "",
	} {
		r, err := c.newRequest(context.Background(), p, raw, "application/octet-stream")
		if err != nil || r.Header.Get("Authorization") != want {
			t.Errorf("%s: %q, %v", raw, r.Header.Get("Authorization"), err)
		}
	}
	c.Token = "test-token\r\nX-Injected: 1"
	if _, err := c.validate(); err == nil {
		t.Fatal("token with line break accepted")
	}
}

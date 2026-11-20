package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// notedRelease is a release as the API sends it, with notes the client does not decode.
type notedRelease struct {
	githubRelease
	Body string `json:"body"`
}

// pagedClient serves releases through a server that honours per_page and page, and counts list requests.
func pagedClient(t *testing.T, releases []notedRelease) (*Client, *int) {
	t.Helper()
	listRequests := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var response any
		switch {
		case r.URL.Path == "/repos/example/sample/releases":
			*listRequests++
			perPage, err1 := strconv.Atoi(r.URL.Query().Get("per_page"))
			page, err2 := strconv.Atoi(r.URL.Query().Get("page"))
			if err1 != nil || err2 != nil || perPage < 1 || page < 1 {
				http.Error(w, "bad paging", http.StatusBadRequest)
				return
			}
			start := min((page-1)*perPage, len(releases))
			response = releases[start:min(start+perPage, len(releases))]
		case strings.HasPrefix(r.URL.Path, "/repos/example/sample/releases/tags/"):
			tag := strings.TrimPrefix(r.URL.Path, "/repos/example/sample/releases/tags/")
			for _, rel := range releases {
				if rel.TagName == tag {
					response = rel
				}
			}
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
			releases[i].Assets[j].APIURL = fmt.Sprintf("%s/releases/assets/%d", base, i*100+j+1)
		}
	}
	return &Client{RepositoryAPI: base, CurrentVersion: "v1.0.0", MinimumVersion: "v1.0.0", ArchiveName: func(v string) string { return "sample_" + v + ".zip" },
		UserAgent: "sample/1", SignatureAsset: "checksums.txt.example.sig"}, listRequests
}

// fullRelease carries the signed release files, twelve further assets and notes of the given size.
func fullRelease(tag string, notes int) notedRelease {
	r := notedRelease{githubRelease: testRelease("", tag, true), Body: strings.Repeat("n", notes)}
	for i := len(r.Assets); i < 15; i++ {
		r.Assets = append(r.Assets, asset{Name: fmt.Sprintf("sample_%s_extra_%d.tar.gz", tag, i)})
	}
	return r
}

func TestReleaseListReadsLargePages(t *testing.T) {
	// Oldest first, so the newest stable release and the newer prerelease sit on the last page.
	var releases []notedRelease
	for i := 0; i < 249; i++ {
		releases = append(releases, fullRelease(fmt.Sprintf("v1.%d.0", i), 20_000))
	}
	releases = append(releases, fullRelease("v1.249.0-rc.1", 20_000))
	c, listRequests := pagedClient(t, releases)
	for i := 0; i < len(releases); i += pageSize {
		page, err := json.Marshal(releases[i:min(i+pageSize, len(releases))])
		if err != nil {
			t.Fatal(err)
		}
		if i < 200 && len(page) <= 2*maxMetadataBytes {
			t.Fatalf("page at %d has only %d bytes", i, len(page))
		}
	}

	result, err := c.Check(context.Background())
	if err != nil || result.Latest != "v1.248.0" || !result.UpdateAvailable {
		t.Fatalf("check: %+v, %v", result, err)
	}
	if *listRequests != 3 {
		t.Fatalf("check sent %d list requests, want 3", *listRequests)
	}
	*listRequests = 0
	list, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 12 || list[0].Version != "v1.249.0-rc.1" || list[1].Version != "v1.248.0" || list[10].Version != "v1.239.0" || !list[11].Current || *listRequests != 3 {
		t.Fatalf("list: %+v, requests %d", list, *listRequests)
	}
}

func TestReleaseListRejectsOversizedPage(t *testing.T) {
	c, _ := pagedClient(t, []notedRelease{fullRelease("v1.1.0", maxReleasePageBytes)})
	if _, err := c.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("check: %v", err)
	}
	if _, err := c.List(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("list: %v", err)
	}
}

func TestTargetRejectsOversizedRelease(t *testing.T) {
	c, _ := pagedClient(t, []notedRelease{fullRelease("v1.1.0", maxMetadataBytes), fullRelease("v1.2.0", 20_000)})
	if _, err := c.Target(context.Background(), "v1.1.0"); err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("oversized release: %v", err)
	}
	if target, err := c.Target(context.Background(), "v1.2.0"); err != nil || target.Version != "v1.2.0" {
		t.Fatalf("release within the limit: %+v, %v", target, err)
	}
}

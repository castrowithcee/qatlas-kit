package sysservice

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
	"golang.org/x/crypto/ssh"
)

// recorder returns a runner that records the calls and fails the one named by failOn.
func recorder(calls *[]string, failOn string) Option {
	return WithRunner(func(_ context.Context, args ...string) error {
		call := strings.Join(args, " ")
		*calls = append(*calls, call)
		if strings.HasPrefix(call, failOn) && failOn != "" {
			return errors.New("boom")
		}
		return nil
	})
}

func TestRestartReloadsOnlyForWrittenUnit(t *testing.T) {
	root := t.TempDir()
	unit := Companion("tool", WithRoot(root)).Path
	tests := []struct {
		name      string
		companion []string
		want      []string
	}{
		{"new unit", []string{"/other", unit}, []string{"daemon-reload", "restart tool"}},
		{"unchanged unit", nil, []string{"restart tool"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			err := Restart(context.Background(), "tool", selfupdate.Result{Latest: "v1.1.0", Companions: tt.companion}, WithRoot(root), recorder(&calls, ""))
			if err != nil || !slices.Equal(calls, tt.want) {
				t.Fatalf("err %v, calls %q, want %q", err, calls, tt.want)
			}
		})
	}
}

func TestRestartErrorsNameVersionAndStep(t *testing.T) {
	result := selfupdate.Result{Current: "v1.0.0", Latest: "v1.1.0", Companions: []string{Companion("tool").Path}}
	var calls []string
	err := Restart(context.Background(), "tool", result, recorder(&calls, "daemon-reload"))
	if err == nil || !strings.Contains(err.Error(), "v1.1.0") || !strings.Contains(err.Error(), "daemon-reload") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("reload error: %v", err)
	}
	if strings.Contains(err.Error(), "v1.0.0") || !slices.Equal(calls, []string{"daemon-reload"}) {
		t.Fatalf("error %v, calls %q", err, calls)
	}
	calls = nil
	err = Restart(context.Background(), "tool", result, recorder(&calls, "restart"))
	if err == nil || !strings.Contains(err.Error(), "v1.1.0") || !strings.Contains(err.Error(), "restart tool") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("restart error: %v", err)
	}
}

func TestRestartRejectsInvalidProgramWithoutCalling(t *testing.T) {
	for _, program := range []string{"", "Tool", "../tool", "tool --now"} {
		var calls []string
		if err := Restart(context.Background(), program, selfupdate.Result{}, recorder(&calls, "")); err == nil || len(calls) != 0 {
			t.Fatalf("%q: err %v, calls %q", program, err, calls)
		}
	}
}

func TestCompanionPassesSelfupdatePathCheck(t *testing.T) {
	root := t.TempDir()
	c := Companion("tool", WithRoot(root))
	if c.Entry != "lib/systemd/system/tool.service" || filepath.Base(c.Path) != "tool.service" || !filepath.IsAbs(c.Path) {
		t.Fatalf("companion: %+v", c)
	}
	if got := Companion("tool"); got.Path != "/usr/local/lib/systemd/system/tool.service" {
		t.Fatalf("path without root: %q", got.Path)
	}
}

func TestShadowed(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "etc", "systemd", "system")
	if err := os.MkdirAll(filepath.Join(dir, "tool.service.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool.service.d", "override.conf"), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check := func(program string, want bool) {
		t.Helper()
		got, err := Shadowed(program, WithRoot(root))
		if err != nil || got != want {
			t.Fatalf("Shadowed(%q) = %v, %v; want %v", program, got, err, want)
		}
	}
	check("tool", false) // drop-in directory only
	if err := os.WriteFile(filepath.Join(dir, "tool.service"), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("tool", true)
	check("other", false)
	if runtime.GOOS != "windows" {
		if err := os.Symlink("/dev/null", filepath.Join(dir, "linked.service")); err != nil {
			t.Fatal(err)
		}
		check("linked", true)
	}
	if err := os.Mkdir(filepath.Join(dir, "dir.service"), 0o755); err != nil {
		t.Fatal(err)
	}
	check("dir", false)
	if _, err := Shadowed("Bad Name", WithRoot(root)); err == nil {
		t.Fatal("invalid program accepted")
	}
}

// releaseAPI is a minimal release API for one repository with a single release.
func releaseAPI(t *testing.T, tag string, assets map[string][]byte) string {
	t.Helper()
	// Asset IDs are numeric, as on GitHub.
	ids, names := map[string]int{}, map[int]string{}
	for i, name := range slices.Sorted(maps.Keys(assets)) {
		ids[name], names[i+1] = i+1, name
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const base = "/repos/example/tool"
		switch {
		case r.URL.Path == base+"/releases" && r.URL.Query().Get("page") == "1",
			r.URL.Path == base+"/releases/tags/"+tag:
			type asset struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			}
			rel := struct {
				Tag    string  `json:"tag_name"`
				Assets []asset `json:"assets"`
			}{Tag: tag}
			for _, name := range slices.Sorted(maps.Keys(assets)) {
				rel.Assets = append(rel.Assets, asset{name, srv.URL + base + "/releases/assets/" + strconv.Itoa(ids[name])})
			}
			var out any = rel
			if strings.HasSuffix(r.URL.Path, "/releases") {
				out = []any{rel}
			}
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, base+"/releases/assets/"):
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, base+"/releases/assets/"))
			body, ok := assets[names[id]]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		case r.URL.Path == base+"/releases" && r.URL.Query().Get("page") != "1":
			_, _ = w.Write([]byte("[]"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/repos/example/tool"
}

func TestUpdateWithUnitThenRestart(t *testing.T) {
	root := t.TempDir()
	const namespace = "tool-release"
	signer := releasetest.NewSigner(t)
	archiveName := func(v string) string { return "tool_" + v + "_linux_amd64.tar.gz" }
	archive := releasetest.Archive(t, archiveName("v1.1.0"),
		releasetest.File{Name: "bin/tool", Body: []byte("new program")},
		releasetest.File{Name: UnitEntry("tool"), Body: []byte(validUnit)})
	assets := releasetest.Release(t, signer, namespace, "checksums.txt.example.sig", map[string][]byte{archiveName("v1.1.0"): archive})
	api := releaseAPI(t, "v1.1.0", assets)

	executable := filepath.Join(root, "usr", "local", "bin", "tool")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("old program"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &selfupdate.Client{RepositoryAPI: api, CurrentVersion: "v1.0.0", MinimumVersion: "v1.0.0", ArchiveName: archiveName,
		UserAgent: "tool/1", HTTPClient: http.DefaultClient, Namespace: namespace, SignatureAsset: "checksums.txt.example.sig", TrustedKeys: []ssh.PublicKey{signer.PublicKey()},
		ProgramName: "tool", Executable: executable,
		Companions: []selfupdate.Companion{Companion("tool", WithRoot(root))}}
	result, err := c.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(root, "usr", "local", "lib", "systemd", "system", "tool.service")
	if !result.Updated || !slices.Equal(result.Companions, []string{unit}) {
		t.Fatalf("result: %+v", result)
	}
	if body, err := os.ReadFile(unit); err != nil || string(body) != validUnit {
		t.Fatalf("unit: %q, %v", body, err)
	}
	var calls []string
	if err := Restart(context.Background(), "tool", result, WithRoot(root), recorder(&calls, "")); err != nil {
		t.Fatal(err)
	}
	if want := []string{"daemon-reload", "restart tool"}; !slices.Equal(calls, want) {
		t.Fatalf("calls %q, want %q", calls, want)
	}
}

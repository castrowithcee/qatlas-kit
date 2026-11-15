package releasebuild_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release"
	"github.com/castrowithcee/qatlas-kit/release/releasebuild"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
	"golang.org/x/crypto/ssh"
)

const (
	previousProgram   = "old-sample"
	previousNamespace = "old-sample-release"
	previousSignature = "checksums.txt.old.sig"
)

func previousConfig(m *module) releasebuild.Config {
	c := m.config()
	c.ManpageCommand = []string{os.Args[0], "manpage", "{dir}", "{program}"}
	c.PreviousArchive = &releasebuild.PreviousArchive{Program: previousProgram}
	return c
}

func platformName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func TestBuildWritesPreviousArchive(t *testing.T) {
	m := newModule(t)
	t.Setenv(manpageMode, "program")
	o := m.options(t)
	names, err := releasebuild.Build(context.Background(), previousConfig(m), o)
	if err != nil {
		t.Fatal(err)
	}
	assets := readAssets(t, o.Output)
	if !slices.Equal(names, slices.Sorted(maps.Keys(assets))) {
		t.Fatalf("assets = %v, output holds %v", names, slices.Sorted(maps.Keys(assets)))
	}
	checksums := assets["checksums.txt"]
	signature := releasetest.Sign(t, m.signer, previousNamespace, "sha512", checksums)
	keys := []ssh.PublicKey{m.signer.PublicKey()}
	for _, target := range targets() {
		goos, goarch, _ := strings.Cut(target, "/")
		current := releasebuild.ArchiveName(program, version, goos, goarch)
		previous := releasebuild.ArchiveName(previousProgram, version, goos, goarch)
		if err := release.VerifyArchive(checksums, signature, assets[previous], previousNamespace, previous, keys); err != nil {
			t.Errorf("VerifyArchive(%s): %v", previous, err)
		}
		entries := listArchive(t, previous, assets[previous])
		wantEntries := []string{"bin/", "bin/" + platformName(previousProgram, goos)}
		if goos != "windows" {
			wantEntries = append(wantEntries, "share/", "share/man/", "share/man/man1/", "share/man/man1/"+previousProgram+".1")
		}
		slices.Sort(wantEntries)
		if got := slices.Sorted(maps.Keys(entries)); !slices.Equal(got, wantEntries) {
			t.Errorf("%s entries = %v, want %v", previous, got, wantEntries)
		}
		for name, e := range entries {
			if !e.modTime.Equal(sourceDate) {
				t.Errorf("%s: %s has time %v", previous, name, e.modTime)
			}
		}
		prog := entries["bin/"+platformName(previousProgram, goos)]
		if prog.mode != 0o755 {
			t.Errorf("%s: program has mode %v", previous, prog.mode)
		}
		if want := listArchive(t, current, assets[current])["bin/"+exeName(goos)].body; !bytes.Equal(prog.body, want) {
			t.Errorf("%s: program differs from %s", previous, current)
		}
		if goos != "windows" {
			if page := string(entries["share/man/man1/"+previousProgram+".1"].body); page != ".TH "+previousProgram+" 1\n" {
				t.Errorf("%s: manpage = %q", previous, page)
			}
			if page := string(listArchive(t, current, assets[current])["share/man/man1/"+program+".1"].body); page != ".TH "+program+" 1\n" {
				t.Errorf("%s: manpage = %q", current, page)
			}
		}
	}

	again := o
	again.Output = filepath.Join(t.TempDir(), "again")
	if _, err := releasebuild.Build(context.Background(), previousConfig(m), again); err != nil {
		t.Fatal(err)
	}
	for name, data := range readAssets(t, again.Output) {
		if !bytes.Equal(data, assets[name]) {
			t.Errorf("%s differs between two builds", name)
		}
	}

	plain := m.options(t)
	c := previousConfig(m)
	c.PreviousArchive = nil
	if _, err := releasebuild.Build(context.Background(), c, plain); err != nil {
		t.Fatal(err)
	}
	plainAssets := readAssets(t, plain.Output)
	for name, data := range plainAssets {
		if name != "checksums.txt" && !bytes.Equal(data, assets[name]) {
			t.Errorf("%s differs from the build without previous archive", name)
		}
	}
	if len(assets) != len(plainAssets)+len(targets()) {
		t.Errorf("assets = %d, want %d", len(assets), len(plainAssets)+len(targets()))
	}

	t.Run("previous updater", func(t *testing.T) { updateAsPreviousRelease(t, m, assets, signature) })
}

// updateAsPreviousRelease verifies and installs each previous archive the way the updater of a release
// before the rename does: with the previous program, archive name, manpage, namespace and signature file.
func updateAsPreviousRelease(t *testing.T, m *module, assets map[string][]byte, signature []byte) {
	files := map[string][]byte{previousSignature: signature}
	maps.Copy(files, assets)
	names := slices.Sorted(maps.Keys(files))
	base := "/repos/example/old-sample"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == base+"/releases/tags/"+version:
			type asset struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			}
			var list []asset
			for i, name := range names {
				list = append(list, asset{name, fmt.Sprintf("%s%s/releases/assets/%d", server.URL, base, i+1)})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": version, "assets": list})
		case strings.HasPrefix(r.URL.Path, base+"/releases/assets/"):
			var i int
			if _, err := fmt.Sscanf(strings.TrimPrefix(r.URL.Path, base+"/releases/assets/"), "%d", &i); err != nil || i < 1 || i > len(names) {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(files[names[i-1]])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	for _, target := range targets() {
		goos, goarch, _ := strings.Cut(target, "/")
		name := releasebuild.ArchiveName(previousProgram, version, goos, goarch)
		prefix := t.TempDir()
		executable := filepath.Join(prefix, "bin", platformName(previousProgram, runtime.GOOS))
		var companions []selfupdate.Companion
		if goos != "windows" {
			companions = append(companions, selfupdate.Companion{Entry: "share/man/man1/" + previousProgram + ".1", Path: filepath.Join(prefix, previousProgram+".1")})
		}
		client := &selfupdate.Client{
			RepositoryAPI:  server.URL + base,
			CurrentVersion: "v1.0.0",
			MinimumVersion: "v1.0.0",
			ArchiveName:    func(string) string { return name },
			UserAgent:      "old-sample/1",
			Namespace:      previousNamespace,
			SignatureAsset: previousSignature,
			TrustedKeys:    []ssh.PublicKey{m.signer.PublicKey()},
			ProgramName:    previousProgram,
			Companions:     companions,
			Executable:     executable,
		}
		selected, err := client.Target(context.Background(), version)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		prepared, err := client.Prepare(context.Background(), selected)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if goos != runtime.GOOS || goarch != runtime.GOARCH {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(executable, []byte("old program"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := client.InstallPrepared(context.Background(), prepared); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out, err := exec.Command(executable).Output(); err != nil || string(out) != "sample "+version+"\n" {
			t.Errorf("updated program prints %q, %v", out, err)
		}
		for _, companion := range companions {
			if _, err := os.Stat(companion.Path); err != nil {
				t.Error(err)
			}
		}
	}
}

func TestBuildRejectsInvalidPreviousArchive(t *testing.T) {
	tests := map[string]func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options){
		"program equal to current": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.PreviousArchive.Program = program
		},
		"program empty": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.PreviousArchive.Program = ""
		},
		"program with upper case": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.PreviousArchive.Program = "Old-sample"
		},
		"program with separator": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.PreviousArchive.Program = "old/sample"
		},
		"program with underscore": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.PreviousArchive.Program = "old_sample"
		},
		"with service": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			service := serviceConfig(t, m)
			c.Targets, c.Service = service.Targets, service.Service
		},
		"manpage command without program": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.ManpageCommand = []string{os.Args[0], "manpage", "{dir}"}
		},
		"signature asset is a previous archive": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			goos, goarch, _ := strings.Cut(c.Targets[0], "/")
			o.SignatureAsset = releasebuild.ArchiveName(previousProgram, version, goos, goarch)
		},
		"previous signature asset is a previous archive": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			goos, goarch, _ := strings.Cut(c.Targets[0], "/")
			o.PreviousSignature = &releasebuild.PreviousSignature{Namespace: previousNamespace, SignatureAsset: releasebuild.ArchiveName(previousProgram, version, goos, goarch)}
		},
		"manpage command fails for previous program": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			t.Setenv(manpageMode, "fail-previous")
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := newModule(t)
			t.Setenv(manpageMode, "program")
			c, o := previousConfig(m), m.options(t)
			mutate(t, m, &c, &o)
			if _, err := releasebuild.Build(context.Background(), c, o); err == nil {
				t.Fatal("Build accepted invalid input")
			}
			if _, err := os.Stat(o.Output); !os.IsNotExist(err) {
				t.Errorf("output was created: %v", err)
			}
		})
	}
}

func TestLoadConfigReadsPreviousArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.json")
	write := func(body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"versionVariable": "main.version", "manpageCommand": ["gen", "{dir}", "{program}"], "previousArchive": {"program": "old-sample"}}`)
	c, err := releasebuild.LoadConfig(path)
	if err != nil || c.PreviousArchive == nil || c.PreviousArchive.Program != "old-sample" {
		t.Fatalf("LoadConfig = %+v, %v", c, err)
	}
	write(`{"previousArchive": {"program": "old-sample", "files": []}}`)
	if _, err := releasebuild.LoadConfig(path); err == nil {
		t.Fatal("LoadConfig accepted an unknown previous archive field")
	}
}

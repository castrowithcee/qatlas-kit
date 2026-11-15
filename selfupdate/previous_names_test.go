package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

const (
	oldProgram = "old-sample"
	oldManpage = "share/man/man1/old-sample.1"
	manpage    = "share/man/man1/sample.1"
)

// renamedArchiveName names the archives of releases up to v1.5.0 after the previous program, as an
// application that renamed its program does.
func renamedArchiveName(ext string) func(string) string {
	return func(version string) string {
		program := "sample"
		last, _ := parseSemver("v1.5.0")
		if v, err := parseSemver(version); err == nil && v.compare(last) <= 0 {
			program = oldProgram
		}
		return program + "_" + version + "_test_amd64" + ext
	}
}

// renamedClient is a client on v2.0.0 whose releases up to v1.5.0 carry the previous signature set,
// program name and manpage. The manpages install below prefix.
func renamedClient(api *releaseAPI, signer ssh.Signer, prefix, executable, ext string) *Client {
	c := transitionClient(api, signer, executable)
	c.ArchiveName = renamedArchiveName(ext)
	c.Companions = []Companion{{Entry: manpage, Path: filepath.Join(prefix, "man", "sample.1")}}
	c.PreviousSignature.ProgramName = oldProgram
	c.PreviousSignature.ReplaceCompanions = true
	c.PreviousSignature.Companions = []Companion{{Entry: oldManpage, Path: filepath.Join(prefix, "man", "old-sample.1")}}
	c.ValidateRollback = func(context.Context, *Prepared) error { return nil }
	return c
}

// publishRenamed publishes version with an archive of the given files, signed with the set of version.
func publishRenamed(t *testing.T, api *releaseAPI, signer ssh.Signer, c *Client, version string, files ...releasetest.File) {
	t.Helper()
	v, err := parseSemver(version)
	if err != nil {
		t.Fatal(err)
	}
	namespace, asset := c.signatureSet(v)
	name := c.ArchiveName(version)
	api.publish(version, releasetest.Release(t, signer, namespace, asset, map[string][]byte{name: releasetest.Archive(t, name, files...)}))
}

func writeCompanion(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, name, body)
}

func file(name, body string) releasetest.File {
	return releasetest.File{Name: name, Body: []byte(body)}
}

func prepareVersion(c *Client, version string) (*Prepared, error) {
	target, err := c.Target(context.Background(), version)
	if err != nil {
		return nil, err
	}
	return c.Prepare(context.Background(), target)
}

func TestPreviousNamesRollbackAcrossBoundary(t *testing.T) {
	for _, tc := range []struct{ name, ext, program string }{
		{"tar", ".tar.gz", "bin/" + oldProgram},
		{"zip", ".zip", "bin/" + oldProgram + ".exe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signer := releasetest.NewSigner(t)
			api := newReleaseAPI(t)
			prefix, executable := installation(t)
			c := renamedClient(api, signer, prefix, executable, tc.ext)
			writeCompanion(t, c.Companions[0].Path, "current man")
			publishRenamed(t, api, signer, c, "v1.4.0", file(tc.program, "program v1.4.0"), file(oldManpage, "old man v1.4.0"))
			p, err := prepareVersion(c, "v1.4.0")
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.InstallPrepared(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			assertFile(t, executable, "program v1.4.0")
			assertFile(t, c.PreviousSignature.Companions[0].Path, "old man v1.4.0")
			assertFile(t, c.Companions[0].Path, "current man")
			if len(result.Companions) != 1 || result.Companions[0] != c.PreviousSignature.Companions[0].Path {
				t.Fatalf("result: %+v", result)
			}
		})
	}
}

func TestPreviousNamesExecutableMayCarryPreviousName(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	prefix, _ := installation(t)
	executable := filepath.Join(prefix, "bin", programFile(oldProgram))
	writeFile(t, executable, "old program")
	c := renamedClient(api, signer, prefix, executable, ".tar.gz")
	publishRenamed(t, api, signer, c, "v1.4.0", file("bin/"+oldProgram, "program v1.4.0"), file(oldManpage, "old man"))
	publishRenamed(t, api, signer, c, "v2.1.0", file("bin/sample", "program v2.1.0"), file(manpage, "man"))
	p, err := prepareVersion(c, "v1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.InstallPrepared(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	assertFile(t, executable, "program v1.4.0")
	// A later release accepts only the current program name.
	if p, err = prepareVersion(c, "v2.1.0"); err != nil {
		t.Fatal(err)
	}
	var unsupported *UnsupportedInstallationError
	if _, err := c.InstallPrepared(context.Background(), p); !errors.As(err, &unsupported) {
		t.Fatalf("later release under the previous name: %v", err)
	}
	assertFile(t, executable, "program v1.4.0")
}

func TestPreviousNamesUpdateAfterBoundaryUsesCurrentEntries(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	prefix, executable := installation(t)
	c := renamedClient(api, signer, prefix, executable, ".tar.gz")
	publishRenamed(t, api, signer, c, "v2.1.0", file("bin/sample", "program v2.1.0"), file(manpage, "man v2.1.0"),
		file("bin/"+oldProgram, "foreign program"), file(oldManpage, "foreign man"))
	result, err := c.Update(context.Background())
	if err != nil || !result.Updated || result.Latest != "v2.1.0" {
		t.Fatalf("update: %+v, %v", result, err)
	}
	assertFile(t, executable, "program v2.1.0")
	assertFile(t, c.Companions[0].Path, "man v2.1.0")
	assertNoFile(t, c.PreviousSignature.Companions[0].Path)
}

func TestPreviousNamesKeepCurrentEntriesWhenUnset(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	prefix, executable := installation(t)
	c := renamedClient(api, signer, prefix, executable, ".tar.gz")
	c.ArchiveName = tarName
	c.PreviousSignature.ProgramName = ""
	c.PreviousSignature.ReplaceCompanions = false
	c.PreviousSignature.Companions = nil
	publishRenamed(t, api, signer, c, "v1.4.0", file("bin/sample", "program v1.4.0"), file(manpage, "man v1.4.0"))
	p, err := prepareVersion(c, "v1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.InstallPrepared(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	assertFile(t, executable, "program v1.4.0")
	assertFile(t, c.Companions[0].Path, "man v1.4.0")
}

func TestPreviousNamesWithoutCompanions(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	prefix, executable := installation(t)
	c := renamedClient(api, signer, prefix, executable, ".tar.gz")
	c.PreviousSignature.Companions = nil
	writeCompanion(t, c.Companions[0].Path, "current man")
	publishRenamed(t, api, signer, c, "v1.4.0", file("bin/"+oldProgram, "program v1.4.0"))
	p, err := prepareVersion(c, "v1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.InstallPrepared(context.Background(), p)
	if err != nil || len(result.Companions) != 0 {
		t.Fatalf("install: %+v, %v", result, err)
	}
	assertFile(t, executable, "program v1.4.0")
	assertFile(t, c.Companions[0].Path, "current man")
}

func TestPreviousNamesArchiveMustMatchVersionsSet(t *testing.T) {
	cases := []struct {
		name    string
		version string
		files   []releasetest.File
		want    string
	}{
		{"current program up to boundary", "v1.4.0", []releasetest.File{file("bin/sample", "p"), file(oldManpage, "m")}, "archive does not contain bin/" + oldProgram},
		{"previous program after boundary", "v2.1.0", []releasetest.File{file("bin/"+oldProgram, "p"), file(manpage, "m")}, "archive does not contain bin/sample"},
		{"current manpage up to boundary", "v1.4.0", []releasetest.File{file("bin/"+oldProgram, "p"), file(manpage, "m")}, "archive has no " + oldManpage},
		{"previous manpage after boundary", "v2.1.0", []releasetest.File{file("bin/sample", "p"), file(oldManpage, "m")}, "archive has no " + manpage},
		{"empty previous manpage", "v1.4.0", []releasetest.File{file("bin/"+oldProgram, "p"), file(oldManpage, "")}, "archive has no " + oldManpage},
		{"empty previous program", "v1.4.0", []releasetest.File{file("bin/"+oldProgram, ""), file(oldManpage, "m")}, "archive has no bin/" + oldProgram},
		{"duplicate previous manpage", "v1.4.0", []releasetest.File{file("bin/"+oldProgram, "p"), file(oldManpage, "m"), file("./"+oldManpage, "m")}, "duplicate archive entry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signer := releasetest.NewSigner(t)
			api := newReleaseAPI(t)
			prefix, executable := installation(t)
			c := renamedClient(api, signer, prefix, executable, ".tar.gz")
			publishRenamed(t, api, signer, c, tc.version, tc.files...)
			if _, err := prepareVersion(c, tc.version); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("prepare: %v; want %q", err, tc.want)
			}
			assertFile(t, executable, "old program")
		})
	}
}

func TestPreviousNamesCompanionMustBeRegularFile(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	prefix, executable := installation(t)
	c := renamedClient(api, signer, prefix, executable, ".tar.gz")
	name := c.ArchiveName("v1.4.0")
	archive := directoryArchive(t,
		tarEntry{name: "bin/" + oldProgram, body: "program"},
		tarEntry{name: oldManpage, body: "bin/" + oldProgram, kind: '2'})
	api.publish("v1.4.0", releasetest.Release(t, signer, oldNamespace, oldSignature, map[string][]byte{name: archive}))
	if _, err := prepareVersion(c, "v1.4.0"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("prepare: %v", err)
	}
}

func TestPreviousNamesEntriesMustBeCanonicalAndUnique(t *testing.T) {
	_, executable := installation(t)
	dir := filepath.Dir(executable)
	for _, entries := range [][]string{{"./share/old.1"}, {"../old.1"}, {"/old.1"}, {""}, {"share\\old.1"}, {"bin/" + oldProgram}, {"a/old.1", "a/old.1"}} {
		c := renamedClient(newReleaseAPI(t), releasetest.NewSigner(t), dir, executable, ".tar.gz")
		c.PreviousSignature.Companions = nil
		for _, entry := range entries {
			c.PreviousSignature.Companions = append(c.PreviousSignature.Companions, Companion{entry, filepath.Join(dir, "x")})
		}
		if _, err := c.binding("v1.4.0"); err == nil {
			t.Errorf("previous companion entries %q accepted", entries)
		}
		if _, err := c.binding("v2.1.0"); err != nil {
			t.Errorf("current entries refused because of previous entries %q: %v", entries, err)
		}
	}
}

func TestPreviousNamesMustBeValid(t *testing.T) {
	api := newReleaseAPI(t)
	signer := releasetest.NewSigner(t)
	for name, edit := range map[string]func(*PreviousSignature){
		"program with separator":     func(p *PreviousSignature) { p.ProgramName = "bin/old" },
		"program dot dot":            func(p *PreviousSignature) { p.ProgramName = ".." },
		"program equal to current":   func(p *PreviousSignature) { p.ProgramName = "sample" },
		"companions without replace": func(p *PreviousSignature) { p.ReplaceCompanions = false },
	} {
		c := renamedClient(api, signer, "/unused", "/unused", ".tar.gz")
		edit(c.PreviousSignature)
		if _, err := c.validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := renamedClient(api, signer, "/unused", "/unused", ".tar.gz").validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPreviousNamesPreparedBoundToSet(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	prefix, executable := installation(t)
	c := renamedClient(api, signer, prefix, executable, ".tar.gz")
	publishRenamed(t, api, signer, c, "v1.4.0", file("bin/"+oldProgram, "program v1.4.0"), file(oldManpage, "old man"))
	p, err := prepareVersion(c, "v1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*Client){
		"current program":     func(c *Client) { c.PreviousSignature.ProgramName = "" },
		"other program":       func(c *Client) { c.PreviousSignature.ProgramName = "other-sample" },
		"current companions":  func(c *Client) { c.PreviousSignature.ReplaceCompanions, c.PreviousSignature.Companions = false, nil },
		"no companions":       func(c *Client) { c.PreviousSignature.Companions = nil },
		"boundary before it":  func(c *Client) { c.PreviousSignature.LastVersion = "v1.3.0" },
		"no previous release": func(c *Client) { c.PreviousSignature = nil },
	} {
		other := renamedClient(api, signer, prefix, executable, ".tar.gz")
		edit(other)
		if _, err := other.InstallPrepared(context.Background(), p); err == nil || !strings.Contains(err.Error(), "different trust policy") {
			t.Errorf("%s: %v", name, err)
		}
	}
	assertFile(t, executable, "old program")
	assertNoFile(t, c.PreviousSignature.Companions[0].Path)
}

func TestPreviousNamesRollbackRefusesForeignExecutable(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	prefix, _ := installation(t)
	executable := filepath.Join(prefix, "bin", programFile("other"))
	writeFile(t, executable, "other program")
	c := renamedClient(api, signer, prefix, executable, ".tar.gz")
	publishRenamed(t, api, signer, c, "v1.4.0", file("bin/"+oldProgram, "program v1.4.0"), file(oldManpage, "old man"))
	p, err := prepareVersion(c, "v1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	var unsupported *UnsupportedInstallationError
	if _, err := c.InstallPrepared(context.Background(), p); !errors.As(err, &unsupported) {
		t.Fatalf("foreign executable: %v", err)
	}
	c.PreviousSignature.Companions[0].Path = filepath.Join(prefix, "man", "sample.1")
	c.Executable = filepath.Join(prefix, "bin", programFile("sample"))
	if _, err := c.InstallPrepared(context.Background(), p); err == nil {
		t.Fatal("previous companion installed under a foreign name")
	}
	assertFile(t, executable, "other program")
	assertFile(t, c.Executable, "old program")
	if _, err := os.Lstat(filepath.Join(prefix, "man")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manpage directory created: %v", err)
	}
}

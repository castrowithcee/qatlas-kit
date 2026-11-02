package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

const testNamespace = "sample-release"

// programFile is the file name InstallPrepared expects for the configured program on this platform.
func programFile(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func tarName(version string) string { return "sample_" + version + "_linux_amd64.tar.gz" }

// releaseAPI serves the release API of example/sample with assets built by releasetest.
type releaseAPI struct {
	*httptest.Server
	base     string
	mu       sync.Mutex
	releases []githubRelease
	blobs    map[string][]byte
	auth     []string
}

func newReleaseAPI(t *testing.T) *releaseAPI {
	t.Helper()
	s := &releaseAPI{blobs: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Server.Close)
	s.base = s.URL + "/repos/example/sample"
	return s
}

func (s *releaseAPI) publish(tag string, assets map[string][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := githubRelease{TagName: tag, Prerelease: strings.Contains(tag, "-")}
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		id := fmt.Sprint(len(s.blobs) + 1)
		s.blobs[id] = assets[name]
		r.Assets = append(r.Assets, asset{Name: name, APIURL: s.base + "/releases/assets/" + id})
	}
	s.releases = append(s.releases, r)
}

func (s *releaseAPI) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	path := strings.TrimPrefix(r.URL.Path, "/repos/example/sample")
	var response any
	switch {
	case path == "/releases":
		response = []githubRelease{}
		if r.URL.Query().Get("page") == "1" {
			response = s.releases
		}
	case strings.HasPrefix(path, "/releases/tags/"):
		for _, rel := range s.releases {
			if rel.TagName == strings.TrimPrefix(path, "/releases/tags/") {
				response = rel
			}
		}
	case strings.HasPrefix(path, "/releases/assets/"):
		if body, ok := s.blobs[strings.TrimPrefix(path, "/releases/assets/")]; ok {
			w.Write(body)
			return
		}
	}
	if response == nil {
		http.NotFound(w, r)
		return
	}
	json.NewEncoder(w).Encode(response)
}

func (s *releaseAPI) client(signer ssh.Signer, executable string) *Client {
	return &Client{RepositoryAPI: s.base, CurrentVersion: "v1.0.0", MinimumVersion: "v1.0.0", ArchiveName: tarName,
		UserAgent: "sample/1", HTTPClient: s.Client(), Namespace: testNamespace, TrustedKeys: []ssh.PublicKey{signer.PublicKey()},
		ProgramName: "sample", Executable: executable}
}

type tarEntry struct {
	name string
	body string
	kind byte
}

// directoryArchive builds a .tar.gz the way tar -C stage . does: with "./" names and directory entries.
func directoryArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o755, Typeflag: e.kind}
		switch e.kind {
		case 0:
			h.Typeflag, h.Size = tar.TypeReg, int64(len(e.body))
		case tar.TypeSymlink:
			h.Linkname = e.body
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func cliArchive(t *testing.T, program, manpage string) []byte {
	return directoryArchive(t,
		tarEntry{name: "./", kind: tar.TypeDir},
		tarEntry{name: "./bin/", kind: tar.TypeDir},
		tarEntry{name: "./bin/sample", body: program},
		tarEntry{name: "./share/", kind: tar.TypeDir},
		tarEntry{name: "./share/doc/", kind: tar.TypeDir},
		tarEntry{name: "./share/doc/sample/", kind: tar.TypeDir},
		tarEntry{name: "./share/doc/sample/LICENSE", body: "license"},
		tarEntry{name: "./share/man/", kind: tar.TypeDir},
		tarEntry{name: "./share/man/man1/", kind: tar.TypeDir},
		tarEntry{name: "./share/man/man1/sample.1", body: manpage},
	)
}

// installation creates <prefix>/bin/<program> holding "old program".
func installation(t *testing.T) (prefix, executable string) {
	t.Helper()
	prefix = t.TempDir()
	executable = filepath.Join(prefix, "bin", programFile("sample"))
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, executable, "old program")
	return prefix, executable
}

func writeFile(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, name, want string) {
	t.Helper()
	got, err := os.ReadFile(name)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
	}
}

func assertNoFile(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists: %v", name, err)
	}
}

// assertTree fails when the files below root differ from want, given as slash paths relative to root.
func assertTree(t *testing.T, root string, want ...string) {
	t.Helper()
	var got []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("files below %s = %v, want %v", root, got, want)
	}
}

func TestUpdateInstallsReleaseBuiltFromDirectory(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	archive := cliArchive(t, "new program", "new manpage")
	api.publish("v1.1.0", releasetest.Release(t, signer, testNamespace, map[string][]byte{tarName("v1.1.0"): archive}))
	prefix, executable := installation(t)
	manpage := filepath.Join(prefix, "share", "man", "man1", "sample.1")
	c := api.client(signer, executable)
	c.Token = "test-token"
	c.Companions = []Companion{{Entry: "share/man/man1/sample.1", Path: manpage}}
	var done, total int64
	c.Progress = func(d, n int64) { done, total = d, n }
	var seen ReleaseFiles
	c.BeforeReplace = func(_ context.Context, files ReleaseFiles) error {
		seen = files
		assertFile(t, executable, "old program")
		assertNoFile(t, manpage)
		read := func(name string) []byte {
			body, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			return body
		}
		if files.ArchiveName != tarName("v1.1.0") || filepath.Base(files.Archive) != files.ArchiveName {
			t.Fatalf("release files: %+v", files)
		}
		if err := release.VerifyArchive(read(files.Checksums), read(files.Signature), read(files.Archive), testNamespace, files.ArchiveName, c.TrustedKeys); err != nil {
			t.Fatalf("release files do not verify: %v", err)
		}
		return nil
	}
	after := false
	c.AfterReplace = func(_ context.Context, files ReleaseFiles) {
		after = true
		assertFile(t, executable, "new program")
		if files != seen {
			t.Fatalf("hooks saw different files: %+v, %+v", files, seen)
		}
	}
	result, err := c.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.Latest != "v1.1.0" || result.Current != "v1.0.0" || !slices.Equal(result.Companions, []string{manpage}) || !after {
		t.Fatalf("result: %+v, after hook: %v", result, after)
	}
	assertFile(t, executable, "new program")
	assertFile(t, manpage, "new manpage")
	assertTree(t, prefix, "bin/"+programFile("sample"), "share/man/man1/sample.1")
	assertNoFile(t, filepath.Dir(seen.Archive))
	if done != int64(len(archive)) || total != int64(len(archive)) {
		t.Fatalf("progress %d of %d, archive has %d bytes", done, total, len(archive))
	}
	for _, auth := range api.auth {
		if auth != "Bearer test-token" {
			t.Fatalf("authorization to the API host: %q", auth)
		}
	}
}

func TestUpdateFromZipArchiveLeavesUnchangedCompanion(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	name := func(v string) string { return "sample_" + v + "_windows_amd64.zip" }
	archive := releasetest.Archive(t, name("v1.2.0"),
		releasetest.File{Name: "bin/sample.exe", Body: []byte("new program")},
		releasetest.File{Name: "share/doc/sample/LICENSE", Body: []byte("license")})
	api.publish("v1.2.0", releasetest.Release(t, signer, testNamespace, map[string][]byte{name("v1.2.0"): archive}))
	prefix, executable := installation(t)
	license := filepath.Join(prefix, "LICENSE")
	writeFile(t, license, "license")
	c := api.client(signer, executable)
	c.ArchiveName = name
	c.Companions = []Companion{{Entry: "share/doc/sample/LICENSE", Path: license}}
	result, err := c.Update(context.Background())
	if err != nil || !result.Updated || len(result.Companions) != 0 {
		t.Fatalf("update: %+v, %v", result, err)
	}
	assertFile(t, executable, "new program")
	assertFile(t, license, "license")
	for _, auth := range api.auth {
		if auth != "" {
			t.Fatalf("authorization without token: %q", auth)
		}
	}
	c.CurrentVersion = "v1.2.0"
	again, err := c.Update(context.Background())
	if err != nil || again.Updated || again.UpdateAvailable {
		t.Fatalf("second update: %+v, %v", again, err)
	}
}

func TestPrepareRejectsUnverifiedRelease(t *testing.T) {
	signer, foreign := releasetest.NewSigner(t), releasetest.NewSigner(t)
	name := tarName("v1.1.0")
	archive := cliArchive(t, "new program", "new manpage")
	checksums := releasetest.Checksums(map[string][]byte{name: archive})
	signed := func(s ssh.Signer, namespace, hash string, sums []byte, body []byte) map[string][]byte {
		return map[string][]byte{name: body, checksumsAsset: sums, signatureAsset: releasetest.Sign(t, s, namespace, hash, sums)}
	}
	cases := []struct {
		name   string
		assets map[string][]byte
	}{
		{"foreign signature", signed(foreign, testNamespace, "sha512", checksums, archive)},
		{"wrong namespace", signed(signer, "another-release", "sha512", checksums, archive)},
		{"unsupported hash", signed(signer, testNamespace, "md5", checksums, archive)},
		{"signature over other checksums", func() map[string][]byte {
			a := signed(signer, testNamespace, "sha512", checksums, archive)
			a[checksumsAsset] = releasetest.Checksums(map[string][]byte{name: []byte("other")})
			return a
		}()},
		{"corrupt archive", signed(signer, testNamespace, "sha512", checksums, []byte("corrupt"))},
		{"archive not listed", signed(signer, testNamespace, "sha512", releasetest.Checksums(map[string][]byte{"other.tar.gz": archive}), archive)},
		{"duplicate checksum", signed(signer, testNamespace, "sha512", append(bytes.Clone(checksums), checksums...), archive)},
		{"unsafe entry", releasetest.Release(t, signer, testNamespace, map[string][]byte{name: directoryArchive(t,
			tarEntry{name: "./bin/sample", body: "new program"}, tarEntry{name: "./share/man/man1/sample.1", body: "man"},
			tarEntry{name: "../escape", body: "x"})})},
		{"duplicate program", releasetest.Release(t, signer, testNamespace, map[string][]byte{name: directoryArchive(t,
			tarEntry{name: "./bin/sample", body: "new program"}, tarEntry{name: "bin/sample", body: "other program"},
			tarEntry{name: "./share/man/man1/sample.1", body: "man"})})},
		{"missing manpage", releasetest.Release(t, signer, testNamespace, map[string][]byte{name: directoryArchive(t,
			tarEntry{name: "./bin/sample", body: "new program"})})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newReleaseAPI(t)
			api.publish("v1.1.0", tc.assets)
			prefix, executable := installation(t)
			c := api.client(signer, executable)
			c.Companions = []Companion{{Entry: "share/man/man1/sample.1", Path: filepath.Join(prefix, "sample.1")}}
			target, err := c.Target(context.Background(), "v1.1.0")
			if err != nil {
				t.Fatal(err)
			}
			if p, err := c.Prepare(context.Background(), target); err == nil {
				t.Fatalf("unverified release prepared: %v", p.Version())
			}
			if _, err := c.Update(context.Background()); err == nil {
				t.Fatal("unverified release installed")
			}
			assertFile(t, executable, "old program")
			assertTree(t, prefix, "bin/"+programFile("sample"))
		})
	}
}

func TestPrepareRefusesIneligibleTargets(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	assets := releasetest.Release(t, signer, testNamespace, map[string][]byte{tarName("v1.0.0"): cliArchive(t, "same", "man")})
	api.publish("v1.0.0", assets)
	unsigned := maps.Clone(assets)
	delete(unsigned, signatureAsset)
	api.publish("v1.1.0", unsigned)
	_, executable := installation(t)
	c := api.client(signer, executable)
	current, err := c.Target(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	var te *TargetError
	if _, err := c.Prepare(context.Background(), current); !errors.As(err, &te) || te.Reason != TargetRunning {
		t.Fatalf("running version: %v", err)
	}
	if _, err := c.Prepare(context.Background(), Target{Entry: Entry{Version: "v1.1.0"}, rel: api.releases[1]}); !errors.As(err, &te) || te.Reason != TargetIneligible {
		t.Fatalf("unsigned release: %v", err)
	}
	assertFile(t, executable, "old program")
}

func TestExtractPayloadChecksEveryEntry(t *testing.T) {
	program, man := "bin/sample", "share/man/man1/sample.1"
	ok := []tarEntry{{name: "./", kind: tar.TypeDir}, {name: "./bin/sample", body: "p"}, {name: "./share/man/man1/sample.1", body: "m"}}
	files, err := extractPayload("a.tar.gz", directoryArchive(t, ok...), program, []string{man})
	if err != nil || string(files[program]) != "p" || string(files[man]) != "m" {
		t.Fatalf("archive built from a directory: %v, %v", files, err)
	}
	files, err = extractPayload("a.tar.gz", directoryArchive(t, tarEntry{name: "bin/sample", body: "p"}, tarEntry{name: "share/man/man1/sample.1", body: "m"}), program, []string{man})
	if err != nil || string(files[program]) != "p" {
		t.Fatalf("archive without ./: %v, %v", files, err)
	}
	bad := map[string][]tarEntry{
		"double ./":           {{name: "././bin/sample", body: "p"}, {name: "./share/man/man1/sample.1", body: "m"}},
		"parent":              append(slices.Clone(ok), tarEntry{name: "./../sample", body: "x"}),
		"absolute":            append(slices.Clone(ok), tarEntry{name: "/etc/sample", body: "x"}),
		"backslash":           append(slices.Clone(ok), tarEntry{name: "share\\sample", body: "x"}),
		"unclean":             append(slices.Clone(ok), tarEntry{name: "./share//sample", body: "x"}),
		"empty name":          append(slices.Clone(ok), tarEntry{name: "", body: "x"}),
		"duplicate program":   append(slices.Clone(ok), tarEntry{name: "bin/sample", body: "q"}),
		"duplicate manpage":   append(slices.Clone(ok), tarEntry{name: "./share/man/man1/sample.1", body: "n"}),
		"duplicate other":     append(slices.Clone(ok), tarEntry{name: "./doc", body: "x"}, tarEntry{name: "doc", body: "y"}),
		"symlink program":     {{name: "./bin/sample", body: "/bin/sh", kind: tar.TypeSymlink}, {name: "./share/man/man1/sample.1", body: "m"}},
		"directory manpage":   {{name: "./bin/sample", body: "p"}, {name: "./share/man/man1/sample.1/", kind: tar.TypeDir}},
		"empty program":       {{name: "./bin/sample"}, {name: "./share/man/man1/sample.1", body: "m"}},
		"missing program":     {{name: "./share/man/man1/sample.1", body: "m"}},
		"missing companion":   {{name: "./bin/sample", body: "p"}},
		"program as root dir": {{name: ".", kind: tar.TypeDir}, {name: "./.", body: "p"}},
	}
	for name, entries := range bad {
		if _, err := extractPayload("a.tar.gz", directoryArchive(t, entries...), program, []string{man}); err == nil {
			t.Errorf("%s: archive accepted", name)
		}
	}
	zipDup := releasetest.Archive(t, "a.zip", releasetest.File{Name: "bin/sample.exe", Body: []byte("p")}, releasetest.File{Name: "./bin/sample.exe", Body: []byte("q")})
	if _, err := extractPayload("a.zip", zipDup, "bin/sample.exe", nil); err == nil {
		t.Error("zip with duplicate program accepted")
	}
	zipUnsafe := releasetest.Archive(t, "a.zip", releasetest.File{Name: "bin/sample.exe", Body: []byte("p")}, releasetest.File{Name: "../sample", Body: []byte("x")})
	if _, err := extractPayload("a.zip", zipUnsafe, "bin/sample.exe", nil); err == nil {
		t.Error("zip with unsafe entry accepted")
	}
	if _, err := extractPayload("a.rar", []byte("x"), program, nil); err == nil {
		t.Error("unknown archive format accepted")
	}
}

// prepared returns a release for c as Prepare would, with the given program and companion contents.
func prepared(t *testing.T, c *Client, version string, program string, companions ...string) *Prepared {
	t.Helper()
	p := &Prepared{version: version, archiveName: c.ArchiveName(version), executable: []byte(program)}
	for _, body := range companions {
		p.companions = append(p.companions, []byte(body))
	}
	var err error
	if p.trust, err = c.binding(version); err != nil {
		t.Fatal(err)
	}
	return p
}

func offlineClient(t *testing.T, executable string, companions ...Companion) *Client {
	return &Client{RepositoryAPI: "https://api.github.com/repos/example/sample", CurrentVersion: "v2.0.0", MinimumVersion: "v1.0.0",
		ArchiveName: tarName, UserAgent: "sample/1", Namespace: testNamespace, TrustedKeys: []ssh.PublicKey{releasetest.NewSigner(t).PublicKey()},
		ProgramName: "sample", Executable: executable, Companions: companions}
}

func TestInstallPreparedRollbackNeedsValidation(t *testing.T) {
	prefix, executable := installation(t)
	manpage := filepath.Join(prefix, "sample.1")
	writeFile(t, manpage, "old man")
	c := offlineClient(t, executable, Companion{Entry: "share/man/man1/sample.1", Path: manpage})
	p := prepared(t, c, "v1.5.0", "new", "new man")
	if _, err := c.InstallPrepared(context.Background(), p); err == nil {
		t.Fatal("rollback without validation accepted")
	}
	c.ValidateRollback = func(_ context.Context, got *Prepared) error {
		if got != p {
			t.Fatal("validation got another release")
		}
		return errors.New("config rejected")
	}
	if _, err := c.InstallPrepared(context.Background(), p); err == nil || !strings.Contains(err.Error(), "config rejected") {
		t.Fatalf("validation: %v", err)
	}
	assertFile(t, executable, "old program")
	assertFile(t, manpage, "old man")
	assertTree(t, prefix, "bin/"+programFile("sample"), "sample.1")
	c.ValidateRollback = func(context.Context, *Prepared) error { return nil }
	result, err := c.InstallPrepared(context.Background(), p)
	if err != nil || !result.Updated || result.UpdateAvailable || result.Latest != "v1.5.0" {
		t.Fatalf("rollback: %+v, %v", result, err)
	}
	assertFile(t, executable, "new")
	assertFile(t, manpage, "new man")
}

func TestInstallPreparedRestoresCompanionsWhenProgramFails(t *testing.T) {
	prefix, executable := installation(t)
	manpage := filepath.Join(prefix, "share", "man", "man1", "sample.1")
	unit := filepath.Join(prefix, "etc", "sample.service")
	if err := os.MkdirAll(filepath.Dir(manpage), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, manpage, "old man")
	c := offlineClient(t, executable, Companion{"share/man/man1/sample.1", manpage}, Companion{"lib/systemd/system/sample.service", unit})
	c.CurrentVersion = "v1.0.0"
	c.BeforeReplace = func(context.Context, ReleaseFiles) error {
		// Removing the program's directory takes the staged program with it, so its replacement fails.
		return os.RemoveAll(filepath.Dir(executable))
	}
	if _, err := c.InstallPrepared(context.Background(), prepared(t, c, "v1.1.0", "new", "new man", "new unit")); err == nil {
		t.Fatal("failed replacement reported success")
	}
	assertFile(t, manpage, "old man")
	assertNoFile(t, unit)
	assertTree(t, prefix, "share/man/man1/sample.1")
}

func TestBeforeReplaceErrorChangesNothing(t *testing.T) {
	prefix, executable := installation(t)
	manpage := filepath.Join(prefix, "sample.1")
	writeFile(t, manpage, "old man")
	c := offlineClient(t, executable, Companion{"share/man/man1/sample.1", manpage})
	c.CurrentVersion = "v1.0.0"
	var files ReleaseFiles
	c.BeforeReplace = func(_ context.Context, f ReleaseFiles) error {
		files = f
		return errors.New("handover refused")
	}
	c.AfterReplace = func(context.Context, ReleaseFiles) { t.Fatal("after hook ran") }
	if _, err := c.InstallPrepared(context.Background(), prepared(t, c, "v1.1.0", "new", "new man")); err == nil || !strings.Contains(err.Error(), "handover refused") {
		t.Fatalf("hook error: %v", err)
	}
	assertFile(t, executable, "old program")
	assertFile(t, manpage, "old man")
	assertTree(t, prefix, "bin/"+programFile("sample"), "sample.1")
	assertNoFile(t, filepath.Dir(files.Archive))
}

func TestInstallPreparedRejectsUnsupportedInstallation(t *testing.T) {
	prefix, executable := installation(t)
	other := filepath.Join(prefix, "bin", programFile("other"))
	writeFile(t, other, "other program")
	dirCompanion := filepath.Join(prefix, "dir", "sample.1")
	if err := os.MkdirAll(dirCompanion, 0o755); err != nil {
		t.Fatal(err)
	}
	unsupported := []struct {
		name string
		edit func(*Client)
	}{
		{"program name", func(c *Client) { c.Executable = other }},
		{"companion name", func(c *Client) { c.Companions[0].Path = filepath.Join(prefix, "other.1") }},
		{"companion directory", func(c *Client) { c.Companions[0].Path = dirCompanion }},
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(prefix, "link", "sample")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(executable, link); err != nil {
			t.Fatal(err)
		}
		unsupported = append(unsupported, struct {
			name string
			edit func(*Client)
		}{"program symlink", func(c *Client) { c.Executable = link }})
	}
	invalid := []struct {
		name string
		edit func(*Client)
	}{
		{"relative program", func(c *Client) { c.Executable = programFile("sample") }},
		{"relative companion", func(c *Client) { c.Companions[0].Path = "sample.1" }},
		{"companion is program", func(c *Client) { c.Companions[0].Path = executable }},
		{"duplicate companion path", func(c *Client) {
			c.Companions = append(c.Companions, Companion{"share/man/man5/sample.1", c.Companions[0].Path})
		}},
	}
	base := func() *Client {
		c := offlineClient(t, executable, Companion{"share/man/man1/sample.1", filepath.Join(prefix, "sample.1")})
		c.CurrentVersion = "v1.0.0"
		return c
	}
	install := func(edit func(*Client)) error {
		c := base()
		edit(c)
		var bodies []string
		for range c.Companions {
			bodies = append(bodies, "new man")
		}
		_, err := c.InstallPrepared(context.Background(), prepared(t, c, "v1.1.0", "new", bodies...))
		return err
	}
	for _, tc := range unsupported {
		var ue *UnsupportedInstallationError
		if err := install(tc.edit); !errors.As(err, &ue) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	for _, tc := range invalid {
		if err := install(tc.edit); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
	assertFile(t, executable, "old program")
	assertFile(t, other, "other program")
	assertNoFile(t, filepath.Join(prefix, "sample.1"))
}

func TestCompanionEntriesMustBeCanonicalAndUnique(t *testing.T) {
	_, executable := installation(t)
	for _, entries := range [][]string{{"./share/sample.1"}, {"../sample.1"}, {"/sample.1"}, {""}, {"share\\sample.1"}, {"bin/sample"}, {"a/sample.1", "a/sample.1"}} {
		c := offlineClient(t, executable)
		for _, entry := range entries {
			c.Companions = append(c.Companions, Companion{entry, filepath.Join(filepath.Dir(executable), "x")})
		}
		if _, err := c.binding("v1.1.0"); err == nil {
			t.Errorf("companion entries %q accepted", entries)
		}
	}
}

func TestPreparedArtifactBoundToTrustPolicy(t *testing.T) {
	signerA, signerB := releasetest.NewSigner(t), releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	api.publish("v1.1.0", releasetest.Release(t, signerA, testNamespace, map[string][]byte{tarName("v1.1.0"): cliArchive(t, "new program", "new man")}))
	prefix, executable := installation(t)
	a := api.client(signerA, executable)
	a.Companions = []Companion{{"share/man/man1/sample.1", filepath.Join(prefix, "sample.1")}}
	selected, err := a.Target(context.Background(), "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.Prepare(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Archive(), api.blobs["3"]) || p.ArchiveName() != tarName("v1.1.0") || len(p.Checksums()) == 0 || len(p.Signature()) == 0 {
		t.Fatal("prepared artifacts differ from the release")
	}
	for _, mutate := range []struct {
		name   string
		change func(*Client)
	}{
		{"foreign key", func(c *Client) { c.TrustedKeys = []ssh.PublicKey{signerB.PublicKey()} }},
		{"wrong namespace", func(c *Client) { c.Namespace = "foreign-release" }},
		{"wrong repository", func(c *Client) { c.RepositoryAPI = api.URL + "/repos/example/foreign" }},
		{"other companions", func(c *Client) { c.Companions = nil }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			b := *a
			mutate.change(&b)
			if _, err := b.InstallPrepared(context.Background(), p); err == nil {
				t.Fatal("foreign client installed prepared release")
			}
			assertFile(t, executable, "old program")
		})
	}
	a.TrustedKeys[0] = signerB.PublicKey()
	if _, err := a.InstallPrepared(context.Background(), p); err == nil {
		t.Fatal("mutated trusted key set installed prepared release")
	}
	assertFile(t, executable, "old program")
	a.TrustedKeys[0] = signerA.PublicKey()
	if _, err := a.InstallPrepared(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	assertFile(t, executable, "new program")
}

func TestAssetRedirectDropsToken(t *testing.T) {
	next := func(raw string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer test-token")
		return r
	}
	ok := next("https://objects.githubusercontent.com/asset")
	if err := assetRedirect(ok, make([]*http.Request, 1)); err != nil || ok.Header.Get("Authorization") != "" {
		t.Fatalf("GitHub redirect: %v, %q", err, ok.Header.Get("Authorization"))
	}
	for _, raw := range []string{"http://objects.githubusercontent.com/asset", "https://example.org/asset", "https://github.com.example.org/asset"} {
		if err := assetRedirect(next(raw), nil); err == nil {
			t.Errorf("redirect to %s accepted", raw)
		}
	}
	if err := assetRedirect(next("https://github.com/asset"), make([]*http.Request, 10)); err == nil {
		t.Error("eleventh redirect accepted")
	}
}

func TestReplaceWindowsRestoresOnFailure(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "sample.exe")
	staged := filepath.Join(dir, "staged")
	writeFile(t, old, "old")
	writeFile(t, staged, "new")
	ops := fileOps{rename: func(from, to string) error {
		if from == staged {
			return fmt.Errorf("locked")
		}
		return os.Rename(from, to)
	}, remove: os.Remove}
	if err := replaceWindows(ops, staged, old); err == nil {
		t.Fatal("replacement succeeded")
	}
	assertFile(t, old, "old")
}

func TestReplaceWindowsKeepsLockedOldFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sample.exe")
	staged := filepath.Join(dir, "staged")
	for file, body := range map[string]string{target: "old", staged: "new", target + ".old": "stale"} {
		writeFile(t, file, body)
	}
	ops := fileOps{rename: os.Rename, remove: func(name string) error {
		if name == target+".old" {
			return fmt.Errorf("locked")
		}
		return os.Remove(name)
	}}
	if err := replaceWindows(ops, staged, target); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, "new")
	assertFile(t, target+".old", "stale")
}

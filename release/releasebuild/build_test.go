package releasebuild_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"time"

	"github.com/castrowithcee/qatlas-kit/release"
	"github.com/castrowithcee/qatlas-kit/release/releasebuild"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
	"golang.org/x/crypto/ssh"
)

const (
	program   = "sample-tool"
	namespace = "sample-tool-release"
	version   = "v1.2.3"
	// manpageMode makes the test binary act as the manpage command of the test module.
	manpageMode = "SAMPLE_MANPAGE_MODE"
)

var sourceDate = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestMain(m *testing.M) {
	if mode := os.Getenv(manpageMode); mode != "" && len(os.Args) == 3 && os.Args[1] == "manpage" {
		os.Exit(writeManpages(mode, os.Args[2]))
	}
	os.Exit(m.Run())
}

func writeManpages(mode, dir string) int {
	write := func(name, body string) int {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	switch mode {
	case "ok":
		return write(program+".1", ".TH SAMPLE 1 "+os.Getenv("SOURCE_DATE_EPOCH")+"\n") + write(program+"-sub.1", ".TH SUB 1\n")
	case "none":
		return 0
	case "other":
		return write(program+".1", "x") + write("notes.txt", "x")
	case "fail":
		return 3
	}
	return 1
}

// module is a minimal application module with a license and an allowed signers file.
type module struct {
	dir    string
	signer ssh.Signer
}

func newModule(t *testing.T) *module {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	m := &module{dir: t.TempDir(), signer: releasetest.NewSigner(t)}
	m.write(t, "go.mod", "module example.invalid/sample\n\ngo 1.21\n")
	m.write(t, "main.go", "package main\n\nimport \"fmt\"\n\nvar version = \"dev\"\n\nfunc main() { fmt.Println(\"sample\", version) }\n")
	m.write(t, "LICENSE", "sample license\n")
	m.write(t, "allowed-signers", "# release keys\n"+namespace+" "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(m.signer.PublicKey())))+" sample\n")
	t.Setenv(manpageMode, "ok")
	return m
}

func (m *module) write(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func targets() []string {
	all := []string{"linux/arm64", "windows/amd64"}
	if host := runtime.GOOS + "/" + runtime.GOARCH; !slices.Contains(all, host) && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		all = append(all, host)
	}
	return all
}

func (m *module) config() releasebuild.Config {
	return releasebuild.Config{
		VersionVariable: "main.version",
		Targets:         targets(),
		ManpageCommand:  []string{os.Args[0], "manpage", "{dir}"},
		Files:           []releasebuild.File{{Source: "LICENSE", Path: "share/doc/" + program + "/LICENSE"}},
		Installer:       releasebuild.Installer{DisplayName: "Sample Tool", NextStep: "Next: run sample-tool."},
	}
}

func (m *module) options(t *testing.T) releasebuild.Options {
	return releasebuild.Options{
		Dir:            m.dir,
		Program:        program,
		Namespace:      namespace,
		AllowedSigners: "allowed-signers",
		ReleasesURL:    "https://example.invalid/owner/sample-tool/releases",
		Version:        version,
		Output:         filepath.Join(t.TempDir(), "dist"),
		SourceDate:     sourceDate,
	}
}

func readAssets(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	assets := map[string][]byte{}
	for _, entry := range entries {
		if assets[entry.Name()], err = os.ReadFile(filepath.Join(dir, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return assets
}

type archived struct {
	mode    os.FileMode
	modTime time.Time
	body    []byte
}

// listArchive returns the entries of an archive by name, failing on owner other than 0.
func listArchive(t *testing.T, name string, data []byte) map[string]archived {
	t.Helper()
	out := map[string]archived{}
	if strings.HasSuffix(name, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range reader.File {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			out[f.Name] = archived{f.Mode(), f.Modified.UTC(), body}
		}
		return out
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
			t.Errorf("%s: %s has owner %d/%d %q/%q", name, header.Name, header.Uid, header.Gid, header.Uname, header.Gname)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		out[header.Name] = archived{header.FileInfo().Mode(), header.ModTime.UTC(), body}
	}
}

func exeName(goos string) string {
	if goos == "windows" {
		return program + ".exe"
	}
	return program
}

func TestBuildWritesVerifiableArchives(t *testing.T) {
	m := newModule(t)
	o := m.options(t)
	names, err := releasebuild.Build(context.Background(), m.config(), o)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, target := range targets() {
		goos, goarch, _ := strings.Cut(target, "/")
		want = append(want, releasebuild.ArchiveName(program, version, goos, goarch))
	}
	want = append(want, "checksums.txt", "install.ps1", "install.sh")
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("assets = %v, want %v", names, want)
	}
	assets := readAssets(t, o.Output)
	if len(assets) != len(want) {
		t.Fatalf("output holds %d files, want %d", len(assets), len(want))
	}
	checksums := assets["checksums.txt"]
	if lines := strings.Count(string(checksums), "\n"); lines != len(want)-1 {
		t.Errorf("checksums.txt lists %d assets:\n%s", lines, checksums)
	}
	signature := releasetest.Sign(t, m.signer, namespace, "sha512", checksums)
	keys := []ssh.PublicKey{m.signer.PublicKey()}
	for _, name := range want {
		if name == "checksums.txt" {
			continue
		}
		if err := release.VerifyArchive(checksums, signature, assets[name], namespace, name, keys); err != nil {
			t.Errorf("VerifyArchive(%s): %v", name, err)
		}
	}
	for _, target := range targets() {
		goos, goarch, _ := strings.Cut(target, "/")
		name := releasebuild.ArchiveName(program, version, goos, goarch)
		entries := listArchive(t, name, assets[name])
		wantEntries := []string{"bin/", "bin/" + exeName(goos), "share/", "share/doc/", "share/doc/" + program + "/", "share/doc/" + program + "/LICENSE"}
		if goos != "windows" {
			wantEntries = append(wantEntries, "share/man/", "share/man/man1/", "share/man/man1/"+program+".1", "share/man/man1/"+program+"-sub.1")
		}
		slices.Sort(wantEntries)
		gotEntries := slices.Sorted(maps.Keys(entries))
		if !slices.Equal(gotEntries, wantEntries) {
			t.Errorf("%s entries = %v, want %v", name, gotEntries, wantEntries)
		}
		for entryName, e := range entries {
			wantMode := os.FileMode(0o644)
			if strings.HasSuffix(entryName, "/") {
				wantMode = os.ModeDir | 0o755
			} else if strings.HasPrefix(entryName, "bin/") {
				wantMode = 0o755
			}
			if e.mode != wantMode || !e.modTime.Equal(sourceDate) {
				t.Errorf("%s: %s has mode %v and time %v", name, entryName, e.mode, e.modTime)
			}
		}
		exe := entries["bin/"+exeName(goos)].body
		sum := sha256.Sum256(exe)
		if got, err := release.ProgramSHA256(name, assets[name], "bin/"+exeName(goos)); err != nil || got != hex.EncodeToString(sum[:]) {
			t.Errorf("ProgramSHA256(%s) = %s, %v", name, got, err)
		}
		if !bytes.Contains(exe, []byte(version)) {
			t.Errorf("%s: program does not carry the version", name)
		}
		if goos == runtime.GOOS && goarch == runtime.GOARCH {
			path := filepath.Join(t.TempDir(), exeName(goos))
			if err := os.WriteFile(path, exe, 0o755); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(path).Output(); err != nil || string(out) != "sample "+version+"\n" {
				t.Errorf("built program prints %q, %v", out, err)
			}
		}
		if goos != "windows" {
			if page := string(entries["share/man/man1/"+program+".1"].body); page != fmt.Sprintf(".TH SAMPLE 1 %d\n", sourceDate.Unix()) {
				t.Errorf("manpage = %q, want the source date passed on", page)
			}
		}
	}
	for _, script := range []string{"install.sh", "install.ps1"} {
		if !bytes.Contains(assets[script], []byte(strings.TrimSpace(string(ssh.MarshalAuthorizedKey(m.signer.PublicKey()))))) {
			t.Errorf("%s does not embed the release key", script)
		}
	}

	again := o
	again.Output = filepath.Join(t.TempDir(), "again")
	if _, err := releasebuild.Build(context.Background(), m.config(), again); err != nil {
		t.Fatal(err)
	}
	for name, data := range readAssets(t, again.Output) {
		if !bytes.Equal(data, assets[name]) {
			t.Errorf("%s differs between two builds", name)
		}
	}

	t.Run("selfupdate", func(t *testing.T) { verifySelfupdate(t, m, assets, signature) })
	t.Run("install.sh", func(t *testing.T) { runShellInstaller(t, assets, signature) })
}

// verifySelfupdate prepares each archive through selfupdate from a local stand-in of the release API.
func verifySelfupdate(t *testing.T, m *module, assets map[string][]byte, signature []byte) {
	files := map[string][]byte{"checksums.txt.sig": signature}
	maps.Copy(files, assets)
	names := slices.Sorted(maps.Keys(files))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/repos/example/sample-tool"
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
		name := releasebuild.ArchiveName(program, version, goos, goarch)
		prefix := t.TempDir()
		executable := filepath.Join(prefix, "bin", exeName(runtime.GOOS))
		companions := []selfupdate.Companion{{Entry: "share/doc/" + program + "/LICENSE", Path: filepath.Join(prefix, "LICENSE")}}
		if goos != "windows" {
			companions = append(companions, selfupdate.Companion{Entry: "share/man/man1/" + program + ".1", Path: filepath.Join(prefix, program+".1")})
		}
		client := &selfupdate.Client{
			RepositoryAPI:  server.URL + "/repos/example/sample-tool",
			CurrentVersion: "v1.0.0",
			MinimumVersion: "v1.0.0",
			ArchiveName:    func(string) string { return name },
			UserAgent:      "sample/1",
			Namespace:      namespace,
			TrustedKeys:    []ssh.PublicKey{m.signer.PublicKey()},
			ProgramName:    program,
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

// runShellInstaller installs the built release with the built install.sh.
func runShellInstaller(t *testing.T, assets map[string][]byte, signature []byte) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" || !slices.Contains(targets(), runtime.GOOS+"/"+runtime.GOARCH) {
		t.Skip("install.sh does not run on " + runtime.GOOS + "/" + runtime.GOARCH)
	}
	for _, tool := range []string{"sh", "curl", "tar", "ssh-keygen"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	server := releasetest.NewServer(t)
	files := map[string][]byte{"checksums.txt.sig": signature}
	maps.Copy(files, assets)
	server.Publish(version, files)
	root := t.TempDir()
	script := filepath.Join(root, "install.sh")
	if err := os.WriteFile(script, assets["install.sh"], 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "SHELL=/bin/sh", "TMPDIR=" + t.TempDir(), "SAMPLE_TOOL_INSTALL_BASE_URL=" + server.URL}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(home, ".local", "bin", program)).Output()
	if err != nil || string(out) != "sample "+version+"\n" {
		t.Errorf("installed program prints %q, %v", out, err)
	}
	for _, file := range []string{"share/man/man1/" + program + ".1", "share/man/man1/" + program + "-sub.1", "share/doc/" + program + "/LICENSE"} {
		if _, err := os.Stat(filepath.Join(home, ".local", filepath.FromSlash(file))); err != nil {
			t.Error(err)
		}
	}
}

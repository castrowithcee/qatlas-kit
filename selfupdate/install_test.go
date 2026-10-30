package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func signedChecksums(t *testing.T, signer ssh.Signer, namespace string, data []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(data)
	magic := [6]byte{'S', 'S', 'H', 'S', 'I', 'G'}
	signed := ssh.Marshal(struct {
		Magic                     [6]byte
		Namespace, Reserved, Hash string
		Digest                    []byte
	}{magic, namespace, "", "sha256", digest[:]})
	sig, err := signer.Sign(rand.Reader, signed)
	if err != nil {
		t.Fatal(err)
	}
	blob := ssh.Marshal(struct {
		Magic                     [6]byte
		Version                   uint32
		PublicKey                 []byte
		Namespace, Reserved, Hash string
		Signature                 []byte
	}{magic, 1, signer.PublicKey().Marshal(), namespace, "", "sha256", ssh.Marshal(struct {
		Format string
		Blob   []byte
	}{sig.Format, sig.Blob})})
	return []byte("-----BEGIN SSH SIGNATURE-----\n" + base64.StdEncoding.EncodeToString(blob) + "\n-----END SSH SIGNATURE-----\n")
}

func zipArchive(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.Create("bin/sample.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("new program")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPrepareGuardsDoNotChangeInstallation(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	_, foreignPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := ssh.NewSignerFromKey(foreignPrivate)
	if err != nil {
		t.Fatal(err)
	}
	archive := zipArchive(t)
	sum := sha256.Sum256(archive)
	name := "sample_v1.1.0.zip"
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), name))
	good := signedChecksums(t, signer, "sample-release", checksums)
	cases := []struct {
		name    string
		sign    []byte
		archive []byte
		minimum string
		valid   bool
	}{
		{"valid prepared release", good, archive, "v1.0.0", true},
		{"foreign signature", signedChecksums(t, foreign, "sample-release", checksums), archive, "v1.0.0", false},
		{"wrong namespace", signedChecksums(t, signer, "another-release", checksums), archive, "v1.0.0", false},
		{"wrong checksum", good, []byte("corrupt"), "v1.0.0", false},
		{"missing signature", nil, archive, "v1.0.0", false},
		{"below minimum", good, archive, "v1.2.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			targetPath := filepath.Join(dir, "sample")
			if err := os.WriteFile(targetPath, []byte("old program"), 0o755); err != nil {
				t.Fatal(err)
			}
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/example/sample/releases/tags/v1.1.0":
					assets := fmt.Sprintf(`{"name":%q,"url":%q},{"name":"checksums.txt","url":%q}`, name, server.URL+"/repos/example/sample/releases/assets/1", server.URL+"/repos/example/sample/releases/assets/2")
					if tc.name != "missing signature" {
						assets += fmt.Sprintf(`,{"name":"checksums.txt.sig","url":%q}`, server.URL+"/repos/example/sample/releases/assets/3")
					}
					fmt.Fprintf(w, `{"tag_name":"v1.1.0","assets":[%s]}`, assets)
				case "/repos/example/sample/releases/assets/1":
					w.Write(tc.archive)
				case "/repos/example/sample/releases/assets/2":
					w.Write(checksums)
				case "/repos/example/sample/releases/assets/3":
					w.Write(tc.sign)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			c := &Client{RepositoryAPI: server.URL + "/repos/example/sample", CurrentVersion: "v1.0.0", MinimumVersion: tc.minimum, ArchiveName: func(string) string { return name }, UserAgent: "sample/1", HTTPClient: server.Client(), Namespace: "sample-release", TrustedKeys: []ssh.PublicKey{signer.PublicKey()}, ProgramName: "sample", ManpageName: "sample.1", Executable: targetPath}
			selected, err := c.Target(context.Background(), "v1.1.0")
			if tc.name == "missing signature" || tc.name == "below minimum" {
				if err == nil {
					t.Fatal("ineligible release selected")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := c.Prepare(context.Background(), selected)
				if tc.valid {
					if err != nil || prepared == nil || prepared.Version() != "v1.1.0" {
						t.Fatalf("valid release rejected: %v", err)
					}
					if err := c.InstallPrepared(context.Background(), prepared); err != nil {
						t.Fatalf("install verified release: %v", err)
					}
				} else if err == nil {
					t.Fatal("invalid release accepted")
				}
			}
			body, err := os.ReadFile(targetPath)
			want := "old program"
			if tc.valid {
				want = "new program"
			}
			if err != nil || string(body) != want {
				t.Fatalf("installed bytes = %q, want %q: %v", body, want, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("unexpected files: %v, %v", entries, err)
			}
		})
	}
}

func TestInstallPreparedAndRollbackCheck(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "sample")
	manpage := filepath.Join(dir, "sample.1")
	for file, body := range map[string]string{binary: "old", manpage: "old man"} {
		if err := os.WriteFile(file, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c := &Client{RepositoryAPI: "https://api.github.com/repos/example/sample", CurrentVersion: "v2.0.0", MinimumVersion: "v1.0.0", ArchiveName: func(string) string { return "sample.zip" }, UserAgent: "sample/1", Executable: binary, ManpagePath: manpage, ProgramName: "sample", ManpageName: "sample.1"}
	p := &Prepared{version: "v1.5.0", executable: []byte("new"), manpage: []byte("new man")}
	wrongBinary := filepath.Join(dir, "other")
	if err := os.WriteFile(wrongBinary, []byte("other old"), 0o755); err != nil {
		t.Fatal(err)
	}
	c.Executable = wrongBinary
	if err := c.InstallPrepared(context.Background(), p); err == nil {
		t.Fatal("mismatched program path accepted")
	}
	c.Executable = binary
	wrongManpage := filepath.Join(dir, "other.1")
	if err := os.WriteFile(wrongManpage, []byte("other man"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.ManpagePath = wrongManpage
	if err := c.InstallPrepared(context.Background(), p); err == nil {
		t.Fatal("mismatched manpage path accepted")
	}
	c.ManpagePath = manpage
	if err := c.InstallPrepared(context.Background(), p); err == nil {
		t.Fatal("rollback without validation accepted")
	}
	c.ValidateRollback = func(_ context.Context, b []byte) error {
		if string(b) != "new" {
			t.Fatal("wrong candidate")
		}
		return fmt.Errorf("config rejected")
	}
	if err := c.InstallPrepared(context.Background(), p); err == nil || !strings.Contains(err.Error(), "config rejected") {
		t.Fatalf("validation: %v", err)
	}
	for file, want := range map[string]string{binary: "old", manpage: "old man"} {
		got, _ := os.ReadFile(file)
		if string(got) != want {
			t.Fatalf("%s changed: %q", file, got)
		}
	}
	c.ValidateRollback = func(context.Context, []byte) error { return nil }
	if err := c.InstallPrepared(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{binary: "new", manpage: "new man"} {
		got, _ := os.ReadFile(file)
		if string(got) != want {
			t.Fatalf("%s = %q", file, got)
		}
	}
}

func TestReplaceWindowsRestoresOnFailure(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "sample.exe")
	staged := filepath.Join(dir, "staged")
	os.WriteFile(old, []byte("old"), 0o755)
	os.WriteFile(staged, []byte("new"), 0o755)
	ops := fileOps{rename: func(from, to string) error {
		if from == staged {
			return fmt.Errorf("locked")
		}
		return os.Rename(from, to)
	}, remove: os.Remove}
	if err := replaceWindows(ops, staged, old); err == nil {
		t.Fatal("replacement succeeded")
	}
	got, _ := os.ReadFile(old)
	if string(got) != "old" {
		t.Fatalf("old program lost: %q", got)
	}
}

func TestReplaceWindowsKeepsLockedOldFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sample.exe")
	staged := filepath.Join(dir, "staged")
	for file, body := range map[string]string{target: "old", staged: "new", target + ".old": "stale"} {
		if err := os.WriteFile(file, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
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
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new" {
		t.Fatalf("target: %q, %v", got, err)
	}
	got, err = os.ReadFile(target + ".old")
	if err != nil || string(got) != "stale" {
		t.Fatalf("locked old file: %q, %v", got, err)
	}
}

func TestExtractPayloadFormatsAndAmbiguity(t *testing.T) {
	archive := zipArchive(t)
	binary, _, err := extractPayload(archive, "sample_v1.1.0.zip", "sample", "sample.1")
	if err != nil || string(binary) != "new program" {
		t.Fatalf("zip extraction: %q, %v", binary, err)
	}
	var tarBytes bytes.Buffer
	gz := gzip.NewWriter(&tarBytes)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"bin/sample": "new program", "share/man/man1/sample.1": "new manpage"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	binary, manpage, err := extractPayload(tarBytes.Bytes(), "sample_v1.1.0.tar.gz", "sample", "sample.1")
	if err != nil || string(binary) != "new program" || string(manpage) != "new manpage" {
		t.Fatalf("tar extraction: %q, %q, %v", binary, manpage, err)
	}
	for _, entries := range [][]string{{"bin/sample.exe", "bin/sample.exe"}, {"../bin/sample.exe"}} {
		var body bytes.Buffer
		w := zip.NewWriter(&body)
		for _, name := range entries {
			f, err := w.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			f.Write([]byte("payload"))
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := extractPayload(body.Bytes(), "sample.zip", "sample", "sample.1"); err == nil {
			t.Fatalf("accepted %v", entries)
		}
	}
}

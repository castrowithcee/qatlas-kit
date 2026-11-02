package installer_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/installer"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

const (
	testProgram   = "sample-tool"
	testNamespace = "sample-tool-release"
	testEnv       = "SAMPLE_TOOL"
	testNextStep  = `Next: run 'sample-tool' and $(not a command) or "quoted" text.`
)

func testParams(t *testing.T, signer ssh.Signer, targets ...string) installer.Params {
	t.Helper()
	return installer.Params{
		Program:     testProgram,
		DisplayName: "Sample Tool",
		ReleasesURL: "https://example.invalid/owner/sample-tool/releases",
		Namespace:   testNamespace,
		TrustedKeys: []ssh.PublicKey{signer.PublicKey()},
		Targets:     targets,
		NextStep:    testNextStep,
	}
}

func TestRenderRejectsInvalidParams(t *testing.T) {
	signer := releasetest.NewSigner(t)
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaPublic, err := ssh.NewPublicKey(&ecdsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*installer.Params){
		"empty program":           func(p *installer.Params) { p.Program = "" },
		"program with quote":      func(p *installer.Params) { p.Program = "tool'x" },
		"program with slash":      func(p *installer.Params) { p.Program = "bin/tool" },
		"program upper case":      func(p *installer.Params) { p.Program = "Tool" },
		"display name with quote": func(p *installer.Params) { p.DisplayName = `Tool"` },
		"http releases URL":       func(p *installer.Params) { p.ReleasesURL = "http://example.invalid/releases" },
		"releases URL with query": func(p *installer.Params) { p.ReleasesURL = "https://example.invalid/releases?x=1" },
		"releases URL trailing /": func(p *installer.Params) { p.ReleasesURL = "https://example.invalid/releases/" },
		"releases URL with quote": func(p *installer.Params) { p.ReleasesURL = "https://example.invalid/re'leases" },
		"releases URL with user":  func(p *installer.Params) { p.ReleasesURL = "https://user@example.invalid/releases" },
		"empty namespace":         func(p *installer.Params) { p.Namespace = "" },
		"namespace with space":    func(p *installer.Params) { p.Namespace = "a b" },
		"no trusted key":          func(p *installer.Params) { p.TrustedKeys = nil },
		"nil trusted key":         func(p *installer.Params) { p.TrustedKeys = []ssh.PublicKey{nil} },
		"non-Ed25519 key":         func(p *installer.Params) { p.TrustedKeys = []ssh.PublicKey{ecdsaPublic} },
		"next step with newline":  func(p *installer.Params) { p.NextStep = "one\ntwo" },
		"next step non-ASCII":     func(p *installer.Params) { p.NextStep = "café" },
		"unsupported os":          func(p *installer.Params) { p.Targets = append(p.Targets, "freebsd/amd64") },
		"unsupported arch":        func(p *installer.Params) { p.Targets = append(p.Targets, "linux/386") },
		"duplicate target":        func(p *installer.Params) { p.Targets = append(p.Targets, "linux/amd64") },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			p := testParams(t, signer, "linux/amd64", "windows/amd64")
			mutate(&p)
			if out, err := installer.Shell(p); err == nil {
				t.Errorf("Shell accepted invalid params:\n%s", out)
			}
			if out, err := installer.PowerShell(p); err == nil {
				t.Errorf("PowerShell accepted invalid params:\n%s", out)
			}
		})
	}
	if _, err := installer.Shell(testParams(t, signer, "windows/amd64")); err == nil {
		t.Error("Shell rendered without a linux or darwin target")
	}
	if _, err := installer.PowerShell(testParams(t, signer, "linux/amd64")); err == nil {
		t.Error("PowerShell rendered without a windows target")
	}
}

func TestRenderEmbedsParams(t *testing.T) {
	signer := releasetest.NewSigner(t)
	other := releasetest.NewSigner(t)
	p := testParams(t, signer, "linux/amd64", "darwin/arm64", "windows/amd64")
	p.TrustedKeys = append(p.TrustedKeys, other.PublicKey())
	for name, render := range map[string]func(installer.Params) ([]byte, error){"install.sh": installer.Shell, "install.ps1": installer.PowerShell} {
		out, err := render(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		script := string(out)
		for _, key := range p.TrustedKeys {
			line := testNamespace + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
			if !strings.Contains(script, line) {
				t.Errorf("%s lacks signer line %q", name, line)
			}
		}
		for _, want := range []string{p.ReleasesURL, testEnv + "_INSTALL_BASE_URL", testEnv + "_VERSION", "Sample Tool"} {
			if !strings.Contains(script, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		if strings.Contains(script, "{{") || strings.Contains(script, "<no value>") {
			t.Errorf("%s has unrendered template parts", name)
		}
	}
}

// release is a published test release of one archive.
type release struct {
	archive   []byte
	checksums string
	signer    ssh.Signer
	namespace string
}

// publish adds tag to server with an archive holding program, a manpage and a license, signed by signer.
func publish(t *testing.T, server *releasetest.Server, signer ssh.Signer, tag, archiveName, program string, mutate func(*release)) {
	t.Helper()
	files := []releasetest.File{{Name: "bin/" + testProgram, Body: []byte(program)}}
	if strings.HasSuffix(archiveName, ".zip") {
		files = []releasetest.File{{Name: "bin/" + testProgram + ".exe", Body: []byte(program)}}
	} else {
		files = append(files, releasetest.File{Name: "share/man/man1/" + testProgram + ".1", Body: []byte(".TH SAMPLE 1 " + tag + "\n")})
	}
	files = append(files, releasetest.File{Name: "share/doc/" + testProgram + "/LICENSE", Body: []byte("license " + tag + "\n")})
	r := &release{archive: releasetest.Archive(t, archiveName, files...), signer: signer, namespace: testNamespace}
	sum := sha256.Sum256(r.archive)
	r.checksums = hex.EncodeToString(sum[:]) + "  " + archiveName + "\n" + strings.Repeat("1", 64) + "  install.sh\n"
	if mutate != nil {
		mutate(r)
	}
	server.Publish(tag, map[string][]byte{
		archiveName:         r.archive,
		"checksums.txt":     []byte(r.checksums),
		"checksums.txt.sig": releasetest.Sign(t, r.signer, r.namespace, "sha512", []byte(r.checksums)),
	})
}

func hostTarget(t *testing.T) string {
	t.Helper()
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("no release archive for " + runtime.GOARCH)
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

func archiveName(tag string) string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s%s", testProgram, tag, runtime.GOOS, runtime.GOARCH, ext)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func stageLeftovers(prefix string) []string {
	matches, _ := filepath.Glob(filepath.Join(prefix, "."+testProgram+"-stage.*"))
	return matches
}

// rejections are releases every installer must refuse without changing an installation.
func rejections(t *testing.T) map[string]struct {
	mutate  func(*release)
	message string
} {
	other := releasetest.NewSigner(t)
	return map[string]struct {
		mutate  func(*release)
		message string
	}{
		"wrong checksum": {func(r *release) {
			r.checksums = strings.Repeat("0", 64) + r.checksums[64:]
		}, "SHA-256 mismatch"},
		"foreign signature": {func(r *release) { r.signer = other }, "signature"},
		"foreign namespace": {func(r *release) { r.namespace = "other-release" }, "signature"},
		"duplicate checksum": {func(r *release) {
			first, _, _ := strings.Cut(r.checksums, "\n")
			r.checksums += first + "\n"
		}, "exactly once"},
		"missing checksum": {func(r *release) {
			_, rest, _ := strings.Cut(r.checksums, "\n")
			r.checksums = rest
		}, "exactly once"},
		"short checksum": {func(r *release) {
			r.checksums = r.checksums[2:]
		}, "exactly once"},
	}
}

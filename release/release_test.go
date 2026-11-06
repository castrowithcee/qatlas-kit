package release

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

const (
	testNamespace      = "sample-release"
	testSignatureAsset = "checksums.txt.example.sig"
)

// sigParts are the fields of an SSH signature; the signature itself is always valid over them, so a
// rejection can only come from a parameter check.
type sigParts struct {
	magic                     [6]byte
	version                   uint32
	namespace, reserved, hash string
	format                    string
	outerTail, innerTail      []byte
}

func buildSignature(t *testing.T, signer ssh.Signer, data []byte, mutate func(*sigParts)) []byte {
	t.Helper()
	p := sigParts{magic: [6]byte{'S', 'S', 'H', 'S', 'I', 'G'}, version: 1, namespace: testNamespace, hash: "sha512"}
	if mutate != nil {
		mutate(&p)
	}
	var digest []byte
	if p.hash == "sha256" {
		sum := sha256.Sum256(data)
		digest = sum[:]
	} else {
		sum := sha512.Sum512(data)
		digest = sum[:]
	}
	sig, err := signer.Sign(rand.Reader, ssh.Marshal(struct {
		Magic                     [6]byte
		Namespace, Reserved, Hash string
		Digest                    []byte
	}{p.magic, p.namespace, p.reserved, p.hash, digest}))
	if err != nil {
		t.Fatal(err)
	}
	if p.format == "" {
		p.format = sig.Format
	}
	inner := append(ssh.Marshal(struct {
		Format string
		Blob   []byte
	}{p.format, sig.Blob}), p.innerTail...)
	blob := append(ssh.Marshal(struct {
		Magic                     [6]byte
		Version                   uint32
		PublicKey                 []byte
		Namespace, Reserved, Hash string
		Signature                 []byte
	}{p.magic, p.version, signer.PublicKey().Marshal(), p.namespace, p.reserved, p.hash, inner}), p.outerTail...)
	return releasetest.Armor(blob)
}

func TestVerifyChecksums(t *testing.T) {
	checksums := []byte(strings.Repeat("0", 64) + "  sample.tar.gz\n")
	signer := releasetest.NewSigner(t)
	foreign := releasetest.NewSigner(t)
	keys := []ssh.PublicKey{signer.PublicKey()}
	for _, hash := range []string{"sha256", "sha512"} {
		if err := VerifyChecksums(checksums, releasetest.Sign(t, signer, testNamespace, hash, checksums), testNamespace, keys); err != nil {
			t.Fatalf("valid %s signature: %v", hash, err)
		}
	}
	crlf := strings.ReplaceAll(string(releasetest.Sign(t, signer, testNamespace, "sha256", checksums)), "\n", "\r\n")
	if err := VerifyChecksums(checksums, []byte(crlf), testNamespace, keys); err != nil {
		t.Fatalf("CRLF signature: %v", err)
	}
	if err := VerifyChecksums(checksums, buildSignature(t, signer, checksums, nil), testNamespace, keys); err != nil {
		t.Fatalf("test builder signature: %v", err)
	}

	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaSigner, err := ssh.NewSignerFromKey(ecdsaKey)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := ssh.FingerprintSHA256(signer.PublicKey())
	good := releasetest.Sign(t, signer, testNamespace, "sha512", checksums)
	tests := map[string]struct {
		checksums, signature []byte
		namespace            string
		keys                 []ssh.PublicKey
		want                 string
	}{
		"foreign key":        {checksums, releasetest.Sign(t, foreign, testNamespace, "sha512", checksums), testNamespace, keys, ssh.FingerprintSHA256(foreign.PublicKey())},
		"no trusted keys":    {checksums, good, testNamespace, nil, fingerprint},
		"nil trusted key":    {checksums, good, testNamespace, []ssh.PublicKey{nil}, "not trusted"},
		"wrong namespace":    {checksums, releasetest.Sign(t, signer, "other-release", "sha512", checksums), testNamespace, keys, fingerprint},
		"empty namespace":    {checksums, releasetest.Sign(t, signer, "", "sha512", checksums), "", keys, "namespace is required"},
		"altered checksums":  {append([]byte("x"), checksums...), good, testNamespace, keys, fingerprint},
		"unsigned":           {checksums, nil, testNamespace, keys, "armored"},
		"not armored":        {checksums, []byte("signature"), testNamespace, keys, "armored"},
		"trailing armor":     {checksums, append(append([]byte(nil), good...), "extra\n"...), testNamespace, keys, "armored"},
		"bad base64":         {checksums, []byte(armorBegin + "\n!!!!\n" + armorEnd + "\n"), testNamespace, keys, "decode"},
		"unsupported hash":   {checksums, releasetest.Sign(t, signer, testNamespace, "md5", checksums), testNamespace, keys, "unsupported hash"},
		"wrong magic":        {checksums, buildSignature(t, signer, checksums, func(p *sigParts) { p.magic[0] = 'X' }), testNamespace, keys, "invalid SSH signature"},
		"version 2":          {checksums, buildSignature(t, signer, checksums, func(p *sigParts) { p.version = 2 }), testNamespace, keys, "unsupported parameters"},
		"reserved field":     {checksums, buildSignature(t, signer, checksums, func(p *sigParts) { p.reserved = "x" }), testNamespace, keys, "unsupported parameters"},
		"surplus bytes":      {checksums, buildSignature(t, signer, checksums, func(p *sigParts) { p.outerTail = []byte{0} }), testNamespace, keys, "invalid SSH signature"},
		"surplus sig bytes":  {checksums, buildSignature(t, signer, checksums, func(p *sigParts) { p.innerTail = []byte{0} }), testNamespace, keys, "not a valid Ed25519 signature"},
		"wrong sig format":   {checksums, buildSignature(t, signer, checksums, func(p *sigParts) { p.format = "ssh-rsa" }), testNamespace, keys, "not a valid Ed25519 signature"},
		"non-Ed25519 signer": {checksums, buildSignature(t, ecdsaSigner, checksums, nil), testNamespace, []ssh.PublicKey{ecdsaSigner.PublicKey()}, "not Ed25519"},
	}
	for title, tc := range tests {
		t.Run(title, func(t *testing.T) {
			err := VerifyChecksums(tc.checksums, tc.signature, tc.namespace, tc.keys)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestChecksumFor(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	for name, checksums := range map[string]string{
		"plain":       sum + "  sample.tar.gz\n",
		"binary mode": sum + " *sample.tar.gz\n",
		"uppercase":   strings.ToUpper(sum) + "  sample.tar.gz\r\n",
		"others":      strings.Repeat("cd", 32) + "  other.tar.gz\n" + sum + "  sample.tar.gz\n",
	} {
		got, err := ChecksumFor([]byte(checksums), "sample.tar.gz")
		if err != nil || got != sum {
			t.Fatalf("%s: %q, %v", name, got, err)
		}
	}
	for name, tc := range map[string]struct{ checksums, archive, want string }{
		"missing":          {sum + "  other.tar.gz\n", "sample.tar.gz", "missing"},
		"duplicate":        {sum + "  sample.tar.gz\n" + sum + "  sample.tar.gz\n", "sample.tar.gz", "duplicate"},
		"duplicate binary": {sum + "  sample.tar.gz\n" + sum + " *sample.tar.gz\n", "sample.tar.gz", "duplicate"},
		"invalid hex":      {strings.Repeat("zz", 32) + "  sample.tar.gz\n", "sample.tar.gz", "invalid SHA-256"},
		"short hash":       {"abcd  sample.tar.gz\n", "sample.tar.gz", "invalid SHA-256"},
		"empty name":       {sum + "  sample.tar.gz\n", "", "required"},
	} {
		if _, err := ChecksumFor([]byte(tc.checksums), tc.archive); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestVerifyArchive(t *testing.T) {
	signer := releasetest.NewSigner(t)
	keys := []ssh.PublicKey{signer.PublicKey()}
	name := "sample_v1_linux_amd64.tar.gz"
	archive := []byte("example archive")
	assets := releasetest.Release(t, signer, testNamespace, testSignatureAsset, map[string][]byte{name: archive})
	checksums, signature := assets["checksums.txt"], assets[testSignatureAsset]
	if err := VerifyArchive(checksums, signature, archive, testNamespace, name, keys); err != nil {
		t.Fatal(err)
	}
	for title, tc := range map[string]struct {
		archive         []byte
		namespace, name string
	}{
		"altered archive": {[]byte("altered archive"), testNamespace, name},
		"other archive":   {archive, testNamespace, "other.tar.gz"},
		"empty name":      {archive, testNamespace, ""},
		"empty namespace": {archive, "", name},
	} {
		if err := VerifyArchive(checksums, signature, tc.archive, tc.namespace, tc.name, keys); err == nil {
			t.Fatalf("%s accepted", title)
		}
	}
	if err := VerifyArchive(checksums, releasetest.Sign(t, releasetest.NewSigner(t), testNamespace, "sha512", checksums), archive, testNamespace, name, keys); err == nil {
		t.Fatal("foreign signature accepted")
	}
}

func TestVerifyArchiveSSHKeygen(t *testing.T) {
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is unavailable")
	}
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if out, err := exec.Command(keygen, "-q", "-t", "ed25519", "-N", "", "-f", keyFile).CombinedOutput(); err != nil {
		t.Fatalf("generate key: %v: %s", err, out)
	}
	archive := []byte("example archive")
	sum := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%s  sample.zip\n", hex.EncodeToString(sum[:])))
	checksumsFile := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(checksumsFile, checksums, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(keygen, "-Y", "sign", "-f", keyFile, "-n", testNamespace, checksumsFile).CombinedOutput(); err != nil {
		t.Fatalf("sign checksums: %v: %s", err, out)
	}
	signature, err := os.ReadFile(checksumsFile + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	publicLine, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	public, _, _, _, err := ssh.ParseAuthorizedKey(publicLine)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyArchive(checksums, signature, archive, testNamespace, "sample.zip", []ssh.PublicKey{public}); err != nil {
		t.Fatal(err)
	}
}

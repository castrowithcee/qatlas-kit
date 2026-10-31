package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

const testNamespace = "sample-release"

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func testSignature(t *testing.T, signer ssh.Signer, namespace, hash string, data []byte) []byte {
	t.Helper()
	var digest []byte
	switch hash {
	case "sha256":
		sum := sha256.Sum256(data)
		digest = sum[:]
	case "sha512":
		sum := sha512.Sum512(data)
		digest = sum[:]
	default:
		digest = []byte("unsupported")
	}
	magic := [6]byte{'S', 'S', 'H', 'S', 'I', 'G'}
	signed := ssh.Marshal(struct {
		Magic     [6]byte
		Namespace string
		Reserved  string
		Hash      string
		Digest    []byte
	}{magic, namespace, "", hash, digest})
	sig, err := signer.Sign(rand.Reader, signed)
	if err != nil {
		t.Fatal(err)
	}
	blob := ssh.Marshal(struct {
		Magic     [6]byte
		Version   uint32
		PublicKey []byte
		Namespace string
		Reserved  string
		Hash      string
		Signature []byte
	}{magic, 1, signer.PublicKey().Marshal(), namespace, "", hash, ssh.Marshal(struct {
		Format string
		Blob   []byte
	}{sig.Format, sig.Blob})})
	return []byte(armorBegin + "\n" + base64.StdEncoding.EncodeToString(blob) + "\n" + armorEnd + "\n")
}

func TestVerifyArchive(t *testing.T) {
	archive := []byte("example archive")
	sum := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%s  sample_v1_linux_amd64.tar.gz\n", hex.EncodeToString(sum[:])))
	signer := testSigner(t)
	foreign := testSigner(t)
	keys := []ssh.PublicKey{signer.PublicKey()}
	name := "sample_v1_linux_amd64.tar.gz"
	for _, hash := range []string{"sha256", "sha512"} {
		if err := VerifyArchive(checksums, testSignature(t, signer, testNamespace, hash, checksums), archive, testNamespace, name, keys); err != nil {
			t.Fatalf("valid %s release: %v", hash, err)
		}
	}
	good := testSignature(t, signer, testNamespace, "sha512", checksums)
	tests := map[string]struct {
		checksums, signature, archive []byte
		namespace, name               string
		keys                          []ssh.PublicKey
	}{
		"foreign signature": {checksums, testSignature(t, foreign, testNamespace, "sha512", checksums), archive, testNamespace, name, keys},
		"wrong namespace":   {checksums, testSignature(t, signer, "other-release", "sha512", checksums), archive, testNamespace, name, keys},
		"wrong checksum":    {checksums, good, []byte("altered archive"), testNamespace, name, keys},
		"unsigned release":  {checksums, nil, archive, testNamespace, name, keys},
		"altered list":      {append(append([]byte(nil), checksums...), 'x'), good, archive, testNamespace, name, keys},
		"no trusted keys":   {checksums, good, archive, testNamespace, name, nil},
		"wrong archive":     {checksums, good, archive, testNamespace, "other.tar.gz", keys},
		"empty namespace":   {checksums, good, archive, "", name, keys},
		"unsupported hash":  {checksums, testSignature(t, signer, testNamespace, "md5", checksums), archive, testNamespace, name, keys},
	}
	for title, tc := range tests {
		t.Run(title, func(t *testing.T) {
			if err := VerifyArchive(tc.checksums, tc.signature, tc.archive, tc.namespace, tc.name, tc.keys); err == nil {
				t.Fatal("invalid release accepted")
			}
		})
	}

	duplicate := append(append([]byte(nil), checksums...), checksums...)
	if err := VerifyArchive(duplicate, testSignature(t, signer, testNamespace, "sha512", duplicate), archive, testNamespace, name, keys); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate checksum: %v", err)
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

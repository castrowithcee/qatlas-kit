package releasetest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"maps"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// NewSigner returns a fresh Ed25519 signer.
func NewSigner(t testing.TB) ssh.Signer {
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

// Sign returns an armored SSH signature over data, made with signer for namespace and the hash "sha256"
// or "sha512". Any other hash name is written as given, with a digest no verifier accepts.
func Sign(t testing.TB, signer ssh.Signer, namespace, hash string, data []byte) []byte {
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
		digest = []byte("unknown")
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
	return Armor(blob)
}

// Armor wraps a raw SSH signature blob in the armor that ssh-keygen writes.
func Armor(blob []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(blob)
	var out strings.Builder
	out.WriteString("-----BEGIN SSH SIGNATURE-----\n")
	for len(encoded) > 70 {
		out.WriteString(encoded[:70] + "\n")
		encoded = encoded[70:]
	}
	out.WriteString(encoded + "\n-----END SSH SIGNATURE-----\n")
	return []byte(out.String())
}

// File is one regular file of a test archive.
type File struct {
	Name string
	Body []byte
}

// Archive returns a .zip archive if archiveName ends in ".zip" and a .tar.gz archive otherwise. It holds
// files in the given order, as executable regular files, so tests can also build duplicate entries.
func Archive(t testing.TB, archiveName string, files ...File) []byte {
	t.Helper()
	var buf bytes.Buffer
	if strings.HasSuffix(archiveName, ".zip") {
		w := zip.NewWriter(&buf)
		for _, f := range files {
			header := &zip.FileHeader{Name: f.Name, Method: zip.Deflate}
			header.SetMode(0o755)
			entry, err := w.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(f.Body); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	w := tar.NewWriter(gz)
	for _, f := range files {
		if err := w.WriteHeader(&tar.Header{Name: f.Name, Mode: 0o755, Size: int64(len(f.Body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.Body); err != nil {
			t.Fatal(err)
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

// Checksums returns a checksums.txt that lists the SHA-256 of each asset, sorted by name.
func Checksums(assets map[string][]byte) []byte {
	var out strings.Builder
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		sum := sha256.Sum256(assets[name])
		out.WriteString(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	}
	return []byte(out.String())
}

// Release returns the assets of a signed release: the given archives, checksums.txt listing them, and
// checksums.txt.sig signed by signer for namespace with sha512.
func Release(t testing.TB, signer ssh.Signer, namespace string, archives map[string][]byte) map[string][]byte {
	t.Helper()
	checksums := Checksums(archives)
	assets := make(map[string][]byte, len(archives)+2)
	maps.Copy(assets, archives)
	assets["checksums.txt"] = checksums
	assets["checksums.txt.sig"] = Sign(t, signer, namespace, "sha512", checksums)
	return assets
}

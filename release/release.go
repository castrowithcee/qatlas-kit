package release

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	armorBegin = "-----BEGIN SSH SIGNATURE-----"
	armorEnd   = "-----END SSH SIGNATURE-----"
)

// VerifyArchive checks the signature over checksums, then the archive's SHA-256 listed there.
func VerifyArchive(checksums, signature, archive []byte, namespace, archiveName string, keys []ssh.PublicKey) error {
	if err := VerifyChecksums(checksums, signature, namespace, keys); err != nil {
		return err
	}
	want, err := ChecksumFor(checksums, archiveName)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("archive checksum does not match %s", archiveName)
	}
	return nil
}

// VerifyChecksums checks that signature is an armored SSH signature over checksums for namespace, made by
// one of keys.
func VerifyChecksums(checksums, signature []byte, namespace string, keys []ssh.PublicKey) error {
	if namespace == "" {
		return errors.New("signature namespace is required")
	}
	armor := strings.TrimSpace(strings.ReplaceAll(string(signature), "\r\n", "\n"))
	if !strings.HasPrefix(armor, armorBegin) || !strings.HasSuffix(armor, armorEnd) {
		return errors.New("signature is not an armored SSH signature")
	}
	body := strings.TrimSuffix(strings.TrimPrefix(armor, armorBegin), armorEnd)
	blob, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	// ssh.Unmarshal rejects trailing bytes, so neither blob may carry surplus data.
	var head struct {
		Magic     [6]byte
		Version   uint32
		PublicKey []byte
		Namespace string
		Reserved  string
		Hash      string
		Signature []byte
	}
	if err := ssh.Unmarshal(blob, &head); err != nil || string(head.Magic[:]) != "SSHSIG" {
		return errors.New("invalid SSH signature")
	}
	key, err := ssh.ParsePublicKey(head.PublicKey)
	if err != nil {
		return fmt.Errorf("parse signing key: %w", err)
	}
	fingerprint := ssh.FingerprintSHA256(key)
	if key.Type() != ssh.KeyAlgoED25519 {
		return fmt.Errorf("signing key %s is not Ed25519", fingerprint)
	}
	if head.Version != 1 || head.Reserved != "" {
		return fmt.Errorf("signature by %s has unsupported parameters", fingerprint)
	}
	if head.Namespace != namespace {
		return fmt.Errorf("signature by %s is for namespace %q, want %q", fingerprint, head.Namespace, namespace)
	}
	var digest []byte
	switch head.Hash {
	case "sha256":
		sum := sha256.Sum256(checksums)
		digest = sum[:]
	case "sha512":
		sum := sha512.Sum512(checksums)
		digest = sum[:]
	default:
		return fmt.Errorf("signature by %s uses unsupported hash %q", fingerprint, head.Hash)
	}
	trusted := false
	for _, candidate := range keys {
		if candidate != nil && bytes.Equal(candidate.Marshal(), head.PublicKey) {
			trusted = true
			break
		}
	}
	if !trusted {
		return fmt.Errorf("signing key %s is not trusted", fingerprint)
	}
	var sig struct {
		Format string
		Blob   []byte
	}
	if err := ssh.Unmarshal(head.Signature, &sig); err != nil || sig.Format != ssh.KeyAlgoED25519 {
		return fmt.Errorf("signature by %s is not a valid Ed25519 signature", fingerprint)
	}
	signed := ssh.Marshal(struct {
		Magic     [6]byte
		Namespace string
		Reserved  string
		Hash      string
		Digest    []byte
	}{head.Magic, head.Namespace, head.Reserved, head.Hash, digest})
	if err := key.Verify(signed, &ssh.Signature{Format: sig.Format, Blob: sig.Blob}); err != nil {
		return fmt.Errorf("signature by %s does not match checksums: %w", fingerprint, err)
	}
	return nil
}

// ChecksumFor returns the lowercase hex SHA-256 that checksums lists for archiveName. Verify checksums with
// VerifyChecksums first.
func ChecksumFor(checksums []byte, archiveName string) (string, error) {
	if archiveName == "" {
		return "", errors.New("archive name is required")
	}
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != archiveName {
			continue
		}
		if want != "" {
			return "", fmt.Errorf("duplicate checksum for %s", archiveName)
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return "", fmt.Errorf("invalid SHA-256 for %s", archiveName)
		}
		want = hex.EncodeToString(decoded)
	}
	if want == "" {
		return "", fmt.Errorf("checksum for %s is missing", archiveName)
	}
	return want, nil
}

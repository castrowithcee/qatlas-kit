// Package release verifies signed release checksums and archive contents without network access.
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

// VerifyArchive checks the SSH signature over checksums, then the archive's SHA-256.
// The caller supplies its own namespace, archive name, and trusted keys.
func VerifyArchive(checksums, signature, archive []byte, namespace, archiveName string, keys []ssh.PublicKey) error {
	if namespace == "" || archiveName == "" {
		return errors.New("namespace and archive name are required")
	}
	if err := verifySignature(checksums, signature, namespace, keys); err != nil {
		return err
	}
	want, err := checksumFor(checksums, archiveName)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if !bytes.Equal(got[:], want[:]) {
		return fmt.Errorf("archive checksum does not match %s", archiveName)
	}
	return nil
}

func verifySignature(checksums, signature []byte, namespace string, keys []ssh.PublicKey) error {
	armor := strings.TrimSpace(strings.ReplaceAll(string(signature), "\r\n", "\n"))
	if !strings.HasPrefix(armor, armorBegin) || !strings.HasSuffix(armor, armorEnd) {
		return errors.New("signature is not an armored SSH signature")
	}
	body := strings.TrimSuffix(strings.TrimPrefix(armor, armorBegin), armorEnd)
	blob, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(body), ""))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
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
	if head.Version != 1 || head.Namespace != namespace || head.Reserved != "" {
		return errors.New("unsupported SSH signature parameters")
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
		return fmt.Errorf("unsupported signature hash %q", head.Hash)
	}
	key, err := ssh.ParsePublicKey(head.PublicKey)
	if err != nil || key.Type() != ssh.KeyAlgoED25519 {
		return errors.New("signature key is not Ed25519")
	}
	trusted := false
	for _, candidate := range keys {
		if candidate != nil && bytes.Equal(candidate.Marshal(), key.Marshal()) {
			trusted = true
			break
		}
	}
	if !trusted {
		return errors.New("signature key is not trusted")
	}
	var sig struct {
		Format string
		Blob   []byte
	}
	if err := ssh.Unmarshal(head.Signature, &sig); err != nil || sig.Format != ssh.KeyAlgoED25519 {
		return errors.New("invalid Ed25519 signature")
	}
	signed := ssh.Marshal(struct {
		Magic     [6]byte
		Namespace string
		Reserved  string
		Hash      string
		Digest    []byte
	}{head.Magic, head.Namespace, head.Reserved, head.Hash, digest})
	if err := key.Verify(signed, &ssh.Signature{Format: sig.Format, Blob: sig.Blob}); err != nil {
		return fmt.Errorf("signature does not match checksums: %w", err)
	}
	return nil
}

func checksumFor(checksums []byte, archiveName string) ([sha256.Size]byte, error) {
	var want [sha256.Size]byte
	found := false
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != archiveName {
			continue
		}
		if found {
			return want, fmt.Errorf("duplicate checksum for %s", archiveName)
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return want, fmt.Errorf("invalid SHA-256 for %s", archiveName)
		}
		copy(want[:], decoded)
		found = true
	}
	if !found {
		return want, fmt.Errorf("checksum for %s is missing", archiveName)
	}
	return want, nil
}

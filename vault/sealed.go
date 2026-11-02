package vault

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"filippo.io/age"
)

// ErrMACMismatch reports a check value that was not computed with the vault key and label.
var ErrMACMismatch = errors.New("the check value does not match: the data was not written with this vault key")

// WriteSealed replaces the sealed file name, one of Files.Sealed, with plaintext encrypted to the
// recipient of the unlocked vault.
func (v *Vault) WriteSealed(name string, plaintext []byte) error {
	if err := v.checkSealed(name); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return ErrLocked
	}
	data, err := encryptTo(plaintext, v.identity.recipient)
	if err != nil {
		return err
	}
	return v.write(v.path(name), data)
}

// ReadSealed decrypts the sealed file name, one of Files.Sealed, with the key of the unlocked vault. A
// missing file is an error that wraps fs.ErrNotExist. Anyone who can write files can encrypt to the
// public recipient, so content that must come from the vault itself carries a MAC.
func (v *Vault) ReadSealed(name string) ([]byte, error) {
	if err := v.checkSealed(name); err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	path := v.path(name)
	data, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	plain, err := decryptFrom(data, v.identity.key)
	if err != nil {
		return nil, fmt.Errorf("cannot decrypt %s: %w", path, ErrDamaged)
	}
	return plain, nil
}

func (v *Vault) checkSealed(name string) error {
	if !slices.Contains(v.opts.Files.Sealed, name) {
		return fmt.Errorf("vault: %q is not a sealed file of this vault", name)
	}
	return nil
}

// DeriveKey derives a 32-byte key for label from the vault key: HKDF-SHA256 with the key's age text as
// input key material, no salt and label as context. Each use takes its own label, so no derived key can
// stand in for another. The caller clears the result.
func DeriveKey(key *age.X25519Identity, label string) ([]byte, error) {
	if key == nil {
		return nil, ErrLocked
	}
	if label == "" {
		return nil, errors.New("vault: a key label must not be empty")
	}
	secret := []byte(key.String())
	defer clear(secret)
	derived, err := hkdf.Key(sha256.New, secret, nil, label, sha256.Size)
	if err != nil {
		return nil, errors.New("vault: cannot derive a key")
	}
	return derived, nil
}

// MAC returns the lowercase hex HMAC-SHA256 of data, keyed with DeriveKey(key, label).
func MAC(key *age.X25519Identity, label string, data []byte) (string, error) {
	derived, err := DeriveKey(key, label)
	if err != nil {
		return "", err
	}
	defer clear(derived)
	h := hmac.New(sha256.New, derived)
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyMAC checks mac against MAC(key, label, data) in constant time and reports ErrMACMismatch.
func VerifyMAC(key *age.X25519Identity, label string, data []byte, mac string) error {
	want, err := MAC(key, label, data)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(mac)) {
		return ErrMACMismatch
	}
	return nil
}

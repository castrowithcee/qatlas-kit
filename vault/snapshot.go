package vault

import (
	"bytes"
	"fmt"
	"slices"

	"filippo.io/age"
)

// Snapshot is what an unlocked vault holds, for handing it to a process that keeps it open. Both fields
// are as secret as the vault itself.
type Snapshot struct {
	Identity *age.X25519Identity
	// Document is a copy of the document, nil while the vault has no document file.
	Document []byte
}

// Snapshot returns the key and a copy of the document of the unlocked vault.
func (v *Vault) Snapshot() (Snapshot, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return Snapshot{}, ErrLocked
	}
	return Snapshot{Identity: v.identity.key, Document: slices.Clone(v.doc)}, nil
}

// OpenWithIdentity returns the vault opts describes, unlocked with key, its own private key, without the
// passphrase. key must belong to the recipient file. The document is read as it is on disk; pending
// entries stay pending until the next Unlock.
func OpenWithIdentity(opts Options, key *age.X25519Identity) (*Vault, error) {
	if key == nil {
		return nil, ErrLocked
	}
	v, err := New(opts)
	if err != nil {
		return nil, err
	}
	text, err := v.readFile(v.recipientPath())
	if err != nil {
		if isNotExist(err) {
			return nil, ErrAbsent
		}
		return nil, fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	id, err := bindIdentity(key, text)
	if err != nil {
		return nil, err
	}
	doc, err := v.loadDocument(id)
	if err != nil {
		return nil, err
	}
	v.unlocked, v.identity, v.doc = true, id, doc
	return v, nil
}

// SealedReadOption changes how ReadSealedWithIdentityOptions reads. Passing no option is the safe choice.
type SealedReadOption func(*sealedReadConfig)

type sealedReadConfig struct{ withoutRecipient bool }

// AllowMissingRecipient lets ReadSealedWithIdentityOptions read a sealed file although the recipient file
// is missing, decrypting it with the key alone. Without it a missing recipient file is ErrAbsent. The
// option only applies while the recipient file is missing: an existing one still has to match the key. The
// decryption with the key stays mandatory, but nothing then ties the key to this vault, so the authenticity
// of the content is the application's to establish, for instance with its own check value (see MAC).
func AllowMissingRecipient() SealedReadOption {
	return func(c *sealedReadConfig) { c.withoutRecipient = true }
}

// ReadSealedWithIdentity decrypts the sealed file name, one of opts.Files.Sealed, with key, the vault's
// own private key, without the passphrase and without reading the document, so a damaged document does not
// stop it. key must belong to the recipient file; a missing recipient file is ErrAbsent. The files are read
// under the same permission check as every other vault file. The plaintext goes only to the caller, who
// clears it; see ReadSealed for what the content may be trusted for. ReadSealedWithIdentityOptions takes
// options.
func ReadSealedWithIdentity(opts Options, key *age.X25519Identity, name string) ([]byte, error) {
	return ReadSealedWithIdentityOptions(opts, key, name)
}

// ReadSealedWithIdentityOptions is ReadSealedWithIdentity with options; without any it behaves the same.
func ReadSealedWithIdentityOptions(opts Options, key *age.X25519Identity, name string, options ...SealedReadOption) ([]byte, error) {
	var cfg sealedReadConfig
	for _, option := range options {
		option(&cfg)
	}
	if key == nil {
		return nil, ErrLocked
	}
	v, err := New(opts)
	if err != nil {
		return nil, err
	}
	if err := v.checkSealed(name); err != nil {
		return nil, err
	}
	text, err := v.readFile(v.recipientPath())
	switch {
	case err == nil:
		if _, err := bindIdentity(key, text); err != nil {
			return nil, err
		}
	case isNotExist(err) && cfg.withoutRecipient:
	case isNotExist(err):
		return nil, ErrAbsent
	default:
		return nil, fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	path := v.path(name)
	data, err := v.readFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	plain, err := decryptFrom(data, key)
	if err != nil {
		return nil, decryptFailure(path, data, err)
	}
	return plain, nil
}

// Recipient returns the public recipient of an encrypted vault as age text. It needs no passphrase.
func (v *Vault) Recipient() (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.unlocked {
		return v.identity.recipient.String(), nil
	}
	if err := v.requireEncrypted(); err != nil {
		return "", err
	}
	text, err := v.readFile(v.recipientPath())
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	recipient, err := parseRecipient(bytes.TrimSpace(text))
	if err != nil {
		return "", err
	}
	return recipient.String(), nil
}

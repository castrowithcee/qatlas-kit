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
	text, err := readFile(v.recipientPath())
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
	text, err := readFile(v.recipientPath())
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	recipient, err := parseRecipient(bytes.TrimSpace(text))
	if err != nil {
		return "", err
	}
	return recipient.String(), nil
}

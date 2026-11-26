package vault

import (
	"fmt"
	"slices"
)

// Create makes a new encrypted vault holding document: a fresh key pair, the private key wrapped with
// passphrase, and document encrypted to the recipient. The vault is unlocked afterwards. The key file is
// written last, so an interrupted Create leaves an absent vault.
func (v *Vault) Create(passphrase string, document []byte) error {
	if passphrase == "" {
		return ErrEmptyPassphrase
	}
	if err := checkSchema(v.documentPath(), document, v.opts.Schemas); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(true)
	if err != nil {
		return err
	}
	defer release()
	state, err := v.state()
	if err != nil {
		return err
	}
	if state != StateAbsent {
		return ErrExists
	}
	if err := v.createDir(); err != nil {
		return err
	}
	return v.encryptNew(passphrase, document)
}

// encryptNew writes a fresh key pair and document and holds both. The caller holds the mutex.
func (v *Vault) encryptNew(passphrase string, document []byte) error {
	id, err := generateIdentity()
	if err != nil {
		return err
	}
	keyData, err := wrapKey(id, passphrase, v.opts.WorkFactor)
	if err != nil {
		return err
	}
	v.noteSwitch()
	if err := v.write(v.recipientPath(), []byte(id.recipient.String()+"\n")); err != nil {
		return err
	}
	if err := v.writeDocument(id, document); err != nil {
		return err
	}
	if err := v.write(v.keyPath(), keyData); err != nil {
		return err
	}
	v.unlocked, v.identity, v.doc = true, id, slices.Clone(document)
	return nil
}

// CreateUnencrypted makes a new unencrypted vault holding document. It needs Files.Plain.
func (v *Vault) CreateUnencrypted(document []byte) error {
	if v.opts.Files.Plain == "" {
		return ErrUnencryptedDisabled
	}
	if err := checkSchema(v.plainPath(), document, v.opts.Schemas); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(true)
	if err != nil {
		return err
	}
	defer release()
	state, err := v.state()
	if err != nil {
		return err
	}
	if state != StateAbsent {
		return ErrExists
	}
	if err := v.createDir(); err != nil {
		return err
	}
	v.noteSwitch()
	return v.write(v.plainPath(), document)
}

// Unlock opens a locked vault with passphrase, merges its pending entries and holds the key and the
// document until Forget. It reports how many pending entries were merged. An unlocked vault is left as it
// is.
func (v *Vault) Unlock(passphrase string) (merged int, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(false)
	if err != nil {
		return 0, err
	}
	defer release()
	state, err := v.state()
	if err != nil {
		return 0, err
	}
	switch state {
	case StateAbsent:
		return 0, ErrAbsent
	case StateUnencrypted:
		return 0, ErrNotEncrypted
	case StateUnlocked:
		return 0, nil
	}
	id, err := v.unwrapIdentity(passphrase)
	if err != nil {
		return 0, err
	}
	doc, err := v.loadDocument(id)
	if err != nil {
		return 0, err
	}
	next, merged, err := v.mergePending(id, doc, false)
	if err != nil {
		clear(doc)
		return 0, err
	}
	v.unlocked, v.identity, v.doc = true, id, next
	return merged, nil
}

// unwrapIdentity reads the key and recipient files fresh and verifies passphrase against them, without
// consulting or changing what this value holds.
func (v *Vault) unwrapIdentity(passphrase string) (*identity, error) {
	keyData, err := v.readFile(v.keyPath())
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", v.keyPath(), err)
	}
	recipientText, err := v.readFile(v.recipientPath())
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	return unwrapKey(keyData, passphrase, recipientText)
}

// loadDocument decrypts the document file; a missing file is a nil document.
func (v *Vault) loadDocument(id *identity) ([]byte, error) {
	data, err := v.readFile(v.documentPath())
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", v.documentPath(), err)
	}
	plain, err := decryptFrom(data, id.key)
	if err != nil {
		return nil, decryptFailure(v.documentPath(), data, err)
	}
	return v.readDocument(v.documentPath(), plain)
}

// Document returns a copy of the document: the one held by an unlocked vault, or the plain file of an
// unencrypted one. It is nil while an unlocked vault has no document file.
func (v *Vault) Document() ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	state, err := v.state()
	if err != nil {
		return nil, err
	}
	switch state {
	case StateAbsent:
		return nil, ErrAbsent
	case StateLocked:
		return nil, ErrLocked
	case StateUnencrypted:
		return v.loadPlain()
	}
	return slices.Clone(v.doc), nil
}

func (v *Vault) loadPlain() ([]byte, error) {
	data, err := v.readFile(v.plainPath())
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", v.plainPath(), err)
	}
	return v.readDocument(v.plainPath(), data)
}

// Write replaces the document of an unlocked or unencrypted vault. The vault keeps holding the previous
// document when the write fails.
func (v *Vault) Write(document []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(false)
	if err != nil {
		return err
	}
	defer release()
	state, err := v.writeState()
	if err != nil {
		return err
	}
	switch state {
	case StateAbsent:
		return ErrAbsent
	case StateLocked:
		return ErrLocked
	case StateUnencrypted:
		if err := checkSchema(v.plainPath(), document, v.opts.Schemas); err != nil {
			return err
		}
		return v.putDocument(savedFile{path: v.plainPath(), data: document, exists: true}, false)
	}
	if err := checkSchema(v.documentPath(), document, v.opts.Schemas); err != nil {
		return err
	}
	if err := v.writeDocument(v.identity, document); err != nil {
		return err
	}
	clear(v.doc)
	v.doc = slices.Clone(document)
	return nil
}

func (v *Vault) writeDocument(id *identity, document []byte) error {
	_, err := v.writeEncrypted(id, document, false)
	return err
}

// writeEncrypted encrypts document to id, writes it and returns the bytes written. exempt is as for
// putDocument.
func (v *Vault) writeEncrypted(id *identity, document []byte, exempt bool) ([]byte, error) {
	data, err := encryptTo(document, id.recipient)
	if err != nil {
		return nil, err
	}
	if err := v.putDocument(savedFile{path: v.documentPath(), data: data, exists: true}, exempt); err != nil {
		return nil, err
	}
	return data, nil
}

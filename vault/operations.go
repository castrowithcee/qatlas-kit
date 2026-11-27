package vault

import (
	"errors"
	"fmt"
	"slices"
)

// ErrCreateUndoNeedsRevert reports CreateWithUndo or CreateUnencryptedWithUndo of a vault without
// Files.Lock or Options.Revert, which its undo needs. Nothing is written.
var ErrCreateUndoNeedsRevert = errors.New("the undo of a create needs Files.Lock and Options.Revert")

// Create makes a new encrypted vault holding document: a fresh key pair, the private key wrapped with
// passphrase, and document encrypted to the recipient. The vault is unlocked afterwards. The key file is
// written last, so an interrupted Create leaves an absent vault.
func (v *Vault) Create(passphrase string, document []byte) error {
	_, _, err := v.create(passphrase, document)
	return err
}

// CreateWithUndo is Create and returns undo, which takes the change of this Create out of the document on
// disk and leaves the vault, its pending entries and every change since in place: under the write lock it
// writes Options.Revert(nil, created, current), which must pass the schema check, and never puts bytes back.
// It needs no passphrase and works after Forget too, since it holds the new key, though no plaintext, until
// it has taken effect or the application lets it go; it never unlocks this value again. Once the files on
// disk no longer belong to that key, as after Decrypt or Encrypt, it fails with ErrSwitched and writes
// nothing; a ChangePassphrase keeps the key. It takes effect once. Without Files.Lock or Options.Revert
// CreateWithUndo fails with ErrCreateUndoNeedsRevert and writes nothing.
func (v *Vault) CreateWithUndo(passphrase string, document []byte) (undo func() error, err error) {
	if !v.locking() || v.opts.Revert == nil {
		return nil, ErrCreateUndoNeedsRevert
	}
	id, wrote, err := v.create(passphrase, document)
	if err != nil {
		return nil, err
	}
	return v.createUndo(id, savedFile{path: v.documentPath(), data: wrote, exists: true}), nil
}

// create is Create and returns the new key and the document file it wrote.
func (v *Vault) create(passphrase string, document []byte) (*identity, []byte, error) {
	if passphrase == "" {
		return nil, nil, ErrEmptyPassphrase
	}
	if err := checkSchema(v.documentPath(), document, v.opts.Schemas); err != nil {
		return nil, nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(true)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	state, err := v.state()
	if err != nil {
		return nil, nil, err
	}
	if state != StateAbsent {
		return nil, nil, ErrExists
	}
	if err := v.createDir(); err != nil {
		return nil, nil, err
	}
	return v.encryptNewKey(passphrase, document)
}

// encryptNew writes a fresh key pair and document and holds both. The caller holds the mutex.
func (v *Vault) encryptNew(passphrase string, document []byte) error {
	_, _, err := v.encryptNewKey(passphrase, document)
	return err
}

// encryptNewKey is encryptNew and returns the new key and the document file it wrote.
func (v *Vault) encryptNewKey(passphrase string, document []byte) (id *identity, wrote []byte, err error) {
	id, err = generateIdentity()
	if err != nil {
		return nil, nil, err
	}
	keyData, err := wrapKey(id, passphrase, v.opts.WorkFactor)
	if err != nil {
		return nil, nil, err
	}
	v.noteSwitch()
	if err := v.write(v.recipientPath(), []byte(id.recipient.String()+"\n")); err != nil {
		return nil, nil, err
	}
	if wrote, err = v.writeEncrypted(id, document, false); err != nil {
		return nil, nil, err
	}
	if err := v.write(v.keyPath(), keyData); err != nil {
		return nil, nil, err
	}
	v.unlocked, v.identity, v.doc = true, id, slices.Clone(document)
	return id, wrote, nil
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

// CreateUnencryptedWithUndo is CreateUnencrypted and returns undo, which takes the change of this create
// out of the plain document as the undo of CreateWithUndo does. Once another writer encrypted the vault it
// fails with ErrSwitched and writes nothing. Without Files.Lock or Options.Revert it fails with
// ErrCreateUndoNeedsRevert and writes nothing.
func (v *Vault) CreateUnencryptedWithUndo(document []byte) (undo func() error, err error) {
	if !v.locking() || v.opts.Revert == nil {
		return nil, ErrCreateUndoNeedsRevert
	}
	wrote := slices.Clone(document)
	if err := v.CreateUnencrypted(wrote); err != nil {
		clear(wrote)
		return nil, err
	}
	return v.createUndo(nil, savedFile{path: v.plainPath(), data: wrote, exists: true}), nil
}

// createUndo returns the undo of a create that wrote the document file wrote, encrypted to id or plain
// with id nil. While this value holds the same key it holds the reverted document afterwards.
func (v *Vault) createUndo(id *identity, wrote savedFile) func() error {
	return func() error {
		v.mu.Lock()
		defer v.mu.Unlock()
		if wrote.path == "" {
			return nil
		}
		release, err := v.lockWrites(false)
		if err != nil {
			return err
		}
		defer release()
		current, err := v.saveFile(wrote.path)
		if err != nil {
			return err
		}
		defer clear(current.data)
		doc, data, err := v.revertWrites(id, []docWrite{{found: savedFile{path: wrote.path}, wrote: wrote}}, current)
		if err != nil {
			return err
		}
		defer clear(data)
		if err := v.putDocument(savedFile{path: wrote.path, data: data, exists: true}, false); err != nil {
			clear(doc)
			return err
		}
		if id != nil && v.unlocked && v.identity.recipient.String() == id.recipient.String() {
			clear(v.doc)
			v.doc = doc
		} else {
			clear(doc)
		}
		clear(wrote.data)
		id, wrote = nil, savedFile{}
		return nil
	}
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

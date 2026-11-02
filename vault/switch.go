package vault

import (
	"fmt"
	"os"
)

// Encrypt turns an unencrypted vault into an encrypted one with passphrase. The plain document is removed
// only once the encrypted vault is complete. The vault is unlocked afterwards.
func (v *Vault) Encrypt(passphrase string) error {
	if passphrase == "" {
		return ErrEmptyPassphrase
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	state, err := v.state()
	if err != nil {
		return err
	}
	switch state {
	case StateAbsent:
		return ErrAbsent
	case StateLocked, StateUnlocked:
		return ErrEncrypted
	}
	doc, err := v.loadPlain()
	if err != nil {
		return err
	}
	defer clear(doc)
	if err := v.encryptNew(passphrase, doc); err != nil {
		return err
	}
	if err := removeAll(v.plainPath()); err != nil {
		v.forget()
		return err
	}
	return nil
}

// Decrypt turns an encrypted vault into an unencrypted one. passphrase is verified fresh, every pending
// entry must merge, and the sealed files are removed with the key. It needs Files.Plain.
func (v *Vault) Decrypt(passphrase string) error {
	if v.opts.Files.Plain == "" {
		return ErrUnencryptedDisabled
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	state, err := v.state()
	if err != nil {
		return err
	}
	switch state {
	case StateAbsent:
		return ErrAbsent
	case StateUnencrypted:
		return ErrNotEncrypted
	}
	id, err := v.unwrapIdentity(passphrase)
	if err != nil {
		return err
	}
	doc, err := v.loadDocument(id)
	if err != nil {
		return err
	}
	doc, _, err = v.mergePending(id, doc, true)
	if err != nil {
		return err
	}
	defer clear(doc)
	if doc == nil {
		return fmt.Errorf("%s is missing: %w", v.documentPath(), ErrDamaged)
	}
	if err := v.write(v.plainPath(), doc); err != nil {
		return err
	}
	v.forget()
	paths := []string{}
	for _, name := range v.opts.Files.Sealed {
		paths = append(paths, v.path(name))
	}
	paths = append(paths, v.documentPath(), v.keyPath(), v.recipientPath())
	if err := removeAll(paths...); err != nil {
		return err
	}
	_ = os.Remove(v.pendingPath())
	return nil
}

// ChangePassphrase wraps the vault key with newPassphrase after verifying oldPassphrase fresh against the
// key file. Only the key file changes.
func (v *Vault) ChangePassphrase(oldPassphrase, newPassphrase string) error {
	if newPassphrase == "" {
		return ErrEmptyPassphrase
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireEncrypted(); err != nil {
		return err
	}
	id, err := v.unwrapIdentity(oldPassphrase)
	if err != nil {
		return err
	}
	keyData, err := wrapKey(id, newPassphrase, v.opts.WorkFactor)
	if err != nil {
		return err
	}
	return v.write(v.keyPath(), keyData)
}

// VerifyPassphrase checks passphrase against the key and recipient files without any other effect.
func (v *Vault) VerifyPassphrase(passphrase string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireEncrypted(); err != nil {
		return err
	}
	_, err := v.unwrapIdentity(passphrase)
	return err
}

func (v *Vault) requireEncrypted() error {
	state, err := v.state()
	if err != nil {
		return err
	}
	switch state {
	case StateAbsent:
		return ErrAbsent
	case StateUnencrypted:
		return ErrNotEncrypted
	}
	return nil
}

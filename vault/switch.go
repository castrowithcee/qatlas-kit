package vault

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Encrypt turns an unencrypted vault into an encrypted one with passphrase. The plain document is removed
// only once the encrypted vault is complete. The vault is unlocked afterwards.
func (v *Vault) Encrypt(passphrase string) error {
	if passphrase == "" {
		return ErrEmptyPassphrase
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(false)
	if err != nil {
		return err
	}
	defer release()
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
	out, err := v.filterDocument(v.plainPath(), doc)
	if err != nil {
		return err
	}
	defer clear(out)
	if err := v.encryptNew(passphrase, out); err != nil {
		return err
	}
	if err := v.removeAll(v.plainPath()); err != nil {
		v.forget()
		return err
	}
	return nil
}

// Decrypt turns an encrypted vault into an unencrypted one. passphrase is verified fresh, every pending
// entry must merge, and the sealed files are removed with the key. Once the plain document is written the
// switch is done: a file that cannot be removed is reported but stops nothing, and a merged pending entry
// left behind is not reported. It needs Files.Plain.
func (v *Vault) Decrypt(passphrase string) error {
	if v.opts.Files.Plain == "" {
		return ErrUnencryptedDisabled
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(false)
	if err != nil {
		return err
	}
	defer release()
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
	// The merge stays in memory: the encrypted files are removed anyway, and every failure before the plain
	// document is written leaves them untouched.
	merged, done, err := v.planMerge(id, doc, true)
	if err != nil {
		clear(doc)
		return err
	}
	if len(done) > 0 {
		clear(doc)
		doc = merged
	}
	defer clear(doc)
	if doc == nil {
		return fmt.Errorf("%s is missing: %w", v.documentPath(), ErrDamaged)
	}
	out, err := v.filterDocument(v.plainPath(), doc)
	if err != nil {
		return err
	}
	defer clear(out)
	v.noteSwitch()
	if err := v.write(v.plainPath(), out); err != nil {
		return err
	}
	v.forget()
	// The switch is done once the plain document is written, so no removal stops it: every encrypted file is
	// tried, the key first, and the failures are reported together.
	paths := []string{v.keyPath(), v.recipientPath(), v.documentPath()}
	for _, name := range v.opts.Files.Sealed {
		paths = append(paths, v.path(name))
	}
	var failed []error
	for _, path := range paths {
		if err := v.retry(func() error { return v.remove(path) }); err != nil && !isNotExist(err) {
			failed = append(failed, err)
		}
	}
	// An entry left behind is encrypted to the recipient and already merged. RemovePending removes it.
	for _, name := range done {
		_ = v.removeEntry(v.remove, filepath.Join(v.pendingPath(), name), false)
	}
	_ = v.removeAll(v.pendingPath())
	return errors.Join(failed...)
}

// ChangePassphrase wraps the vault key with newPassphrase after verifying oldPassphrase fresh against the
// key file. Only the key file changes.
func (v *Vault) ChangePassphrase(oldPassphrase, newPassphrase string) error {
	if newPassphrase == "" {
		return ErrEmptyPassphrase
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(false)
	if err != nil {
		return err
	}
	defer release()
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
	v.noteSwitch()
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

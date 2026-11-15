package vault

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

// UpdateFunc changes document, which is nil while the vault has no document file, and may merge pending
// entries into it. It returns the changed document and the names of the entries it merged; those files are
// removed once the changed document is written. An entry it leaves out stays pending.
type UpdateFunc func(document []byte, pending []Pending) (updated []byte, done []string, err error)

// ReadPending hands the pending entries of the unlocked vault, sorted by name, to fn and returns its error.
// Payloads are cleared once fn returns. Anyone who can write the vault directory can add an entry, so a
// payload is data for fn, nothing more. fn runs after the vault is released and may call it. An encrypted
// vault that is not unlocked reports ErrLocked, an unencrypted one ErrNotEncrypted.
func (v *Vault) ReadPending(fn func(pending []Pending) error) error {
	pending, err := func() ([]Pending, error) {
		v.mu.Lock()
		defer v.mu.Unlock()
		if err := v.requireUnlocked(); err != nil {
			return nil, err
		}
		names, err := v.pendingFiles()
		if err != nil {
			return nil, err
		}
		pending, _, err := v.readPending(v.identity, names)
		return pending, err
	}()
	if err != nil {
		return err
	}
	defer clearPending(pending)
	return fn(pending)
}

// Update hands a copy of the document and every pending entry of the unlocked vault to change, writes the
// document it returns once and only then removes the entries it reports, so it needs no passphrase. Should
// a removal fail, the document file and the entries removed so far are put back and the error is reported,
// because an entry left behind would merge again on the next Unlock. On any error the files and the held
// document stay as they were. The document copy and the payloads are cleared after change returns. change
// runs while the vault is held and must not call it.
//
// undo puts back the document file and the pending files this call removed, byte for byte and private,
// and leaves every other pending entry alone, unlike a Checkpoint's restore. It needs no passphrase and
// restores the files after Forget too; while the value is unlocked it holds the restored document again.
// It takes effect once.
func (v *Vault) Update(change UpdateFunc) (undo func() error, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return nil, err
	}
	names, err := v.pendingFiles()
	if err != nil {
		return nil, err
	}
	pending, raw, err := v.readPending(v.identity, names)
	if err != nil {
		return nil, err
	}
	defer clearPending(pending)
	saved, err := saveFile(v.documentPath())
	if err != nil {
		return nil, err
	}
	document := slices.Clone(v.doc)
	defer clear(document)

	updated, done, err := change(document, pending)
	if err != nil {
		return nil, err
	}
	for _, name := range done {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("vault: the update reported %q, which is not a pending entry", name)
		}
	}
	done = slices.Clone(done)
	slices.Sort(done)
	done = slices.Compact(done)
	if err := checkSchema(v.documentPath(), updated, v.opts.Schemas); err != nil {
		return nil, err
	}
	updated = slices.Clone(updated)
	if err := v.writeDocument(v.identity, updated); err != nil {
		clear(updated)
		return nil, err
	}

	files := []savedFile{saved}
	for _, name := range done {
		files = append(files, savedFile{path: filepath.Join(v.pendingPath(), name),
			data: raw[slices.Index(names, name)], exists: true})
	}
	for i := 1; i < len(files); i++ {
		if err := v.remove(files[i].path); err != nil && !isNotExist(err) {
			clear(updated)
			// The failed file is written back too, in case the removal took effect after all.
			restoreErr := v.restoreFiles(files[:i+1])
			return nil, errors.Join(fmt.Errorf("cannot remove %s: %w", files[i].path, err), restoreErr)
		}
	}
	clear(v.doc)
	v.doc = updated
	return v.undo(files, true), nil
}

// WriteWithUndo replaces the document of an unlocked or unencrypted vault like Write and returns undo,
// which puts the previous document file back byte for byte and private, or removes it when there was none,
// and touches no other file. It needs no passphrase and restores the file after Forget too; while the value
// is unlocked it holds the restored document again. It takes effect once.
func (v *Vault) WriteWithUndo(document []byte) (undo func() error, err error) {
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
		if err := checkSchema(v.plainPath(), document, v.opts.Schemas); err != nil {
			return nil, err
		}
		saved, err := saveFile(v.plainPath())
		if err != nil {
			return nil, err
		}
		if err := v.write(v.plainPath(), document); err != nil {
			clear(saved.data)
			return nil, err
		}
		return v.undo([]savedFile{saved}, false), nil
	}
	if err := checkSchema(v.documentPath(), document, v.opts.Schemas); err != nil {
		return nil, err
	}
	saved, err := saveFile(v.documentPath())
	if err != nil {
		return nil, err
	}
	if err := v.writeDocument(v.identity, document); err != nil {
		return nil, err
	}
	clear(v.doc)
	v.doc = slices.Clone(document)
	return v.undo([]savedFile{saved}, true), nil
}

func (v *Vault) requireUnlocked() error {
	state, err := v.state()
	if err != nil {
		return err
	}
	switch state {
	case StateAbsent:
		return ErrAbsent
	case StateUnencrypted:
		return ErrNotEncrypted
	case StateLocked:
		return ErrLocked
	}
	return nil
}

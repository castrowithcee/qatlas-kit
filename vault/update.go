package vault

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

// UpdateFunc changes document, which is nil while the vault has no document file, and may merge pending
// entries into it. It returns the changed document and the names of the entries it merged; those files are
// removed once the changed document is written. An entry it leaves out stays pending. It returns ErrNoChange
// to end the Update without writing anything.
type UpdateFunc func(document []byte, pending []Pending) (updated []byte, done []string, err error)

// ErrNoChange is returned by an UpdateFunc that leaves the vault as it is. Update then writes nothing and
// reports no error, and its undo does nothing.
var ErrNoChange = errors.New("the update changes nothing")

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
// With Files.Lock the value first takes the document as it is on disk under the write lock and holds it from
// then on, even when the update ends without a write, so change sees what another process wrote meanwhile
// and can compare it with what it expects before it changes anything.
//
// undo puts back the document file and the pending files this call removed, byte for byte and private,
// and leaves every other pending entry alone. It needs no passphrase and restores the files after Forget
// too; while the value is unlocked it holds the restored document again. It takes effect once. With
// Files.Lock it puts the bytes back only while the document file holds what this call wrote; otherwise
// Options.Revert takes the change out of the document on disk, which needs the value unlocked, and
// without Revert undo fails with ErrConflict and writes nothing.
func (v *Vault) Update(change UpdateFunc) (undo func() error, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(false)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := v.requireUnlocked(); err != nil {
		return nil, err
	}
	if v.locking() {
		if err := v.checkHeld(); err != nil {
			return nil, err
		}
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
	saved, err := v.saveFile(v.documentPath())
	if err != nil {
		return nil, err
	}
	if v.locking() {
		onDisk, err := v.documentOf(saved)
		if err != nil {
			clear(saved.data)
			return nil, err
		}
		clear(v.doc)
		v.doc = onDisk
	}
	document := slices.Clone(v.doc)
	defer clear(document)

	updated, done, err := change(document, pending)
	if errors.Is(err, ErrNoChange) {
		clear(saved.data)
		return func() error { return nil }, nil
	}
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
	wrote, err := v.writeEncrypted(v.identity, updated)
	if err != nil {
		clear(updated)
		return nil, err
	}

	files := []savedFile{saved}
	for _, name := range done {
		files = append(files, savedFile{path: filepath.Join(v.pendingPath(), name),
			data: raw[slices.Index(names, name)], exists: true})
	}
	for i := 1; i < len(files); i++ {
		if err := v.removeEntry(v.remove, files[i].path); err != nil && !isNotExist(err) {
			clear(updated)
			// The failed file is written back too, in case the removal took effect after all.
			restoreErr := v.restoreFiles(files[:i+1])
			return nil, errors.Join(fmt.Errorf("cannot remove %s: %w", files[i].path, err), restoreErr)
		}
	}
	clear(v.doc)
	v.doc = updated
	return v.undo(files, savedFile{path: v.documentPath(), data: wrote, exists: true}, true), nil
}

// WriteWithUndo replaces the document of an unlocked or unencrypted vault like Write and returns undo,
// which puts the previous document file back byte for byte and private, or removes it when there was none,
// and touches no other file. It needs no passphrase and restores the file after Forget too; while the value
// is unlocked it holds the restored document again. It takes effect once. With Files.Lock it takes back
// this write alone, as the undo of Update does.
func (v *Vault) WriteWithUndo(document []byte) (undo func() error, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	release, err := v.lockWrites(false)
	if err != nil {
		return nil, err
	}
	defer release()
	state, err := v.writeState()
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
		saved, err := v.saveFile(v.plainPath())
		if err != nil {
			return nil, err
		}
		wrote := savedFile{path: v.plainPath(), data: slices.Clone(document), exists: true}
		if err := v.putDocument(wrote); err != nil {
			clear(saved.data)
			clear(wrote.data)
			return nil, err
		}
		return v.undo([]savedFile{saved}, wrote, false), nil
	}
	if err := checkSchema(v.documentPath(), document, v.opts.Schemas); err != nil {
		return nil, err
	}
	saved, err := v.saveFile(v.documentPath())
	if err != nil {
		return nil, err
	}
	wrote, err := v.writeEncrypted(v.identity, document)
	if err != nil {
		return nil, err
	}
	clear(v.doc)
	v.doc = slices.Clone(document)
	return v.undo([]savedFile{saved}, savedFile{path: v.documentPath(), data: wrote, exists: true}, true), nil
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

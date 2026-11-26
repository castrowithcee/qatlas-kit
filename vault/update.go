package vault

import (
	"errors"
	"fmt"
	"os"
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

// UpdateOption changes one call of UpdateWithOptions. Passing no option is the safe choice.
type UpdateOption func(*updateConfig)

type updateConfig struct{ exempt bool }

// ExemptFromCheckpoints keeps the restore of every open Checkpoint of this value from taking this update
// back, as for a value that must never return to an earlier state, such as a rotated token. Each open
// checkpoint counts the update as a write of another process: its restore takes this value's other writes
// out of the document on disk through Options.Revert and leaves this one, and without Revert it fails with
// ErrConflict. The undo of the update is exempt as well. It needs Files.Lock, since only there does a
// restore take back this value's writes alone; without it the update fails with ErrExemptNeedsLock and
// writes nothing.
func ExemptFromCheckpoints() UpdateOption {
	return func(c *updateConfig) { c.exempt = true }
}

// ErrExemptNeedsLock reports an update exempt from checkpoints of a vault without Files.Lock. Nothing is
// written.
var ErrExemptNeedsLock = errors.New("an update exempt from checkpoints needs Files.Lock")

// Update hands a copy of the document and every pending entry of the unlocked vault to change, writes the
// document it returns once and only then removes the entries it reports, so it needs no passphrase. Should
// a removal fail, the document file and the entries removed so far are put back and the error is reported,
// because an entry left behind would merge again on the next Unlock. On any error the files and the held
// document stay as they were. The document copy and the payloads are cleared after change returns. change
// runs while the vault is held and must not call it.
//
// An unencrypted vault hands change the plain document and no pending entries, since it has none; an entry
// reported anyway is an error. It holds no document, as with Write.
//
// With Files.Lock the value first takes the document as it is on disk under the write lock, and an
// unlocked value holds it from then on, even when the update ends without a write, so change sees what
// another process wrote meanwhile and can compare it with what it expects before it changes anything.
// Without it updates are serialized within this value alone.
//
// undo puts back the document file and the pending files this call removed, byte for byte and private,
// and leaves every other pending entry alone. It needs no passphrase and restores the files after Forget
// too; while the value is unlocked it holds the restored document again. It takes effect once. With
// Files.Lock it puts the bytes back only while the document file holds what this call wrote; otherwise
// Options.Revert takes the change out of the document on disk, which needs an encrypted vault unlocked, and
// without Revert undo fails with ErrConflict and writes nothing.
func (v *Vault) Update(change UpdateFunc) (undo func() error, err error) {
	return v.UpdateWithOptions(change)
}

// UpdateWithOptions is Update with options; without any it behaves the same.
func (v *Vault) UpdateWithOptions(change UpdateFunc, options ...UpdateOption) (undo func() error, err error) {
	var cfg updateConfig
	for _, option := range options {
		option(&cfg)
	}
	if cfg.exempt && !v.locking() {
		return nil, ErrExemptNeedsLock
	}
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
		return v.updatePlain(change, cfg.exempt)
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
	wrote, err := v.writeEncrypted(v.identity, updated, cfg.exempt)
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
		if err := v.removeEntry(v.remove, files[i].path, cfg.exempt); err != nil && !isNotExist(err) {
			clear(updated)
			// The failed file is written back too, in case the removal took effect after all.
			restoreErr := v.restoreFiles(files[:i+1], cfg.exempt)
			return nil, errors.Join(fmt.Errorf("cannot remove %s: %w", files[i].path, err), restoreErr)
		}
	}
	clear(v.doc)
	v.doc = updated
	return v.undo(files, savedFile{path: v.documentPath(), data: wrote, exists: true}, true, cfg.exempt), nil
}

// updatePlain is Update of an unencrypted vault. The caller holds the mutex and, with Files.Lock, the write
// lock.
func (v *Vault) updatePlain(change UpdateFunc, exempt bool) (undo func() error, err error) {
	saved, err := v.saveFile(v.plainPath())
	if err != nil {
		return nil, err
	}
	if !saved.exists {
		return nil, fmt.Errorf("cannot read %s: %w", v.plainPath(), os.ErrNotExist)
	}
	kept := false
	defer func() {
		if !kept {
			clear(saved.data)
		}
	}()
	document, err := v.readDocument(v.plainPath(), slices.Clone(saved.data))
	if err != nil {
		return nil, err
	}
	defer clear(document)

	updated, done, err := change(document, nil)
	if errors.Is(err, ErrNoChange) {
		return func() error { return nil }, nil
	}
	if err != nil {
		return nil, err
	}
	if len(done) > 0 {
		return nil, fmt.Errorf("vault: the update reported %q, which is not a pending entry", done[0])
	}
	if err := checkSchema(v.plainPath(), updated, v.opts.Schemas); err != nil {
		return nil, err
	}
	wrote := savedFile{path: v.plainPath(), data: slices.Clone(updated), exists: true}
	if err := v.putDocument(wrote, exempt); err != nil {
		clear(wrote.data)
		return nil, err
	}
	kept = true
	return v.undo([]savedFile{saved}, wrote, false, exempt), nil
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
		if err := v.putDocument(wrote, false); err != nil {
			clear(saved.data)
			clear(wrote.data)
			return nil, err
		}
		return v.undo([]savedFile{saved}, wrote, false, false), nil
	}
	if err := checkSchema(v.documentPath(), document, v.opts.Schemas); err != nil {
		return nil, err
	}
	saved, err := v.saveFile(v.documentPath())
	if err != nil {
		return nil, err
	}
	wrote, err := v.writeEncrypted(v.identity, document, false)
	if err != nil {
		return nil, err
	}
	clear(v.doc)
	v.doc = slices.Clone(document)
	return v.undo([]savedFile{saved}, savedFile{path: v.documentPath(), data: wrote, exists: true}, true, false), nil
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

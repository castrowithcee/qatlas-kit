package vault

import (
	"errors"
	"fmt"
	"slices"
)

// savedFile is a file as it was before a change: its bytes, or that it did not exist.
type savedFile struct {
	path   string
	data   []byte
	exists bool
}

// saveFile records path for an undo. It refuses a file others can reach, like every read.
func (v *Vault) saveFile(path string) (savedFile, error) {
	data, err := v.readFile(path)
	switch {
	case err == nil:
		return savedFile{path: path, data: data, exists: true}, nil
	case isNotExist(err):
		return savedFile{path: path}, nil
	default:
		return savedFile{}, fmt.Errorf("cannot read %s: %w", path, err)
	}
}

// restoreFiles writes every saved file back privately, or removes it when it did not exist; files[0] is the
// document file, the others are pending files. It goes on after a failure so as much as possible is
// restored. exempt is as for putDocument. The caller holds the mutex.
func (v *Vault) restoreFiles(files []savedFile, exempt bool) error {
	return errors.Join(v.putDocument(files[0], exempt), v.putBackEntries(files[1:]))
}

// undo returns the undo of one operation: it puts back files and nothing else. files[0] is the document
// file, encrypted with document, and wrote is what the operation left in it. While this value is unlocked
// the document it holds is decrypted afresh from the restored bytes, so the undo keeps no plaintext of the
// encrypted document. With Files.Lock it takes the write lock and puts the bytes back only while the file
// still holds wrote; otherwise Options.Revert takes the change out of the document on disk. The undo of an
// exempt write is exempt too, so a checkpoint's restore does not bring the write back. Once a call has
// taken effect, further calls do nothing; a call that failed may be repeated.
func (v *Vault) undo(files []savedFile, wrote savedFile, document, exempt bool) func() error {
	done := false
	return func() error {
		v.mu.Lock()
		defer v.mu.Unlock()
		if done {
			return nil
		}
		release, err := v.lockWrites(false)
		if err != nil {
			return err
		}
		defer release()
		if v.locking() {
			current, err := v.saveFile(files[0].path)
			if err != nil {
				return err
			}
			defer clear(current.data)
			if !sameFile(current, wrote) {
				if err := v.undoRevert(files, current, wrote, document, exempt); err != nil {
					return err
				}
				done = true
				clearSaved(files, wrote)
				return nil
			}
		}
		if err := v.restoreFiles(files, exempt); err != nil {
			return err
		}
		done = true
		if document && v.unlocked {
			err = v.reloadHeld(files[0])
		}
		clearSaved(files, wrote)
		return err
	}
}

func clearSaved(files []savedFile, wrote savedFile) {
	for _, f := range files {
		clear(f.data)
	}
	clear(wrote.data)
}

// undoRevert takes one write out of the document file another writer changed since and puts back the
// pending files it removed. The caller holds the mutex and the write lock.
func (v *Vault) undoRevert(files []savedFile, current, wrote savedFile, document, exempt bool) error {
	doc, data, err := v.revertWrites(v.identity, []docWrite{{found: files[0], wrote: wrote}}, current)
	if err != nil {
		return err
	}
	defer clear(data)
	if err := v.putDocument(savedFile{path: current.path, data: data, exists: true}, exempt); err != nil {
		clear(doc)
		return err
	}
	if document {
		clear(v.doc)
		v.doc = doc
	} else {
		clear(doc)
	}
	return v.putBackEntries(files[1:])
}

// revertWrites takes writes, oldest first, out of current, the document file on disk, newest first through
// Options.Revert, and returns the checked document and the bytes to write. It writes nothing. An encrypted
// document needs id, the key the files on disk belong to; nil is ErrLocked. The caller holds the mutex and
// the write lock.
func (v *Vault) revertWrites(id *identity, writes []docWrite, current savedFile) (doc, data []byte, err error) {
	if v.opts.Revert == nil {
		return nil, nil, ErrConflict
	}
	encrypted := current.path == v.documentPath()
	if encrypted {
		if id == nil {
			return nil, nil, ErrLocked
		}
		if err := v.checkHeld(id); err != nil {
			return nil, nil, err
		}
	} else if !current.exists {
		// Another writer encrypted the vault; a plain document written now would lie beside it.
		return nil, nil, ErrSwitched
	}
	read := func(f savedFile) ([]byte, error) {
		switch {
		case !f.exists:
			return nil, nil
		case encrypted:
			return v.documentOf(id, f)
		}
		return v.readDocument(f.path, slices.Clone(f.data))
	}
	if doc, err = read(current); err != nil {
		return nil, nil, err
	}
	for i := len(writes) - 1; i >= 0; i-- {
		before, err := read(writes[i].found)
		if err != nil {
			clear(doc)
			return nil, nil, err
		}
		after, err := read(writes[i].wrote)
		if err != nil {
			clear(before)
			clear(doc)
			return nil, nil, err
		}
		out, err := v.opts.Revert(before, after, doc)
		out = slices.Clone(out)
		clear(before)
		clear(after)
		clear(doc)
		if err != nil {
			clear(out)
			return nil, nil, err
		}
		doc = out
	}
	if err := checkSchema(current.path, doc, v.opts.Schemas); err != nil {
		clear(doc)
		return nil, nil, err
	}
	if !encrypted {
		return doc, slices.Clone(doc), nil
	}
	if data, err = encryptTo(doc, id.recipient); err != nil {
		clear(doc)
		return nil, nil, err
	}
	return doc, data, nil
}

// reloadHeld replaces the held document with the restored document file. A file that does not decrypt
// with the held key locks this value, so it never holds a document that differs from the file. The caller
// holds the mutex.
func (v *Vault) reloadHeld(f savedFile) error {
	doc, err := v.documentOf(v.identity, f)
	if err != nil {
		v.forget()
		return err
	}
	clear(v.doc)
	v.doc = doc
	return nil
}

// documentOf decrypts and checks the saved document file with id; a missing file is nil.
func (v *Vault) documentOf(id *identity, f savedFile) ([]byte, error) {
	if !f.exists {
		return nil, nil
	}
	plain, err := decryptFrom(f.data, id.key)
	if err != nil {
		return nil, decryptFailure(f.path, f.data, err)
	}
	return v.readDocument(f.path, plain)
}

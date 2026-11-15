package vault

import (
	"errors"
	"fmt"
)

// savedFile is a file as it was before a change: its bytes, or that it did not exist.
type savedFile struct {
	path   string
	data   []byte
	exists bool
}

// saveFile records path for an undo. It refuses a file others can reach, like every read.
func saveFile(path string) (savedFile, error) {
	data, err := readFile(path)
	switch {
	case err == nil:
		return savedFile{path: path, data: data, exists: true}, nil
	case isNotExist(err):
		return savedFile{path: path}, nil
	default:
		return savedFile{}, fmt.Errorf("cannot read %s: %w", path, err)
	}
}

// restoreFiles writes every saved file back privately, or removes it when it did not exist. It goes on
// after a failure so as much as possible is restored. The caller holds the mutex.
func (v *Vault) restoreFiles(files []savedFile) error {
	var failed []error
	for _, f := range files {
		if f.exists {
			if err := v.write(f.path, f.data); err != nil {
				failed = append(failed, err)
			}
		} else if err := removeAll(f.path); err != nil {
			failed = append(failed, err)
		}
	}
	return errors.Join(failed...)
}

// undo returns the undo of one operation: it puts back files and nothing else. With document, files[0] is
// the encrypted document, and while this value is unlocked the document it holds is decrypted afresh from
// the restored bytes, so the undo keeps no plaintext of the encrypted document. Once a call has restored
// the files, further calls do nothing; a call that failed may be repeated.
func (v *Vault) undo(files []savedFile, document bool) func() error {
	done := false
	return func() error {
		v.mu.Lock()
		defer v.mu.Unlock()
		if done {
			return nil
		}
		if err := v.restoreFiles(files); err != nil {
			return err
		}
		done = true
		var err error
		if document && v.unlocked {
			err = v.reloadHeld(files[0])
		}
		for _, f := range files {
			clear(f.data)
		}
		return err
	}
}

// reloadHeld replaces the held document with the restored document file. A file that does not decrypt
// with the held key locks this value, so it never holds a document that differs from the file. The caller
// holds the mutex.
func (v *Vault) reloadHeld(f savedFile) error {
	var doc []byte
	if f.exists {
		plain, err := decryptFrom(f.data, v.identity.key)
		if err != nil {
			v.forget()
			return &DocumentError{Path: f.path, Kind: DocumentDecrypt}
		}
		doc, err = v.readDocument(f.path, plain)
		if err != nil {
			v.forget()
			return err
		}
	}
	clear(v.doc)
	v.doc = doc
	return nil
}

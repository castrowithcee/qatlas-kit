package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Checkpoint records the document, the plain document, the key, the recipient and the pending entries
// together with what this value holds, and returns restore, which puts those bytes back, removes such a
// file or pending entry that was not there and returns this value to the recorded state. A vault directory
// the checkpoint did not find is removed again once it is empty. restore needs no passphrase. Sealed files
// are not recorded.
func (v *Vault) Checkpoint() (restore func() error, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	files := []string{v.documentPath(), v.keyPath(), v.recipientPath()}
	if v.opts.Files.Plain != "" {
		files = append(files, v.plainPath())
	}
	saved := map[string][]byte{}
	for _, path := range files {
		data, err := readFile(path)
		switch {
		case err == nil:
			saved[path] = data
		case !isNotExist(err):
			return nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
	}
	pending, err := v.pendingFiles()
	if err != nil {
		return nil, err
	}
	dirExisted, err := fileExists(v.opts.Dir)
	if err != nil {
		return nil, err
	}
	pendingExisted, err := fileExists(v.pendingPath())
	if err != nil {
		return nil, err
	}
	unlocked, id, doc := v.unlocked, v.identity, slices.Clone(v.doc)

	return func() error {
		v.mu.Lock()
		defer v.mu.Unlock()
		var failed []error
		for _, path := range files {
			if data, ok := saved[path]; ok {
				if err := v.write(path, data); err != nil {
					failed = append(failed, err)
				}
			} else if err := removeAll(path); err != nil {
				failed = append(failed, err)
			}
		}
		now, err := v.pendingFiles()
		if err != nil {
			failed = append(failed, err)
		}
		for _, name := range now {
			if !slices.Contains(pending, name) {
				if err := removeAll(filepath.Join(v.pendingPath(), name)); err != nil {
					failed = append(failed, err)
				}
			}
		}
		if !pendingExisted {
			if err := os.Remove(v.pendingPath()); err != nil && !isNotExist(err) {
				failed = append(failed, err)
			}
		}
		if !dirExisted {
			if err := os.Remove(v.opts.Dir); err != nil && !isNotExist(err) {
				failed = append(failed, err)
			}
		}
		clear(v.doc)
		v.unlocked, v.identity, v.doc = unlocked, id, slices.Clone(doc)
		return errors.Join(failed...)
	}, nil
}

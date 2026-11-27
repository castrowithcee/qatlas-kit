package vault

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

// Checkpoint records the document, the plain document, the key, the recipient and the pending entries
// together with what this value holds, and returns restore, which puts those bytes back, also of a pending
// entry merged or removed since, removes such a file or pending entry that was not there and returns this
// value to the recorded state. A vault directory the checkpoint did not find is removed again once it is
// empty. restore needs no passphrase. Sealed files are not recorded.
//
// With Files.Lock restore takes the write lock and takes back what this value wrote since, and only that.
// While nobody else wrote the document file in between, it puts the recorded document back byte for byte;
// otherwise Options.Revert takes this value's writes out of the document on disk, which needs the value
// unlocked, and without Revert restore fails with ErrConflict and writes nothing. It puts back the pending
// entries this value merged and removes only those it added. An update exempt from checkpoints (see
// ExemptFromCheckpoints) counts as another writer's and stays; when this value wrote nothing else, restore
// leaves the document as it is. After a Create, CreateUnencrypted, Encrypt, Decrypt or ChangePassphrase of
// this value it puts every recorded file back, and once such a switch and an exempt update both happened it
// fails with ErrConflict and writes nothing, since putting the files back would take the update back. A
// vault directory the checkpoint did not find is removed once only the lock file is left. restore then
// takes effect once.
func (v *Vault) Checkpoint() (restore func() error, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	files := []string{v.documentPath(), v.keyPath(), v.recipientPath()}
	if v.opts.Files.Plain != "" {
		files = append(files, v.plainPath())
	}
	saved := map[string][]byte{}
	for _, path := range files {
		data, err := v.readFile(path)
		switch {
		case err == nil:
			saved[path] = data
		case !isNotExist(err):
			return nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
	}
	names, err := v.pendingFiles()
	if err != nil {
		return nil, err
	}
	dirExisted, err := v.fileExists(v.opts.Dir)
	if err != nil {
		return nil, err
	}
	pendingExisted, err := v.fileExists(v.pendingPath())
	if err != nil {
		return nil, err
	}
	unlocked, id, doc := v.unlocked, v.identity, slices.Clone(v.doc)
	if v.locking() {
		c := &checkpoint{files: files, saved: saved, dirExisted: dirExisted, pendingExisted: pendingExisted,
			unlocked: unlocked, id: id, doc: doc}
		j := v.openJournal(saved)
		return func() error {
			v.mu.Lock()
			defer v.mu.Unlock()
			return v.restoreLocked(j, c)
		}, nil
	}
	pending := map[string][]byte{}
	for _, name := range names {
		path := filepath.Join(v.pendingPath(), name)
		data, err := v.readFile(path)
		switch {
		case err == nil:
			pending[name] = data
		case !isNotExist(err):
			return nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
	}

	return func() error {
		v.mu.Lock()
		defer v.mu.Unlock()
		var failed []error
		for _, path := range files {
			if data, ok := saved[path]; ok {
				if err := v.write(path, data); err != nil {
					failed = append(failed, err)
				}
			} else if err := v.removeAll(path); err != nil {
				failed = append(failed, err)
			}
		}
		now, err := v.pendingFiles()
		if err != nil {
			failed = append(failed, err)
		}
		for _, name := range now {
			if _, ok := pending[name]; !ok {
				if err := v.removeAll(filepath.Join(v.pendingPath(), name)); err != nil {
					failed = append(failed, err)
				}
			}
		}
		for _, name := range names {
			if data, ok := pending[name]; ok && !slices.Contains(now, name) {
				if err := v.write(filepath.Join(v.pendingPath(), name), data); err != nil {
					failed = append(failed, err)
				}
			}
		}
		if !pendingExisted {
			if err := v.removeAll(v.pendingPath()); err != nil {
				failed = append(failed, err)
			}
		}
		if !dirExisted {
			if err := v.removeAll(v.opts.Dir); err != nil {
				failed = append(failed, err)
			}
		}
		clear(v.doc)
		v.unlocked, v.identity, v.doc = unlocked, id, slices.Clone(doc)
		return errors.Join(failed...)
	}, nil
}

// checkpoint is what Checkpoint recorded.
type checkpoint struct {
	files                      []string
	saved                      map[string][]byte
	dirExisted, pendingExisted bool
	unlocked                   bool
	id                         *identity
	doc                        []byte
}

// restoreLocked is restore with Files.Lock: under the write lock it takes back the writes j recorded and
// nothing else. It refuses before it writes anything when it cannot take them back. The caller holds the
// mutex.
func (v *Vault) restoreLocked(j *journal, c *checkpoint) error {
	if j.ended {
		return nil
	}
	release, err := v.lockWrites(false)
	if errors.Is(err, ErrAbsent) && !c.dirExisted {
		// The vault directory is gone again, and with it everything this value wrote.
		j.ended = true
		v.holdRecorded(c, nil)
		return nil
	}
	if err != nil {
		return err
	}
	failed, err := v.restoreJournal(j, c)
	release()
	if err != nil {
		return err
	}
	if !c.dirExisted {
		failed = append(failed, v.removeEmptyDir())
	}
	err = errors.Join(failed...)
	j.ended = err == nil
	return err
}

// restoreJournal puts the recorded bytes back while nobody else wrote the document file since the
// checkpoint, or else takes this value's writes out of it through Options.Revert, then puts back the
// pending files this value removed and removes the entries it added. Every part that succeeds is dropped
// from j, so a failed restore may be repeated. It reports refusal apart from the failures of its writes.
// The caller holds the mutex and the write lock.
//
// An exempt write is not in j.writes, so the revert leaves it; after a switch it refuses instead, because
// only putting every file back takes a switch back.
func (v *Vault) restoreJournal(j *journal, c *checkpoint) (failed []error, refused error) {
	var paths []string // the files to put back byte for byte
	var reverted, data []byte
	var path string
	switch {
	case j.switched && j.exempt:
		return nil, ErrConflict
	case j.switched:
		paths = c.files
	case len(j.writes) > 0:
		path = j.writes[0].found.path
		for _, w := range j.writes {
			if w.found.path != path {
				return nil, ErrSwitched
			}
		}
		current, err := v.saveFile(path)
		if err != nil {
			return nil, err
		}
		if j.clean && sameFile(current, j.writes[len(j.writes)-1].wrote) {
			paths = []string{path}
		} else if reverted, data, err = v.revertWrites(v.identity, j.writes, current); err != nil {
			clear(current.data)
			return nil, err
		}
		clear(current.data)
	}

	j.paused = true // so the writes below are not recorded as this value's own
	defer func() { j.paused = false }()
	docFailed := false
	for _, p := range paths {
		saved, ok := c.saved[p]
		var err error
		switch {
		case !j.switched:
			err = v.putDocument(savedFile{path: p, data: saved, exists: ok}, false)
		case ok:
			err = v.write(p, saved)
		default:
			err = v.removeAll(p)
		}
		if err != nil {
			failed, docFailed = append(failed, err), true
		}
	}
	if j.switched {
		v.noteSwitch()
	}
	if data != nil {
		err := v.putDocument(savedFile{path: path, data: data, exists: true}, false)
		clear(data)
		if err != nil {
			failed, docFailed = append(failed, err), true
		}
	}
	if !docFailed {
		j.writes, j.switched = nil, false
		var kept []savedFile
		for _, f := range j.consumed {
			if err := v.putBackEntries([]savedFile{f}); err != nil {
				failed, kept = append(failed, err), append(kept, f)
			}
		}
		j.consumed = kept
	}
	var queued []string
	for _, p := range j.queued {
		if err := v.removeAll(p); err != nil {
			failed, queued = append(failed, err), append(queued, p)
		}
	}
	j.queued = queued
	if !c.pendingExisted {
		if entries, err := v.readDir(v.pendingPath()); err == nil && len(entries) == 0 {
			if err := v.removeAll(v.pendingPath()); err != nil {
				failed = append(failed, err)
			}
		}
	}

	switch {
	case docFailed:
		clear(reverted)
		v.forget()
	case reverted != nil:
		v.holdRecorded(c, reverted)
	case paths != nil && c.unlocked:
		v.holdRecorded(c, nil)
		saved, ok := c.saved[v.documentPath()]
		doc, err := v.documentOf(v.identity, savedFile{path: v.documentPath(), data: saved, exists: ok})
		if err != nil {
			v.forget()
			failed = append(failed, err)
		} else {
			v.doc = doc
		}
	case j.exempt && c.unlocked:
		// The recorded document lacks the exempt write that stays on disk.
		v.holdRecorded(c, nil)
		current, err := v.saveFile(v.documentPath())
		if err == nil {
			v.doc, err = v.documentOf(v.identity, current)
			clear(current.data)
		}
		if err != nil {
			v.forget()
			failed = append(failed, err)
		}
	default:
		v.holdRecorded(c, slices.Clone(c.doc))
	}
	return failed, nil
}

// holdRecorded returns this value to the recorded state, holding doc while it was unlocked.
func (v *Vault) holdRecorded(c *checkpoint, doc []byte) {
	clear(v.doc)
	v.unlocked, v.identity, v.doc = c.unlocked, c.id, nil
	if c.unlocked {
		v.doc = doc
	} else {
		clear(doc)
	}
}

// removeEmptyDir removes the vault directory the checkpoint did not find once it holds nothing but the lock
// file; a directory another writer filled meanwhile stays. It runs after the lock is released, because an
// open file cannot be removed on Windows.
func (v *Vault) removeEmptyDir() error {
	entries, err := v.readDir(v.opts.Dir)
	if isNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 1 && entries[0].Name() == v.opts.Files.Lock {
		if err := v.removeAll(v.path(v.opts.Files.Lock)); err != nil {
			return err
		}
	} else if len(entries) > 0 {
		return nil
	}
	return v.removeAll(v.opts.Dir)
}

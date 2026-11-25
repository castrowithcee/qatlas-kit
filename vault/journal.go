package vault

import (
	"bytes"
	"errors"
	"slices"
	"weak"
)

// journal is what an open Checkpoint of a value with Files.Lock knows of this value's writes since, so its
// restore takes back those writes alone.
type journal struct {
	// saved holds the files the checkpoint recorded by path; a missing path did not exist.
	saved map[string][]byte
	// writes are this value's writes of the document file, oldest first.
	writes []docWrite
	// clean stays set while every write found the document file as the checkpoint or the previous write
	// left it, so nobody else wrote it in between.
	clean bool
	// switched is set by a Create, CreateUnencrypted, Encrypt, Decrypt or ChangePassphrase of this value.
	switched bool
	// consumed are the pending files this value removed, to be put back.
	consumed []savedFile
	// queued are the paths of the pending entries this value added.
	queued []string
	// paused while its own restore writes; ended once that restore took effect.
	paused, ended bool
}

// docWrite is one write of a document file: the file as the write found it and as it left it.
type docWrite struct {
	found, wrote savedFile
}

func sameFile(a, b savedFile) bool { return a.exists == b.exists && bytes.Equal(a.data, b.data) }

// liveJournals returns the open checkpoints and drops those that ended or whose restore nobody holds any
// more, so a checkpoint that is never restored stops recording once the application lets it go. The caller
// holds the mutex.
func (v *Vault) liveJournals() []*journal {
	var live []*journal
	v.journals = slices.DeleteFunc(v.journals, func(p weak.Pointer[journal]) bool {
		j := p.Value()
		if j == nil || j.ended {
			return true
		}
		if !j.paused {
			live = append(live, j)
		}
		return false
	})
	return live
}

func (v *Vault) openJournal(saved map[string][]byte) *journal {
	j := &journal{saved: saved, clean: true}
	v.journals = append(v.journals, weak.Make(j))
	return j
}

// putDocument writes f, a document file, back or removes it when it did not exist, and records the write in
// every open checkpoint. The caller holds the mutex and, with Files.Lock, the write lock.
func (v *Vault) putDocument(f savedFile) error {
	live := v.liveJournals()
	var found savedFile
	if len(live) > 0 {
		var err error
		if found, err = saveFile(f.path); err != nil {
			return err
		}
	}
	var err error
	if f.exists {
		err = v.write(f.path, f.data)
	} else {
		err = removeAll(f.path)
	}
	if err != nil {
		return err
	}
	wrote := savedFile{path: f.path, data: slices.Clone(f.data), exists: f.exists}
	for _, j := range live {
		prev := savedFile{path: f.path}
		if n := len(j.writes); n > 0 {
			prev = j.writes[n-1].wrote
		} else if data, ok := j.saved[f.path]; ok {
			prev = savedFile{path: f.path, data: data, exists: true}
		}
		if prev.path != f.path || !sameFile(prev, found) {
			j.clean = false
		}
		j.writes = append(j.writes, docWrite{found: found, wrote: wrote})
	}
	return nil
}

// removeEntry removes the pending file path with remove and records it in every open checkpoint, so its
// restore can put the entry back. The caller holds the mutex.
func (v *Vault) removeEntry(remove func(string) error, path string) error {
	live := v.liveJournals()
	var data []byte
	if len(live) > 0 {
		var err error
		if data, err = readFile(path); err != nil && !isNotExist(err) {
			return err
		}
	}
	if err := remove(path); err != nil {
		return err
	}
	if data != nil {
		for _, j := range live {
			j.consumed = append(j.consumed, savedFile{path: path, data: data, exists: true})
		}
	}
	return nil
}

// putBackEntries writes removed pending files back. It goes on after a failure. The caller holds the mutex.
func (v *Vault) putBackEntries(files []savedFile) error {
	var failed []error
	for _, f := range files {
		if err := v.write(f.path, f.data); err != nil {
			failed = append(failed, err)
			continue
		}
		for _, j := range v.liveJournals() {
			j.consumed = slices.DeleteFunc(j.consumed, func(c savedFile) bool { return c.path == f.path })
		}
	}
	return errors.Join(failed...)
}

func (v *Vault) noteQueued(path string) {
	for _, j := range v.liveJournals() {
		j.queued = append(j.queued, path)
	}
}

// noteSwitch marks a write that creates the vault, switches its mode or rewraps its key: a restore then puts
// every recorded file back. The caller holds the mutex.
func (v *Vault) noteSwitch() {
	for _, j := range v.liveJournals() {
		j.switched = true
	}
}

package vault

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"filippo.io/age"
)

const pendingSuffix = ".age"

// pendingFiles returns the names of the pending entries, sorted, so a merge is deterministic.
func (v *Vault) pendingFiles() ([]string, error) {
	entries, err := v.readDir(v.pendingPath())
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", v.pendingPath(), err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && isPendingName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func isPendingName(name string) bool {
	return isFileName(name) && !strings.HasPrefix(name, ".") && strings.HasSuffix(name, pendingSuffix)
}

// AddPending stores payload as a pending entry of an encrypted vault, encrypted to its recipient, so it
// needs no passphrase. The next Unlock hands it to the MergeFunc. It returns the entry's file name.
func (v *Vault) AddPending(payload []byte) (name string, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	state, err := v.state()
	if err != nil {
		return "", err
	}
	var recipient *age.X25519Recipient
	switch state {
	case StateAbsent:
		return "", ErrAbsent
	case StateUnencrypted:
		return "", ErrNotEncrypted
	case StateUnlocked:
		recipient = v.identity.recipient
	default:
		text, err := v.readFile(v.recipientPath())
		if err != nil {
			return "", fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
		}
		if recipient, err = parseRecipient(text); err != nil {
			return "", err
		}
	}
	data, err := encryptTo(payload, recipient)
	if err != nil {
		return "", err
	}
	random := make([]byte, 16)
	_, _ = rand.Read(random) // crypto/rand.Read never returns an error; it ends the program instead.
	name = hex.EncodeToString(random) + pendingSuffix
	path := filepath.Join(v.pendingPath(), name)
	if err := v.write(path, data); err != nil {
		return "", err
	}
	v.noteQueued(path)
	return name, nil
}

// RemovePending removes the pending entry name, for example to undo an AddPending. An entry that does not
// exist is not an error.
func (v *Vault) RemovePending(name string) error {
	if !isPendingName(name) {
		return fmt.Errorf("vault: %q is not a pending entry name", name)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.removeAll(filepath.Join(v.pendingPath(), name))
}

// planMerge hands every pending entry to the MergeFunc and returns the merged document and the names of the
// entries it merged, writing and removing nothing. With all, an entry left pending is an error. When it
// merged none, it returns doc itself and no names; otherwise doc is left for the caller to clear. The
// caller holds the mutex.
func (v *Vault) planMerge(id *identity, doc []byte, all bool) ([]byte, []string, error) {
	names, err := v.pendingFiles()
	if err != nil || len(names) == 0 {
		return doc, nil, err
	}
	if v.opts.Merge == nil {
		return nil, nil, ErrPendingUnmerged
	}
	pending, _, err := v.readPending(id, names)
	if err != nil {
		return nil, nil, err
	}
	defer clearPending(pending)
	merged, done, err := v.opts.Merge(slices.Clone(doc), pending)
	if err != nil {
		return nil, nil, err
	}
	for _, name := range done {
		if !slices.Contains(names, name) {
			return nil, nil, fmt.Errorf("vault: the merge reported %q, which is not a pending entry", name)
		}
	}
	done = slices.Clone(done)
	slices.Sort(done)
	done = slices.Compact(done)
	if all && len(done) != len(names) {
		return nil, nil, ErrPendingUnmerged
	}
	if len(done) == 0 {
		return doc, nil, nil
	}
	if err := checkSchema(v.documentPath(), merged, v.opts.Schemas); err != nil {
		return nil, nil, err
	}
	return slices.Clone(merged), done, nil
}

// mergePending merges like planMerge and, when it merged any entry, writes the merged document and only
// then removes the merged files, so a failed write loses nothing. It returns the document and how many
// entries were merged. The caller holds the mutex.
func (v *Vault) mergePending(id *identity, doc []byte, all bool) ([]byte, int, error) {
	merged, done, err := v.planMerge(id, doc, all)
	if err != nil || len(done) == 0 {
		return merged, 0, err
	}
	if err := v.writeDocument(id, merged); err != nil {
		clear(merged)
		return nil, 0, err
	}
	// A file left behind merges again on the next unlock, which a MergeFunc treats as a repeat.
	for _, name := range done {
		_ = v.removeEntry(os.Remove, filepath.Join(v.pendingPath(), name), false)
	}
	clear(doc)
	return merged, len(done), nil
}

// readPending reads and decrypts the pending entries names and returns each entry's file content beside
// it. On error no decrypted payload is left in memory. The caller holds the mutex and clears the payloads.
func (v *Vault) readPending(id *identity, names []string) ([]Pending, [][]byte, error) {
	pending := make([]Pending, 0, len(names))
	raw := make([][]byte, 0, len(names))
	for _, name := range names {
		path := filepath.Join(v.pendingPath(), name)
		data, err := v.readFile(path)
		if err != nil {
			clearPending(pending)
			return nil, nil, fmt.Errorf("cannot read %s: %w", path, err)
		}
		payload, err := decryptFrom(data, id.key)
		if err != nil {
			clearPending(pending)
			return nil, nil, decryptFailure(path, data, err)
		}
		pending = append(pending, Pending{Name: name, Payload: payload})
		raw = append(raw, data)
	}
	return pending, raw, nil
}

func clearPending(pending []Pending) {
	for _, p := range pending {
		clear(p.Payload)
	}
}

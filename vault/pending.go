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
	entries, err := os.ReadDir(v.pendingPath())
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
		text, err := readFile(v.recipientPath())
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
	if err := v.write(filepath.Join(v.pendingPath(), name), data); err != nil {
		return "", err
	}
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
	return removeAll(filepath.Join(v.pendingPath(), name))
}

// mergePending hands every pending entry to the MergeFunc and, when it merged any, writes the merged
// document and only then removes the merged files, so a failed write loses nothing. With all, an entry
// left pending is an error and nothing is written. It returns the document and how many entries were
// merged. The caller holds the mutex.
func (v *Vault) mergePending(id *identity, doc []byte, all bool) ([]byte, int, error) {
	names, err := v.pendingFiles()
	if err != nil || len(names) == 0 {
		return doc, 0, err
	}
	if v.opts.Merge == nil {
		return nil, 0, ErrPendingUnmerged
	}
	pending := make([]Pending, 0, len(names))
	defer func() {
		for _, p := range pending {
			clear(p.Payload)
		}
	}()
	for _, name := range names {
		path := filepath.Join(v.pendingPath(), name)
		data, err := readFile(path)
		if err != nil {
			return nil, 0, fmt.Errorf("cannot read %s: %w", path, err)
		}
		payload, err := decryptFrom(data, id.key)
		if err != nil {
			return nil, 0, fmt.Errorf("cannot decrypt %s: %w", path, ErrDamaged)
		}
		pending = append(pending, Pending{Name: name, Payload: payload})
	}
	merged, done, err := v.opts.Merge(slices.Clone(doc), pending)
	if err != nil {
		return nil, 0, err
	}
	for _, name := range done {
		if !slices.Contains(names, name) {
			return nil, 0, fmt.Errorf("vault: the merge reported %q, which is not a pending entry", name)
		}
	}
	slices.Sort(done)
	done = slices.Compact(done)
	if all && len(done) != len(names) {
		return nil, 0, ErrPendingUnmerged
	}
	if len(done) == 0 {
		return doc, 0, nil
	}
	if err := checkSchema(merged, v.opts.Schemas); err != nil {
		return nil, 0, fmt.Errorf("the merged document: %w", err)
	}
	merged = slices.Clone(merged)
	if err := v.writeDocument(id, merged); err != nil {
		clear(merged)
		return nil, 0, err
	}
	// A file left behind merges again on the next unlock, which a MergeFunc treats as a repeat.
	for _, name := range done {
		_ = os.Remove(filepath.Join(v.pendingPath(), name))
	}
	clear(doc)
	return merged, len(done), nil
}

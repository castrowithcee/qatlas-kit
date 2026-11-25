package vault

import (
	"fmt"
	"os"

	"github.com/castrowithcee/qatlas-kit/filelock"
)

func (v *Vault) locking() bool { return v.opts.Files.Lock != "" }

// lockWrites takes the lock of Files.Lock and returns its release; without Files.Lock it does nothing. A
// write takes it once, at its outermost call, and reads what it changes only afterwards. The lock belongs to
// the open file, so a second open file of this process waits for the first: nothing called with the lock
// held takes it again. With create the vault directory is made first; otherwise a missing one is ErrAbsent,
// so a failed write leaves no directory behind. The caller holds the mutex.
func (v *Vault) lockWrites(create bool) (release func(), err error) {
	if !v.locking() {
		return func() {}, nil
	}
	if create {
		if err := mkdirPrivate(v.opts.Dir); err != nil {
			return nil, fmt.Errorf("cannot create %s: %w", v.opts.Dir, err)
		}
	}
	path := v.path(v.opts.Files.Lock)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		if !create && isNotExist(err) {
			return nil, ErrAbsent
		}
		return nil, fmt.Errorf("cannot open the lock file %s: %w", path, err)
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}

// checkHeld refuses with ErrSwitched once the files on disk no longer belong to the key this unlocked value
// holds, so a write never lands beside a plain document or encrypted to a key nobody holds any more. The
// caller holds the write lock.
func (v *Vault) checkHeld() error {
	if v.opts.Files.Plain != "" {
		if exists, err := v.fileExists(v.plainPath()); err != nil {
			return err
		} else if exists {
			return ErrSwitched
		}
	}
	if exists, err := v.fileExists(v.keyPath()); err != nil {
		return err
	} else if !exists {
		return ErrSwitched
	}
	text, err := v.readFile(v.recipientPath())
	if err != nil {
		if isNotExist(err) {
			return ErrSwitched
		}
		return fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	recipient, err := parseRecipient(text)
	if err != nil {
		return err
	}
	if recipient.String() != v.identity.recipient.String() {
		return ErrSwitched
	}
	return nil
}

// writeState is state for a write that holds the write lock: an unlocked value is checked against the files
// on disk with Files.Lock.
func (v *Vault) writeState() (State, error) {
	state, err := v.state()
	if err != nil {
		return "", err
	}
	if state == StateUnlocked && v.locking() {
		if err := v.checkHeld(); err != nil {
			return "", err
		}
	}
	return state, nil
}

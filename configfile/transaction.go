package configfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/castrowithcee/qatlas-kit/filelock"
	"github.com/castrowithcee/qatlas-kit/fileretry"
)

// Revision identifies the exact bytes read from a file. It is opaque to callers.
type Revision string

// RevisionAbsent identifies a file that did not exist when it was read.
const RevisionAbsent Revision = ""

// ErrConflict means that no write occurred because the file changed.
var ErrConflict = errors.New("configuration changed since it was read")

// ConflictError identifies the file whose revision no longer matches.
type ConflictError struct{ Path string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("configuration %s changed since it was read", e.Path)
}
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// Codec supplies document-specific validation and serialization. Encode must produce
// deterministic canonical bytes so a decoded hand edit can be recognized.
type Codec[T any] struct {
	Validate func(T) error
	Encode   func(T) ([]byte, error)
	Decode   func([]byte) (T, error)
}

// NotFoundError reports a file that does not exist. errors.Is(err, fs.ErrNotExist) is true for it.
type NotFoundError struct{ Path string }

func (e *NotFoundError) Error() string        { return fmt.Sprintf("%s does not exist", e.Path) }
func (e *NotFoundError) Is(target error) bool { return target == fs.ErrNotExist }

// Observer receives measurement events of one store. A nil member is ignored. Callbacks may run on any
// goroutine that uses the store.
type Observer struct {
	// FileRetry is called before each wait of a file retry with the length of the wait.
	FileRetry func(time.Duration)
	// LockWaited is called with how long taking the cross-process lock blocked, also when it failed.
	LockWaited func(time.Duration)
}

// Store holds a file path and its caller-supplied document codec.
type Store[T any] struct {
	path      string
	codec     Codec[T]
	observe   Observer
	now       func() time.Time
	transient func(error) bool
}

// NewStore creates a store for one file. The path is made absolute at construction.
func NewStore[T any](path string, codec Codec[T]) (*Store[T], error) {
	if path == "" || codec.Validate == nil || codec.Encode == nil || codec.Decode == nil {
		return nil, errors.New("configuration path and codec callbacks are required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &Store[T]{path: abs, codec: codec, now: time.Now, transient: fileretry.Transient}, nil
}

// Observed returns a store like s that reports its file retries and lock waits to obs. s is unchanged.
func (s *Store[T]) Observed(obs Observer) *Store[T] {
	out := *s
	out.observe = obs
	return &out
}

// Path returns the path supplied to this store, made absolute.
func (s *Store[T]) Path() string { return s.path }

func (s *Store[T]) sleep() func(time.Duration) {
	if s.observe.FileRetry == nil {
		return nil
	}
	return func(d time.Duration) {
		s.observe.FileRetry(d)
		time.Sleep(d)
	}
}

// retry repeats op while it fails transiently. A missing file is an answer, never transient.
func (s *Store[T]) retry(op func() error) error {
	return fileretry.Do(op, fileretry.Options{Transient: func(err error) bool {
		return !errors.Is(err, os.ErrNotExist) && s.transient(err)
	}, Sleep: s.sleep()})
}

func revisionOf(data []byte) Revision {
	sum := sha256.Sum256(data)
	return Revision(hex.EncodeToString(sum[:]))
}

// resolvedTarget fixes the target used for reading, locking, and replacing.
// A dangling final symlink is an error rather than a new file to create.
func (s *Store[T]) resolvedTarget() (string, error) {
	dir := filepath.Dir(s.path)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			dir = resolved
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		_, statErr := os.Lstat(dir)
		if statErr == nil {
			return "", err // A dangling directory symlink cannot be created through.
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		missing = append(missing, filepath.Base(dir))
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", err
		}
		dir = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		dir = filepath.Join(dir, missing[i])
	}
	target := filepath.Join(dir, filepath.Base(s.path))
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return target, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return filepath.EvalSymlinks(target)
	}
	return target, nil
}

func (s *Store[T]) readFile(path string) ([]byte, error) {
	var data []byte
	err := s.retry(func() (err error) {
		data, err = os.ReadFile(path)
		return err
	})
	return data, err
}

func (s *Store[T]) readRevision(path string) ([]byte, Revision, error) {
	data, err := s.readFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, RevisionAbsent, nil
	}
	if err != nil {
		return nil, RevisionAbsent, err
	}
	return data, revisionOf(data), nil
}

// LoadVersioned reads without a lock and returns the SHA-256 revision of the bytes decoded.
// A missing file returns a *NotFoundError with RevisionAbsent. Decode errors keep the read revision.
func (s *Store[T]) LoadVersioned() (T, Revision, error) {
	var zero T
	target, err := s.resolvedTarget()
	if err != nil {
		return zero, RevisionAbsent, err
	}
	data, rev, err := s.readRevision(target)
	if err != nil {
		return zero, rev, err
	}
	if rev == RevisionAbsent {
		return zero, rev, &NotFoundError{Path: target}
	}
	value, err := s.codec.Decode(data)
	if err != nil {
		return zero, rev, fmt.Errorf("decode configuration %s: %w", target, err)
	}
	if err := s.codec.Validate(value); err != nil {
		return zero, rev, fmt.Errorf("invalid configuration %s: %w", target, err)
	}
	return value, rev, nil
}

func (s *Store[T]) lockTarget(target string) (func(), error) {
	if err := mkdirPrivate(filepath.Dir(target)); err != nil {
		return nil, err
	}
	path := target + ".lock"
	f, err := openLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open configuration lock %s: %w", path, err)
	}
	start := time.Now()
	err = filelock.Lock(f)
	if s.observe.LockWaited != nil {
		s.observe.LockWaited(time.Since(start))
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock configuration %s: %w", path, err)
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}

// saveLocked validates and encodes the caller's document before a guarded replacement.
func (s *Store[T]) saveLocked(target string, value T, expected Revision) (Revision, error) {
	data, err := prepare(value, s.codec.Validate, s.codec.Encode, s.codec.Decode)
	if err != nil {
		return RevisionAbsent, err
	}
	rev, _, err := s.saveDataLocked(target, data, expected)
	return rev, err
}

// saveDataLocked returns the new revision and the name of the backup of the replaced bytes, empty when
// there was no file to replace.
func (s *Store[T]) saveDataLocked(target string, data []byte, expected Revision) (Revision, string, error) {
	old, current, err := s.readRevision(target)
	if err != nil {
		return RevisionAbsent, "", err
	}
	if current != expected {
		return RevisionAbsent, "", &ConflictError{Path: target}
	}
	backup, err := s.backupCurrent(old)
	if err != nil {
		return RevisionAbsent, "", err
	}
	if err := s.replaceIfUnchanged(target, data, expected); err != nil {
		return RevisionAbsent, "", err
	}
	return revisionOf(data), backup, nil
}

// replaceIfUnchanged checks the revision again right before the rename, because a writer that bypasses
// the lock may have replaced the file meanwhile.
func (s *Store[T]) replaceIfUnchanged(target string, data []byte, expected Revision) error {
	unchanged := func() error {
		_, latest, err := s.readRevision(target)
		if err != nil {
			return err
		}
		if latest != expected {
			return &ConflictError{Path: target}
		}
		return nil
	}
	if err := unchanged(); err != nil {
		return err
	}
	return replaceResolved(target, data, unchanged, s.retry)
}

// SaveIfUnchanged writes only if the file still has expected's revision.
// The check and replacement are under the stable cross-process lock next to the resolved target.
// Codec callbacks must not call this store's write methods: the lock is not reentrant.
func (s *Store[T]) SaveIfUnchanged(value T, expected Revision) (Revision, error) {
	target, err := s.resolvedTarget()
	if err != nil {
		return RevisionAbsent, err
	}
	unlock, err := s.lockTarget(target)
	if err != nil {
		return RevisionAbsent, err
	}
	defer unlock()
	return s.saveLocked(target, value, expected)
}

// Update reads, changes, and saves a document under one cross-process lock.
// If the file is absent, change receives the zero value of T. A malformed file is never overwritten.
// change and codec callbacks must not call this store's write methods: the lock is not reentrant.
func (s *Store[T]) Update(change func(T) (T, error)) (Revision, error) {
	if change == nil {
		return RevisionAbsent, errors.New("missing configuration change callback")
	}
	target, err := s.resolvedTarget()
	if err != nil {
		return RevisionAbsent, err
	}
	unlock, err := s.lockTarget(target)
	if err != nil {
		return RevisionAbsent, err
	}
	defer unlock()
	data, expected, err := s.readRevision(target)
	if err != nil {
		return RevisionAbsent, err
	}
	var value T
	if expected != RevisionAbsent {
		value, err = s.codec.Decode(data)
		if err != nil {
			return RevisionAbsent, fmt.Errorf("decode configuration %s: %w", target, err)
		}
		if err := s.codec.Validate(value); err != nil {
			return RevisionAbsent, fmt.Errorf("invalid configuration %s: %w", target, err)
		}
	}
	value, err = change(value)
	if err != nil {
		return RevisionAbsent, err
	}
	return s.saveLocked(target, value, expected)
}

// RevisionOf returns the revision of the bytes a save writes for value, without reading the file. A caller
// that just saved value and keeps it as its state uses this as the base of its next change instead of
// reading the file again, which could pair a newer file with an older state.
func (s *Store[T]) RevisionOf(value T) (Revision, error) {
	data, err := prepare(value, s.codec.Validate, s.codec.Encode, s.codec.Decode)
	if err != nil {
		return RevisionAbsent, err
	}
	return revisionOf(data), nil
}

// Snapshot returns the bytes of the file as they are now and their revision, without decoding them.
// A missing file returns a *NotFoundError.
func (s *Store[T]) Snapshot() ([]byte, Revision, error) {
	target, err := s.resolvedTarget()
	if err != nil {
		return nil, RevisionAbsent, err
	}
	data, rev, err := s.readRevision(target)
	if err != nil {
		return nil, RevisionAbsent, err
	}
	if rev == RevisionAbsent {
		return nil, RevisionAbsent, &NotFoundError{Path: target}
	}
	return data, rev, nil
}

// Transact runs work under the cross-process lock if the file still has expected's revision, for a change
// that must do something else before it saves and must not do it unless its base is still current.
// A conflict is reported before work runs. save validates, backs up, and writes like SaveIfUnchanged
// against expected, so a second save in one transaction conflicts with the first. If save fails, nothing
// was written and work can undo its own steps before it returns. save fails once Transact has returned.
// work must be short and must not call this store's write methods: the lock is not reentrant.
// Whatever work returns is returned unchanged.
func (s *Store[T]) Transact(expected Revision, work func(save func(T) (Revision, error)) error) error {
	if work == nil {
		return errors.New("missing configuration transaction callback")
	}
	target, err := s.resolvedTarget()
	if err != nil {
		return err
	}
	unlock, err := s.lockTarget(target)
	if err != nil {
		return err
	}
	defer unlock()
	_, current, err := s.readRevision(target)
	if err != nil {
		return err
	}
	if current != expected {
		return &ConflictError{Path: target}
	}
	var ended atomic.Bool
	defer ended.Store(true)
	return work(func(value T) (Revision, error) {
		if ended.Load() {
			return RevisionAbsent, errors.New("configuration transaction has ended")
		}
		return s.saveLocked(target, value, expected)
	})
}

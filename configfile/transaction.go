package configfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// Store holds a file path and its caller-supplied document codec.
type Store[T any] struct {
	path  string
	codec Codec[T]
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
	return &Store[T]{path: abs, codec: codec}, nil
}

// Path returns the path supplied to this store, made absolute.
func (s *Store[T]) Path() string { return s.path }

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

func readRevision(path string) ([]byte, Revision, error) {
	var data []byte
	err := fileretry.Do(func() (err error) {
		data, err = os.ReadFile(path)
		return err
	}, func(err error) bool { return !errors.Is(err, os.ErrNotExist) && fileretry.Transient(err) }, nil)
	if errors.Is(err, os.ErrNotExist) {
		return nil, RevisionAbsent, nil
	}
	if err != nil {
		return nil, RevisionAbsent, err
	}
	return data, revisionOf(data), nil
}

// LoadVersioned reads without a lock and returns the SHA-256 revision of the bytes decoded.
// A missing file returns os.ErrNotExist with RevisionAbsent. Decode errors keep the read revision.
func (s *Store[T]) LoadVersioned() (T, Revision, error) {
	var zero T
	target, err := s.resolvedTarget()
	if err != nil {
		return zero, RevisionAbsent, err
	}
	data, rev, err := readRevision(target)
	if err != nil {
		return zero, rev, err
	}
	if rev == RevisionAbsent {
		return zero, rev, os.ErrNotExist
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

func lockTarget(target string) (func(), error) {
	if err := mkdirPrivate(filepath.Dir(target)); err != nil {
		return nil, err
	}
	path := target + ".lock"
	f, err := openLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open configuration lock %s: %w", path, err)
	}
	if err := filelock.Lock(f); err != nil {
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
	return s.saveDataLocked(target, data, expected)
}

func (s *Store[T]) saveDataLocked(target string, data []byte, expected Revision) (Revision, error) {
	old, current, err := readRevision(target)
	if err != nil {
		return RevisionAbsent, err
	}
	if current != expected {
		return RevisionAbsent, &ConflictError{Path: target}
	}
	if err := s.backupCurrent(old); err != nil {
		return RevisionAbsent, err
	}
	_, current, err = readRevision(target)
	if err != nil {
		return RevisionAbsent, err
	}
	if current != expected {
		return RevisionAbsent, &ConflictError{Path: target}
	}
	if err := replaceResolved(target, data, func() error {
		_, latest, err := readRevision(target)
		if err != nil {
			return err
		}
		if latest != expected {
			return &ConflictError{Path: target}
		}
		return nil
	}); err != nil {
		return RevisionAbsent, err
	}
	return revisionOf(data), nil
}

// SaveIfUnchanged writes only if the file still has expected's revision.
// The check and replacement are under the stable cross-process lock next to the resolved target.
// Codec callbacks must not call this store's write methods: the lock is not reentrant.
func (s *Store[T]) SaveIfUnchanged(value T, expected Revision) (Revision, error) {
	target, err := s.resolvedTarget()
	if err != nil {
		return RevisionAbsent, err
	}
	unlock, err := lockTarget(target)
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
	unlock, err := lockTarget(target)
	if err != nil {
		return RevisionAbsent, err
	}
	defer unlock()
	data, expected, err := readRevision(target)
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

package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	backupDirName = "backups"
	keepBackups   = 10
	stampLayout   = "20060102T150405Z"
	// copyMarker keeps copies out of the regular backup set even when the source has the store's file name.
	copyMarker = "copy-"
)

// Backup identifies a byte-for-byte copy of a replaced configuration.
type Backup struct {
	Name      string
	Time      time.Time
	FirstHand bool
	seq       int
}

func (s *Store[T]) backupDir() string { return filepath.Join(filepath.Dir(s.path), backupDirName) }

func (s *Store[T]) backupParts(name string) (Backup, bool) {
	base := filepath.Base(s.path) + "-"
	if !strings.HasPrefix(name, base) || !strings.HasSuffix(name, ".bak") {
		return Backup{}, false
	}
	part := strings.TrimSuffix(strings.TrimPrefix(name, base), ".bak")
	firstHand := strings.HasPrefix(part, "first-hand-")
	if firstHand {
		part = strings.TrimPrefix(part, "first-hand-")
	}
	stamp, seqText, ok := strings.Cut(part, "-")
	if !ok || len(seqText) != 6 {
		return Backup{}, false
	}
	when, err := time.Parse(stampLayout, stamp)
	if err != nil || when.UTC().Format(stampLayout) != stamp {
		return Backup{}, false
	}
	seq, err := strconv.Atoi(seqText)
	if err != nil || fmt.Sprintf("%06d", seq) != seqText {
		return Backup{}, false
	}
	return Backup{Name: name, Time: when, FirstHand: firstHand, seq: seq}, true
}

// Backups lists regular backups of this store, newest first.
func (s *Store[T]) Backups() ([]Backup, error) {
	info, err := os.Lstat(s.backupDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect configuration backup directory: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("configuration backup path is not a directory")
	}
	entries, err := os.ReadDir(s.backupDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read configuration backups: %w", err)
	}
	var backups []Backup
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if backup, ok := s.backupParts(entry.Name()); ok {
			backups = append(backups, backup)
		}
	}
	sort.Slice(backups, func(i, j int) bool {
		if !backups[i].Time.Equal(backups[j].Time) {
			return backups[i].Time.After(backups[j].Time)
		}
		return backups[i].seq > backups[j].seq
	})
	return backups, nil
}

// ReadBackup reads one named backup. Names outside this store's regular backup set are rejected.
func (s *Store[T]) ReadBackup(name string) ([]byte, error) {
	backups, err := s.Backups()
	if err != nil {
		return nil, err
	}
	for _, backup := range backups {
		if backup.Name == name {
			data, err := s.readFile(filepath.Join(s.backupDir(), name))
			if err != nil {
				return nil, fmt.Errorf("read configuration backup: %w", err)
			}
			return data, nil
		}
	}
	return nil, &NotFoundError{Path: name}
}

func (s *Store[T]) isHandVersion(data []byte) bool {
	value, err := s.codec.Decode(data)
	if err != nil {
		return true
	}
	canonical, err := s.codec.Encode(value)
	return err != nil || !bytes.Equal(data, canonical)
}

// backupCurrent stores data as a new regular backup and returns its name, or "" when data is nil.
func (s *Store[T]) backupCurrent(data []byte) (string, error) {
	if data == nil {
		return "", nil
	}
	if err := s.ensureBackupDir(); err != nil {
		return "", err
	}
	backups, err := s.Backups()
	if err != nil {
		return "", err
	}
	firstHand := false
	if s.isHandVersion(data) {
		firstHand = true
		for _, backup := range backups {
			if backup.FirstHand {
				firstHand = false
				break
			}
		}
	}
	stamp := s.now().UTC().Format(stampLayout)
	seq := 0
	for _, backup := range backups {
		if backup.Time.UTC().Format(stampLayout) == stamp && backup.seq >= seq {
			seq = backup.seq + 1
		}
	}
	prefix := filepath.Base(s.path) + "-"
	if firstHand {
		prefix += "first-hand-"
	}
	name, err := s.writeBackupFile(prefix+stamp+"-", seq, data)
	if err != nil {
		return "", err
	}
	s.rotateBackups()
	return name, nil
}

func (s *Store[T]) ensureBackupDir() error {
	dir := s.backupDir()
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := mkdirPrivate(dir); err != nil {
			return fmt.Errorf("create configuration backup directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect configuration backup directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("configuration backup path is not a directory")
	}
	return nil
}

// writeBackupFile creates <prefix><six-digit sequence>.bak exclusively from seq upwards, so a backup never
// overwrites another one, and returns its name.
func (s *Store[T]) writeBackupFile(prefix string, seq int, data []byte) (string, error) {
	dir := s.backupDir()
	for ; seq < 1000000; seq++ {
		name := fmt.Sprintf("%s%06d.bak", prefix, seq)
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create configuration backup: %w", err)
		}
		if err = restrictFile(path); err == nil {
			_, err = f.Write(data)
		}
		if err == nil {
			err = f.Sync()
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("write configuration backup: %w", err)
		}
		return name, nil
	}
	return "", errors.New("configuration backup sequence exhausted")
}

// BackupCopy stores a byte-for-byte copy of src in this store's backup directory with the private
// permissions of a backup and returns the copy's path. It is meant for files a change must be able to roll
// back, including this store's own file before a migration. Copies are not part of Backups and never
// rotate, so a rotation cannot evict the only copy of such a file. It takes no lock and may run inside
// Transact.
func (s *Store[T]) BackupCopy(src string) (string, error) {
	data, err := s.readFile(src)
	if errors.Is(err, os.ErrNotExist) {
		return "", &NotFoundError{Path: src}
	}
	if err != nil {
		return "", fmt.Errorf("read %s for backup: %w", src, err)
	}
	if err := s.ensureBackupDir(); err != nil {
		return "", err
	}
	name, err := s.writeBackupFile(filepath.Base(src)+"-"+copyMarker+s.now().UTC().Format(stampLayout)+"-", 0, data)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.backupDir(), name), nil
}

func (s *Store[T]) rotateBackups() {
	backups, err := s.Backups()
	if err != nil {
		return
	}
	ordinary := 0
	for _, backup := range backups {
		if backup.FirstHand {
			continue
		}
		ordinary++
		if ordinary > keepBackups {
			_ = os.Remove(filepath.Join(s.backupDir(), backup.Name))
		}
	}
}

// Restored records what RestoreBackup replaced, for UndoRestore.
type Restored struct {
	// Revision is the revision of the restored bytes.
	Revision Revision
	// Previous names the backup RestoreBackup made of the bytes it replaced, empty when there was no file.
	Previous string
}

// RestoreBackup restores the exact bytes of a named backup under the store lock.
// The current file is backed up first, and expected must match its current revision.
// Backup bytes are not decoded, so an older schema can be recovered.
func (s *Store[T]) RestoreBackup(name string, expected Revision) (Restored, error) {
	target, err := s.resolvedTarget()
	if err != nil {
		return Restored{}, err
	}
	unlock, err := s.lockTarget(target)
	if err != nil {
		return Restored{}, err
	}
	defer unlock()
	data, err := s.ReadBackup(name)
	if err != nil {
		return Restored{}, err
	}
	rev, previous, err := s.saveDataLocked(target, data, expected)
	if err != nil {
		return Restored{}, err
	}
	return Restored{Revision: rev, Previous: previous}, nil
}

// UndoRestore puts back the bytes a RestoreBackup replaced, from the backup it made of them, or removes the
// file when the restore created it. It writes only while the file still holds the restored bytes, so a
// later change is never lost, and makes no further backup because the restored bytes are one already.
func (s *Store[T]) UndoRestore(r Restored) error {
	if r.Revision == RevisionAbsent {
		return errors.New("configuration restore result is empty")
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
	if r.Previous == "" {
		_, current, err := s.readRevision(target)
		if err != nil {
			return err
		}
		if current != r.Revision {
			return &ConflictError{Path: target}
		}
		if err := s.retry(func() error { return os.Remove(target) }); err != nil {
			return fmt.Errorf("remove restored configuration: %w", err)
		}
		return nil
	}
	data, err := s.ReadBackup(r.Previous)
	if err != nil {
		return err
	}
	return s.replaceIfUnchanged(target, data, r.Revision)
}

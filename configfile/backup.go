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
)

var backupNow = time.Now

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
	when, err := time.Parse("20060102T150405Z", stamp)
	if err != nil || when.UTC().Format("20060102T150405Z") != stamp {
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
			data, err := os.ReadFile(filepath.Join(s.backupDir(), name))
			if err != nil {
				return nil, fmt.Errorf("read configuration backup: %w", err)
			}
			return data, nil
		}
	}
	return nil, os.ErrNotExist
}

func (s *Store[T]) isHandVersion(data []byte) bool {
	value, err := s.codec.Decode(data)
	if err != nil {
		return true
	}
	canonical, err := s.codec.Encode(value)
	return err != nil || !bytes.Equal(data, canonical)
}

func (s *Store[T]) backupCurrent(data []byte) error {
	if data == nil {
		return nil
	}
	dir := s.backupDir()
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := mkdirPrivate(dir); err != nil {
			return fmt.Errorf("create configuration backup directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect configuration backup directory: %w", err)
	} else if !info.IsDir() {
		return errors.New("configuration backup path is not a directory")
	}
	backups, err := s.Backups()
	if err != nil {
		return err
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
	stamp := backupNow().UTC().Format("20060102T150405Z")
	seq := 0
	for _, backup := range backups {
		if backup.Time.UTC().Format("20060102T150405Z") == stamp && backup.seq >= seq {
			seq = backup.seq + 1
		}
	}
	prefix := filepath.Base(s.path) + "-"
	if firstHand {
		prefix += "first-hand-"
	}
	for ; seq < 1000000; seq++ {
		name := fmt.Sprintf("%s%s-%06d.bak", prefix, stamp, seq)
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("create configuration backup: %w", err)
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
			return fmt.Errorf("write configuration backup: %w", err)
		}
		s.rotateBackups()
		return nil
	}
	return errors.New("configuration backup sequence exhausted")
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

// RestoreBackup restores the exact bytes of a named backup under the store lock.
// The current file is backed up first, and expected must match its current revision.
// Backup bytes are not decoded, so an older schema can be recovered.
func (s *Store[T]) RestoreBackup(name string, expected Revision) (Revision, error) {
	target, err := s.resolvedTarget()
	if err != nil {
		return RevisionAbsent, err
	}
	unlock, err := lockTarget(target)
	if err != nil {
		return RevisionAbsent, err
	}
	defer unlock()
	data, err := s.ReadBackup(name)
	if err != nil {
		return RevisionAbsent, err
	}
	return s.saveDataLocked(target, data, expected)
}

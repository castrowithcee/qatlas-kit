package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

const (
	backupDirName = "backups"
	keepBackups   = 10
	// The fixed-width stamp makes the name order the time order.
	stampLayout = "20060102T150405.000000000Z"
)

// Backup writes a consistent copy of the database to dst and returns once it is complete. dst must not
// exist; its directory is created private if missing, and the copy is private to the current user.
func (d *DB) Backup(dst string) error {
	if dst == "" {
		return newError(output.CodeInvalidRequest, "the backup path is empty", nil)
	}
	abs, err := filepath.Abs(dst)
	if err != nil {
		return wrap("resolve the backup path", err)
	}
	if err := backupTo(context.Background(), d.db, abs); err != nil {
		return wrap("back up "+d.subject, err)
	}
	return nil
}

// backupTo copies the database with VACUUM INTO, which reads one consistent snapshot while other
// connections keep writing. The target is created private and empty first, because the statement would
// otherwise create it with the process umask. abs must be absolute, so the driver never reads it as a URI.
func backupTo(ctx context.Context, db *sql.DB, abs string) error {
	if err := mkdirPrivate(filepath.Dir(abs)); err != nil {
		return err
	}
	f, err := createPrivate(abs)
	if errors.Is(err, os.ErrExist) {
		return newError(output.CodeInvalidRequest, "the backup path already exists", err)
	}
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(abs)
		return err
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", abs); err != nil {
		_ = os.Remove(abs)
		return err
	}
	return nil
}

// backupBeforeMigration writes <name>.<stamp>.db into dir and keeps the ten newest of them.
func backupBeforeMigration(ctx context.Context, db *sql.DB, dbPath, dir string) error {
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		return errors.New("the backup path is not a directory")
	}
	base := filepath.Base(dbPath)
	name := base + "." + time.Now().UTC().Format(stampLayout) + ".db"
	if err := backupTo(ctx, db, filepath.Join(dir, name)); err != nil {
		return err
	}
	rotateBackups(dir, base)
	return nil
}

// rotateBackups is best effort: a backup that cannot be removed stays and never fails the migration.
func rotateBackups(dir, base string) {
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(base) + `\.[0-9]{8}T[0-9]{6}\.[0-9]{9}Z\.db$`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && pattern.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keepBackups {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

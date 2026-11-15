package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-kit/filelock"
	"github.com/castrowithcee/qatlas-kit/output"

	// The driver registers itself as "sqlite" with database/sql.
	_ "modernc.org/sqlite"
)

// busyTimeoutMillis is how long a connection waits for another writer before it reports SQLITE_BUSY.
const busyTimeoutMillis = 5000

// Options configures Open. The zero value opens a database without migrations and keeps backups beside it.
type Options struct {
	// Migrations holds the numbered scripts NNNN_name.sql in its root, numbered from 0001 without gap.
	// Nil means the program knows no migration, so a database that has applied one is refused.
	Migrations fs.FS
	// BackupDir receives the backup taken before a migration. Empty means "backups" beside the database.
	BackupDir string
	// Label names the database in error messages, for example "history". Empty reads "database".
	Label string
}

// DB is an open database. It is safe for concurrent use.
type DB struct {
	db      *sql.DB
	subject string
}

// Open creates the database if needed, applies the pending migrations and returns it.
func Open(path string, opts Options) (*DB, error) {
	subject := "the database"
	if opts.Label != "" {
		subject = "the " + opts.Label + " database"
	}
	if path == "" || path == ":memory:" {
		return nil, newError(output.CodeInvalidRequest, "the path of "+subject+" must be a file path", nil)
	}
	known, err := loadMigrations(opts.Migrations)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, wrap("resolve the path of "+subject, err)
	}
	backupDir := opts.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(abs), backupDirName)
	}
	if backupDir, err = filepath.Abs(backupDir); err != nil {
		return nil, wrap("resolve the backup directory of "+subject, err)
	}
	if err := prepareFile(abs); err != nil {
		return nil, wrap("prepare "+subject, err)
	}
	// The lock covers the first connection too: switching a new file to write-ahead logging needs the file
	// to itself, and a second process doing the same at that moment fails with SQLITE_BUSY.
	unlock, err := lockBeside(abs)
	if err != nil {
		return nil, wrap("lock "+subject, err)
	}
	defer unlock()
	db, err := sql.Open("sqlite", dsn(abs))
	if err != nil {
		return nil, wrap("open "+subject, err)
	}
	d := &DB{db: db, subject: subject}
	if err := d.setup(abs, backupDir, known); err != nil {
		_ = db.Close()
		return nil, err
	}
	return d, nil
}

// prepareFile makes the directory private and creates the file private when it is missing. An existing
// file keeps its permissions.
func prepareFile(path string) error {
	if err := mkdirPrivate(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := createPrivate(path)
	if err == nil {
		return f.Close()
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("the path is not a regular file")
	}
	return nil
}

// dsn builds a file URI. The pragmas go into it rather than into Exec calls because busy_timeout and
// foreign_keys belong to a connection, and the pool opens more of them.
func dsn(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(p)
	q := url.Values{}
	for _, pragma := range []string{
		"busy_timeout(" + strconv.Itoa(busyTimeoutMillis) + ")",
		"journal_mode(WAL)",
		"foreign_keys(1)",
		"synchronous(NORMAL)",
	} {
		q.Add("_pragma", pragma)
	}
	return "file://" + p + "?" + q.Encode()
}

func (d *DB) setup(path, backupDir string, known []migration) error {
	ctx := context.Background()
	var mode string
	if err := d.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return wrap("open "+d.subject, err)
	}
	if !strings.EqualFold(mode, "wal") {
		return newError(output.CodeRuntime, "open "+d.subject+": the file system does not support write-ahead logging", nil)
	}
	applied, err := appliedVersion(ctx, d.db, d.subject, known)
	if err != nil {
		return err
	}
	if applied == len(known) {
		return nil
	}
	return d.migrate(ctx, path, backupDir, known, applied)
}

// migrate runs under the lock Open holds, so processes of one program version migrate one after the other
// and the later one finds nothing left to do.
func (d *DB) migrate(ctx context.Context, path, backupDir string, known []migration, applied int) error {
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return wrap("migrate "+d.subject, err)
	}
	defer conn.Close()
	// A database without any schema object has nothing to lose, so only an existing one is backed up.
	data, err := hasSchema(ctx, conn, d.subject)
	if err != nil {
		return err
	}
	if data {
		if err := backupBeforeMigration(ctx, d.db, path, backupDir); err != nil {
			return wrap("back up "+d.subject+" before the migration", err)
		}
	}
	for _, m := range known[applied:] {
		if err := applyMigration(ctx, conn, d.subject, m); err != nil {
			return err
		}
	}
	return nil
}

// lockBeside takes the exclusive lock on <path>.lock. The lock file is stable and never removed.
func lockBeside(path string) (func(), error) {
	lockPath := path + ".lock"
	f, err := createPrivate(lockPath)
	if errors.Is(err, os.ErrExist) {
		f, err = os.OpenFile(lockPath, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("the lock path is not a regular file")
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}

// SQL returns the database/sql handle. Closing it directly bypasses Close.
func (d *DB) SQL() *sql.DB { return d.db }

// Checkpoint folds the write-ahead log into the database file and truncates it. It fails when another
// connection holds the log long enough to keep the checkpoint from finishing.
func (d *DB) Checkpoint() error {
	var busy, logFrames, checkpointed int
	err := d.db.QueryRowContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)").
		Scan(&busy, &logFrames, &checkpointed)
	if err != nil {
		return wrap("checkpoint "+d.subject, err)
	}
	if busy != 0 {
		return newError(output.CodeTimeout, "checkpoint "+d.subject+": another connection keeps the log in use", nil)
	}
	return nil
}

// Close closes every connection. SQLite then folds the log into the file and removes it.
func (d *DB) Close() error {
	if err := d.db.Close(); err != nil {
		return wrap("close "+d.subject, err)
	}
	return nil
}

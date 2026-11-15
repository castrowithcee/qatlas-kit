package sqlite

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/castrowithcee/qatlas-kit/output"
)

func migrations(files ...string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for i := 0; i+1 < len(files); i += 2 {
		fsys[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
	}
	return fsys
}

var (
	v1 = []string{"0001_items.sql", "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);"}
	v2 = []string{"0002_extra.sql", "ALTER TABLE items ADD COLUMN note TEXT; INSERT INTO items (name) VALUES ('from-v2');"}
)

func open(t *testing.T, path string, opts Options) *DB {
	t.Helper()
	db, err := Open(path, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func wantCode(t *testing.T, err error, code output.Code) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not an *Error", err)
	}
	if e.Code != code {
		t.Fatalf("code = %q, want %q (%v)", e.Code, code, err)
	}
}

func count(t *testing.T, db *DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.SQL().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func backupFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestOpenCreatesPrivateFilesAndSetsPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "data.db")
	db := open(t, path, Options{Migrations: migrations(v1...)})
	if runtime.GOOS != "windows" {
		for _, p := range []string{filepath.Dir(path), path} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			want := os.FileMode(0o600)
			if info.IsDir() {
				want = 0o700
			}
			if info.Mode().Perm() != want {
				t.Errorf("%s mode = %v, want %v", p, info.Mode().Perm(), want)
			}
		}
		if _, err := db.SQL().Exec("INSERT INTO items (name) VALUES ('x')"); err != nil {
			t.Fatal(err)
		}
		for _, side := range []string{"-wal", "-shm"} {
			if info, err := os.Stat(path + side); err == nil && info.Mode().Perm() != 0o600 {
				t.Errorf("%s mode = %v, want 0600", side, info.Mode().Perm())
			}
		}
	}
	// Two connections held at once prove the pragmas belong to every pooled connection.
	var conns []*sql.Conn
	for i := 0; i < 2; i++ {
		c, err := db.SQL().Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conns = append(conns, c)
	}
	for _, c := range conns {
		for pragma, want := range map[string]string{
			"journal_mode": "wal", "foreign_keys": "1", "synchronous": "1", "busy_timeout": "5000",
		} {
			var got string
			if err := c.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s = %s, want %s", pragma, got, want)
			}
		}
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "d.db"), Options{Migrations: migrations(
		"0001_fk.sql", "CREATE TABLE a (id INTEGER PRIMARY KEY); CREATE TABLE b (a_id INTEGER REFERENCES a(id));")})
	if _, err := db.SQL().Exec("INSERT INTO b VALUES (42)"); err == nil {
		t.Fatal("insert violating a foreign key succeeded")
	}
}

func TestOpenAppliesMigrationsOnceAndNeedsNoBackupForNewDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.db")
	opts := Options{Migrations: migrations(append(append([]string{}, v1...), v2...)...)}
	db := open(t, path, opts)
	if n := count(t, db, "SELECT count(*) FROM schema_migrations"); n != 2 {
		t.Fatalf("recorded migrations = %d, want 2", n)
	}
	if n := count(t, db, "SELECT count(*) FROM items"); n != 1 {
		t.Fatalf("items = %d, want 1", n)
	}
	_ = db.Close()
	db = open(t, path, opts)
	if n := count(t, db, "SELECT count(*) FROM items"); n != 1 {
		t.Fatalf("items after reopen = %d, want 1", n)
	}
	if names := backupFiles(t, filepath.Join(dir, "backups")); len(names) != 0 {
		t.Fatalf("backups = %v, want none", names)
	}
}

func TestMigrationOfExistingDatabaseBacksUpFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.db")
	db := open(t, path, Options{Migrations: migrations(v1...)})
	if _, err := db.SQL().Exec("INSERT INTO items (name) VALUES ('old')"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	backups := filepath.Join(dir, "elsewhere")
	db = open(t, path, Options{Migrations: migrations(append(append([]string{}, v1...), v2...)...), BackupDir: backups})
	names := backupFiles(t, backups)
	if len(names) != 1 || !strings.HasPrefix(names[0], "d.db.") || !strings.HasSuffix(names[0], ".db") {
		t.Fatalf("backups = %v", names)
	}
	copyPath := filepath.Join(backups, names[0])
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(copyPath); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("backup mode = %v, %v", info.Mode().Perm(), err)
		}
	}
	raw, err := sql.Open("sqlite", copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var items, versions, hasNote int
	if err := raw.QueryRow("SELECT count(*) FROM items").Scan(&items); err != nil || items != 1 {
		t.Fatalf("backup items = %d, %v", items, err)
	}
	if err := raw.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("backup versions = %d, %v", versions, err)
	}
	if err := raw.QueryRow("SELECT count(*) FROM pragma_table_info('items') WHERE name = 'note'").Scan(&hasNote); err != nil || hasNote != 0 {
		t.Fatalf("backup already has the migrated column: %d, %v", hasNote, err)
	}
	if n := count(t, db, "SELECT count(*) FROM schema_migrations"); n != 2 {
		t.Fatalf("recorded migrations = %d, want 2", n)
	}
}

func TestNoBackupWithoutPendingMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.db")
	opts := Options{Migrations: migrations(v1...)}
	_ = open(t, path, opts).Close()
	_ = open(t, path, opts).Close()
	if names := backupFiles(t, filepath.Join(dir, "backups")); len(names) != 0 {
		t.Fatalf("backups = %v, want none", names)
	}
}

func TestNewerSchemaIsRefusedWithoutChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.db")
	_ = open(t, path, Options{Migrations: migrations(append(append([]string{}, v1...), v2...)...)}).Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, opts := range map[string]Options{
		"older program": {Migrations: migrations(v1...)},
		"no migrations": {},
	} {
		t.Run(name, func(t *testing.T) {
			db, err := Open(path, opts)
			if err == nil {
				_ = db.Close()
				t.Fatal("Open accepted a newer schema")
			}
			wantCode(t, err, output.CodeConfigInvalid)
		})
	}
	if names := backupFiles(t, filepath.Join(dir, "backups")); len(names) != 0 {
		t.Fatalf("backups = %v, want none", names)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// WAL mode keeps the main file stable while connections read, so a refused Open leaves it identical.
	if len(before) != len(after) {
		t.Fatalf("database file size changed from %d to %d", len(before), len(after))
	}
}

func TestRenamedMigrationIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	_ = open(t, path, Options{Migrations: migrations(v1...)}).Close()
	db, err := Open(path, Options{Migrations: migrations("0001_other.sql", "CREATE TABLE z (a);")})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open accepted a renamed migration")
	}
	wantCode(t, err, output.CodeConfigInvalid)
}

func TestGapInRecordIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	opts := Options{Migrations: migrations(append(append([]string{}, v1...), v2...)...)}
	db := open(t, path, opts)
	if _, err := db.SQL().Exec("DELETE FROM schema_migrations WHERE version = 1"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if db, err := Open(path, opts); err == nil {
		_ = db.Close()
		t.Fatal("Open accepted a gap in the migration record")
	} else {
		wantCode(t, err, output.CodeConfigInvalid)
	}
}

func TestInvalidMigrationSetsAreRefusedBeforeTouchingDisk(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"no leading zeros": migrations("1_a.sql", "SELECT 1;"),
		"upper case":       migrations("0001_A.sql", "SELECT 1;"),
		"wrong extension":  migrations("0001_a.txt", "SELECT 1;"),
		"no name":          migrations("0001.sql", "SELECT 1;"),
		"stray file":       migrations("0001_a.sql", "SELECT 1;", "README.md", "x"),
		"directory":        {"0001_a.sql/x": &fstest.MapFile{Data: []byte("x")}},
		"starts at two":    migrations("0002_a.sql", "SELECT 1;"),
		"zero":             migrations("0000_a.sql", "SELECT 1;"),
		"gap":              migrations("0001_a.sql", "SELECT 1;", "0003_b.sql", "SELECT 1;"),
		"duplicate":        migrations("0001_a.sql", "SELECT 1;", "0001_b.sql", "SELECT 1;"),
		"empty":            migrations("0001_a.sql", " \n"),
	}
	for name, fsys := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			db, err := Open(filepath.Join(dir, "d.db"), Options{Migrations: fsys})
			if err == nil {
				_ = db.Close()
				t.Fatal("Open accepted the migration set")
			}
			wantCode(t, err, output.CodeInvalidRequest)
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Open touched the disk: %v", err)
			}
		})
	}
}

func TestBrokenMigrationRollsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.db")
	broken := migrations(append(append([]string{}, v1...),
		"0002_broken.sql", "CREATE TABLE half (a); INSERT INTO missing VALUES (1);")...)
	db, err := Open(path, Options{Migrations: broken})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open accepted a broken migration")
	}
	wantCode(t, err, output.CodeRuntime)
	if !strings.Contains(err.Error(), "0002_broken.sql") {
		t.Errorf("error does not name the migration: %v", err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var version, half int
	if err := raw.QueryRow("SELECT max(version) FROM schema_migrations").Scan(&version); err != nil || version != 1 {
		t.Fatalf("version = %d, %v; want 1", version, err)
	}
	if err := raw.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'half'").Scan(&half); err != nil || half != 0 {
		t.Fatalf("the failed migration left its table behind: %d, %v", half, err)
	}

	fixed := open(t, path, Options{Migrations: migrations(append(append([]string{}, v1...), v2...)...)})
	if n := count(t, fixed, "SELECT max(version) FROM schema_migrations"); n != 2 {
		t.Fatalf("version after the fix = %d, want 2", n)
	}
	if names := backupFiles(t, filepath.Join(dir, "backups")); len(names) != 1 {
		t.Fatalf("backups = %v, want one from the fixed run", names)
	}
}

func TestConcurrentOpenMigratesOnce(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new database"
		if existing {
			name = "existing database"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "d.db")
			// Both scripts fail when run twice, so a double migration cannot go unnoticed.
			all := migrations(append(append([]string{}, v1...),
				"0002_more.sql", "CREATE TABLE more (a); INSERT INTO more VALUES (1);")...)
			if existing {
				_ = open(t, path, Options{Migrations: migrations(v1...)}).Close()
			}
			const workers = 8
			dbs := make([]*DB, workers)
			errs := make([]error, workers)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					dbs[i], errs[i] = Open(path, Options{Migrations: all})
				}()
			}
			close(start)
			wg.Wait()
			for i := range workers {
				if errs[i] != nil {
					t.Errorf("worker %d: %v", i, errs[i])
				}
				if dbs[i] != nil {
					defer dbs[i].Close()
				}
			}
			if t.Failed() {
				return
			}
			if n := count(t, dbs[0], "SELECT count(*) FROM more"); n != 1 {
				t.Fatalf("rows = %d, want 1", n)
			}
			wantBackups := 0
			if existing {
				wantBackups = 1
			}
			if names := backupFiles(t, filepath.Join(dir, "backups")); len(names) != wantBackups {
				t.Fatalf("backups = %v, want %d", names, wantBackups)
			}
		})
	}
}

func TestBackupRotationKeepsTenNewest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.db")
	_ = open(t, path, Options{Migrations: migrations(v1...)}).Close()
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	var old []string
	for i := 0; i < 12; i++ {
		name := "d.db.20200101T0000" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + ".000000000Z.db"
		old = append(old, name)
		if err := os.WriteFile(filepath.Join(backups, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(backups, "other.db.20200101T000000.000000000Z.db")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = open(t, path, Options{Migrations: migrations(append(append([]string{}, v1...), v2...)...)}).Close()
	names := backupFiles(t, backups)
	if len(names) != 11 { // ten of this database plus the unrelated file
		t.Fatalf("backups = %v", names)
	}
	for _, gone := range old[:3] {
		if _, err := os.Stat(filepath.Join(backups, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s was kept", gone)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("an unrelated file was rotated: %v", err)
	}
}

func TestBackupMethod(t *testing.T) {
	dir := t.TempDir()
	db := open(t, filepath.Join(dir, "d.db"), Options{Migrations: migrations(v1...), Label: "history"})
	if _, err := db.SQL().Exec("INSERT INTO items (name) VALUES ('kept')"); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "copy.db")
	if err := db.Backup(dst); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(dst); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("backup mode = %v, %v", info.Mode().Perm(), err)
		}
	}
	raw, err := sql.Open("sqlite", dst)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var n int
	if err := raw.QueryRow("SELECT count(*) FROM items WHERE name = 'kept'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("copy rows = %d, %v", n, err)
	}

	before, _ := os.ReadFile(dst)
	err = db.Backup(dst)
	wantCode(t, err, output.CodeInvalidRequest)
	after, _ := os.ReadFile(dst)
	if string(before) != string(after) {
		t.Error("Backup overwrote an existing file")
	}
	wantCode(t, db.Backup(""), output.CodeInvalidRequest)
	if err := db.Backup(filepath.Join(dir, "d.db")); err == nil {
		t.Error("Backup replaced the live database")
	}
}

func TestCheckpointAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	db := open(t, path, Options{Migrations: migrations(v1...)})
	for i := 0; i < 20; i++ {
		if _, err := db.SQL().Exec("INSERT INTO items (name) VALUES ('x')"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path + "-wal"); err == nil && info.Size() != 0 {
		t.Errorf("log size after checkpoint = %d", info.Size())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	err := db.Checkpoint()
	if err == nil {
		t.Fatal("Checkpoint on a closed database succeeded")
	}
	wantCode(t, err, output.CodeRuntime)
}

func TestOpenRejectsBadPaths(t *testing.T) {
	dir := t.TempDir()
	for name, path := range map[string]string{"empty": "", "memory": ":memory:", "directory": dir} {
		t.Run(name, func(t *testing.T) {
			db, err := Open(path, Options{})
			if err == nil {
				_ = db.Close()
				t.Fatal("Open accepted the path")
			}
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("error %v is not an *Error", err)
			}
		})
	}
}

func TestOpenRejectsFileThatIsNotADatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("this is not a database ", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path, Options{Migrations: migrations(v1...)})
	if err == nil {
		_ = db.Close()
		t.Fatal("Open accepted a text file")
	}
	wantCode(t, err, output.CodeConfigInvalid)
}

func TestOpenHandlesUriCharactersInPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a#b%c d.db")
	db := open(t, path, Options{Migrations: migrations(v1...)})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database is not at the requested path: %v", err)
	}
	_ = db
}

func TestLabelNamesDatabaseInErrors(t *testing.T) {
	_, err := Open("", Options{Label: "history"})
	if err == nil || !strings.Contains(err.Error(), "history") {
		t.Fatalf("error = %v", err)
	}
}

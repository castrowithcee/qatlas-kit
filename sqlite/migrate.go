package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

const migrationsTable = "schema_migrations"

var migrationFile = regexp.MustCompile(`^([0-9]{4})_([a-z0-9][a-z0-9_-]*)\.sql$`)

type migration struct {
	version int
	name    string
	file    string
	script  string
}

// loadMigrations reads and validates the whole set before anything touches the disk. Every entry of the
// root must be a migration file, and the numbers must run 1, 2, 3 without gap or duplicate.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	if fsys == nil {
		return nil, nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, newError(output.CodeInvalidRequest, "read the migrations: "+err.Error(), err)
	}
	var list []migration
	for _, entry := range entries {
		file := entry.Name()
		m := migrationFile.FindStringSubmatch(file)
		if m == nil || !entry.Type().IsRegular() {
			return nil, newError(output.CodeInvalidRequest,
				fmt.Sprintf("migration %q is not a regular file named NNNN_name.sql", file), nil)
		}
		version, _ := strconv.Atoi(m[1])
		if len(list) > 0 && version == list[len(list)-1].version {
			return nil, newError(output.CodeInvalidRequest,
				fmt.Sprintf("migrations %q and %q have the same number", list[len(list)-1].file, file), nil)
		}
		if want := len(list) + 1; version != want {
			return nil, newError(output.CodeInvalidRequest,
				fmt.Sprintf("migration %q breaks the numbering: expected number %04d", file, want), nil)
		}
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, newError(output.CodeInvalidRequest, fmt.Sprintf("read migration %q: %v", file, err), err)
		}
		script := string(data)
		if strings.TrimSpace(script) == "" {
			return nil, newError(output.CodeInvalidRequest, fmt.Sprintf("migration %q is empty", file), nil)
		}
		list = append(list, migration{version: version, name: m[2], file: file, script: script})
	}
	return list, nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// appliedVersion returns how many migrations the database has applied. A database that is ahead of the
// known set, has a gap, or recorded another name for a number is refused rather than guessed at.
func appliedVersion(ctx context.Context, q queryer, subject string, known []migration) (int, error) {
	var tables int
	err := q.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", migrationsTable).Scan(&tables)
	if err != nil {
		return 0, wrap("read the schema version of "+subject, err)
	}
	if tables == 0 {
		return 0, nil
	}
	rows, err := q.QueryContext(ctx, "SELECT version, name FROM "+migrationsTable+" ORDER BY version")
	if err != nil {
		return 0, wrap("read the schema version of "+subject, err)
	}
	defer rows.Close()
	applied := 0
	for rows.Next() {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return 0, newError(output.CodeConfigInvalid, "the migration record of "+subject+" is not valid: "+err.Error(), err)
		}
		if version != applied+1 {
			return 0, newError(output.CodeConfigInvalid,
				fmt.Sprintf("the migration record of %s has a gap: version %d follows %d", subject, version, applied), nil)
		}
		if applied >= len(known) {
			return 0, newError(output.CodeConfigInvalid,
				fmt.Sprintf("the schema of %s is at version %d, newer than the %d this program knows; upgrade the program",
					subject, lastVersion(rows, version), len(known)), nil)
		}
		if known[applied].name != name {
			return 0, newError(output.CodeConfigInvalid,
				fmt.Sprintf("migration %d of %s was applied as %q but this program knows it as %q",
					version, subject, name, known[applied].name), nil)
		}
		applied = version
	}
	if err := rows.Err(); err != nil {
		return 0, wrap("read the schema version of "+subject, err)
	}
	return applied, nil
}

// lastVersion drains the remaining rows to report the newest recorded version.
func lastVersion(rows *sql.Rows, current int) int {
	newest := current
	for rows.Next() {
		var version int
		var name string
		if rows.Scan(&version, &name) == nil && version > newest {
			newest = version
		}
	}
	return newest
}

// hasSchema reports whether the database holds any table, index, view or trigger of its own.
func hasSchema(ctx context.Context, q queryer, subject string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE substr(name, 1, 7) <> 'sqlite_'").Scan(&n)
	if err != nil {
		return false, wrap("inspect "+subject, err)
	}
	return n > 0, nil
}

// applyMigration runs one migration and its record in a single transaction. Any failure rolls the whole
// migration back, so the recorded version stays at the previous migration.
func applyMigration(ctx context.Context, conn *sql.Conn, subject string, m migration) (err error) {
	fail := func(step string, cause error) error {
		return newError(output.CodeRuntime, fmt.Sprintf("migration %q of %s failed in %s: %v", m.file, subject, step, cause), cause)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return wrap("start migration "+m.file+" of "+subject, err)
	}
	defer func() {
		if err != nil {
			// A fresh context: the rollback must run even when ctx is the reason for the failure.
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if _, err = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+migrationsTable+
		" (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)"); err != nil {
		return fail("the migration record", err)
	}
	if _, err = conn.ExecContext(ctx, m.script); err != nil {
		return fail("its script", err)
	}
	if _, err = conn.ExecContext(ctx, "INSERT INTO "+migrationsTable+" (version, name, applied_at) VALUES (?, ?, ?)",
		m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fail("the migration record", err)
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fail("the commit", err)
	}
	return nil
}

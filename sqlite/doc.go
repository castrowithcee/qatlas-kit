// Package sqlite opens an embedded SQLite database without cgo, migrates its schema in numbered steps and
// backs it up before a migration. It uses the pure Go driver modernc.org/sqlite behind database/sql, so DB.SQL
// hands the application a standard *sql.DB. The kit is not an ORM or query builder and does not encrypt.
//
// Open creates the directory and the file when missing and keeps the permissions of an existing file. Write
// transactions that first read should start with BEGIN IMMEDIATE, because SQLite cannot wait on a read
// transaction that upgrades to a write. Checkpoint truncates the write-ahead log and fails while another
// connection keeps it in use. Close closes all connections, after which SQLite removes the log.
//
// Failures are an *Error with an output.Code: invalid input and migration sets output.CodeInvalidRequest, a
// foreign or damaged database output.CodeConfigInvalid, denied access output.CodePermission, a busy
// database output.CodeTimeout, anything else output.CodeRuntime.
//
// # Migrations
//
// Options.Migrations holds files NNNN_name.sql in its root. The number has four digits and runs from 0001
// without gap or duplicate; the name is lower-case letters, digits, "_" and "-". Each migration runs in its
// own BEGIN IMMEDIATE transaction together with its row in the schema_migrations table (version, name,
// applied_at). A failing migration rolls back, leaves the recorded version at the previous migration and is
// an output.CodeRuntime error naming the file. A script must not manage transactions itself; the driver runs
// a multi-statement script in one call.
//
// # Backup
//
// Before the first of the pending migrations, Open copies the database to
// <BackupDir>/<file name>.<UTC time>.db and keeps the ten newest copies of that file name. A database
// without any table, index, view or trigger has no data to lose and is not backed up, nor is a database
// without pending migration. The copy is made with VACUUM INTO, which reads one consistent snapshot while
// other connections write and creates the target private and exclusively; rotation is best effort.
// DB.Backup writes the same kind of copy to a path the application names, never replaces a file, and is not
// rotated.
//
// # Always enforced
//
// The directory of the database is created with mode 0700 and the file with 0600, with a user-only ACL on
// Windows. Every pooled connection runs with journal_mode=WAL, busy_timeout=5000, foreign_keys=on and
// synchronous=NORMAL. A file system that cannot do write-ahead logging is an error, so the path must be
// local: write-ahead logging does not work across a network file system. A path that is empty or
// ":memory:", and a path that is not a regular file, are refused.
//
// A migration set with any other entry than the numbered scripts, an empty file, a gap, a duplicate or a bad
// name is an output.CodeInvalidRequest error, reported before the disk is touched. The recorded history
// must match the known set: a version above the newest known migration (a newer program wrote it), a gap in
// the record, or another name for a number is an output.CodeConfigInvalid error. Nothing is migrated down,
// defaulted or skipped. A file that is not a SQLite database is also output.CodeConfigInvalid. A failed
// backup stops the migration.
//
// Open holds an exclusive lock on <file>.lock (see package filelock) while it sets up the first connection
// and migrates, so processes of one program version that start together migrate one after the other and the
// later one finds nothing to do. The lock does not protect against two program versions with different
// migration sets: an older program refuses a newer schema, but a newer program can migrate under an older
// one that is already running. The application must prevent that, for example by having the new version
// replace the old one before it opens the database. The lock file is stable and never removed.
//
// The file path, schema, migration scripts, backup directory and label belong to the caller.
//
// # Options
//
//   - Options.Migrations: the numbered scripts that build and change the schema. Default: the program knows
//     no migration, so a database that has applied one is refused.
//   - Options.BackupDir: receives the backup taken before a migration. Default: "backups" beside the
//     database file.
//   - Options.Label: names the database in error messages, for example "history". Default: "database".
package sqlite

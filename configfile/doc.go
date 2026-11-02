// Package configfile provides schema-independent file operations for configuration documents.
//
// A Store reads and writes one file through a caller-supplied Codec. Reads take no lock and return the
// SHA-256 revision of the bytes read. Every write runs under a stable cross-process lock file beside the
// resolved target and only while the file still has the revision the change was based on; otherwise it
// fails with a *ConflictError and nothing is merged. Writes validate, encode, decode the encoding again,
// back up the current bytes, and replace the target atomically. Any failure before the rename leaves the
// file byte-identical.
//
// A symlinked file is replaced at its target, and a dangling final link is an error. New files are
// private to the current user (0600, directories 0700, a user-only ACL on Windows), existing permissions
// are kept, and a lock file that is not private is rejected without being changed.
//
// Backups lie in a backups directory beside the configured path and are never overwritten. The ten newest
// regular backups and the first hand-edited version are kept. RestoreBackup returns a Restored that
// UndoRestore can put back. Copies made with BackupCopy are kept apart from rotation.
//
// The file path and the document schema belong to the caller.
package configfile

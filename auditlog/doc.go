// Package auditlog keeps an append-only log of entries in daily JSON Lines files, linked by a SHA-256 hash
// chain across day boundaries and optionally signed with an HMAC-SHA256 check value.
//
// # Format
//
// A day file is DIR/YYYY-MM-DD.jsonl, named by its UTC date, with one JSON object per line. A line holds
// seq, time, kind (markers only), the members of the application's entry, cut or damage (markers only),
// prev_hash, mac_key (signed lines with a key ID only) and mac, in that order; mac is always last. seq
// counts every line, markers included, from 1. prev_hash is the hex SHA-256 of the exact bytes of the
// previous line, or of zero bytes for the first line, so the first line of a day chains from the last line
// of the last day that has one. mac is empty for an unsigned line, which carries no mac_key; a signed line
// carries the hex HMAC-SHA256 of the line as written with an empty mac, under the label its mac_key names,
// and a line without mac_key names key ID zero. See KeyLabel and Key for the derivation.
//
// # Contract
//
// The application supplies the directory, the name of the lock file, the entry type T with its JSON
// members and a validation function, and, for signing, the secret, the label the key is derived from with
// its key ID and, after a change of label, the previous labels with theirs.
// An entry must encode as a JSON object without the members the log owns. Append holds an exclusive lock on
// the lock file for its whole read, compute and write, so writers in goroutines and processes never race
// the chain. Directories are created 0700 and day files 0600. A failed Append is returned; the caller
// reports it as a warning and never changes the result of the action the entry records.
//
// Retention removes whole day files older than Options.RetentionDays and writes a retention-cut marker
// that names the last sequence number removed, so Verify accepts the cut. A retention of zero never
// removes a file.
//
// # Security boundary
//
// Verify reports a changed, deleted, inserted or reordered line, a day file removed from the middle or the
// start without a retention cut, a first line that does not chain from the genesis hash, and, given a
// Checker, a check value that is malformed or does not match. A line signed under a previous label after
// the first line signed under the current one breaks the chain as well, with or without a Checker; a
// previous label only checks and never signs. An unsigned line counts as unverified and does not break the
// chain by itself. A check that cannot be made fails Verify instead of passing a line.
// A line that does not parse breaks the chain, unless Options.TornLine documents torn lines and a
// damaged-line marker that holds follows it.
//
// The chain cannot reveal that its newest lines were removed, since nothing after them refers to them.
// Whoever can write the files and has no key can rewrite unsigned lines consistently; only the check
// values bind a line to the key.
package auditlog

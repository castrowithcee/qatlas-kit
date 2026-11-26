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
// its key ID and, after a change of label, the previous labels with theirs. Append writes the entry as the
// next line of the chain. A failed Append is returned; the caller reports it as a warning and never changes
// the result of the action the entry records.
//
// Retention removes whole day files and writes a retention-cut marker that names the last sequence number
// removed, so Verify accepts the cut.
//
// # Always enforced
//
// An entry must encode as a JSON object without the members the log owns, and Append refuses an entry that
// Options.Validate rejects before anything is written. Append holds an exclusive lock on the lock file for
// its whole read, compute and write, so writers in goroutines and processes never race the chain. Directories
// are created 0700 and day files 0600. A key that signs under another key ID than Options.KeyID makes Append
// fail.
//
// Verify reports a changed, deleted, inserted or reordered line, a day file removed from the middle or the
// start without a retention cut, a first line that does not chain from the genesis hash, and, given a
// Checker, a check value that is malformed or does not match. A line signed under a previous label after
// the first line signed under the current one breaks the chain as well, with or without a Checker, and is
// named ProblemKeyIDChange; a previous label only checks and never signs. A signed line whose mac_key is
// not once, spelled literally, right before mac breaks it too, with or without a Checker, and is named
// ProblemMAC before any other check value problem of the line. An unsigned line counts as unverified and
// does not break the chain by itself. A check that cannot be made fails Verify instead of passing a line.
// Verify accepts a line that does not parse only as Options.TornLine documents.
//
// The chain cannot reveal that its newest lines were removed, since nothing after them refers to them.
// Whoever can write the files and has no key can rewrite unsigned lines consistently; only the check
// values bind a line to the key.
//
// # Options
//
//   - Options.RetentionDays and Log.WithRetention: how many days of day files Append keeps, removing older
//     ones under the lock. Default: zero, no day file is ever removed.
//   - Options.TornLine: TornLineDocument makes Append write a KindDamage marker right after a newest line that
//     does not parse, chained from the last line before it that does, and Verify accept such a line only when
//     that marker follows it and holds as a line itself, chain and check value included. Default:
//     TornLineRefuse, Append fails while the newest line does not parse and Verify reports every such line as
//     a break.
//   - Log.WithKey with DeriveKey or DeriveKeys: signs every line with an HMAC-SHA256 check value under the key.
//     Default: no key, lines are written unsigned.
//   - Options.KeyID: the key ID of the current label of the log key. Append writes it as mac_key into every
//     line it signs, and Verify needs no key for this: from the first line signed under KeyID on,
//     a line signed under any other key ID is a mismatch, so a chain changes labels at most once and never
//     back. Default: zero, no mac_key is written.
//   - DeriveKeys with previous labels: keys that only check the lines carrying their key ID after a change of
//     label and never sign. Default: only the current label checks.
//   - The Checker given to Verify and VerifyLines: checks the check values of stored lines; a *Key is one.
//     Default: none, check values are not checked and a line that carries one counts as unchecked.
//   - Options.Now: the clock of Append, for tests. Default: time.Now.
package auditlog

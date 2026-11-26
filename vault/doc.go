// Package vault stores an application's secrets in an age-encrypted directory unlocked by a passphrase.
//
// # Layout
//
// A vault is one directory. The application names its files in Files; the kit has no default names. The
// key file holds the vault's own X25519 private key, encrypted to the passphrase with age's scrypt
// recipient. The recipient file holds the matching public key as age text. The document file holds the
// application's document, encrypted to that recipient. Because the recipient is public, an entry can be
// added without the passphrase: AddPending encrypts it to the recipient into the pending directory, and
// the next Unlock hands every pending entry to the application's MergeFunc and writes the merged
// document before it removes the merged files. An unlocked vault merges selectively without the
// passphrase: ReadPending reads the entries, and Update hands them with the document to the application,
// writes the document it returns once and only then removes the entries it reports; if a removal fails,
// Update puts everything back, because an entry left behind would merge again. Sealed files are further
// documents encrypted to the recipient, such as settings. A change of passphrase rewraps only the key
// file. Every file is a plain age file, so the age tool can decrypt it in an emergency.
//
// The document is the application's: the kit requires only a JSON object whose integer member "schema"
// is one of Options.Schemas.
//
// # Modes
//
// A vault is encrypted unless the application enables the unencrypted mode. Then CreateUnencrypted stores the
// document as plain JSON, and Encrypt and Decrypt switch between the modes. Decrypt verifies the passphrase,
// merges every pending entry and removes the sealed files with the key.
//
// # Unlocking
//
// ReadPassphrase reads the passphrase without echo from the controlling terminal. ReadHidden does the same
// for a secret typed while something else may answer first: as soon as ctx ends it gives the terminal its
// echo back and returns ctx.Err(). ReadConfirm asks a yes-or-no question. A Vault holds the
// key and the decrypted document in memory until Forget; Snapshot hands both to a process that keeps the
// vault open, and OpenWithIdentity reopens it from the key alone.
// ReadSealedWithIdentity reads one sealed file with the key alone and never touches the document, so a
// damaged document does not stop it. Checkpoint records the vault files and pending entries and returns a
// function that restores them without the passphrase, also an entry merged since. Update and WriteWithUndo
// return the undo of their own change: it puts back the document file and the pending files that operation
// removed, byte for byte, and leaves every other entry pending.
//
// # Always enforced
//
// Every file is read only if nobody but the user can reach it: on Unix its mode grants nothing to group
// or others, on Windows its owner and ACL admit only the user, the system and the Administrators group.
// Otherwise a PermissionError names the file and the command that makes it private. The directory is
// created 0700 and every file 0600, or with an ACL for the user alone, and every write replaces its target
// atomically through a temporary file beside it. A Create, Encrypt or Unlock that is interrupted leaves
// the previous state readable: the key file is written after the document, and pending files are removed
// only after the merged document is written.
//
// The document must be a JSON object whose integer member "schema" is one of Options.Schemas. Every read and
// write refuses any other. A document file of 0 bytes, plain or decrypted, is ErrDamaged unless
// Options.EmptyDocument is set. Blanks alone stay damaged, and no write accepts 0 bytes.
//
// Writes are serialized within one Vault value. A write of an unlocked value whose files another writer
// switched to unencrypted or to a new key meanwhile fails with ErrSwitched. Write and WriteWithUndo replace
// the whole document and merge nothing. Update writes the document it returns once and only then removes the
// entries it reports; if a removal fails, it puts everything back, because an entry left behind would merge
// again.
//
// Only a person unlocks. ReadPassphrase, ReadHidden and ReadConfirm refuse with ErrNoTerminal when no standard
// stream is a terminal, so an agent or a CI job is never asked, and show control characters in the prompt as
// escapes.
//
// A result of Options.SwitchFilter that fails the schema check, like a filter error, writes nothing: Encrypt
// leaves no key file and keeps the plain document, Decrypt keeps the encrypted files and the pending entries
// as they were. Decrypt refuses a vault whose document file is missing, because a file that is only temporarily missing
// would be unreadable once the key is gone. An application that means to turn such a vault into an empty
// plain one says so itself: Unlock, Write an empty document when Document returns nil, then Decrypt.
//
// A wrong passphrase reports ErrWrongPassphrase, a key file that does not match the recipient file
// ErrKeyMismatch, and a file that does not decrypt or decode ErrDamaged. A problem with the document, on
// read or write, is a *DocumentError that names the file and tells Kind (DocumentDecrypt, DocumentDecode or
// DocumentSchema) and, for DocumentSchema, the Version found, 0 for a JSON object without a schema; it
// wraps ErrDamaged or ErrUnsupportedSchema.
// A file that does not decrypt, whether the document, a sealed file or a pending entry, is a
// *DocumentError of Kind DocumentDecrypt whose Cause is CauseWrongKey (an age file for another key),
// CauseNotAge (not an age file, such as a plain text file) or CauseDamaged (a damaged or altered age
// file, and any failure that cannot be told apart safely). Each cause has a fixed message of its own.
// The library's own messages and file contents never appear in an error.
//
// The scrypt work factor of a new key is at least DefaultWorkFactor and at most MaxWorkFactor. A lower one is
// accepted only in test binaries.
//
// ReadSealedWithIdentity binds the key to the recipient file and reports ErrAbsent when that file is missing.
// Anyone who can write the directory can encrypt to the public recipient, so a sealed file whose content
// must come from the vault itself carries a check value: MAC and VerifyMAC compute it with a key derived
// by DeriveKey from the private key and a label the application chooses for each use. A replaced
// recipient file makes the next Unlock fail with ErrKeyMismatch, but pending entries written before then
// were encrypted to it.
//
// The vault protects against accidental reading by other processes of the same user and against secrets
// in files, history and the process list. It does not protect against a deliberate attacker with the
// user's rights or with administrator rights.
//
// # Options
//
//   - Files.Plain: names the file of the unencrypted mode and enables CreateUnencrypted, Encrypt and Decrypt.
//     Default: the vault is always encrypted and these fail with ErrUnencryptedDisabled.
//   - Files.Lock: names a lock file that every write of the document, the key or the recipient holds
//     exclusively across processes, reading what it changes from disk under the lock, so no write rests on a
//     state read before it. Update then works as compare-and-swap: its UpdateFunc sees the document on disk
//     and returns ErrNoChange to write nothing when the value it expects has moved on. Undo and restore take
//     back only this value's change: they put bytes back only while nobody else wrote the document since, and
//     otherwise hand the change to Options.Revert; restore removes only the pending entries this value added.
//     Default: writes are serialized within one Vault value alone, and Checkpoint's restore removes every
//     pending entry it did not record, also one another process added.
//   - Options.Revert: with Files.Lock, takes this value's change out of a document another writer changed
//     since, for an undo or a restore. Default: those fail with ErrConflict and write nothing.
//   - Options.Merge: merges pending entries into the document on unlock. Default: a vault with pending entries
//     does not unlock.
//   - Options.SwitchFilter: receives the document on Encrypt and Decrypt and returns what is carried over, so
//     the application carries over only what it allows. Default: the document is carried over unchanged.
//   - Options.EmptyDocument: the document a document file of 0 bytes reads as; New refuses one that fails the
//     schema check. Default: such a file is ErrDamaged.
//   - Options.WorkFactor: raises the scrypt work factor of a newly wrapped key. Default: DefaultWorkFactor.
//   - Options.RetryFiles: repeats a file operation that fails because another process holds the file for a
//     moment, such as a sharing violation on Windows, after the waits of fileretry.DefaultDelays, 2.775 s at
//     most per operation: stat, read, listing the pending directory, replace and remove. A missing file is
//     never repeated, nor is the lock file or writing the temporary file. Default: every failure is returned
//     at once.
//   - AllowMissingRecipient, passed to ReadSealedWithIdentityOptions: reads the sealed file with the key alone
//     when the recipient file is missing, and then the authenticity of the content is the application's to
//     check. Default: ReadSealedWithIdentity reports ErrAbsent in that case.
package vault

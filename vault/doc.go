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
// document before it removes the merged files. Sealed files are further documents encrypted to the
// recipient, such as settings. A change of passphrase rewraps only the key file. Every file is a plain
// age file, so the age tool can decrypt it in an emergency.
//
// The document is the application's: the kit requires only a JSON object whose integer member "schema"
// is one of Options.Schemas, and refuses any other on every read and write.
//
// # Modes
//
// A vault is encrypted unless the application enables the unencrypted mode by naming Files.Plain. Then
// CreateUnencrypted stores the document as plain JSON in that file, and Encrypt and Decrypt switch
// between the modes. Decrypt verifies the passphrase, merges every pending entry and removes the sealed
// files with the key. Without Files.Plain these operations fail with ErrUnencryptedDisabled.
//
// # Checks
//
// Every file is read only if nobody but the user can reach it: on Unix its mode grants nothing to group
// or others, on Windows its owner and ACL admit only the user, the system and the Administrators group.
// Otherwise a PermissionError names the file and the command that makes it private. The directory is
// created 0700 and every file 0600, or with an ACL for the user alone, and every write replaces its target
// atomically through a temporary file beside it. A Create, Encrypt or Unlock that is interrupted leaves
// the previous state readable: the key file is written after the document, and pending files are removed
// only after the merged document is written.
//
// A wrong passphrase reports ErrWrongPassphrase, a key file that does not match the recipient file
// ErrKeyMismatch, and a file that does not decrypt or decode ErrDamaged. The library's own messages and
// file contents never appear in an error. The scrypt work factor is DefaultWorkFactor unless
// Options.WorkFactor raises it; a lower one is accepted only in test binaries.
//
// Anyone who can write the directory can encrypt to the public recipient, so a sealed file whose content
// must come from the vault itself carries a check value: MAC and VerifyMAC compute it with a key derived
// by DeriveKey from the private key and a label the application chooses for each use. A replaced
// recipient file makes the next Unlock fail with ErrKeyMismatch, but pending entries written before then
// were encrypted to it.
//
// # Unlocking
//
// Only a person unlocks. ReadPassphrase reads the passphrase without echo from the controlling terminal
// and refuses with ErrNoTerminal when no standard stream is a terminal, so an agent or a CI job is never
// asked. A Vault holds the key and the decrypted document in memory until Forget; Snapshot hands both to
// a process that keeps the vault open, and OpenWithIdentity reopens it from the key alone. Checkpoint
// records the vault files and returns a function that restores them without the passphrase.
//
// # Boundary
//
// The vault protects against accidental reading by other processes of the same user and against secrets
// in files, history and the process list. It does not protect against a deliberate attacker with the
// user's rights or with administrator rights.
package vault

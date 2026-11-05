// Package approval binds what an application may do with a secret to a state a person approved.
//
// # Fingerprint
//
// The application decides what a state is: every value that determines where and for what a secret is
// used, in a canonical form it builds itself, for instance with sorted lists. Fingerprint is the SHA-256 of
// the application's label followed by the JSON encoding of that state. A state that differs in any value
// has another fingerprint and needs a new approval, since the configuration it comes from is not trusted.
// An application whose approvals predate this package keeps them valid by handing over the same label and
// the same state it hashed before. An application that changes a label lists the old one among the previous
// labels (see Options): approvals and the approvals file made under it keep holding, and the next change of
// the store rewrites them under the current label. A previous label only verifies, never writes.
//
// # Store
//
// A Store keeps the approvals in a sealed file of an encrypted vault, named by the application, with each
// approved state, its fingerprint and the time it was approved. Because anyone who can write the vault
// directory can encrypt to its public recipient, the file carries a check value keyed from the vault key
// with the application's label (see vault.MAC). A file whose check value does not match, that does not
// decode, or whose fingerprints do not match their states is refused, never read in part. A missing file
// holds no approvals. An unencrypted vault holds none either. Every change locks a lock file the
// application names, so changes of several processes follow one another instead of undoing each other.
//
// Check tells whether a current state is approved exactly as it is; Compare lists what differs, with
// every rune that is not printable escaped. Approve takes the fingerprint of the state as it was shown,
// so a state that changed in between is not approved unseen. Adopt takes over approvals given before,
// such as those an application kept in its own document, when their fingerprints match their states.
//
// # Only a person approves
//
// Approve, Revoke and Adopt need an Admin of the store's vault, and only Authorize creates one. Authorize
// refuses an agent and a process none of whose standard streams is a terminal before it asks for
// anything, then reads the passphrase from the terminal and verifies it against the vault, also when the
// vault is unlocked already. Checking and comparing need no Admin, so a process that holds the vault
// open, such as a background process, checks every use against the approvals.
//
// # Boundary
//
// The approvals protect against a configuration changed by someone other than the person, an agent among
// them. Like the vault, they do not protect against a deliberate attacker with the user's rights, who can
// for instance put back an older approvals file.
package approval

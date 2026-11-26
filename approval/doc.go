// Package approval binds what an application may do with a secret to a state a person approved.
//
// # Fingerprint
//
// The application decides what a state is: every value that determines where and for what a secret is
// used, in a canonical form it builds itself, for instance with sorted lists. Fingerprint is the SHA-256 of
// the application's label followed by the JSON encoding of that state. A state that differs in any value
// has another fingerprint and needs a new approval, since the configuration it comes from is not trusted.
// An application whose approvals predate this package keeps them valid by handing over the same label and
// the same state it hashed before.
//
// # Store
//
// A Store keeps the approvals in a sealed file of an encrypted vault, named by the application, with each
// approved state, its fingerprint and the time it was approved. A missing file holds no approvals. An
// unencrypted vault holds none either.
//
// Check tells whether a current state is approved exactly as it is; Compare lists what differs. Approve takes
// the fingerprint of the state as it was shown, so a state that changed in between is not approved unseen.
// Adopt takes over approvals given before, such as those an application kept in its own document, when their
// fingerprints match their states.
//
// # Only a person approves
//
// Approve and Adopt add approved uses, so they need an Admin of the store's vault, and only Authorize creates
// one. Revoke, Move, Rename and Undo add no approved use and need no Admin. Revoke removes approvals. Move
// files an approval under another name with the same state, fingerprint and approval time; the fingerprint
// covers the state only, so the approval holds for exactly the state it was given for. Rename carries
// approvals over to a state that differs only in a name, and the approvals get the fingerprint of the new
// state and keep their approval time. Each returns an Undo that writes back the bytes it replaced. The
// application decides whether and when to offer them, and whether to ask the person first. Checking and
// comparing need no Admin either, so a process that holds the vault open, such as a background process,
// checks every use against the approvals.
//
// # Always enforced
//
// Authorize refuses an agent with ErrAgent and a process none of whose standard streams is a terminal with
// vault.ErrNoTerminal before it asks for anything, then reads the passphrase and verifies it against the
// vault, also when the vault is unlocked already, so a wrong passphrase fails with vault.ErrWrongPassphrase
// and an unencrypted vault with vault.ErrNotEncrypted. Authorize also refuses an empty prompt. Approve and
// Adopt refuse an Admin that Authorize did not create with ErrNotAdmin, and one whose validity has expired
// with ErrAdminExpired. Admin.Extend asks for no passphrase, so it renews only an Admin that is still valid:
// a zero Admin and an expired one fail, and so does an Admin without a limit. Nothing is approved without an
// Admin or a delegated proof.
//
// Because anyone who can write the vault directory can encrypt to its public recipient, the approvals file
// carries a check value keyed from the vault key with the application's MACLabel (see vault.MAC). A file
// whose check value does not match, that does not decode, or whose fingerprints do not match their states is
// refused, never read in part. Every change locks the file named by Options.LockName, so changes of several
// processes follow one another instead of undoing each other. Every label is non-empty.
//
// Approve refuses with ErrChanged a state that differs from the fingerprint it was shown with, and then
// nothing is approved. Adopt refuses an approval whose fingerprint does not match its state, and then nothing
// is adopted. Compare writes every rune that is not printable as an escape, so a state cannot steer the
// terminal it is shown on. Rename replaces the old name by the new one in the stored, verified state, only at
// the paths the application names and only where the old name stands, and refuses with ErrChanged unless the
// result equals the new state it was handed, so no right is added. Lists keep their order, and only whole
// items of the lists named in NameChange.Unordered may move. All approvals of a change are written together
// or none is. An Undo writes back only while the file still holds the bytes that change wrote, and otherwise
// fails with ErrModified without writing. Revoke, Move, Rename and Undo use the same lock, check value and
// consistency checks as every other change, and a damaged file is refused.
//
// A delegated proof must name the store's vault, have an expiry or state explicitly that it has none, not
// have expired, and cover every state before anything is written; otherwise the call fails with
// ErrNoDelegation, ErrDelegationExpired or ErrNotCovered. The kit cannot check where a proof comes from: the
// application guarantees that it is genuine, was established by a person and is presented only by whom it
// was given to.
//
// The approvals protect against a configuration changed by someone other than the person, an agent among
// them. Like the vault, they do not protect against a deliberate attacker with the user's rights, who can
// for instance put back an older approvals file.
//
// # Options
//
//   - AuthorizeOptions.Valid: limits how long the Admin is accepted by Approve and Adopt, counted from
//     Authorize; only Admin.Extend renews it, and only while it is still valid. A negative value is an error.
//     Default: no limit, an Admin holds as long as the application keeps it.
//   - AuthorizeOptions.Dialog: reads the passphrase in a masked dialog of a terminal user interface that runs
//     on this process's terminal. Default: vault.ReadPassphrase from the controlling terminal.
//   - AuthorizeOptions.PersonChannel: authorizes without a terminal, for instance through a form of the
//     application's web interface; the application guarantees that the passphrase comes from a person, and
//     Authorize still refuses an agent and verifies the passphrase. It cannot be set together with Dialog.
//     Default: the terminal rules.
//   - DelegatedProof with Store.ApproveDelegated and Store.AdoptDelegated: approves on a proof a person gave
//     earlier instead of an Admin. Default: only an Admin approves.
//   - Options.PreviousFingerprintLabels: the labels FingerprintLabel replaced. Approvals made under them keep
//     holding and a current state matches them under any of them; they only verify and never write, and the
//     next change of the store rewrites every such approval under the current label. Default: only
//     FingerprintLabel verifies.
//   - Options.PreviousMACLabels: the labels MACLabel replaced. An approvals file whose check value fails under
//     MACLabel is accepted when it verifies under one of them, and is only ever written under MACLabel.
//     Default: only MACLabel verifies.
//   - NameChange.Unordered: names the lists whose items a Rename may reorder, whole item by whole item.
//     Default: every list keeps its order.
package approval

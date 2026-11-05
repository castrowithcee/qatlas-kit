// Package vaultproc keeps an unlocked vault in a hardened background process of its own, so later
// invocations of the application use it without asking for the passphrase again.
//
// # Start
//
// After a person unlocked the vault, Start runs the application's hidden subcommand as a detached
// background process and hands it the vault's key and document (vault.Snapshot) through an inherited pipe,
// never through arguments, the environment or a file. That subcommand calls Run. Run hardens the process
// with localipc.Harden before it reads anything secret, reads the state and overwrites what it read, and
// listens on the application's localipc.Endpoint; a second process for the same channel reports running
// and ends. SocketBase derives the socket name from the vault's path. The document is held outside the Go
// heap and locked into memory where the system allows it (Status.MemoryLocked tells), and it is
// overwritten when the process locks. The key's own bytes belong to the age library and are only dropped.
//
// # Channel
//
// Every connection carries two exchanges, each one line of JSON within MaxMessage and one deadline: the
// challenge and its proof, then one request and its answer. Both lines carry ProtocolVersion and the
// application's Config.Version; another version is refused. The listener checks every client's user and
// program before anything is read. A Client checks the process that listens with localipc.CheckHardened
// and then challenges it: it sends a fresh nonce encrypted to the vault's recipient, and the process
// answers with a hash of what it decrypted, never the plaintext. Nothing else is sent before both checks
// passed. A process that fails them, or stays silent, is refused with a *localipc.PeerError that names its
// process id. The application supplies the proof label that prefixes the hash (Config.ProofLabel and
// Client.ProofLabel, equal on both sides, never empty); it separates the proof from any other use of the
// nonce's hash, two applications never share one, and the kit has no default. Answers carry fixed codes
// only, never anything a request sent.
//
// The kit answers OpStatus and OpLock itself. Every other operation is the application's: Config.Handlers
// maps its names to HandlerFunc, which runs alone, reads the held document through Held and may replace it.
// Only a handler that calls Held.Touch restarts the idle period, so the application decides which requests
// count as use.
//
// # Locking
//
// The process locks itself, overwrites what it holds and ends: after Config.IdleTimeout without a touch,
// at the end of the maximum unlock time counted from the unlock, on OpLock, on a stop signal, when its
// socket is gone or replaced, and when its program was replaced without a handover under way. A process
// whose program was replaced refuses every client.
//
// # Handover
//
// On Linux and macOS an update can hand the vault over to the program it installs. NewReplacement returns
// the hooks for selfupdate.Client.BeforeReplace and AfterReplace. Before the program is replaced, the
// process reads the release files itself, verifies the signature, the archive's checksum and the program
// in it (HandoverConfig), and asks the user's setting whether to hand over; once the program is replaced,
// it checks that the program at its own path is the one of the release, starts it with
// HandoverConfig.SuccessorArgs, hands it its state through a pipe and then moves the channel to it with
// localipc.HandOver. The successor keeps the unlock time and the lock time of its predecessor and the
// shorter maximum, so a handover never extends an unlock. Every failure locks, and so do a rollback, a
// user's setting that asks for it, a process without HandoverConfig, and Windows, which has no handover.
//
// # Boundary
//
// The process protects the vault against accidental reading by other processes of the same user and
// against secrets in files, history and the process list. It does not protect against a deliberate
// attacker with the user's rights or with administrator rights; see localipc and vault for the limits of
// the channel and the hardening.
package vaultproc

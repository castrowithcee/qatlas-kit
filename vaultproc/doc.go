// Package vaultproc keeps an unlocked vault in a hardened background process of its own, so later
// invocations of the application use it without asking for the passphrase again.
//
// # Start
//
// After a person unlocked the vault, Start runs the application's hidden subcommand as a detached
// background process and hands it the vault's key and document (vault.Snapshot) through an inherited pipe.
// That subcommand calls Run. Run hardens the process with localipc.Harden before it reads anything secret,
// reads the state and overwrites what it read, and listens on the application's localipc.Endpoint; a second
// process for the same channel reports running and ends. SocketBase derives the socket name from the vault's
// path. The document is held outside the Go heap and locked into memory where the system allows it
// (Status.MemoryLocked tells), and it is overwritten when the process locks. The key's own bytes belong to
// the age library and are only dropped.
//
// # Channel
//
// Every connection carries two exchanges, each one line of JSON: the challenge and its proof, then one request
// and its answer. A Client challenges the process that listens: it sends a fresh nonce encrypted to the
// vault's recipient, and the process answers with a hash of what it decrypted, never the plaintext. The
// application supplies the proof label that prefixes the hash (Config.ProofLabel and Client.ProofLabel); it
// separates the proof from any other use of the nonce's hash.
//
// The kit answers OpStatus and OpLock itself. Every other operation is the application's: Config.Handlers
// maps its names to HandlerFunc, which runs alone, reads the held document through Held and may replace it.
// Only a handler that calls Held.Touch restarts the idle period, so the application decides which requests
// count as use.
//
// # Handover
//
// On Linux and macOS an update can hand the vault over to the program it installs. NewReplacement returns
// the hooks for selfupdate.Client.BeforeReplace and AfterReplace. Before the program is replaced, the
// process reads the release files itself, verifies them (HandoverConfig) and asks the user's setting whether
// to hand over; once the program is replaced, it checks that the program at its own path is the one of the
// release, starts it with HandoverConfig.SuccessorArgs, hands it its state through a pipe and then moves the
// channel to it with localipc.HandOver. Where the application changed its endpoint, a successor takes the
// vault over from a process that serves the earlier one and runs its own handover; it serves nothing before
// that process handed over. A successor started by an earlier version of the application receives that
// version's state format. After an install without a handover, the application can find and end a process
// that still serves the earlier endpoint with localipc's Endpoint.Holder and EndHolder. A handover reports
// its result as a HandoverResult, with a HandoverReason when none happened. A client reads each refusal as a
// *HandoverError, and ReplacementResult.Reason names the reason of every OutcomeLocked.
//
// # Always enforced
//
// Secrets reach the process only through the inherited pipe, never through arguments, the environment or a
// file. A message is one line within MaxMessage, a handover state within MaxState, and one connection within
// one deadline, DefaultRequestTimeout unless Config.RequestTimeout or Client.Timeout set another. Both lines
// of the first exchange carry ProtocolVersion and the application's Config.Version, and either side refuses
// another version. The listener checks every client's user and program before anything is read. A Client
// checks the process that listens with localipc.CheckHardened and then challenges it, and sends nothing else
// before both checks passed. A process that fails them, or stays silent, is refused with a
// *localipc.PeerError that names its process id. The proof label must be non-empty and equal on both sides,
// and the kit has no default, so two applications never share one; a client without one sends nothing.
// Answers carry fixed codes only, never anything a request sent. OpStatus, OpLock and OpHandover cannot be
// registered as handler names, and an *Error whose code is a code of the kit answers with the code of
// ErrFailed.
//
// The process locks itself, overwrites what it holds and ends: after Config.IdleTimeout without a touch, at
// the end of the maximum unlock time counted from the unlock, on OpLock, on a stop signal, when its socket is
// gone or replaced, and when its program was replaced without a handover under way. Config.IdleTimeout must
// be positive, and the kit sets no default. A process whose program was replaced refuses every client, and
// the first refused client locks it. The first cause is the reason of a lock. The process overwrites what it
// holds even if a hook fails or panics; Serve then returns ErrHook, which carries nothing of the hook's error.
//
// A handover verifies the release signature, the archive's checksum and the program in it, and checks at the
// commit that the program at its own path is the one of the release. The successor keeps the unlock time and
// the lock time of its predecessor and the shorter maximum, so a handover never extends an unlock. The kit
// reads at most 256 MiB of the release archive and hashes at most that much of the installed program, and a
// file over the limit locks with HandoverTooLarge. Every failure locks, and so do a rollback, a user's
// setting that asks for it, a process without HandoverConfig, and Windows, which has no handover. A request
// refused because another handover is under way (HandoverBusy) or the process is locking reports nothing.
// Each phase of a handover is bounded by HandoverTimeout. A state translated by RunOptions.DecodeEarlier
// is rejected without a key, without a lock time or with a negative maximum, and Run validates the Config in
// full.
//
// The process protects the vault against accidental reading by other processes of the same user and
// against secrets in files, history and the process list. It does not protect against a deliberate
// attacker with the user's rights or with administrator rights; see localipc and vault for the limits of
// the channel and the hardening.
//
// # Options
//
//   - Config.MaxUnlock: the longest the process stays unlocked from State.UnlockedAt, whatever touches it.
//     Default: no limit.
//   - Config.RequestTimeout: bounds each connection. Default: DefaultRequestTimeout.
//   - Client.Timeout: bounds one call on the client side. Default: DefaultRequestTimeout.
//   - Config.Handover: lets the process hand the vault over to a successor started from the program an update
//     installed. Default: every handover request locks the process.
//   - HandoverConfig.MaxFileSize: lowers the limit of the release archive and of the installed program the
//     process reads and hashes, never raises it. Default: 256 MiB.
//   - Config.OnHandover: is told the result of every handover this process prepared, after a successor took
//     the vault over and before the client is answered, and after a failed handover locked the process and
//     before the client is told. It reaches the vault through Held, runs alone, and must be short. Default:
//     nothing is called.
//   - Config.OnLock: overwrites state the application derived from the vault. It runs once for every lock of
//     a process that served, after the requests in flight ended and before the process overwrites what it
//     holds, and says why in a LockEvent: LockIdle, LockMaxUnlock, LockCommand, LockClosed (Close, a stop
//     signal, the end of Run's context), LockSocketGone, LockProgramReplaced (LockEvent.Trigger tells whether
//     the watch or a refused client found it), LockHandedOver, LockHandoverFailed with the reason of the
//     handover, LockHandoverDeclined or LockListenerFailed. Default: nothing is called, and a process that
//     never served calls nothing either way.
//   - Config.ReplacedProgram: ReplacedLocksOnClient makes only the first refused client lock a process whose
//     program was replaced without a handover and leaves the watch of the socket on. Default:
//     ReplacedLocksEarly, the process also finds the replacement itself within seconds and locks without a
//     client.
//   - RunOptions.Successor: starts the process as the successor of a running vault process, as
//     HandoverConfig.SuccessorArgs ask. Default: a first start.
//   - RunOptions.Earlier: with Successor, the channel of a vault process of an earlier version that served the
//     vault under another endpoint, taken over with the checks and limits of
//     localipc.ListenOptions.Earlier. Default: no earlier channel.
//   - RunOptions.DecodeEarlier: with Successor, translates a state in an earlier format of the application,
//     only when the kit's own format does not parse. Default: only the kit's own format is read.
//   - RunOptions.Configure: forms the Config and the Earlier endpoint from the key once the state is read, for
//     settings the application keeps encrypted under that key. Both hooks run after the process is hardened
//     and report a failure without its text; Run overwrites the bytes it read and listens only if both
//     succeed. Default: the embedded Config and Earlier are used as given.
package vaultproc

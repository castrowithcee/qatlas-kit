// Package localipc provides a local process channel with checked peer identity.
//
// Endpoint is the channel between processes of one user. Both ends check the peer's user and program file.
// Linux compares the program file the kernel reports for the peer. macOS and Windows compare the file
// identity at the peer's kernel-reported image path with the own program file. ProgramReplaced tells a
// process whether its own program file was removed or replaced since it started.
//
// ServiceEndpoint is the channel of a system service on Linux that runs as its own user: the service admits
// root and members of one group, the client admits only a server running as the service user or as itself.
// Both use the kernel's credentials of the peer.
//
// # Layout
//
// Unix places the socket at <root>/<RuntimeDirName>/<SocketBase>.sock. The root is Endpoint.RuntimeRoot when
// set; otherwise it is XDG_RUNTIME_DIR, then /run/user/<uid> on Linux, then FallbackDir, where
// Endpoint.FallbackDirName, when set, takes the place of RuntimeDirName; on macOS a path too long for a
// socket moves to TMPDIR. Unix also uses socket permissions and a lock; Close removes the socket and the
// lock file. Windows uses a protected named pipe. Running lists the SocketBase of every channel of the
// application, found as private sockets of the user in the runtime directory (on macOS also in the TMPDIR
// location) or as the user's open pipes with the endpoint's PipePrefix. The list does not prove a channel
// alive; only Dial does. A crashed Unix process leaves its name until a Listen of the same name takes it
// over.
//
// # Handover
//
// On Unix a running listener can hand its channel to a successor that listens beside it (HandOver,
// AwaitHandover): the lock and then the socket move atomically, so exactly one process serves the channel at
// any time. A successor can also take over from a process that serves an earlier endpoint of the
// application, such as one under a former name, and that runs its own handover. It then listens at the
// endpoint's channel but accepts nothing before the handover, and places beside the earlier socket a lock it
// holds and a socket nothing listens on, which the earlier process moves onto its own. Nothing already there
// is replaced, and the successor never speaks the earlier process's protocol.
//
// After an install without a handover, a process may still serve an earlier endpoint. Holder finds it and
// EndHolder asks it to end. EndHolder does not wait; the application can call Holder until it reports
// ErrNotServed.
//
// A refusal is a *PeerError with the peer's process id. Its Err is a *RefusalError, whose Reason names the
// cause without a text comparison. Accept closes a refused connection; ListenOptions.Refused may send a
// refusal first.
//
// # Always enforced
//
// Both ends admit only a peer of the same user running the same program file. A process whose own program
// file was removed or replaced since it started refuses every peer (ErrProgramReplaced). A check that cannot
// inspect the peer or the own program fails closed.
//
// A Unix runtime root must be a private directory of the user. A root that is a symbolic link, belongs to
// another user, or is writable by group or others (even with the sticky bit) is refused, because whoever can
// write there can replace the directory below it. The directory <RuntimeDirName> stays private (mode 0700,
// the user's own, not a link) in every case and is never corrected when it exists with other permissions.
// RuntimeReadable never applies to FallbackDir, TMPDIR or ServiceEndpoint. Windows uses a named pipe that
// rejects network clients and another first instance. A socket path over the portable Unix
// limit is a *PathTooLongError.
//
// A listener takes over from an earlier endpoint only where the earlier channel passes the checks a listener
// makes of its own (a private socket in a private directory of the user, its lock held) and no process
// serves the endpoint itself. Windows does not support a handover, and a ServiceEndpoint has no successor and
// no CrossVersion check.
//
// Holder and EndHolder signal a process only when called, and nothing else in the package signals one. Both
// accept only another process of this user: on Unix behind a channel that passes the checks above with its
// lock held, on Windows behind a pipe owned by the user or by the default owner of this process's token. They
// never replace or remove anything at the endpoint. EndHolder checks again just before the signal. Linux
// (with pidfd, from kernel 5.3) and Windows hold the checked process open across that check; macOS signals by
// process id, so a process id reused in that moment by another process of the user would receive the signal.
//
// A cross-version secret must be at least MinSecretSize bytes (ErrShortSecret), and the package uses a copy
// of it. CrossVersion replaces the program check, including the refusal of a replaced own program, by the
// user check plus a mutual challenge the package runs over the shared secret. The application creates the
// secret and hands it to its background process over the inherited pipe of package background (Start,
// Child.ReadHandover), never through arguments, the environment, or an unencrypted file. Both ends must
// select it.
//
// A dial that selects CheckHardened without a Challenge is refused (ErrNoChallenge). Where Harden hides the
// program file (Linux), CheckHardened replaces the program check by the user check plus the Challenge; on
// macOS and Windows, where Harden leaves the program readable, it checks the program as well. Windows Harden
// limits process access to identity queries, waiting, and termination while denying memory access through the
// process DACL; debug privilege and existing handles bypass it.
//
// These checks protect against other processes of the same user only as far as they verify, and never
// against root or an administrator. On macOS and Windows, replacing the file at the image path after the peer
// started is not detected, so the check cannot prove which file a peer loaded; such a swap needs the user's
// write access and lies outside this boundary, as does deliberate impersonation by a process with the user's
// rights. A Challenge and CrossVersion are only as strong as the secret they prove and do not protect
// against a process that can read it.
//
// # Options
//
//   - Endpoint.RuntimeRoot: the Unix runtime root, which must be an absolute, existing, private directory of
//     the user. Default: XDG_RUNTIME_DIR, then /run/user/<uid> on Linux, then FallbackDir. Windows ignores it.
//   - Endpoint.FallbackDirName: the directory under FallbackDir that holds the socket when no runtime directory
//     exists. Default: RuntimeDirName.
//   - Endpoint.RuntimeAccess: RuntimeReadable also accepts a runtime root of this user that group and others
//     may read but not write. Default: RuntimePrivate, only a private root. Windows ignores it.
//   - ListenOptions.CrossVersion and DialOptions.CrossVersion: admit a peer started from another program file
//     that proves the shared secret, such as a background process of the new program and long-running
//     processes of the old one. Accept runs its exchange under a short deadline and blocks while it runs.
//     Default: the program check.
//   - DialOptions.Server: CheckHardened checks a server that called Harden. Default: CheckProgram, the same
//     user and the same program file.
//   - DialOptions.Challenge: lets the listening process prove that it is the application's own server after
//     the kernel checks and before the dial returns; the client sends nothing of its own before it succeeds.
//     Required with CheckHardened. Default: no challenge.
//   - Harden: disables core dumps and same-user inspection and, on Linux, hides the executable from same-user
//     processes, so a plain Dial to a hardened listener fails closed. Default: the process is not hardened.
//   - ListenOptions.Successor: listens beside the endpoint's channel, where clients do not reach it until the
//     running listener calls HandOver and AwaitHandover returns. Default: the listener owns the channel.
//   - ListenOptions.Earlier: with Successor, an endpoint the application used before, whose listener the
//     successor takes over from; Accept fails until AwaitHandover returns. Default: no earlier endpoint.
//   - ListenOptions.Refused: receives each connection Accept refuses, with the *PeerError, before Accept closes
//     it. The peer is unverified: the function may send one short refusal, must treat anything read as
//     untrusted, runs under a short deadline and blocks Accept while it runs. Default: a refused connection
//     is closed without a reply.
//   - ServiceEndpoint instead of Endpoint: the channel of a system service on Linux that runs as its own
//     user, for which other platforms return an error wrapping errors.ErrUnsupported. Default: Endpoint, a
//     channel between processes of one user.
package localipc

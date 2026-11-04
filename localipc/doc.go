// Package localipc provides a local process channel with checked peer identity.
//
// Endpoint is the channel between processes of one user. Both ends check that
// the peer runs as the same user and from the same program file. Linux
// compares the program file the kernel reports for the peer. macOS and Windows
// compare the file identity at the peer's kernel-reported image path with the
// own program file. A process whose own program file was removed or replaced
// since it started refuses every peer (ErrProgramReplaced); ProgramReplaced
// tells it so.
//
// CrossVersion lets processes of one application reach each other across an
// update, such as a background process of the new program and long-running
// processes of the old one. Both ends must select it. It replaces the program
// check, including the refusal of a replaced own program, by the user check
// plus a mutual challenge the package runs over a secret both ends share. The
// application creates the secret and hands it to its background process over
// the inherited pipe of package background (Start, Child.ReadHandover), never
// through arguments, the environment, or an unencrypted file. ServiceEndpoint
// does not support it.
//
// Harden is optional. Linux Harden hides the executable from same-user
// processes, so a plain Dial to a hardened listener fails closed. A client
// reaches it only with CheckHardened, which replaces the program check by the
// user check plus a Challenge the application supplies; without a Challenge
// the option is refused. On macOS and Windows, where Harden leaves the program
// readable, CheckHardened checks the program as well. Windows Harden limits
// process access to identity queries, waiting, and termination while denying
// memory access through the process DACL; debug privilege and existing
// handles bypass it.
//
// Unix places the socket at <root>/<RuntimeDirName>/<SocketBase>.sock. The
// root is Endpoint.RuntimeRoot when set; otherwise it is XDG_RUNTIME_DIR, then
// /run/user/<uid> on Linux, then FallbackDir, and on macOS a path too long for
// a socket moves to TMPDIR. Unix also uses a private runtime directory, socket
// permissions, and a lock; Close removes the socket and the lock file. Windows
// uses a protected named pipe that rejects network clients and another first
// instance. On Unix a running listener can hand its channel to a successor
// that listens beside it (ListenOptions.Successor, HandOver, AwaitHandover):
// the lock and then the socket move atomically, so exactly one process serves
// the channel at any time. Windows does not support a handover.
//
// Running lists the SocketBase of every channel of the application, found as
// private sockets of the user in the runtime directory (on macOS also in the
// TMPDIR location) or as the user's open pipes with the endpoint's PipePrefix.
// The list does not prove a channel alive; only Dial does. A crashed Unix
// process leaves its name until a Listen of the same name takes it over.
//
// ServiceEndpoint is the channel of a system service on Linux that runs as its
// own user: the service admits root and members of one group, the client
// admits only a server running as the service user or as itself. Both use the
// kernel's credentials of the peer.
//
// A refusal is a *PeerError with the peer's process id. Accept closes a
// refused connection; ListenOptions.Refused may send a refusal first.
//
// Security boundary: these checks protect against other processes of the same
// user only as far as they verify, and never against root or an administrator.
// On macOS and Windows, replacing the file at the image path after the peer
// started is not detected, so the check cannot prove which file a peer loaded;
// such a swap needs the user's write access and lies outside this boundary, as
// does deliberate impersonation by a process with the user's rights. A
// Challenge and CrossVersion are only as strong as the secret they prove and
// do not protect against a process that can read it.
package localipc

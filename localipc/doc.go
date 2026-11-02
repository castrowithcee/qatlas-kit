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
// Unix uses a private runtime directory, socket permissions, and a lock.
// Windows uses a protected named pipe that rejects network clients and another
// first instance. On Unix a running listener can hand its channel to a
// successor that listens beside it (ListenOptions.Successor, HandOver,
// AwaitHandover): the lock and then the socket move atomically, so exactly one
// process serves the channel at any time. Windows does not support a handover.
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
// Challenge is only as strong as the secret it proves.
package localipc

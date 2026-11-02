// Package localipc provides a local process channel with checked peer identity.
//
// Both ends check that the peer runs as the same user and from the same program
// file. Linux compares the program file the kernel reports for the peer. macOS
// and Windows compare the file identity at the peer's kernel-reported image path
// with the own program file.
//
// Security boundary: these checks protect against other processes of the same
// user only as far as they verify, and never against root or an administrator.
// On macOS and Windows, replacing the file at the image path after the peer
// started is not detected, so the check cannot prove which file a peer loaded;
// such a swap needs the user's write access and lies outside this boundary, as
// does deliberate impersonation by a process with the user's rights.
//
// Unix uses a private runtime directory, socket permissions, and a lock.
// Windows uses a protected named pipe that rejects network clients and another
// first instance. Harden is optional. Linux Harden hides the executable from
// same-user processes, so connections to a hardened listener fail closed.
// Windows Harden limits process access to identity queries, waiting, and
// termination while denying memory access through the process DACL; debug
// privilege and existing handles bypass it.
package localipc

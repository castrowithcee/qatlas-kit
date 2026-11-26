// Package release verifies signed release files without network access.
//
// A release publishes checksums.txt, an armored SSH signature over it, and archives. VerifyChecksums
// checks the signature, ChecksumFor looks up the SHA-256 of one archive, and ProgramSHA256 hashes the
// program inside a .tar.gz or .zip archive. VerifyArchive combines the first two steps with the archive
// hash. Callers verify the signature before they trust anything read from checksums.txt.
//
// The caller supplies the signature namespace, the trusted Ed25519 keys, the archive name and the program
// path.
//
// # Always enforced
//
// VerifyChecksums accepts only an armored SSH signature that carries no surplus bytes, uses version 1, the
// caller's namespace, an empty reserved field, sha256 or sha512, and an Ed25519 key that is one of the
// trusted keys, and whose signature matches checksums.txt. A namespace is required.
//
// ChecksumFor refuses an archive name that is missing, listed more than once or listed with anything but a
// SHA-256 in hex. VerifyArchive compares the SHA-256 of the archive with it.
//
// ProgramSHA256 reads only a .tar.gz or .zip archive. Every archive entry must have a safe, canonical path
// without a leading slash, a backslash or a ".." element, and no two entries may share a path; a single
// leading "./" is accepted. The program must be one regular file of at most 256 MiB.
//
// # Options
//
// The package has no options.
package release

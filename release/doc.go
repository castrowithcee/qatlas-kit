// Package release verifies signed release files without network access.
//
// A release publishes checksums.txt, an armored SSH signature over it, and archives. VerifyChecksums
// checks the signature, ChecksumFor looks up the SHA-256 of one archive, and ProgramSHA256 hashes the
// program inside a .tar.gz or .zip archive. VerifyArchive combines the first two steps with the archive
// hash. Callers verify the signature before they trust anything read from checksums.txt.
//
// The caller supplies the signature namespace, the trusted Ed25519 keys, the archive name and the program
// path. None of the checks can be relaxed: the signature must use version 1, the caller's namespace, an
// empty reserved field, sha256 or sha512, and an Ed25519 key, and must carry no surplus bytes; an archive
// must be listed exactly once; archive entries must have safe, unique paths, and the program must be a
// single regular file of at most 64 MiB.
package release

// Package selfupdate selects, verifies and installs signed GitHub releases of the running program.
//
// A Client reads the releases of one repository through its API. Check reports the newest eligible stable
// release, List offers versions to choose from, and Target selects an exact one, an older one for a
// rollback included. Update installs the newest stable release in one call when it is newer. For a chosen
// target, Prepare downloads and verifies the release without changing anything, and InstallPrepared
// installs it. Consequences returns the data the application words its question before an installation
// from; all texts belong to the application.
//
// The application supplies the repository, the versions, the archive name of each version, the program
// name, the signature namespace, the name of the signature file, the trusted keys and every installation
// path; the signature file has no default. A release is eligible only if it is published, carries a valid
// version not below MinimumVersion, and lists its archive, checksums.txt and the signature file exactly
// once each, at asset URLs under the repository's own API path. Prepare checks the signature with
// release.VerifyChecksums before it downloads the archive, then the archive's checksum and its contents
// with release.ProgramSHA256. Every archive entry must have a safe path that no other entry shares; a
// single leading "./" is accepted. The program is "bin/<program>" in the archive, with ".exe" added in a
// .zip archive, and each Companion must be one non-empty regular file. A download ends after 30 seconds
// without data, and the token is sent to the repository's API host only, never along a redirect.
//
// After an application renamed its signature namespace and file, Client.PreviousSignature names the old
// set and the newest release signed only with it: releases up to and including that version are verified
// with the old set alone, every later release with the current set alone, under the same trusted keys.
// The installers of the release package always use the current set.
//
// A Prepared release is bound to the repository, the namespace and signature file of its version, keys,
// archive, program and companion entries of the client that prepared it; InstallPrepared refuses it under
// any other policy. It refuses a program path that is not a regular file named after the program, and
// companion paths that are not regular files named after their entries. An older release is installed
// only after ValidateRollback accepted it, and Prepared.ReadsConfig runs the new program against an
// isolated copy of a configuration for that check. Companions are installed first and the program last,
// each staged next to its target and renamed into place; on Windows the running program is moved aside
// first and CleanupLeftovers removes it later. If the program cannot be replaced, every companion is
// restored. BeforeReplace and AfterReplace receive the verified release files, for example to hand a
// running process over to the new program.
package selfupdate

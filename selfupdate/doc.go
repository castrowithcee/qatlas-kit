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
// path; the signature file has no default.
//
// # Always enforced
//
// The repository API address must be https, or http on the local machine, and name /repos/owner/repository.
// A release is eligible only if it is published, carries a valid version not below MinimumVersion, and lists
// its archive, checksums.txt and the signature file exactly once each, at asset URLs under the repository's
// own API path. A build without a release version is refused with ErrDevelopmentBuild.
//
// Prepare checks the signature with release.VerifyChecksums before it downloads the archive, then the
// archive's checksum and its contents with release.ProgramSHA256. Every archive entry must have a safe path
// that no other entry shares; a single leading "./" is accepted. The program is "bin/<program>" in the
// archive, with ".exe" added in a .zip archive, and each Companion must be one non-empty regular file. The
// program is at most 256 MiB and a companion at most 2 MiB. Metadata files and the archive are read only up
// to a size limit, and a download ends after 30 seconds without data. The release API never follows a
// redirect. An asset download follows a redirect only to https hosts of GitHub, at most ten times. The token
// is sent to the repository's API host only, never along a redirect.
//
// A Prepared release is bound to the repository and keys of the client that prepared it and to the namespace,
// signature file, archive, program and companion entries of its version; InstallPrepared refuses it under any
// other policy. It refuses a program path that is not a regular file named after the program, or for a
// release of the old set after the current or the previous program, and companion paths that are not regular
// files named after the entries of the release's set, that are not absolute or that are not unique. An older
// release is installed only after ValidateRollback accepted it. Companions are installed first and the
// program last, each staged next to its target and renamed into place; on Windows the running program is
// moved aside first and CleanupLeftovers removes it later. If the program cannot be replaced, every
// companion is restored.
//
// # Options
//
//   - Client.Token: a bearer token for the repository's API, for example for a private repository.
//     Default: no token is sent.
//   - Client.HTTPClient: the HTTP client for the release API and the downloads, copied with its redirect
//     rules replaced. Default: a client with a 30 second timeout for the API; downloads are bounded by the
//     stall limit only.
//   - Client.Companions: files the archive carries next to the program, such as a manpage, installed with
//     it and restored if the program cannot be replaced. Default: the program only.
//   - Client.PreviousSignature: after an application renamed its signature namespace and file, names the old
//     set and the newest release signed only with it. Releases up to and including that version are
//     verified with the old set alone, every later release with the current set alone, under the same
//     trusted keys and the same archive checks. The old set may also name the previous program and
//     companion entries, with which the archives of those releases are read and installed. Which archive
//     name a version has stays with ArchiveName. The installers of the release package always use the
//     current set. Default: one set for every release.
//   - Client.ValidateRollback: checks the prepared program of an older release, for example with
//     Prepared.ReadsConfig against an isolated copy of a configuration. Default: installing an older
//     release is refused.
//   - Client.BeforeReplace: receives the verified release files right before the first installed file is
//     replaced, for example to hand a running process over; an error ends the installation with nothing
//     replaced. Default: nothing is called.
//   - Client.AfterReplace: receives the verified release files once the program is replaced. Default:
//     nothing is called.
//   - Client.Progress: reports the bytes received and the announced size, or -1, while the archive
//     downloads. Default: no report.
package selfupdate

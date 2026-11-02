// Package releasebuild builds the assets of a signed release reproducibly: one archive per target,
// install.sh and install.ps1, and checksums.txt over all of them. Signing checksums.txt is left to a
// separate step that sees nothing else, such as the reusable workflow .github/workflows/release.yml of
// this module.
//
// # Archives
//
// ArchiveName names each archive <program>_<version>_<goos>_<goarch>, with .zip for windows and .tar.gz
// otherwise. An archive holds bin/<program> (bin/<program>.exe on windows), the section 1 manpages under
// share/man/man1 (not on windows), and the extra files of the Config. Entries have no leading "./", are
// sorted by name, belong to user and group 0, carry the source date as modification time, and have mode
// 0755 for directories and the program and 0644 otherwise. This is the layout the release and selfupdate
// packages and the installers of the installer package read. Build checks every archive with
// release.ProgramSHA256 before it writes checksums.txt.
//
// # Build
//
// The program is built with CGO_ENABLED=0, -trimpath, -buildvcs=false and -ldflags "-s -w -X
// <VersionVariable>=<version>", so the same source, Go version and source date give byte-identical
// assets. The manpage command of the Config runs first with SOURCE_DATE_EPOCH set and must write at
// least one <name>.1 file and nothing else. The version must be a SemVer tag such as v1.2.3 or
// v1.2.3-rc.1, the signature namespace must be <program>-release, and the allowed signers file must list
// only Ed25519 keys for that namespace, without options; the installers embed these keys.
//
// The command github.com/castrowithcee/qatlas-kit/cmd/releasebuild runs Build for the module in the
// current directory.
//
// # Workflow
//
// An application calls the reusable workflow from a workflow that runs on its release tags:
//
//	on:
//	  push:
//	    tags: ["v*.*.*"]
//	permissions: {}
//	jobs:
//	  release:
//	    uses: castrowithcee/qatlas-kit/.github/workflows/release.yml@<commit>
//	    permissions:
//	      contents: write
//	    with:
//	      program: tool
//	      namespace: tool-release
//	    secrets:
//	      RELEASE_SIGNING_KEY: ${{ secrets.RELEASE_SIGNING_KEY }}
//
// The build job checks out the tag, runs gofmt, go vet and go test, and runs the releasebuild command of
// the kit version that the application's go.mod requires. The sign job runs in the protected environment
// named by the input environment (default release), checks out nothing, receives only checksums.txt and
// the allowed signers file, signs with the private key from the secret RELEASE_SIGNING_KEY of the calling
// repository (passed by the caller or set in that environment), and verifies the signature. The publish
// job creates the GitHub release; a tag with a pre-release suffix becomes a pre-release. The inputs config
// (default release.json) and allowed-signers (default .github/release-allowed-signers) name the files in
// the application repository.
package releasebuild

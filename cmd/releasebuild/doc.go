// Command releasebuild writes the assets of a signed release for the Go module in the current directory;
// see the releasebuild package for the archive layout and the checks.
//
//	releasebuild -config release.json -program tool -namespace tool-release \
//		-allowed-signers .github/release-allowed-signers \
//		-releases-url https://github.com/owner/tool/releases -version v1.2.3 -out dist
//
// The source date is SOURCE_DATE_EPOCH or, when that is unset, the commit time of the version tag.
package main

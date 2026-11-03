# qatlas-kit

qatlas-kit is a Go module with reusable building blocks for terminal and command-line applications: CLI output
and diagnostics, terminal user interface texts and preferences, configuration files, hash-chained logs, local
processes and channels, an encrypted vault, and signed updates and releases. Several independent applications
build on it, so a fix, especially a security fix, reaches each of them with a version upgrade.

## Packages

One line per package, in alphabetical order. The package documentation holds the contract.

- [`approval`](approval): binds the use of a secret to a state a person approved, with fingerprints, a sealed approvals file and a readable diff.
- [`archtest`](archtest): checks import boundaries, forbidden names, key literals and text catalogs of a module's source against exceptions that can only shrink.
- [`auditlog`](auditlog): appends entries to daily files linked by a signed hash chain and verifies, reads and aggregates them.
- [`background`](background): starts a detached background process and waits for its report.
- [`cli`](cli): maps errors to exit codes and diagnostics and checks the confirmation of commands.
- [`cmd/releasebuild`](cmd/releasebuild): runs `release/releasebuild` for the module in the current directory.
- [`cobracli`](cobracli): applies the shared flags, help, version and error conventions to a Cobra command tree.
- [`configfile`](configfile): writes configuration documents atomically, with locking, transactions and backups.
- [`filelock`](filelock): provides an exclusive whole-file lock across processes.
- [`fileretry`](fileretry): repeats a file operation that fails only because another process holds the file.
- [`helptopics`](helptopics): shares plain-text help topics between CLI, TUI and manpage.
- [`httpx`](httpx): provides a client without redirects, bound cursors, status classes and bounded reading of answers.
- [`i18n`](i18n): holds the language choice of an application and the lookup of its texts.
- [`jsonschema`](jsonschema): checks JSON values against a strict subset of JSON Schema and names the first violation.
- [`localipc`](localipc): provides a local channel between processes with checked peer identity.
- [`manpage`](manpage): adds help topics to a manpage generated from a command tree.
- [`output`](output): renders ordered results in the CLI output formats and defines the shared error codes.
- [`projectpath`](projectpath): expands, resolves and compares paths and finds Git working tree roots.
- [`ptytest`](ptytest): runs a program at a pseudo-terminal or pseudo console in tests, types input and waits for its output.
- [`ratelimit`](ratelimit): spaces the requests of one credential and holds them when the remote side asks to wait.
- [`redact`](redact): replaces registered secret values, also quoted or JSON-escaped, in text, errors and writers.
- [`release`](release): verifies signed release checksums and archive contents offline.
- [`release/installer`](release/installer): renders the install.sh and install.ps1 installers of a signed release.
- [`release/releasebuild`](release/releasebuild): builds reproducible release archives, installers and checksums.txt, and documents the reusable release workflow.
- [`release/releasetest`](release/releasetest): builds signed test releases and serves them from a local release server in tests.
- [`selfupdate`](selfupdate): selects, verifies and installs signed releases of the running program.
- [`tui`](tui): provides the text catalogs, themes, styles, basic widgets, key table, frame with tabs, focus zones and footer, mouse targets and command line of a terminal user interface.
- [`uiprefs`](uiprefs): loads and saves the device-local preferences of a terminal user interface.
- [`vault`](vault): stores secrets in an age-encrypted directory unlocked by a passphrase.
- [`vaultproc`](vaultproc): keeps an unlocked vault in a hardened background process and hands it to the program an update installs.

## Installation

Add the module with an exact tagged version:

```sh
go get github.com/castrowithcee/qatlas-kit@vX.Y.Z
```

`go.mod` and `go.sum` of the application then pin that version. A newer version reaches the application
only when it upgrades on purpose.

## Strict by default, the application supplies names

Security checks are part of the kit and cannot be switched off or relaxed by an application. Where a
legitimate second way exists, the kit offers it as an explicit, typed option whose zero value is the safe
choice. Such an option replaces a check with an equivalent one, never with none.

The kit knows no application. Names, prefixes, paths, signing namespaces, trusted keys, cryptographic
labels, texts and schemas come from the application through parameters, options or interfaces. Two
applications never share a technical name.

## Versioning

The module follows [Semantic Versioning](https://semver.org) with tags `vX.Y.Z`. Before `v1.0.0`, a minor
release may break the API, and its release notes name each break and how to adapt. Patch releases never
break the API.

## Local development

To develop an application against a local checkout of the kit, create a Go workspace in the application
and keep it out of version control:

```sh
go work init . ../qatlas-kit
```

Do not commit `go.work` or `go.work.sum`. A release of the application builds without the workspace and
binds only a tagged version of the kit.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for conventions and checks, and [SECURITY.md](SECURITY.md) for
reporting a vulnerability.

## License

AGPL-3.0, see [LICENSE](LICENSE). Origins of derived components are listed in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

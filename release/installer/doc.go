// Package installer renders the release installers install.sh, for Linux, macOS and WSL, and install.ps1,
// for Windows, of a caller-supplied program.
//
// By default the scripts install without administrator rights: install.sh into ~/.local, install.ps1 into
// %LOCALAPPDATA%\Programs\<program>. They download the archive of the running platform with checksums.txt
// and checksums.txt.sig from the release, verify the signature with ssh-keygen against the trusted keys
// embedded at render time, require the archive to be listed exactly once in checksums.txt with a matching
// SHA-256, and only then extract it next to the target. The program is moved into place last. They
// install bin/<program> (bin\<program>.exe on Windows), the section 1 manpages under share/man/man1 and
// the regular files under share/doc/<program>, and touch nothing else except the PATH entry.
//
// None of the checks can be switched off: a missing ssh-keygen, an invalid signature, a missing or
// duplicate checksum and a mismatching archive each abort before the installation changes. The
// environment variables <PREFIX>_VERSION, <PREFIX>_INSTALL_BASE_URL and <PREFIX>_INSTALL_PREFIX, with
// <PREFIX> the program name in upper case, select a release, a download mirror and the installation
// prefix; none of them changes which key is trusted. install.ps1 also reads
// <PREFIX>_INSTALL_USER_PATH_FILE, a file that stands in for the user PATH in the registry, for tests.
//
// The caller supplies the program name, the releases URL, the signature namespace, the trusted Ed25519
// keys and the built targets. The installers are release assets themselves, so checksums.txt has to list
// them and its signature covers them.
//
// # System service variant
//
// With [Params.Service], install.sh installs the program as a systemd service that runs as its own system
// user, and install.ps1 is refused. The program name must also be a valid service user name and the
// supplementary groups pass [sysservice.ValidateGroups]; only linux/amd64 and linux/arm64 targets are
// allowed. The script runs as root through sudo and installs to the [sysservice.InstallPaths]; manpages
// and documents go under /usr/local/share. Before any change it checks root, Linux, a running systemd,
// the architecture, the required commands, the supplementary groups and, for AddInstallingUser, SUDO_USER;
// it then downloads and verifies the release as above into a private temporary directory and requires
// bin/<program> and the unit at [sysservice.UnitEntry] as regular files. Only then does it create the
// group and user <program> if missing, add missing supplementary groups, create /etc/<program> if missing,
// replace unit, manpages, documents and program in this order and enable and start, or on an update
// restart, the service. A fresh install, one without the installed unit, adds SUDO_USER to the group
// <program> if AddInstallingUser is set; an update changes no membership of a person. The script never
// touches /etc/systemd/system: it warns when a unit there hides the installed one and leaves drop-ins
// alone. It reads <PREFIX>_VERSION and <PREFIX>_INSTALL_BASE_URL but no installation prefix.
// <PREFIX>_INSTALL_ROOT exists for tests only: it is prepended to every host path the script reads or
// writes, but not to commands, the trusted keys or the home directory recorded for the service user.
package installer

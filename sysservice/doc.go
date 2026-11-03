// Package sysservice defines the layout of a systemd system service for a caller-supplied program and
// checks the unit file that runs it.
//
// A system service runs the program as its own unprivileged service user and group, named like the
// program. [UnitEntry] is the archive path of the unit and [InstallPaths] the absolute POSIX paths the
// installation uses, independent of the host operating system. The program name and the supplementary
// groups come from the caller.
//
// [ValidateUnit] checks a unit file against fixed rules that cannot be switched off. The [Service] section
// must run as User and Group <program>, set NoNewPrivileges, PrivateTmp and ProtectSystem=full or strict,
// let systemd manage ConfigurationDirectory and StateDirectory as <program>, start /usr/local/bin/<program>,
// and list exactly the requested SupplementaryGroups.
// With WritableConfig, ReadWritePaths must release /etc/<program>. The unit must not use root or user ID 0,
// DynamicUser, AmbientCapabilities, PermissionsStartOnly, or the Exec command prefixes + and !.
//
// The check reads the unit the way systemd does: comments, continuation lines, whitespace around =, resets by
// an empty assignment and the last value of a single-valued key. Required values are checked on the
// effective value, so a later line cannot undo them silently. Forbidden settings are rejected in every
// section and wherever they appear, and anything the parser cannot interpret is an error. The check
// verifies the unit text only; it does not run systemd and is not a sandbox audit of the program.
package sysservice

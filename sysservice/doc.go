// Package sysservice defines the layout of a systemd system service for a caller-supplied program and
// checks the unit file that runs it.
//
// A system service has two names. The program is the executable the service starts. The service name
// names the unit file, the unprivileged service user and group, the configuration and state directories and
// the argument to systemctl. It defaults to the program name; a program can also run a service under
// another name, such as "tool-worker" for the program "tool". Both names come from the caller.
//
// [UnitEntry] is the archive path of a unit, [ServicePaths] the absolute POSIX paths of a service and
// [InstallPaths] those of the service named like its program, independent of the host operating system.
// They derive paths without validating the names.
//
// [ValidateUnit] checks a unit file against the rules of a service, and [ValidateGroups] checks the
// supplementary groups of its user.
//
// [Companion] lets selfupdate install the unit of a service together with the program. After the
// installation, [Restart] runs "systemctl daemon-reload" if the unit was written, then "systemctl restart
// <service>". [Shadowed] reports a complete unit of the same name in /etc/systemd/system, which overrides
// the installed one, so the program can warn.
//
//	client.Companions = []selfupdate.Companion{sysservice.Companion("tool")}
//	result, err := client.Update(ctx)
//	if err == nil && result.Updated {
//		err = sysservice.Restart(ctx, "tool", result)
//	}
//
// # Always enforced
//
// Program and service names follow the same rules, and the service name must not be root, sudo, wheel or
// admin. A supplementary group must be well formed, must not be the service group itself, root, sudo, wheel
// or admin, and must not be listed twice. Restart and Shadowed validate the service name before they use
// it. For an invalid service name Companion has no path, which selfupdate refuses.
//
// [ValidateUnit] checks a unit file against fixed rules that cannot be switched off, whatever the service is
// named. The [Service] section must run as User and Group <service>, set NoNewPrivileges, PrivateTmp and
// ProtectSystem=full or strict, let systemd manage ConfigurationDirectory and StateDirectory as <service>,
// start /usr/local/bin/<program>, optionally with arguments, and list exactly the requested
// SupplementaryGroups. The unit must not use root or user ID 0, DynamicUser, AmbientCapabilities,
// PermissionsStartOnly, or the Exec command prefixes + and !.
//
// The check reads the unit the way systemd does: comments, continuation lines, whitespace around =, resets by
// an empty assignment and the last value of a single-valued key. Required values are checked on the
// effective value, so a later line cannot undo them silently. Forbidden settings are rejected in every
// section and wherever they appear, and anything the parser cannot interpret is an error. The check
// verifies the unit text only; it does not run systemd and is not a sandbox audit of the program.
//
// Restart errors name the installed version and the failed step, but never systemctl's output. Restart does
// not check for root. A failed reload ends it before the restart. Shadowed ignores a drop-in directory and
// anything but a regular file or a symbolic link.
//
// # Options
//
//   - UnitRules.Service: the unit, user, group and directory name. Default: the program name.
//   - UnitRules.Groups: the exact set of SupplementaryGroups the unit must list. Default: none, so the unit
//     must list no supplementary group.
//   - UnitRules.WritableConfig: requires ReadWritePaths to release /etc/<service>. Default: not required.
//   - WithRoot: prepends a directory to every host path of Companion, Restart and Shadowed, for tests.
//     Default: the host paths are used as they are.
//   - WithRunner: replaces running "systemctl <args>", for tests. Default: systemctl runs.
package sysservice

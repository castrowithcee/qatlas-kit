// Package sysservice defines the layout of a systemd system service for a caller-supplied program and
// checks the unit file that runs it.
//
// A system service has two names. The program is the executable the service starts. The service name
// names the unit file, the unprivileged service user and group, the configuration and state directories and
// the argument to systemctl. It defaults to the program name; a program can also run a service under
// another name, such as "tool-worker" for the program "tool". Both names come from the caller and follow
// the same rules, and the service name must not be root, sudo, wheel or admin.
//
// [UnitEntry] is the archive path of a unit, [ServicePaths] the absolute POSIX paths of a service and
// [InstallPaths] those of the service named like its program, independent of the host operating system.
// They derive paths without validating the names.
//
// [ValidateUnit] checks a unit file against fixed rules that cannot be switched off, whatever the service is
// named. The [Service] section must run as User and Group <service>, set NoNewPrivileges, PrivateTmp and
// ProtectSystem=full or strict, let systemd manage ConfigurationDirectory and StateDirectory as <service>,
// start /usr/local/bin/<program>, optionally with arguments, and list exactly the requested
// SupplementaryGroups. With WritableConfig, ReadWritePaths must release /etc/<service>. The unit must not
// use root or user ID 0, DynamicUser, AmbientCapabilities, PermissionsStartOnly, or the Exec command prefixes
// + and !.
//
// The check reads the unit the way systemd does: comments, continuation lines, whitespace around =, resets by
// an empty assignment and the last value of a single-valued key. Required values are checked on the
// effective value, so a later line cannot undo them silently. Forbidden settings are rejected in every
// section and wherever they appear, and anything the parser cannot interpret is an error. The check
// verifies the unit text only; it does not run systemd and is not a sandbox audit of the program.
//
// [Companion] lets selfupdate install the unit of a service together with the program; for an invalid
// service name it has no path, which selfupdate refuses. After the installation, [Restart] runs
// "systemctl daemon-reload" if the unit was written, then "systemctl restart <service>". Its errors name the
// installed version and the failed step, but never systemctl's output. It does not check for root.
// [Shadowed] reports a complete unit of the same name in /etc/systemd/system, which overrides the installed
// one, so the program can warn. A drop-in directory does not count. Restart and Shadowed validate the service
// name before they use it.
//
//	client.Companions = []selfupdate.Companion{sysservice.Companion("tool")}
//	result, err := client.Update(ctx)
//	if err == nil && result.Updated {
//		err = sysservice.Restart(ctx, "tool", result)
//	}
package sysservice

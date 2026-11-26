// Package background starts a program as a background process that is detached from the terminal and
// session of the starter, hands it optional bytes through an inherited pipe, and waits for the report the
// process gives once it runs.
//
// The caller supplies the program, its arguments and its environment; the arguments typically select a
// hidden subcommand of the same binary. The started process uses Inherit to read the handover and to
// report. Start returns a Process handle: its Done channel closes when the process ends, and Kill ends it,
// as a process that hands its state to a successor needs.
//
// # Always enforced
//
// Nothing is put on the command line or in the environment on the package's behalf, so a handover never
// appears there. It travels through an inherited pipe, and ReadHandover requires a size limit, fails above
// it, and clears what it read when it fails. Any report other than ReportReady or ReportRunning ends the
// process before Start returns an error, and so does a context or timeout that ends first. A report is read
// only up to 4096 bytes and a failure reason is cut to one line. A negative timeout is refused.
//
// The process gets the null device as its standard streams and a fixed working directory (the root
// directory on Unix, the system directory on Windows). On Linux and macOS it also leads a session of its
// own. On Windows it has no console and its own process group; its standard input and output are the
// handover and report pipes, and it leaves the job object of the starter where the job allows that.
//
// Limits: detaching from the session does not protect the process from a service manager that ends it
// with the user's login session; see LogoutBehavior. A Windows job that forbids breakaway still ends the
// process with the job. Neither side protects against a process of the same user or an administrator.
//
// # Options
//
//   - Command.Program: the file to run. Default: the running program (os.Executable).
//   - Command.Args: the arguments after the program name. Default: none.
//   - Command.Env: the environment as key=value pairs. Default: the environment of the starter.
//   - Options.Timeout: bounds the wait for the report; the context can end it sooner. Default:
//     DefaultTimeout, 10 seconds.
package background

// Package cobracli applies the shared command conventions to the Cobra command tree of an application.
//
// New takes the application's root command and adds the global flags --output and --agent, the version
// template "<program> <version>", one command per help topic and a help command that refuses an unknown
// topic. The output format is resolved once per run with output.ResolveFormat: an explicit --output wins,
// otherwise --agent selects compact and a person gets table. It is resolved before any hook of a command
// runs, so an invalid value fails before anything takes effect.
//
// Execute runs the tree and writes a failure through cli.WriteError, on the agent route when --agent is
// set and with an external answer as one JSON line when the format is JSON. A syntax error, that is an
// unknown command or flag, a flag value that does not parse, a missing required flag, or a wrong number
// of positional arguments, exits with the usage code and is followed by the usage of the command; a well
// formed request that is refused is not. Execute checks the whole tree first: a command without an
// argument check accepts no arguments, a command with subcommands but nothing to run shows its help, a
// short help is one line, and the flags --output, --agent, --fields, --yes and --confirm exist only as
// this package registers them. A tree that breaks a convention does not run, and the failure names every
// violation with its command path.
//
// Program.Check applies that tree check without running anything and reports all violations joined;
// Manpage applies it too. Check, the package function, does the same for a tree that has no Program yet,
// together with everything New refuses, and leaves the tree unchanged. Neither registers a flag or installs
// a hook. A subcommand named "help" is a violation, because the help command takes that name.
//
// AddFields registers --fields, whose selection output.Project checks centrally. Fields.Names returns the
// selection (nil without the flag, the single empty name for an explicitly empty one). Fields.ProjectJSON
// applies the same check to a JSON array of objects that an application has unwrapped from its own envelope:
// it validates the names against the fields the command declares, keeps the selected members of each entry in
// selection order as raw JSON, and returns the list unchanged without --fields. AddGate registers --yes for a
// command that asks and --confirm for one that changes data; Gate.Run passes both to cli.ConfirmAction, so a
// request that changes data needs --confirm in addition to the answer to its question, and in agent mode a
// question needs --yes.
//
// Manpage generates the pages of the same tree through the application's generator and adds the help
// topics, so help command and manpage show one text.
package cobracli

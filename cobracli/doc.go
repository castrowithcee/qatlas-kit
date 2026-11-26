// Package cobracli applies the shared command conventions to the Cobra command tree of an application.
//
// New takes the application's root command and adds the global flags --output and --agent, the version
// template "<program> <version>", one command per help topic and a help command that refuses an unknown
// topic. The output format is resolved once per run with output.ResolveFormat: an explicit --output wins,
// otherwise --agent selects compact and a person gets table.
//
// Execute runs the tree and writes a failure through cli.WriteError, on the agent route when --agent is
// set and with an external answer as one JSON line when the format is JSON.
//
// Program.Check applies the tree check without running anything and reports all violations joined; Manpage
// applies it too. Check, the package function, does the same for a tree that has no Program yet, together
// with everything New refuses, and leaves the tree unchanged. Neither registers a flag or installs a hook.
//
// AddFields registers --fields, whose selection output.Project checks centrally. Fields.Names returns the
// selection (nil without the flag, the single empty name for an explicitly empty one). Fields.ProjectJSON
// applies the same check to a JSON array of objects that an application has unwrapped from its own envelope:
// it validates the names against the fields the command declares, keeps the selected members of each entry in
// selection order as raw JSON, and returns the list unchanged without --fields. AddGate registers --yes for a
// command that asks and --confirm for one that changes data; Gate.Run passes both to cli.ConfirmAction.
//
// An application chooses its exit code by returning an Exit, built with NewExit, from a command. With an Err
// the diagnostic of Err is written and Code is returned; without one the run exits silently with Code.
//
// Manpage generates the pages of the same tree through the application's generator and adds the help
// topics, so help command and manpage show one text.
//
// # Always enforced
//
// The output format is resolved before any hook of a command runs, so an invalid value fails before
// anything takes effect.
//
// A syntax error, that is an unknown command or flag, a flag value that does not parse, a missing required
// flag, or a wrong number of positional arguments, exits with the usage code and is followed by the usage of
// the command; a well formed request that is refused is not.
//
// Execute checks the whole tree first: a command without an argument check accepts no arguments, a command
// with subcommands but nothing to run shows its help, a short help is one line, a subcommand named "help"
// is a violation because the help command takes that name, and the flags --output, --agent, --fields,
// --yes and --confirm exist only as this package registers them. A tree that breaks a convention does not
// run, and the failure names every violation with its command path.
//
// A request that changes data needs --confirm in addition to the answer to its question, and in agent mode
// a question needs --yes.
//
// Exit codes 3 through 125 are allowed; 0, 1 and 2 belong to the kit and 126 and above have shell
// meanings. An Exit with another code, also one built as a literal, is refused with a runtime diagnostic
// and exit code 1. Syntax errors and failures of the kit keep their exit codes.
//
// Options.Version is required and printed by --version as "<program> <version>"; New refuses an empty
// version and one with white space or control characters. Options.AgentHelp and Options.Notice must be one
// line without control characters, which New refuses otherwise. The Redactor applies to the notice. The
// notice is not written when the tree is refused.
//
// Options.Diagnosed is called once after every failing run, with the command path, the error as returned
// (an Exit included, not redacted) and the exit code Execute returns, after the diagnostic was written or
// left out by a silent Exit. It sees no writer and changes neither output nor result, and it must not panic.
// A refused tree reports the name of the root as the command.
//
// # Options
//
//   - Options.Topics: help topics, each a command that prints its text and a listed additional help topic.
//     Default: no topic commands.
//   - Options.Redactor: removes known secrets from every diagnostic. Default: nil, the text is left as it is.
//   - Options.OutputHelp: replaces OutputFlagHelp as the help of --output. Default: OutputFlagHelp.
//   - Options.AgentHelp: replaces cli.AgentFlagHelp as the help of --agent. Default: cli.AgentFlagHelp.
//   - Options.Notice: written to stderr as "<program>: <notice>" once before the command runs, on both
//     routes and also for --help and --version. Default: no notice.
//   - Options.Diagnosed: observes every failing run. Default: no callback.
//   - AddFields: registers --fields on a command. Default: the command has no --fields.
//   - AddGate with Gates.Question or Gates.Change: registers --yes or --confirm. Default: neither flag.
//   - NewExit: a command exit with its own code. Default: a failure exits with the code of its error.
package cobracli
